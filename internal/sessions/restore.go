package sessions

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// SettingRestoreOnLaunch says whether Hadron brings back the sessions that were running when it
// quit, crashed or updated. On unless set to false.
const SettingRestoreOnLaunch = "sessions.restore_on_launch"

// RestoreEvent is the text posted on a card (or the planner channel) that was restored.
const RestoreEvent = "Resumed after Hadron restarted."

// SettingRestoreNudge says whether a restored session that was working when Hadron quit, updated
// or crashed is told to carry on. On unless set to false. Idle sessions are never touched.
const SettingRestoreNudge = "sessions.restore_nudge"

// RestoreNudge is what a restored session that was mid-task is sent once its prompt shows.
const RestoreNudge = "Hadron restarted while you were working. Continue where you left off."

// RestoreNudgedEvent is posted on a card (or the planner channel) that was restored and nudged.
const RestoreNudgedEvent = "Resumed after Hadron restarted, and asked to continue: it was working."

// RestoreNudgeOn reports the setting.
func RestoreNudgeOn(st *store.Store) bool {
	if raw, err := st.Settings.Get(SettingRestoreNudge); err == nil {
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return b
		}
	}
	return true
}

// Nudged is a restored session that was told to continue.
type Nudged struct {
	WorkspaceID string
	// Name is the card's nickname, or "planner".
	Name string
}

// RestoreOnLaunch reports the setting.
func RestoreOnLaunch(st *store.Store) bool {
	if raw, err := st.Settings.Get(SettingRestoreOnLaunch); err == nil {
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return b
		}
	}
	return true
}

// RestoreAll brings back every session flagged to be running, the planners first and then the
// cards, stagger apart. Each goes through Prepare like any start, so it resumes its conversation
// when the transcript exists and starts it again under the same id when it does not. A restore
// never types a first prompt or a handoff. With the setting off, nothing is restored and the
// flags are cleared, so a later change of the setting does not bring back old sessions. It returns
// how many sessions it started.
func (s *Service) RestoreAll(ctx context.Context, stagger time.Duration) int {
	n, _ := s.RestoreAllNudging(ctx, stagger)
	return n
}

// RestoreAllNudging is RestoreAll that also sends RestoreNudge to each restored session that was
// working when Hadron went down (sessions.restore_nudge), once its prompt shows, and lists them.
func (s *Service) RestoreAllNudging(ctx context.Context, stagger time.Duration) (int, []Nudged) {
	st := s.cfg.Store
	if !RestoreOnLaunch(st) {
		_ = st.Sessions.ClearRestore()
		return 0, nil
	}
	rows, err := st.Sessions.ListRestorable()
	if err != nil {
		s.cfg.Log.Error("restore: could not list sessions", "error", err.Error())
		return 0, nil
	}
	nudge := RestoreNudgeOn(st)
	started := 0
	var nudged []Nudged
	for i, old := range rows {
		if ctx.Err() != nil {
			return started, nudged
		}
		if i > 0 && stagger > 0 {
			select {
			case <-ctx.Done():
				return started, nudged
			case <-time.After(stagger):
			}
		}
		ok, name := s.restoreOne(old, nudge && old.Working)
		if ok {
			started++
			if name != "" {
				nudged = append(nudged, Nudged{WorkspaceID: old.WorkspaceID, Name: name})
			}
		}
	}
	return started, nudged
}

// restoreOne restarts one session. With nudge it is sent RestoreNudge once ready, and the name it
// returns ("planner" or the card's nickname) says so.
func (s *Service) restoreOne(old domain.Session, nudge bool) (bool, string) {
	st := s.cfg.Store
	_ = st.Sessions.SetRestore(old.ID, false) // a failed restore must not be retried at every launch
	ws, err := st.Workspaces.Get(old.WorkspaceID)
	if err != nil || ws.Archived {
		return false, ""
	}
	var card *domain.Card
	channel := "planner"
	if old.CardID != "" {
		c, err := st.Cards.Get(old.CardID)
		if err != nil || c.Archived {
			return false, ""
		}
		card, channel = &c, "card:"+c.ID
	}
	var launch func()
	var row domain.Session
	err = st.Tx(func(tx *store.Store) error {
		var perr error
		req := Request{Workspace: ws, Card: card, Restore: true}
		if nudge {
			req.Nudge = RestoreNudge
		}
		row, launch, perr = s.Prepare(tx, req)
		return perr
	})
	if err != nil {
		s.cfg.Log.Error("restore: could not restart a session", "session", old.ID, "error", err.Error())
		ev := domain.Message{WorkspaceID: ws.ID, Channel: channel, Author: "system", Kind: "event", Text: "Could not resume after Hadron restarted: " + err.Error(), Delivered: true}
		_ = st.Messages.Create(&ev)
		s.changed(ws.ID, "messages")
		return false, ""
	}
	launch()
	s.cfg.Log.Debug("restored a session", "session", row.ID, "kind", row.Kind, "claude_session", row.ClaudeSessionID)
	text, name := RestoreEvent, ""
	if nudge {
		text, name = RestoreNudgedEvent, "planner"
		if card != nil {
			name = card.Nick
		}
	}
	ev := domain.Message{WorkspaceID: ws.ID, Channel: channel, Author: "system", Kind: "event", Text: text, Delivered: true}
	_ = st.Messages.Create(&ev)
	if card != nil {
		s.changed(ws.ID, "cards", "sessions", "messages")
	} else {
		s.changed(ws.ID, "sessions", "messages")
	}
	return true, name
}

// NudgedGroup is the nudged sessions of one workspace, in restore order.
type NudgedGroup struct {
	WorkspaceID string
	Names       []string
}

// GroupNudged groups nudged sessions by workspace, keeping the order the workspaces first appear in.
func GroupNudged(list []Nudged) []NudgedGroup {
	var out []NudgedGroup
	at := map[string]int{}
	for _, n := range list {
		i, ok := at[n.WorkspaceID]
		if !ok {
			i = len(out)
			at[n.WorkspaceID] = i
			out = append(out, NudgedGroup{WorkspaceID: n.WorkspaceID})
		}
		out[i].Names = append(out[i].Names, n.Name)
	}
	return out
}

// NudgeReport is the one line the planner gets about the sessions told to continue.
func NudgeReport(names []string) string {
	return "Hadron restarted; these sessions were working and were asked to continue: " + strings.Join(names, ", ") + "."
}
