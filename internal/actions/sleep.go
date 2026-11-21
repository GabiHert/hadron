package actions

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/GabiHert/hadron/internal/pty"
	"github.com/GabiHert/hadron/internal/sessions"
)

// SleepStatusIn is the input of session_sleep_status.
type SleepStatusIn struct {
	Children bool `json:"children,omitempty" jsonschema:"also sample the processes under each idle session (about a second each) to see whether a test or build keeps it awake"`
}

// SleepStatusOut is what session_sleep_status returns.
type SleepStatusOut struct {
	// SleepAfterMinutes is the idle time before a card's session sleeps; 0 is never.
	SleepAfterMinutes int  `json:"sleep_after_minutes"`
	MemoryGuard       bool `json:"memory_guard"`
	// Pressure is the Mac's memory pressure: normal, warning, critical or unknown.
	Pressure string `json:"pressure"`
	// Awake are the running card sessions, longest idle first.
	Awake []SleepEntry `json:"awake"`
	// Asleep are the cards whose session sleeps.
	Asleep []SleepEntry `json:"asleep"`
}

// SleepEntry is one card session in session_sleep_status.
type SleepEntry struct {
	WorkspaceID string `json:"workspace_id"`
	CardID      string `json:"card_id"`
	Nick        string `json:"nick"`
	Liveness    string `json:"liveness"`
	IdleSeconds int    `json:"idle_seconds,omitempty"`
	// Blocker says why it cannot sleep now; empty when it can.
	Blocker string `json:"blocker,omitempty"`
}

func registerSleep(r *Registry) {
	Register(r, Spec{Name: "session_sleep", Description: "Put a card's session to sleep now to free its memory: Hadron stops its claude process and marks the card asleep, keeping the conversation. The next session_send, answer, or key typed in its terminal wakes it (it resumes the conversation first). Refused while it is working, has a question open, shows a dialog, or a process under it (a test, a build) is working. The planner never sleeps. Settings: sessions.sleep_after_minutes (idle minutes before a session sleeps on its own, default 30, 0 never) and sessions.memory_guard (sleep the longest-idle sessions when macOS reports memory pressure, default true).",
		Mutates: true, NoTx: true, Entities: []string{"sessions", "cards", "messages"}},
		func(ctx context.Context, c *Call, in SessionCardIn) (SessionStatus, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, true, "session_sleep"); err != nil {
				return SessionStatus{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := sv.SleepCard(c.Store, card.WorkspaceID, card.ID); err != nil {
				return SessionStatus{}, fmt.Errorf("card %q did not sleep: %v", card.Nick, err)
			}
			t, err := target(c, card.WorkspaceID, &card)
			if err != nil {
				return SessionStatus{}, err
			}
			return statusOf(&card, card.WorkspaceID, t), nil
		})

	Register(r, Spec{Name: "session_wake", Description: "Wake a card's sleeping session: resume its Claude conversation and return once Claude's prompt shows. A running session is returned as it is. session_send wakes a sleeping session by itself; use this to have it ready ahead of time.",
		Mutates: true, NoTx: true, Entities: []string{"sessions", "cards", "messages"}},
		func(ctx context.Context, c *Call, in SessionCardIn) (SessionStatus, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return SessionStatus{}, err
			}
			if err := mayControl(c, card.WorkspaceID, &card, true, "session_wake"); err != nil {
				return SessionStatus{}, err
			}
			sv, err := svc(c)
			if err != nil {
				return SessionStatus{}, err
			}
			t, err := sv.Wake(ctx, card.WorkspaceID, card.ID)
			if errors.Is(err, sessions.ErrNotAsleep) {
				return SessionStatus{}, fmt.Errorf("card %q is not asleep and not running. Start it with session_start", card.Nick)
			}
			if errors.Is(err, sessions.ErrNoSession) {
				return SessionStatus{}, fmt.Errorf("card %q has no session yet. Start it with session_start", card.Nick)
			}
			if err != nil {
				return SessionStatus{}, err
			}
			return statusOf(&card, card.WorkspaceID, t), nil
		})

	Register(r, Spec{Name: "session_sleep_status", Description: "Show the session sleep settings, the Mac's memory pressure, every running card session with how long it has been idle and what keeps it awake, and the cards that sleep. Across all workspaces."},
		func(ctx context.Context, c *Call, in SleepStatusIn) (SleepStatusOut, error) {
			sv, err := svc(c)
			if err != nil {
				return SleepStatusOut{}, err
			}
			out := SleepStatusOut{
				SleepAfterMinutes: int(sessions.SleepAfter(c.Store) / time.Minute),
				MemoryGuard:       sessions.MemoryGuardOn(c.Store),
				Pressure:          pty.PressureName(sv.Pressure()),
				Awake:             []SleepEntry{},
				Asleep:            []SleepEntry{},
			}
			all := sv.Sleepers(in.Children)
			sort.SliceStable(all, func(i, j int) bool { return all[i].Idle > all[j].Idle })
			for _, s := range all {
				liveness := s.Card.Liveness
				if ps, ok := sv.Live(s.Session.ID); ok {
					liveness = ps.Liveness()
				}
				out.Awake = append(out.Awake, SleepEntry{WorkspaceID: s.Session.WorkspaceID, CardID: s.Card.ID, Nick: s.Card.Nick, Liveness: liveness, IdleSeconds: int(s.Idle / time.Second), Blocker: s.Blocker})
			}
			cards, err := c.Store.Cards.ListAsleep()
			if err != nil {
				return SleepStatusOut{}, err
			}
			for _, cd := range cards {
				out.Asleep = append(out.Asleep, SleepEntry{WorkspaceID: cd.WorkspaceID, CardID: cd.ID, Nick: cd.Nick, Liveness: cd.Liveness})
			}
			return out, nil
		})
}
