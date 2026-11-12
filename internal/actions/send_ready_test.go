package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

// gatedHarness is a session harness whose fake claude draws its input box only when the test
// opens the gate, so "the send waited for the prompt" does not depend on how fast this machine
// starts a process. The ready timeout is long: only the gate decides when the prompt shows.
func gatedHarness(t *testing.T) (*sessionHarness, string) {
	t.Helper()
	gate := filepath.Join(t.TempDir(), "gate")
	return newSessionHarnessReady(t, false, slowWait, "HADRON_TEST_BOX_GATE="+gate), gate
}

func openGate(t *testing.T, gate string) {
	t.Helper()
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
}

// sendHeld runs a send in the background and checks it is still waiting once claude has started
// and before the gate opens: nothing is typed and the call has not returned.
func sendHeld(t *testing.T, h *sessionHarness, name string, args m, text string) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- h.try(name, args, nil) }()
	waitFor(t, "claude to start", slowWait, func() bool { return h.file(".started") != "" })
	time.Sleep(100 * time.Millisecond) // a send that did not wait would have typed by now
	select {
	case err := <-done:
		t.Fatalf("%s returned (%v) before Claude's prompt showed", name, err)
	default:
	}
	if strings.Contains(h.file(".in"), text) {
		t.Fatalf("%s typed %q before Claude's prompt showed", name, text)
	}
	return done
}

func sent(t *testing.T, done <-chan error, name string) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	case <-time.After(slowWait):
		t.Fatalf("%s did not return after the prompt showed", name)
	}
}

// TestSessionSendRightAfterAStartWaitsForClaudesPrompt is the regression test for feedback
// a27f903d: a session_send right after session_start (or a restart) was typed before Claude Code
// showed its prompt, so the text was lost while session_send reported success.
func TestSessionSendRightAfterAStartWaitsForClaudesPrompt(t *testing.T) {
	h, gate := gatedHarness(t)
	w := h.workspace("Acme")
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix"}, nil)

	done := sendHeld(t, h, "session_send", m{"card": "billing-fix", "text": "first words"}, "first words")
	openGate(t, gate)
	sent(t, done, "session_send")
	if _, err := os.Stat(h.out + ".ready"); err != nil {
		t.Fatal("session_send returned before the prompt showed")
	}
	waitUntil(t, "the text in the session", func() bool { return strings.Contains(h.file(".in"), "first words") })

	// Once the prompt has shown, a send goes straight through: the gate stays shut and it still arrives.
	h.do("session_send", m{"card": "billing-fix", "text": "second"}, nil)
	waitUntil(t, "the second text", func() bool { return strings.Contains(h.file(".in"), "second") })

	// A restart is a new start: the next send waits for the new prompt.
	_ = os.Remove(h.out + ".ready")
	_ = os.Remove(h.out + ".started")
	h.do("session_restart", m{"card": "billing-fix"}, nil)
	done = sendHeld(t, h, "session_send", m{"card": "billing-fix", "text": "after the restart"}, "after the restart")
	openGate(t, gate)
	sent(t, done, "session_send after the restart")
	if _, err := os.Stat(h.out + ".ready"); err != nil {
		t.Fatal("session_send after a restart did not wait for the new prompt")
	}
	waitUntil(t, "the text after the restart", func() bool { return strings.Contains(h.file(".in"), "after the restart") })
}

func TestPlannerSendRightAfterAStartWaitsForThePrompt(t *testing.T) {
	h, gate := gatedHarness(t)
	w := h.workspace("Acme")
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	done := sendHeld(t, h, "planner_send", m{"workspace_id": w.ID, "text": "[planner] hello"}, "[planner] hello")
	openGate(t, gate)
	sent(t, done, "planner_send")
	if _, err := os.Stat(h.out + ".ready"); err != nil {
		t.Fatal("planner_send did not wait for the planner's prompt")
	}
	waitUntil(t, "the planner text", func() bool { return strings.Contains(h.file(".in"), "[planner] hello") })
}

func TestSessionSendFailsClearlyWhenThePromptNeverShows(t *testing.T) {
	h := newSessionHarness(t, true) // silent: this claude never draws its prompt
	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "stuck"}, &card)
	waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })
	start := time.Now()
	h.fails("session_send", m{"card": "stuck", "text": "lost?"}, `nothing was typed into card "stuck"`, "did not show within 1s", "session_read_screen")
	if d := time.Since(start); d > harnessReadyTimeout+10*time.Second { // it gives up, rather than hanging
		t.Fatalf("the failing send took %s", d)
	}
	time.Sleep(200 * time.Millisecond)
	if got := h.file(".in"); strings.Contains(got, "lost?") {
		t.Fatalf("the text was typed anyway: %q", got)
	}
	// Past the start-up window the session counts as running anything, and a send goes through.
	h.do("session_send", m{"card": "stuck", "text": "now it goes"}, nil)
	waitUntil(t, "the late send", func() bool { return strings.Contains(h.file(".in"), "now it goes") })
}
