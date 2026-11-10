package actions

import (
	"context"
	"fmt"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/integrations"
)

// DiscoverSetIn is the input of integration_discover_set.
type DiscoverSetIn struct {
	WorkspaceID string  `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Name        string  `json:"name" jsonschema:"the integration name; it must declare ops.discover"`
	Enabled     *bool   `json:"enabled,omitempty" jsonschema:"switch discovery on or off for this workspace"`
	Column      *string `json:"column,omitempty" jsonschema:"where new cards go: a column id, name or role such as backlog; empty means the backlog column, else the first active one"`
	MaxPerRun   *int    `json:"max_per_run,omitempty" jsonschema:"the most cards one run makes, 1 to 50 (default 10); the rest wait for the next run"`
	// Forget drops the memory of discovered items, so items whose cards were archived come back.
	Forget bool `json:"forget,omitempty" jsonschema:"forget every item discovered so far, so the next run may make cards of them again"`
}

// DiscoverRunIn is the input of integration_discover_run.
type DiscoverRunIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Name        string `json:"name" jsonschema:"the integration name"`
}

func registerDiscover(r *Registry) {
	Register(r, Spec{Name: "integration_discover_set", Description: "Switch an integration's card discovery on or off for a workspace. When on, the integration's ops.discover runs every poll_seconds and Hadron creates a session-less card, linked to the item, for each new item no card holds yet, in the chosen column, at most max_per_run a run. An item is never made into a card twice, even after its card is archived (forget: true resets that). The integration must be switched on in the workspace.",
		Mutates: true, Entities: []string{EntityScripts}},
		func(ctx context.Context, c *Call, in DiscoverSetIn) (ScriptView, error) {
			if err := userOrPlanner(c, "change an integration's discovery"); err != nil {
				return ScriptView{}, err
			}
			w, sc, man, err := loadScript(c, in.WorkspaceID, in.Name, integrations.KindIntegration)
			if err != nil {
				return ScriptView{}, err
			}
			if !man.HasOp("discover") {
				return ScriptView{}, fmt.Errorf("%s has no ops.discover, so it cannot create cards. Add a discover op to its manifest and index.js", sc.Name)
			}
			d := integrations.LoadDiscover(c.Store, w.ID, sc.Name)
			if in.Enabled != nil {
				if *in.Enabled && !contains(w.Integrations, sc.Name) {
					return ScriptView{}, fmt.Errorf("%s is not switched on in workspace %q. Switch it on with integration_use first", sc.Name, w.Name)
				}
				d.Enabled = *in.Enabled
			}
			if in.Column != nil {
				cols, err := c.Store.Columns.List(w.ID)
				if err != nil {
					return ScriptView{}, err
				}
				if _, err := integrations.ResolveColumn(cols, *in.Column); err != nil {
					return ScriptView{}, err
				}
				d.Column = *in.Column
			}
			if in.MaxPerRun != nil {
				if *in.MaxPerRun < 1 || *in.MaxPerRun > integrations.MaxDiscoverMax {
					return ScriptView{}, fmt.Errorf("max_per_run must be between 1 and %d", integrations.MaxDiscoverMax)
				}
				d.MaxPerRun = *in.MaxPerRun
			}
			if in.Forget {
				if err := c.Store.Discovered.Forget(w.ID, sc.Name); err != nil {
					return ScriptView{}, err
				}
			}
			if err := integrations.SaveDiscover(c.Store, w.ID, sc.Name, d); err != nil {
				return ScriptView{}, err
			}
			return scriptView(c, w, sc), nil
		})

	Register(r, Spec{Name: "integration_discover_run", Description: "Run an integration's discovery for a workspace now, whether or not it is switched on, and create the cards for new items. Returns the cards created, how many items were already known, and how many wait for the next run because of max_per_run.",
		Mutates: true, NoTx: true, Entities: []string{"cards", "links", EntityScripts}},
		func(ctx context.Context, c *Call, in DiscoverRunIn) (integrations.DiscoverResult, error) {
			if err := userOrPlanner(c, "run an integration's discovery"); err != nil {
				return integrations.DiscoverResult{}, err
			}
			m, err := integ(c)
			if err != nil {
				return integrations.DiscoverResult{}, err
			}
			w, sc, _, err := loadScript(c, in.WorkspaceID, in.Name, integrations.KindIntegration)
			if err != nil {
				return integrations.DiscoverResult{}, err
			}
			if sc.Status != integrations.StatusEnabled {
				return integrations.DiscoverResult{}, fmt.Errorf("%s is not approved yet. integration_approve it first", sc.Name)
			}
			if !contains(w.Integrations, sc.Name) {
				return integrations.DiscoverResult{}, fmt.Errorf("%s is not switched on in workspace %q. Switch it on with integration_use first", sc.Name, w.Name)
			}
			return m.Discover(ctx, w, sc)
		})
}

// RunEvents runs the rules for events a background integration run made, such as cards discovery created.
func (r *Registry) RunEvents(ctx context.Context, workspaceID string, events []board.Event) error {
	var firstErr error
	for _, ev := range events {
		if err := r.runTimed(ctx, workspaceID, "", ev); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
