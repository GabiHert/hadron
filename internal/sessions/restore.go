package sessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// SettingRestoreOnLaunch says whether Hadron brings back the sessions that were running when it
// quit, crashed or updated. On unless set to false.
const SettingRestoreOnLaunch = "sessions.restore_on_launch"

// RestoreEvent is the text posted on a card (or the planner channel) that was restored.
const RestoreEvent = "Resumed after Hadron restarted."

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
	st := s.cfg.Store
	if !RestoreOnLaunch(st) {
		_ = st.Sessions.ClearRestore()
		return 0
	}
	rows, err := st.Sessions.ListRestorable()
	if err != nil {
		s.cfg.Log.Error("restore: could not list sessions", "error", err.Error())
		return 0
	}
	started := 0
	for i, old := range rows {
		if ctx.Err() != nil {
			return started
		}
		if i > 0 && stagger > 0 {
			select {
			case <-ctx.Done():
				return started
			case <-time.After(stagger):
			}
		}
		if s.restoreOne(old) {
			started++
		}
	}
	return started
}

func (s *Service) restoreOne(old domain.Session) bool {
	st := s.cfg.Store
	_ = st.Sessions.SetRestore(old.ID, false) // a failed restore must not be retried at every launch
	ws, err := st.Workspaces.Get(old.WorkspaceID)
	if err != nil || ws.Archived {
		return false
	}
	var card *domain.Card
	channel := "planner"
	if old.CardID != "" {
		c, err := st.Cards.Get(old.CardID)
		if err != nil || c.Archived {
			return false
		}
		card, channel = &c, "card:"+c.ID
	}
	var launch func()
	var row domain.Session
	err = st.Tx(func(tx *store.Store) error {
		var perr error
		row, launch, perr = s.Prepare(tx, Request{Workspace: ws, Card: card, Restore: true})
		return perr
	})
	if err != nil {
		s.cfg.Log.Error("restore: could not restart a session", "session", old.ID, "error", err.Error())
		ev := domain.Message{WorkspaceID: ws.ID, Channel: channel, Author: "system", Kind: "event", Text: "Could not resume after Hadron restarted: " + err.Error(), Delivered: true}
		_ = st.Messages.Create(&ev)
		s.changed(ws.ID, "messages")
		return false
	}
	launch()
	s.cfg.Log.Debug("restored a session", "session", row.ID, "kind", row.Kind, "claude_session", row.ClaudeSessionID)
	ev := domain.Message{WorkspaceID: ws.ID, Channel: channel, Author: "system", Kind: "event", Text: RestoreEvent, Delivered: true}
	_ = st.Messages.Create(&ev)
	if card != nil {
		s.changed(ws.ID, "cards", "sessions", "messages")
	} else {
		s.changed(ws.ID, "sessions", "messages")
	}
	return true
}
