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

// Free sessions are the terminals of the Sessions space (sessions.Service, free.go): global, with
// an optional workspace tag. Card sessions cannot reach them: they are the user's own shells.

// FreeRefIn names one free session.
type FreeRefIn struct {
	Session string `json:"session" jsonschema:"the free session: its id, or its name when only one has it"`
}

// FreeStartIn is the input of free_session_start.
type FreeStartIn struct {
	Kind        string `json:"kind,omitempty" jsonschema:"shell (default: the user's login shell with their full environment) or claude"`
	Cwd         string `json:"cwd,omitempty" jsonschema:"the folder it starts in: an absolute path, ~ is expanded. Empty means the folder the last free session started in, else the home folder"`
	Name        string `json:"name,omitempty" jsonschema:"an optional name for its tab"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"an optional workspace tag; free sessions are global and keep running in every workspace"`
}

// FreeListIn is the input of free_session_list.
type FreeListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"list only the sessions tagged with this workspace"`
}

// FreeListOut is the result of free_session_list.
type FreeListOut struct {
	Sessions []sessions.FreeStatus `json:"sessions"`
	// DefaultCwd is where a new free session starts when no folder is given.
	DefaultCwd string `json:"default_cwd"`
}

// FreeSendIn is the input of free_session_send.
type FreeSendIn struct {
	Session string   `json:"session" jsonschema:"the free session: its id, or its name when only one has it"`
	Text    string   `json:"text,omitempty" jsonschema:"text to type, sent like a paste"`
	Enter   *bool    `json:"enter,omitempty" jsonschema:"press Enter after the text (default true)"`
	Keys    []string `json:"keys,omitempty" jsonschema:"keys to press after the text, in order: Enter, Escape, Tab, Up, Down, Left, Right, Backspace, Ctrl-<letter>, or one character"`
}

// FreeScreenIn is the input of free_session_read_screen.
type FreeScreenIn struct {
	Session    string `json:"session" jsonschema:"the free session: its id, or its name when only one has it"`
	Lines      int    `json:"lines,omitempty" jsonschema:"how many lines from the bottom (default 60)"`
	Scrollback bool   `json:"scrollback,omitempty" jsonschema:"include lines that scrolled off the top"`
}

// FreeStopIn is the input of free_session_stop.
type FreeStopIn struct {
	Session string `json:"session" jsonschema:"the free session: its id, or its name when only one has it"`
	Close   bool   `json:"close,omitempty" jsonschema:"also close it: its tab and its output go. Without it the session stays, ended, and free_session_restart starts it again"`
}

// FreeUpdateIn is the input of free_session_update.
type FreeUpdateIn struct {
	Session     string  `json:"session" jsonschema:"the free session: its id, or its name when only one has it"`
	Name        *string `json:"name,omitempty" jsonschema:"the new name; empty clears it"`
	WorkspaceID *string `json:"workspace_id,omitempty" jsonschema:"the workspace tag; empty removes it"`
}

// FreeToCardIn is the input of free_session_to_card.
type FreeToCardIn struct {
	Session     string `json:"session" jsonschema:"the free session: its id, or its name when only one has it"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the board it goes to; defaults to the session's workspace tag, else the active workspace"`
	Nick        string `json:"nick" jsonschema:"the new card's nickname, kebab-case"`
	Key         string `json:"key,omitempty" jsonschema:"an optional key such as ACME-508"`
	ColumnID    string `json:"column_id,omitempty" jsonschema:"the column; default is the first active one"`
}

// FreeToCardOut is the result of free_session_to_card.
type FreeToCardOut struct {
	Card    domain.Card   `json:"card"`
	Status  SessionStatus `json:"status"`
	Warning string        `json:"warning,omitempty"`
}

// freeByRef finds a free session by id, else by a name only one session has.
func freeByRef(st *store.Store, ref string) (domain.FreeSession, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return domain.FreeSession{}, errors.New("session is required: a free session's id or name (free_session_list lists them)")
	}
	if f, err := st.FreeSessions.Get(ref); err == nil {
		return f, nil
	}
	list, err := st.FreeSessions.List()
	if err != nil {
		return domain.FreeSession{}, err
	}
	var hits []domain.FreeSession
	for _, f := range list {
		if strings.EqualFold(f.Name, ref) {
			hits = append(hits, f)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return domain.FreeSession{}, fmt.Errorf("no free session is called %q. Call free_session_list for the ids", ref)
	}
	return domain.FreeSession{}, fmt.Errorf("%d free sessions are called %q. Use the id from free_session_list", len(hits), ref)
}

// freeWorkspaceTag checks an optional workspace tag.
func freeWorkspaceTag(c *Call, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", nil
	}
	if _, err := c.Store.Workspaces.Get(id); err != nil {
		return "", fmt.Errorf("workspace %q does not exist. Call workspace_list for the ids", id)
	}
	return id, nil
}

const freeWho = "use free sessions: they are the user's own terminals"

func registerFree(r *Registry) {
	ents := []string{sessions.EntityFree}

	Register(r, Spec{Name: "free_session_start", Description: "Start a free session in the Sessions space: a terminal that belongs to no card. kind shell (default) runs the user's login shell with their full environment; kind claude runs claude, with none of Hadron's prompt. It starts in cwd, else the folder the last one started in, else home. Free sessions are global, keep running in every workspace and view, and come back after Hadron restarts (a shell in its last folder, claude resuming its conversation).",
		Mutates: true, Entities: ents},
		func(ctx context.Context, c *Call, in FreeStartIn) (sessions.FreeStatus, error) {
			if err := userOrPlanner(c, freeWho); err != nil {
				return sessions.FreeStatus{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			folder, err := resolveFolder("cwd", in.Cwd)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			tag, err := freeWorkspaceTag(c, in.WorkspaceID)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			row, launch, err := sv.StartFree(c.Store, sessions.FreeRequest{Kind: strings.ToLower(strings.TrimSpace(in.Kind)), Name: in.Name, Cwd: folder, WorkspaceID: tag})
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			c.AfterCommit(launch)
			return sessions.FreeStatus{FreeSession: row, Running: true, Liveness: domain.LivenessIdle}, nil
		})

	Register(r, Spec{Name: "free_session_list", Description: "List the free sessions of the Sessions space, oldest first: id, name, kind (shell or claude), folder (a shell's current one), workspace tag, whether it runs, its liveness and the program in its foreground (vim, htop, claude). default_cwd is where a new one starts."},
		func(ctx context.Context, c *Call, in FreeListIn) (FreeListOut, error) {
			if err := userOrPlanner(c, freeWho); err != nil {
				return FreeListOut{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return FreeListOut{}, err
			}
			all, err := sv.ListFree(c.Store)
			if err != nil {
				return FreeListOut{}, err
			}
			out := FreeListOut{Sessions: []sessions.FreeStatus{}, DefaultCwd: sv.FreeDefaultCwd(c.Store)}
			for _, f := range all {
				if in.WorkspaceID == "" || f.WorkspaceID == in.WorkspaceID {
					out.Sessions = append(out.Sessions, f)
				}
			}
			return out, nil
		})

	Register(r, Spec{Name: "free_session_send", Description: "Type text into a free session like a paste, then press Enter (enter false skips it), then press keys if given. Fails when the session is not running.",
		Mutates: true, NoTx: true, Entities: ents},
		func(ctx context.Context, c *Call, in FreeSendIn) (sessions.FreeStatus, error) {
			if err := userOrPlanner(c, freeWho); err != nil {
				return sessions.FreeStatus{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			f, err := freeByRef(c.Store, in.Session)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			if in.Text == "" && len(in.Keys) == 0 {
				return sessions.FreeStatus{}, errors.New("give text, keys or both")
			}
			ps, ok := sv.FreeLive(f.ID)
			if !ok {
				return sessions.FreeStatus{}, fmt.Errorf("free session %s is not running. Start it again with free_session_restart", freeLabel(f))
			}
			if in.Text != "" {
				if err := ps.Send(in.Text, in.Enter == nil || *in.Enter); err != nil {
					return sessions.FreeStatus{}, err
				}
			}
			if len(in.Keys) > 0 {
				if err := ps.Keys(in.Keys); err != nil {
					return sessions.FreeStatus{}, err
				}
			}
			return sv.FreeStatusOf(c.Store, f.ID)
		})

	Register(r, Spec{Name: "free_session_read_screen", Description: "Read what a free session's terminal shows, as plain text: the last lines (default 60), optionally with the scrollback. Works after it ended too, until Hadron restarts."},
		func(ctx context.Context, c *Call, in FreeScreenIn) (ScreenOut, error) {
			if err := userOrPlanner(c, freeWho); err != nil {
				return ScreenOut{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return ScreenOut{}, err
			}
			if in.Lines < 0 || in.Lines > 5000 {
				return ScreenOut{}, errors.New("lines must be between 1 and 5000")
			}
			if in.Lines == 0 {
				in.Lines = 60
			}
			f, err := freeByRef(c.Store, in.Session)
			if err != nil {
				return ScreenOut{}, err
			}
			screen, ok := sv.FreeScreen(f.ID, in.Lines, in.Scrollback)
			if !ok {
				return ScreenOut{}, fmt.Errorf("free session %s has no screen to read: it has not run since Hadron started", freeLabel(f))
			}
			st := sv.FreeStatusOfRow(f)
			return ScreenOut{Running: st.Running, Liveness: st.Liveness, Screen: screen}, nil
		})

	Register(r, Spec{Name: "free_session_stop", Description: "Hang up a free session. close: true also closes it, removing its tab and output; without it the session stays, ended, for free_session_restart.",
		Mutates: true, Entities: ents},
		func(ctx context.Context, c *Call, in FreeStopIn) (map[string]any, error) {
			if err := userOrPlanner(c, freeWho); err != nil {
				return nil, err
			}
			sv, err := svc(c)
			if err != nil {
				return nil, err
			}
			f, err := freeByRef(c.Store, in.Session)
			if err != nil {
				return nil, err
			}
			if err := sv.StopFree(c.Store, f.ID, in.Close); err != nil {
				return nil, err
			}
			return map[string]any{"id": f.ID, "stopped": true, "closed": in.Close}, nil
		})

	Register(r, Spec{Name: "free_session_restart", Description: "Start an ended free session again in its tab: a shell in its last folder, claude resuming its conversation.",
		Mutates: true, Entities: ents},
		func(ctx context.Context, c *Call, in FreeRefIn) (sessions.FreeStatus, error) {
			if err := userOrPlanner(c, freeWho); err != nil {
				return sessions.FreeStatus{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			f, err := freeByRef(c.Store, in.Session)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			row, launch, err := sv.RestartFree(c.Store, f.ID)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			c.AfterCommit(launch)
			return sessions.FreeStatus{FreeSession: row, Running: true, Liveness: domain.LivenessIdle}, nil
		})

	Register(r, Spec{Name: "free_session_update", Description: "Rename a free session, or change its workspace tag. An empty name clears it; an empty workspace_id removes the tag.",
		Mutates: true, Entities: ents},
		func(ctx context.Context, c *Call, in FreeUpdateIn) (sessions.FreeStatus, error) {
			if err := userOrPlanner(c, freeWho); err != nil {
				return sessions.FreeStatus{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			f, err := freeByRef(c.Store, in.Session)
			if err != nil {
				return sessions.FreeStatus{}, err
			}
			if in.Name == nil && in.WorkspaceID == nil {
				return sessions.FreeStatus{}, errors.New("give name, workspace_id or both")
			}
			if in.Name != nil {
				name := strings.TrimSpace(*in.Name)
				if len([]rune(name)) > 80 {
					return sessions.FreeStatus{}, errors.New("name is longer than 80 characters")
				}
				f.Name = name
			}
			if in.WorkspaceID != nil {
				tag, err := freeWorkspaceTag(c, *in.WorkspaceID)
				if err != nil {
					return sessions.FreeStatus{}, err
				}
				f.WorkspaceID = tag
			}
			if err := c.Store.FreeSessions.Update(&f); err != nil {
				return sessions.FreeStatus{}, err
			}
			return sv.FreeStatusOfRow(f), nil
		})

	Register(r, Spec{Name: "free_session_to_card", Description: "Make a card of a free session without stopping it: a new card on a board takes over the running process, which keeps its screen and output and now reports to the card. The free session leaves the Sessions space. A claude keeps its conversation, so later restarts of the card resume it with Hadron's prompt and tools; until then the running claude has none of them. A shell card's later restarts run the card's launch profile (claude).",
		Mutates: true, Entities: []string{sessions.EntityFree, "cards", "sessions"}},
		func(ctx context.Context, c *Call, in FreeToCardIn) (FreeToCardOut, error) {
			if err := userOrPlanner(c, freeWho); err != nil {
				return FreeToCardOut{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return FreeToCardOut{}, err
			}
			f, err := freeByRef(c.Store, in.Session)
			if err != nil {
				return FreeToCardOut{}, err
			}
			wsID := in.WorkspaceID
			if wsID == "" {
				wsID = f.WorkspaceID
			}
			w, err := c.Workspace(wsID)
			if err != nil {
				return FreeToCardOut{}, err
			}
			if f.ClaudeSessionID != "" {
				if who, held := holder(c.Store, w.ID, f.ClaudeSessionID); held {
					return FreeToCardOut{}, fmt.Errorf("its conversation is already in Hadron (%s)", who)
				}
			}
			card, err := createCardFor(c, w, in.Nick, in.Key, in.ColumnID)
			if err != nil {
				return FreeToCardOut{}, err
			}
			st := sv.FreeStatusOfRow(f)
			if folder := storedFolder(w, st.Cwd); folder != "" {
				card.Cwd = folder
				if err := c.Store.Cards.Update(&card); err != nil {
					return FreeToCardOut{}, err
				}
			}
			row, commit, err := sv.FreeToCard(c.Store, f.ID, card)
			if err != nil {
				return FreeToCardOut{}, err
			}
			c.AfterCommit(commit)
			c.SetWorkspace(w.ID)
			out := FreeToCardOut{Card: card}
			out.Status = SessionStatus{WorkspaceID: w.ID, CardID: card.ID, Nick: card.Nick, Running: st.Running, Liveness: st.Liveness, Session: &row}
			if f.Kind == domain.FreeClaude && st.Running {
				out.Warning = "The running claude has none of Hadron's card prompt or tools until the card's session restarts (session_restart resumes the conversation with them)."
			}
			return out, nil
		})
}

func freeLabel(f domain.FreeSession) string {
	if f.Name != "" {
		return fmt.Sprintf("%q", f.Name)
	}
	return f.ID
}
