package actions

import (
	"context"
	"errors"
	"fmt"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/pty"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

// SessionCardIn is the input of the session actions that need only a card.
type SessionCardIn struct {
	Card string `json:"card" jsonschema:"the card: its id, nickname or key"`
}

// SessionStartIn is the input of session_start.
type SessionStartIn struct {
	Card          string `json:"card" jsonschema:"the card: its id, nickname or key"`
	LaunchProfile string `json:"launch_profile,omitempty" jsonschema:"a launch profile name or id; defaults to the card's own"`
}

// SessionRestartIn is the input of session_restart.
type SessionRestartIn struct {
	Card          string `json:"card" jsonschema:"the card: its id, nickname or key"`
	LaunchProfile string `json:"launch_profile,omitempty" jsonschema:"a launch profile name or id; defaults to the card's own"`
	Fresh         bool   `json:"fresh,omitempty" jsonschema:"start a new conversation instead of resuming the last one"`
}

// SessionSendIn is the input of session_send.
type SessionSendIn struct {
	Card  string `json:"card" jsonschema:"the card: its id, nickname or key"`
	Text  string `json:"text" jsonschema:"what to type into the session, sent like a paste"`
	Enter *bool  `json:"enter,omitempty" jsonschema:"press Enter after the text (default true)"`
}

// SessionKeysIn is the input of session_keys.
type SessionKeysIn struct {
	Card string   `json:"card" jsonschema:"the card: its id, nickname or key"`
	Keys []string `json:"keys" jsonschema:"keys to press, in order: Enter, Escape, Tab, Up, Down, Left, Right, Backspace, Ctrl-<letter>, or one character"`
}

// SessionReadScreenIn is the input of session_read_screen.
type SessionReadScreenIn struct {
	Card       string `json:"card" jsonschema:"the card: its id, nickname or key"`
	Lines      int    `json:"lines,omitempty" jsonschema:"how many lines from the bottom (default 60)"`
	Scrollback bool   `json:"scrollback,omitempty" jsonschema:"include lines that scrolled off the top"`
}

// PlannerIn is the input of the planner actions that need only a workspace.
type PlannerIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// PlannerSendIn is the input of planner_send.
type PlannerSendIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Text        string `json:"text" jsonschema:"what to type into the planner, sent like a paste"`
	Enter       *bool  `json:"enter,omitempty" jsonschema:"press Enter after the text (default true)"`
}

// PlannerReadScreenIn is the input of planner_read_screen.
type PlannerReadScreenIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Lines       int    `json:"lines,omitempty" jsonschema:"how many lines from the bottom (default 60)"`
	Scrollback  bool   `json:"scrollback,omitempty" jsonschema:"include lines that scrolled off the top"`
}

// PlannerRestartIn is the input of planner_restart.
type PlannerRestartIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Fresh       bool   `json:"fresh,omitempty" jsonschema:"start a new conversation instead of resuming the last one"`
}

// SessionStatus is what session_status and planner_get report.
type SessionStatus struct {
	WorkspaceID string          `json:"workspace_id"`
	CardID      string          `json:"card_id"`
	Nick        string          `json:"nick"`
	Running     bool            `json:"running"`
	Liveness    string          `json:"liveness"`
	Session     *domain.Session `json:"session"`
	Cols        int             `json:"cols"`
	Rows        int             `json:"rows"`
	Error       string          `json:"error"`
	// Warnings are notes about how the session started, such as a worktree that was skipped.
	Warnings []string `json:"warnings,omitempty"`
}

// ScreenOut is what the read-screen actions return.
type ScreenOut struct {
	Running  bool   `json:"running"`
	Liveness string `json:"liveness"`
	Screen   string `json:"screen"`
}

// KeyNames is the list of key names session_keys accepts, for the UI.
var KeyNames = pty.KeyNames

func registerSessions(r *Registry) {
	Register(r, Spec{Name: "session_start", Description: "Start the Claude Code session of a card. It is an error if the card already has a running session; use session_restart then.",
		Mutates: true, Entities: []string{"sessions", "cards"}},
		func(ctx context.Context, c *Call, in SessionStartIn) (SessionStatus, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, true, "session_start"); err != nil {
				return SessionStatus{}, err
			}
			return startSession(c, card, in.LaunchProfile, false, false)
		})

	Register(r, Spec{Name: "session_restart", Description: "Stop a card's session if it runs and start it again. It resumes the last Claude conversation when that is known; fresh: true starts a new one.",
		Mutates: true, Entities: []string{"sessions", "cards"}},
		func(ctx context.Context, c *Call, in SessionRestartIn) (SessionStatus, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, true, "session_restart"); err != nil {
				return SessionStatus{}, err
			}
			return startSession(c, card, in.LaunchProfile, in.Fresh, true)
		})

	Register(r, Spec{Name: "session_stop", Description: "Hang up a card's running session. The card keeps its history; session_restart starts it again.",
		Mutates: true, Entities: []string{"sessions", "cards"}},
		func(ctx context.Context, c *Call, in SessionCardIn) (SessionStatus, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, true, "session_stop"); err != nil {
				return SessionStatus{}, err
			}
			t, err := target(c, card.WorkspaceID, &card)
			if err != nil {
				return SessionStatus{}, err
			}
			if t.Live == nil {
				return SessionStatus{}, fmt.Errorf("card %q has no running session", card.Nick)
			}
			c.AfterCommit(t.Live.Stop)
			return statusOf(&card, card.WorkspaceID, t), nil
		})

	Register(r, Spec{Name: "session_send", Description: "Type text into a card's session like a paste, then press Enter. The call fails if the session is not running.",
		Mutates: true, Entities: []string{"sessions"}},
		func(ctx context.Context, c *Call, in SessionSendIn) (SessionStatus, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, true, "session_send"); err != nil {
				return SessionStatus{}, err
			}
			return sendTo(c, card.WorkspaceID, &card, in.Text, in.Enter)
		})

	Register(r, Spec{Name: "session_keys", Description: "Press keys in a card's session: Enter, Escape, Tab, arrows, Backspace, Ctrl-<letter>, or single characters. Use it for menus and confirmations.",
		Mutates: true, Entities: []string{"sessions"}},
		func(ctx context.Context, c *Call, in SessionKeysIn) (SessionStatus, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, true, "session_keys"); err != nil {
				return SessionStatus{}, err
			}
			return keysTo(c, card.WorkspaceID, &card, in.Keys)
		})

	Register(r, Spec{Name: "session_interrupt", Description: "Press Ctrl-C in a card's session, which stops what Claude is doing.",
		Mutates: true, Entities: []string{"sessions"}},
		func(ctx context.Context, c *Call, in SessionCardIn) (SessionStatus, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, true, "session_interrupt"); err != nil {
				return SessionStatus{}, err
			}
			return keysTo(c, card.WorkspaceID, &card, []string{"Ctrl-C"})
		})

	Register(r, Spec{Name: "session_read_screen", Description: "Read what a card's terminal shows, as plain text: the last lines (default 60), optionally with the scrollback."},
		func(ctx context.Context, c *Call, in SessionReadScreenIn) (ScreenOut, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return ScreenOut{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, false, "session_read_screen"); err != nil {
				return ScreenOut{}, err
			}
			return readScreen(c, card.WorkspaceID, &card, in.Lines, in.Scrollback)
		})

	Register(r, Spec{Name: "session_status", Description: "Report whether a card's session is running, its liveness (working, idle, waiting, ended), process id and terminal size."},
		func(ctx context.Context, c *Call, in SessionCardIn) (SessionStatus, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, false, "session_status"); err != nil {
				return SessionStatus{}, err
			}
			t, err := target(c, card.WorkspaceID, &card)
			if err != nil {
				return SessionStatus{}, err
			}
			return statusOf(&card, card.WorkspaceID, t), nil
		})

	Register(r, Spec{Name: "planner_get", Description: "Report the workspace planner's session: whether it runs, its liveness, process id and terminal size."},
		func(ctx context.Context, c *Call, in PlannerIn) (SessionStatus, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, w.ID, nil, false, "planner_get"); err != nil {
				return SessionStatus{}, err
			}
			t, err := target(c, w.ID, nil)
			if err != nil {
				return SessionStatus{}, err
			}
			return statusOf(nil, w.ID, t), nil
		})

	Register(r, Spec{Name: "planner_start", Description: "Start the workspace planner's Claude Code session. It is an error if it already runs; use planner_restart then.",
		Mutates: true, Entities: []string{"sessions"}},
		func(ctx context.Context, c *Call, in PlannerIn) (SessionStatus, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, w.ID, nil, true, "planner_start"); err != nil {
				return SessionStatus{}, err
			}
			return startPlanner(c, w, false, false)
		})

	Register(r, Spec{Name: "planner_restart", Description: "Stop the planner's session if it runs and start it again, resuming its conversation when that is known.",
		Mutates: true, Entities: []string{"sessions"}},
		func(ctx context.Context, c *Call, in PlannerRestartIn) (SessionStatus, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, w.ID, nil, true, "planner_restart"); err != nil {
				return SessionStatus{}, err
			}
			return startPlanner(c, w, in.Fresh, true)
		})

	Register(r, Spec{Name: "planner_send", Description: "Type text into the planner's session like a paste, then press Enter.",
		Mutates: true, Entities: []string{"sessions"}},
		func(ctx context.Context, c *Call, in PlannerSendIn) (SessionStatus, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, w.ID, nil, true, "planner_send"); err != nil {
				return SessionStatus{}, err
			}
			return sendTo(c, w.ID, nil, in.Text, in.Enter)
		})

	Register(r, Spec{Name: "planner_read_screen", Description: "Read what the planner's terminal shows, as plain text: the last lines (default 60), optionally with the scrollback."},
		func(ctx context.Context, c *Call, in PlannerReadScreenIn) (ScreenOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return ScreenOut{}, err
			}
			if err := mayControl(c, w.ID, nil, false, "planner_read_screen"); err != nil {
				return ScreenOut{}, err
			}
			return readScreen(c, w.ID, nil, in.Lines, in.Scrollback)
		})
}

// mayControl decides whether the caller may act on a card's session (card != nil) or on the
// workspace planner (card == nil). write is true for anything that types, presses keys, stops or
// starts. The user and a connected external client may do everything. The planner may do anything
// to its workspace's card sessions, and only read its own screen. A card session may read its own
// screen and status and nothing else: it reports with card_report and asks with question_ask.
func mayControl(c *Call, workspaceID string, card *domain.Card, write bool, what string) error {
	a := c.Actor
	switch a.Kind {
	case KindSession, KindPlanner:
		if a.WorkspaceID != "" && workspaceID != "" && a.WorkspaceID != workspaceID {
			return fmt.Errorf("%s belongs to another workspace, so %s may not use %s on it", targetName(card), a, what)
		}
	}
	switch a.Kind {
	case KindSession:
		if card == nil {
			return fmt.Errorf("%s may not use %s on the planner. Report to it with card_report, or ask the user with question_ask", a, what)
		}
		if card.ID != a.CardID {
			return fmt.Errorf("%s may not use %s on card %q: a session controls only itself, and only to read. Ask the planner with card_report", a, what, card.Nick)
		}
		if write {
			return fmt.Errorf("%s may not use %s on its own session. Report with card_report; the planner or the user restarts sessions", a, what)
		}
	case KindPlanner:
		if card == nil && write {
			return fmt.Errorf("the planner may not use %s on itself. Ask the user to restart it", what)
		}
	}
	return nil
}

func targetName(card *domain.Card) string {
	if card == nil {
		return "the planner"
	}
	return fmt.Sprintf("card %q", card.Nick)
}

// cardByRef finds a card by id, else by nickname or key in the caller's workspace.
func cardByRef(c *Call, ref string) (domain.Card, error) {
	if card, err := c.Store.Cards.Get(ref); err == nil {
		c.SetWorkspace(card.WorkspaceID)
		return card, nil
	}
	w, err := c.Workspace("")
	if err != nil {
		return domain.Card{}, err
	}
	card, err := c.Store.Cards.ByNick(w.ID, ref)
	if errors.Is(err, store.ErrNotFound) {
		return card, fmt.Errorf("no card %q in workspace %q. Pass a card id, nickname or key; card_list shows them", ref, w.Name)
	}
	if err == nil {
		c.SetWorkspace(card.WorkspaceID)
	}
	return card, err
}

func svc(c *Call) (*sessions.Service, error) {
	if c.Deps.Sessions == nil {
		return nil, errors.New("sessions are not available in this build")
	}
	return c.Deps.Sessions, nil
}

// target finds the latest session of a card or planner; no session yet is an empty target.
func target(c *Call, workspaceID string, card *domain.Card) (sessions.Target, error) {
	sv, err := svc(c)
	if err != nil {
		return sessions.Target{}, err
	}
	cardID := ""
	if card != nil {
		cardID = card.ID
	}
	t, err := sv.Find(c.Store, workspaceID, cardID)
	if errors.Is(err, sessions.ErrNoSession) {
		return sessions.Target{}, nil
	}
	return t, err
}

func statusOf(card *domain.Card, workspaceID string, t sessions.Target) SessionStatus {
	st := SessionStatus{WorkspaceID: workspaceID, Liveness: domain.LivenessEnded, Error: t.Err}
	if card != nil {
		st.CardID, st.Nick = card.ID, card.Nick
	}
	if t.Row.ID != "" {
		row := t.Row
		st.Session = &row
	}
	if t.Live != nil {
		st.Running, st.Liveness = true, t.Live.Liveness()
		st.Cols, st.Rows = t.Live.Size()
	}
	if t.Row.ID == "" {
		st.Liveness = "none"
	}
	return st
}

func startSession(c *Call, card domain.Card, profile string, fresh, restart bool) (SessionStatus, error) {
	return startSessionWith(c, card, profile, fresh, restart, "")
}

// startSessionAdopted resumes an adopted card's conversation.
func startSessionAdopted(c *Call, card domain.Card, handoff bool) (SessionStatus, error) {
	return startSessionWith(c, card, "", false, false, handoffFor(c, card, handoff))
}

func startSessionWith(c *Call, card domain.Card, profile string, fresh, restart bool, handoff string) (SessionStatus, error) {
	sv, err := svc(c)
	if err != nil {
		return SessionStatus{}, err
	}
	w, err := c.Workspace(card.WorkspaceID)
	if err != nil {
		return SessionStatus{}, err
	}
	var warnings []string
	row, launch, err := sv.Prepare(c.Store, sessions.Request{Workspace: w, Card: &card, Profile: profile, Fresh: fresh, Restart: restart, Strict: true, Warnings: &warnings, Handoff: handoff})
	if err != nil {
		return SessionStatus{}, err
	}
	c.AfterCommit(launch)
	c.Touch("cards")
	return SessionStatus{WorkspaceID: w.ID, CardID: card.ID, Nick: card.Nick, Running: true, Liveness: domain.LivenessIdle, Session: &row, Warnings: warnings}, nil
}

func startPlanner(c *Call, w domain.Workspace, fresh, restart bool) (SessionStatus, error) {
	return startPlannerWith(c, w, fresh, restart, "")
}

// startPlannerAdopted resumes an adopted conversation as the planner.
func startPlannerAdopted(c *Call, w domain.Workspace, handoff bool) (SessionStatus, error) {
	return startPlannerWith(c, w, false, false, plannerHandoffFor(c, w, handoff))
}

func startPlannerWith(c *Call, w domain.Workspace, fresh, restart bool, handoff string) (SessionStatus, error) {
	sv, err := svc(c)
	if err != nil {
		return SessionStatus{}, err
	}
	row, launch, err := sv.Prepare(c.Store, sessions.Request{Workspace: w, Fresh: fresh, Restart: restart, Strict: true, Handoff: handoff})
	if err != nil {
		return SessionStatus{}, err
	}
	c.AfterCommit(launch)
	return SessionStatus{WorkspaceID: w.ID, Running: true, Liveness: domain.LivenessIdle, Session: &row}, nil
}

// autostartPlanner starts a workspace's planner when the setting allows and none runs. Failing to
// start it never fails the action that opened the workspace.
func autostartPlanner(c *Call, w domain.Workspace) {
	sv := c.Deps.Sessions
	if sv == nil || w.Archived || !sv.PlannerAutostart(c.Store) {
		return
	}
	if t, err := sv.Find(c.Store, w.ID, ""); err == nil && t.Live != nil {
		return
	}
	if _, launch, err := sv.Prepare(c.Store, sessions.Request{Workspace: w}); err == nil {
		c.AfterCommit(launch)
		c.Touch("sessions")
	}
}

func liveTarget(c *Call, workspaceID string, card *domain.Card) (sessions.Target, string, error) {
	t, err := target(c, workspaceID, card)
	if err != nil {
		return t, "", err
	}
	who, start := "the planner", "planner_start"
	if card != nil {
		who, start = fmt.Sprintf("card %q", card.Nick), "session_start"
	}
	if t.Live == nil {
		switch {
		case t.Row.ID == "":
			return t, who, fmt.Errorf("%s has no session yet. Start it with %s", who, start)
		case t.Err != "":
			return t, who, fmt.Errorf("the session of %s never started: %s", who, t.Err)
		}
		return t, who, fmt.Errorf("the session of %s has ended. Start it again with %s_restart", who, map[bool]string{true: "session", false: "planner"}[card != nil])
	}
	return t, who, nil
}

func sendTo(c *Call, workspaceID string, card *domain.Card, text string, enter *bool) (SessionStatus, error) {
	t, _, err := liveTarget(c, workspaceID, card)
	if err != nil {
		return SessionStatus{}, err
	}
	if err := t.Live.Send(text, enter == nil || *enter); err != nil {
		return SessionStatus{}, err
	}
	return statusOf(card, workspaceID, t), nil
}

func keysTo(c *Call, workspaceID string, card *domain.Card, keys []string) (SessionStatus, error) {
	t, _, err := liveTarget(c, workspaceID, card)
	if err != nil {
		return SessionStatus{}, err
	}
	if err := t.Live.Keys(keys); err != nil {
		return SessionStatus{}, err
	}
	return statusOf(card, workspaceID, t), nil
}

func readScreen(c *Call, workspaceID string, card *domain.Card, lines int, scrollback bool) (ScreenOut, error) {
	if lines < 0 || lines > 5000 {
		return ScreenOut{}, errors.New("lines must be between 1 and 5000")
	}
	if lines == 0 {
		lines = 60
	}
	t, err := target(c, workspaceID, card)
	if err != nil {
		return ScreenOut{}, err
	}
	// An ended session's process is gone from the live set, but the manager still holds its last
	// screen, which is what you want to read after a crash.
	ps, ok := c.Deps.Sessions.Live(t.Row.ID)
	if !ok {
		who := "the planner"
		if card != nil {
			who = fmt.Sprintf("card %q", card.Nick)
		}
		return ScreenOut{}, fmt.Errorf("%s has no screen to read: its session has not run in this app session", who)
	}
	return ScreenOut{Running: t.Live != nil, Liveness: ps.Liveness(), Screen: ps.Screen(lines, scrollback)}, nil
}
