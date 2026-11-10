package actions

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

// TestSessionSendRightAfterAStartWaitsForClaudesPrompt is the regression test for feedback
// a27f903d: a session_send right after session_start (or a restart) was typed before Claude Code
// showed its prompt, so the text was lost while session_send reported success.
func TestSessionSendRightAfterAStartWaitsForClaudesPrompt(t *testing.T) {
	h := newSessionHarnessEnv(t, false, "HADRON_TEST_BOX_DELAY=0.6") // this claude takes 0.6 s to show its prompt
	w := h.workspace("Acme")
	h.do("card_create", m{"workspace_id": w.ID, "nick": "phonon-fix"}, nil)

	start := time.Now()
	h.do("session_send", m{"card": "phonon-fix", "text": "first words"}, nil)
	if _, err := os.Stat(h.out + ".ready"); err != nil {
		t.Fatalf("session_send returned %s after the start, before the prompt showed", time.Since(start))
	}
	waitUntil(t, "the text in the session", func() bool { return strings.Contains(h.file(".in"), "first words") })

	// Once the prompt has shown, a send does not wait.
	quick := time.Now()
	h.do("session_send", m{"card": "phonon-fix", "text": "second"}, nil)
	if d := time.Since(quick); d > 300*time.Millisecond {
		t.Fatalf("a send to a ready session waited %s", d)
	}

	// A restart is a new start: the next send waits for the new prompt.
	_ = os.Remove(h.out + ".ready")
	h.do("session_restart", m{"card": "phonon-fix"}, nil)
	h.do("session_send", m{"card": "phonon-fix", "text": "after the restart"}, nil)
	if _, err := os.Stat(h.out + ".ready"); err != nil {
		t.Fatal("session_send after a restart did not wait for the new prompt")
	}
	waitUntil(t, "the text after the restart", func() bool { return strings.Contains(h.file(".in"), "after the restart") })
}

func TestPlannerSendRightAfterAStartWaitsForThePrompt(t *testing.T) {
	h := newSessionHarnessEnv(t, false, "HADRON_TEST_BOX_DELAY=0.5")
	w := h.workspace("Acme")
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	h.do("planner_send", m{"workspace_id": w.ID, "text": "[planner] hello"}, nil)
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
	if d := time.Since(start); d > harnessReadyTimeout+2*time.Second {
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
