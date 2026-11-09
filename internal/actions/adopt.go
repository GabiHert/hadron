package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

const exitElsewhere = "Exit that conversation in its terminal first: two processes resuming one Claude Code conversation corrupt it."

// AdoptableListIn is the input of session_adoptable_list.
type AdoptableListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// AdoptableOut is one conversation Hadron could take over.
type AdoptableOut struct {
	sessions.Adoptable
	AlreadyInHadron bool   `json:"already_in_hadron"`
	HeldBy          string `json:"held_by,omitempty" jsonschema:"the card or planner that holds it, when already_in_hadron"`
}

// SessionAdoptIn is the input of session_adopt.
type SessionAdoptIn struct {
	WorkspaceID     string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	ClaudeSessionID string `json:"claude_session_id" jsonschema:"the conversation's id from session_adoptable_list"`
	Nick            string `json:"nick" jsonschema:"the new card's nickname, kebab-case"`
	Key             string `json:"key,omitempty" jsonschema:"an optional key such as ACME-508"`
	ColumnID        string `json:"column_id,omitempty" jsonschema:"the column; default is the first active one"`
	Start           *bool  `json:"start,omitempty" jsonschema:"resume it right away (default true)"`
	Handoff         *bool  `json:"handoff,omitempty" jsonschema:"type the handoff message once it is ready (default true)"`
}

// PlannerAdoptIn is the input of planner_adopt.
type PlannerAdoptIn struct {
	WorkspaceID     string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	ClaudeSessionID string `json:"claude_session_id" jsonschema:"the conversation's id from session_adoptable_list"`
	Handoff         *bool  `json:"handoff,omitempty" jsonschema:"type the planner handoff message once it is ready (default true)"`
}

// AdoptOut is the result of session_adopt and planner_adopt.
type AdoptOut struct {
	Card    *domain.Card  `json:"card,omitempty"`
	Status  SessionStatus `json:"status"`
	Warning string        `json:"warning"`
}

func claudeDir() string { return sessions.ClaudeConfigDir() }

// holder says which Hadron card or planner already holds a Claude session id.
func holder(st *store.Store, id string) (string, bool) {
	rows, err := st.Sessions.ByClaudeID(id)
	if err != nil || len(rows) == 0 {
		return "", false
	}
	r := rows[0]
	if r.CardID == "" {
		return "the planner", true
	}
	if card, err := st.Cards.Get(r.CardID); err == nil {
		return "card " + card.Nick, true
	}
	return "a card that no longer exists", true
}

func registerAdopt(r *Registry) {
	Register(r, Spec{Name: "session_adoptable_list", Description: "List the Claude Code conversations on this Mac that were run in the workspace's root folder and could be brought into Hadron (read from ~/.claude/projects, or CLAUDE_CONFIG_DIR), newest first: id, the first thing the user typed, when it was last active and its size. Conversations under 2 KB are skipped. " + exitElsewhere},
		func(ctx context.Context, c *Call, in AdoptableListIn) ([]AdoptableOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			list, err := sessions.ListAdoptable(claudeDir(), w.Root)
			if err != nil {
				return nil, err
			}
			out := make([]AdoptableOut, 0, len(list))
			for _, a := range list {
				o := AdoptableOut{Adoptable: a}
				o.HeldBy, o.AlreadyInHadron = holder(c.Store, a.ClaudeSessionID)
				out = append(out, o)
			}
			return out, nil
		})

	Register(r, Spec{Name: "session_adopt", Description: "Bring an existing Claude Code conversation into Hadron as a new card and resume it with claude --resume, with Hadron's prompt and MCP server attached. Refused if another Hadron card already holds that conversation. " + exitElsewhere,
		Mutates: true, Entities: []string{"cards", "sessions"}},
		func(ctx context.Context, c *Call, in SessionAdoptIn) (AdoptOut, error) {
			if err := userOrPlanner(c, "adopt sessions"); err != nil {
				return AdoptOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return AdoptOut{}, err
			}
			if err := checkAdoptable(c, w, in.ClaudeSessionID); err != nil {
				return AdoptOut{}, err
			}
			start := in.Start == nil || *in.Start
			card, err := createCardFor(c, w, in.Nick, in.Key, in.ColumnID)
			if err != nil {
				return AdoptOut{}, err
			}
			row := domain.Session{WorkspaceID: w.ID, CardID: card.ID, Kind: "card", Command: "claude", Cwd: w.Root, Status: "exited", ClaudeSessionID: in.ClaudeSessionID}
			if err := c.Store.Sessions.Create(&row); err != nil {
				return AdoptOut{}, err
			}
			out := AdoptOut{Card: &card, Warning: exitElsewhere}
			out.Status = SessionStatus{WorkspaceID: w.ID, CardID: card.ID, Nick: card.Nick, Liveness: domain.LivenessEnded, Session: &row}
			if start {
				st, err := startSessionAdopted(c, card, in.Handoff == nil || *in.Handoff)
				if err != nil {
					return AdoptOut{}, err
				}
				out.Status = st
			}
			c.Touch("cards", "sessions")
			return out, nil
		})

	Register(r, Spec{Name: "planner_adopt", Description: "Make an existing Claude Code conversation the workspace's planner: Hadron resumes it with claude --resume, with the planner prompt and MCP server attached. Refused while the planner runs (stop it first) or if another Hadron card holds the conversation. " + exitElsewhere,
		Mutates: true, Entities: []string{"sessions"}},
		func(ctx context.Context, c *Call, in PlannerAdoptIn) (AdoptOut, error) {
			if err := userOrPlanner(c, "adopt the planner"); err != nil {
				return AdoptOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return AdoptOut{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return AdoptOut{}, err
			}
			if t, terr := sv.Find(c.Store, w.ID, ""); terr == nil && t.Live != nil {
				return AdoptOut{}, errors.New("the planner is running. Stop it first (planner_restart starts a new one), then adopt")
			}
			if err := checkAdoptable(c, w, in.ClaudeSessionID); err != nil {
				return AdoptOut{}, err
			}
			row := domain.Session{WorkspaceID: w.ID, Kind: "planner", Command: "claude", Cwd: w.Root, Status: "exited", ClaudeSessionID: in.ClaudeSessionID}
			if err := c.Store.Sessions.Create(&row); err != nil {
				return AdoptOut{}, err
			}
			st, err := startPlannerAdopted(c, w, in.Handoff == nil || *in.Handoff)
			if err != nil {
				return AdoptOut{}, err
			}
			c.Touch("sessions")
			return AdoptOut{Status: st, Warning: exitElsewhere}, nil
		})
}

func checkAdoptable(c *Call, w domain.Workspace, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("claude_session_id is required. session_adoptable_list shows the ids")
	}
	if !sessions.HasConversation(claudeDir(), w.Root, id) {
		return fmt.Errorf("no Claude Code conversation %q was run in %s. session_adoptable_list shows what is there", id, w.Root)
	}
	if who, held := holder(c.Store, id); held {
		return fmt.Errorf("that conversation is already in Hadron (%s). Open that card instead", who)
	}
	return nil
}

// createCardFor makes a card without starting it.
func createCardFor(c *Call, w domain.Workspace, nick, key, columnID string) (domain.Card, error) {
	if err := checkNick(nick); err != nil {
		return domain.Card{}, err
	}
	cols, err := c.Store.Columns.List(w.ID)
	if err != nil {
		return domain.Card{}, err
	}
	if columnID == "" {
		columnID = defaultColumn(cols)
	}
	found := false
	for _, col := range cols {
		found = found || col.ID == columnID
	}
	if !found {
		return domain.Card{}, fmt.Errorf("column %q is not in workspace %q. Call column_list for the ids", columnID, w.Name)
	}
	card := domain.Card{WorkspaceID: w.ID, ColumnID: columnID, Nick: nick, Key: strings.TrimSpace(key), Assignee: "You", Liveness: domain.LivenessIdle}
	if err := c.Store.Cards.Create(&card); err != nil {
		return card, nickError(err, nick, w.Name)
	}
	return card, nil
}

// handoffFor is the text typed into an adopted card's session. WI-23 fills it in.
func handoffFor(c *Call, card domain.Card, on bool) string { return "" }

func plannerHandoffFor(c *Call, w domain.Workspace, on bool) string { return "" }
