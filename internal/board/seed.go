// Package board holds the board's own logic: seeding a workspace, placing cards in columns, and
// (later) the rules engine and filters.
package board

import (
	"encoding/json"
	"fmt"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/prompts"
	"github.com/GabiHert/hadron/internal/store"
)

// Column templates for a new workspace.
const (
	TemplatePlanner = "planner"
	TemplateSimple  = "simple"
	TemplateCopy    = "copy"
)

// ColumnSpec is a column to create.
type ColumnSpec struct {
	Name string
	Role string
	WIP  int
}

// Templates returns the columns of a built-in template.
func Templates(name string) ([]ColumnSpec, bool) {
	switch name {
	case TemplatePlanner, "":
		return []ColumnSpec{
			{"Needs you", domain.RoleNeedsYou, 0},
			{"Working", domain.RoleActive, 0},
			{"Blocked", domain.RoleBlocked, 0},
			{"Done", domain.RoleDone, 0},
			{"Backlog", domain.RoleBacklog, 0},
		}, true
	case TemplateSimple:
		return []ColumnSpec{{"To do", domain.RoleBacklog, 0}, {"Doing", domain.RoleActive, 0}, {"Done", domain.RoleDone, 0}}, true
	}
	return nil, false
}

// Built-in rule names. They can be switched off but not deleted.
const (
	RuleToNeedsYou = "Open question or gate moves the card to Needs you"
	RuleBackAgain  = "Answered question or cleared gate returns the card"
)

// SeedWorkspace gives a new workspace its columns, the two built-in rules, the default dashboard
// and its prompt rows. Run it inside a transaction with the workspace's creation. The columns come
// from cols; nil means the planner default.
func SeedWorkspace(tx *store.Store, ws domain.Workspace, cols []ColumnSpec) ([]domain.Column, error) {
	if cols == nil {
		cols, _ = Templates(TemplatePlanner)
	}
	created := make([]domain.Column, 0, len(cols))
	for _, c := range cols {
		col := domain.Column{WorkspaceID: ws.ID, Name: c.Name, Role: c.Role, WIP: c.WIP}
		if err := tx.Columns.Create(&col); err != nil {
			return nil, fmt.Errorf("seed columns: %w", err)
		}
		created = append(created, col)
	}
	for _, r := range BuiltinRules(ws.ID) {
		r := r
		if err := tx.Rules.Create(&r); err != nil {
			return nil, fmt.Errorf("seed rules: %w", err)
		}
	}
	for _, b := range DefaultDashboard(ws.ID) {
		b := b
		if err := tx.Blocks.Create(&b); err != nil {
			return nil, fmt.Errorf("seed dashboard: %w", err)
		}
	}
	// A null template means "use the built-in default", so editing the default later reaches every
	// workspace that has not customized it.
	for _, kind := range []string{prompts.KindPlanner, prompts.KindSession} {
		if err := tx.Prompts.Set(ws.ID, kind, nil); err != nil {
			return nil, fmt.Errorf("seed prompts: %w", err)
		}
	}
	for _, p := range DefaultLaunchProfiles(ws.ID) {
		p := p
		if err := tx.LaunchProfiles.Create(&p); err != nil {
			return nil, fmt.Errorf("seed launch profiles: %w", err)
		}
	}
	return created, nil
}

// DefaultLaunchProfiles are the three recipes every workspace starts with (spec section 4 and the
// prototype's New session dialog). A first prompt may use {{nick}}, {{key}} (the key, else the
// nickname) and {{title}}.
func DefaultLaunchProfiles(workspaceID string) []domain.LaunchProfile {
	return []domain.LaunchProfile{
		{WorkspaceID: workspaceID, Name: "brief", Command: "claude", FirstPrompt: "/brief {{nick}}"},
		{WorkspaceID: workspaceID, Name: "orchestrate", Command: "claude", FirstPrompt: "/orchestrate {{key}}"},
		{WorkspaceID: workspaceID, Name: "plain", Command: "claude"},
	}
}

// launchProfilesSeededKey marks that existing workspaces got their default launch profiles.
const launchProfilesSeededKey = "launch_profiles_seeded"

// BackfillLaunchProfiles gives workspaces made before launch profiles existed their three defaults,
// once. It never runs again, so deleting a profile stays deleted.
func BackfillLaunchProfiles(st *store.Store) error {
	if _, err := st.Settings.Get(launchProfilesSeededKey); err == nil {
		return nil
	}
	return st.Tx(func(tx *store.Store) error {
		list, err := tx.Workspaces.List(true)
		if err != nil {
			return err
		}
		for _, ws := range list {
			existing, err := tx.LaunchProfiles.List(ws.ID)
			if err != nil {
				return err
			}
			if len(existing) > 0 {
				continue
			}
			for _, p := range DefaultLaunchProfiles(ws.ID) {
				p := p
				if err := tx.LaunchProfiles.Create(&p); err != nil {
					return err
				}
			}
		}
		return tx.Settings.Set(launchProfilesSeededKey, json.RawMessage("true"))
	})
}

func raw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// BuiltinRules are the two rules every workspace starts with (spec section 7). The rules engine
// that runs them arrives with the question and gate work; until then they are stored and listed.
func BuiltinRules(workspaceID string) []domain.Rule {
	return []domain.Rule{
		{
			WorkspaceID: workspaceID, Name: RuleToNeedsYou, Enabled: true, Builtin: true,
			Trigger: raw(map[string]any{"on": "question_opened"}),
			Actions: raw([]map[string]any{{"type": "move", "role": domain.RoleNeedsYou, "remember_return": true}}),
		},
		{
			WorkspaceID: workspaceID, Name: RuleBackAgain, Enabled: true, Builtin: true,
			Trigger: raw(map[string]any{"on": "question_answered"}),
			Actions: raw([]map[string]any{{"type": "move", "to": "return_column"}}),
		},
	}
}

// DefaultDashboard matches the prototype's. It has no planner block on a schedule, so it spends no
// tokens (spec section 9).
func DefaultDashboard(workspaceID string) []domain.Block {
	q := func(query string, args map[string]any) json.RawMessage {
		return raw(map[string]any{"query": query, "args": args})
	}
	return []domain.Block{
		{WorkspaceID: workspaceID, Type: "stats", Title: "Cards by column", Span: 2, Source: q("cards.count", map[string]any{"by": "column"})},
		{WorkspaceID: workspaceID, Type: "table", Title: "Waiting on you", Span: 1, Source: q("questions.open", nil)},
		{WorkspaceID: workspaceID, Type: "bars", Title: "Sessions by liveness", Span: 1, Source: q("cards.count", map[string]any{"by": "card.liveness"})},
		{WorkspaceID: workspaceID, Type: "planner", Title: "Planner's summary", Span: 1, Source: raw(map[string]any{})},
		{WorkspaceID: workspaceID, Type: "custom", Title: "Session map", Span: 1, Source: q("cards.list", nil), HTML: "live:diagram"},
		{WorkspaceID: workspaceID, Type: "activity", Title: "Recent tool calls", Span: 2, Source: q("mcp.calls", map[string]any{"limit": 7})},
	}
}
