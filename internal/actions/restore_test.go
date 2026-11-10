package actions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
)

// relaunch simulates quitting Hadron and opening it again: the processes die with the old service,
// the rows left running are marked exited as Run does, and a new service starts over the same
// store, home and fake claude.
func (h *sessionHarness) relaunch(t *testing.T) *sessions.Service {
	t.Helper()
	h.svc.StopAll()
	waitUntil(t, "processes to end", func() bool { return len(h.svc.LiveSessions()) == 0 })
	if _, err := h.reg.Deps().Store.Sessions.MarkRunningExited(); err != nil {
		t.Fatal(err)
	}
	env := h.env
	svc := sessions.New(sessions.Config{Store: h.reg.Deps().Store, Home: h.home, Env: func() []string { return env },
		FirstPromptDelay: 200 * time.Millisecond, Window: 150 * time.Millisecond})
	t.Cleanup(svc.StopAll)
	return svc
}

func (h *sessionHarness) history() []string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(h.file(".history")), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestRunningSessionsComeBackAfterARestartPlannerFirst(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var a, b domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "launch_profile": "brief"}, &a)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "invoice-sync"}, &b)
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "three starts", func() bool { return len(h.history()) == 3 })
	pl, ra, rb := h.latest(w.ID, ""), h.latest(w.ID, a.ID), h.latest(w.ID, b.ID)
	// billing-fix and the planner had a first message (their transcripts exist); invoice-sync had not.
	writeConversation(t, cfg, w.Root, pl.ClaudeSessionID, "hello", 3000, time.Minute)
	writeConversation(t, cfg, w.Root, ra.ClaudeSessionID, "hello", 3000, time.Minute)
	waitUntil(t, "running count", func() bool { return len(h.svc.LiveSessions()) == 3 })
	var run RunningSessionsOut
	h.do("sessions_running", m{}, &run)
	if run.Count != 3 || !run.RestoreOnLaunch {
		t.Fatalf("running = %+v", run)
	}
	st := h.reg.Deps().Store
	for _, id := range []string{pl.ID, ra.ID, rb.ID} {
		if got, _ := st.Sessions.Get(id); !got.Restore {
			t.Fatalf("a started session is flagged to come back: %+v", got)
		}
	}

	waitUntil(t, "the first prompt", func() bool { return strings.Contains(h.file(".in"), "/brief billing-fix") })
	typedBefore := h.file(".in")
	svc := h.relaunch(t)
	for _, id := range []string{pl.ID, ra.ID, rb.ID} {
		if got, _ := st.Sessions.Get(id); !got.Restore {
			t.Fatalf("quitting keeps the flag: %+v", got)
		}
	}
	before := len(h.history())
	n := svc.RestoreAll(context.Background(), 20*time.Millisecond)
	if n != 3 {
		t.Fatalf("restored %d sessions, want 3", n)
	}
	waitUntil(t, "three restarts", func() bool { return len(h.history()) == before+3 })
	got := h.history()[before:]
	want := []string{"planner --resume " + pl.ClaudeSessionID, "card --resume " + ra.ClaudeSessionID, "card --session-id " + rb.ClaudeSessionID}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("restart order and flags:\n got %q\nwant %q", got, want)
	}
	// Nothing is typed into a restored session: no first prompt (billing-fix has /brief) and no handoff.
	time.Sleep(500 * time.Millisecond)
	if h.file(".in") != typedBefore {
		t.Errorf("a restore never sends a first prompt: %q", h.file(".in"))
	}
	// Each restored card says so, and the old rows no longer ask to be restored.
	for _, c := range []domain.Card{a, b} {
		found := false
		for _, msg := range h.messagesOf(c) {
			found = found || (msg.Kind == "event" && msg.Text == "Resumed after Hadron restarted.")
		}
		if !found {
			t.Errorf("%s has no 'Resumed after Hadron restarted.' event", c.Nick)
		}
	}
	for _, id := range []string{pl.ID, ra.ID, rb.ID} {
		if old, _ := st.Sessions.Get(id); old.Restore {
			t.Errorf("the old row %s is still flagged", id)
		}
	}
	if cur := h.latest(w.ID, a.ID); !cur.Restore || cur.ID == ra.ID || cur.ClaudeSessionID != ra.ClaudeSessionID {
		t.Errorf("the restored session is flagged and keeps the conversation id: %+v", cur)
	}
}

func TestAStoppedOrExitedSessionIsNotRestored(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var a, b, c domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix"}, &a)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "invoice-sync"}, &b)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "ledger-audit"}, &c)
	waitFor(t, "starts", slowWait, func() bool { return len(h.history()) == 3 })
	// The user stops one; another ends by itself (the user typed /exit: the process leaves).
	h.do("session_stop", m{"card": "billing-fix"}, nil)
	h.do("session_send", m{"card": "invoice-sync", "text": "/exit"}, nil)
	// Wait for what the restore reads: both rows recorded as exited, with their restore flag
	// cleared. Under a loaded suite the processes can take seconds to go.
	st := h.reg.Deps().Store
	ended := func(card domain.Card) bool {
		row, err := st.Sessions.Get(h.latest(w.ID, card.ID).ID)
		return err == nil && row.Status == "exited" && !row.Restore
	}
	waitFor(t, "billing-fix and invoice-sync to end", slowWait, func() bool { return ended(a) && ended(b) })
	if n := len(h.svc.LiveSessions()); n != 1 {
		t.Fatalf("live sessions = %d, want only ledger-audit", n)
	}

	svc := h.relaunch(t)
	before := len(h.history())
	if n := svc.RestoreAll(context.Background(), 10*time.Millisecond); n != 1 {
		t.Fatalf("only the session that was running comes back, got %d", n)
	}
	waitFor(t, "one restart", slowWait, func() bool { return len(h.history()) == before+1 })
	if got := h.history()[before]; !strings.HasPrefix(got, "card --session-id ") {
		t.Errorf("history = %q", got)
	}
	if st := h.status("billing-fix"); st.Running {
		t.Error("the stopped card stays down")
	}
}

func TestRestoreOnLaunchOffRestoresNothingAndForgetsThem(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix"}, nil)
	waitUntil(t, "start", func() bool { return len(h.history()) == 1 })
	h.do("settings_update", m{"values": m{"sessions.restore_on_launch": false}}, nil)
	h.do("sessions_running", m{}, nil)

	svc := h.relaunch(t)
	if n := svc.RestoreAll(context.Background(), 10*time.Millisecond); n != 0 {
		t.Fatalf("restored %d with the setting off", n)
	}
	time.Sleep(300 * time.Millisecond)
	if len(h.history()) != 1 {
		t.Fatalf("nothing may start: %q", h.history())
	}
	// Turning it back on later does not bring back what was running long ago.
	h.do("settings_update", m{"values": m{"sessions.restore_on_launch": true}}, nil)
	if rows, _ := h.reg.Deps().Store.Sessions.ListRestorable(); len(rows) != 0 {
		t.Errorf("the flags were cleared: %+v", rows)
	}
}
