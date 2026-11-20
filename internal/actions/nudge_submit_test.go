package actions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/testwait"
)

// boxClaude draws an input box like Claude's and edits it in raw mode: typed text shows on the box
// line, Enter sends it (written to $out.in, echoed above a fresh, empty box). Like Claude right
// after a resume, it prints a notice first, and it drops the first Enter it gets.
const boxClaude = `#!/bin/bash
out="$HADRON_TEST_OUT"
echo started >> "$out.box-started"
stty -icanon -echo min 1 2>/dev/null
printf 'Artifact comment monitor did not resume\r\n'
printf '│ > '
buf=""; dropped=""
while IFS= read -r -n1 -d '' c; do
  case "$c" in
    $'\r'|$'\n')
      if [ -z "$dropped" ]; then dropped=1; echo dropped >> "$out.drops"; continue; fi
      printf '%s\n' "$buf" >> "$out.in"
      printf '\r\n> %s\r\n│ > ' "$buf"; buf="" ;;
    *) buf="$buf$c"; printf '%s' "$c" ;;
  esac
done
`

// The restart nudge must end up submitted even when Claude loses the first Enter (seen live: the
// text sat in the box until the user pressed Enter).
func TestRestoreNudgeIsSubmittedWhenTheFirstEnterIsLost(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var a domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix"}, &a)
	waitUntil(t, "start", func() bool { return len(h.history()) == 1 })
	ra := h.latest(w.ID, a.ID)
	svc := h.relaunch(t)
	if err := h.reg.Deps().Store.Sessions.SetWorking(ra.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.bin, "claude"), []byte(boxClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	before := h.file(".in")
	if n, nudged := svc.RestoreAllNudging(context.Background(), 0); n != 1 || len(nudged) != 1 {
		t.Fatalf("restored %d, nudged %+v", n, nudged)
	}
	waitUntil(t, "the nudge to be submitted", func() bool { return strings.Contains(h.file(".in")[len(before):], sessions.RestoreNudge) })
	if h.file(".drops") == "" {
		t.Fatal("the fake claude never dropped an Enter, so the retry was not tested")
	}
	if c := strings.Count(h.file(".in")[len(before):], sessions.RestoreNudge); c != 1 {
		t.Errorf("the nudge was submitted %d times, want once: %q", c, h.file(".in"))
	}
	// The box is empty again: nothing is left for the user to send.
	ps, ok := svc.Live(h.latest(w.ID, a.ID).ID)
	if !ok {
		t.Fatal("the restored session is not running")
	}
	// The fake records the line before it redraws, so the screen may lag .in for a moment.
	lastLine := func() string {
		lines := strings.Split(strings.TrimRight(ps.Screen(0, false), "\n "), "\n")
		return strings.TrimSpace(lines[len(lines)-1])
	}
	waitUntil(t, "the box to be empty after the nudge", func() bool { return lastLine() == "│ >" })
}

// A first Enter that works is not followed by a second one.
func TestRestoreNudgeIsSentOnceWhenTheEnterWorks(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var a domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix"}, &a)
	waitUntil(t, "start", func() bool { return len(h.history()) == 1 })
	ra := h.latest(w.ID, a.ID)
	svc := h.relaunch(t)
	_ = h.reg.Deps().Store.Sessions.SetWorking(ra.ID, true)
	box := strings.Replace(boxClaude, `if [ -z "$dropped" ]`, `if false`, 1)
	if err := os.WriteFile(filepath.Join(h.bin, "claude"), []byte(box), 0o755); err != nil {
		t.Fatal(err)
	}
	before := h.file(".in")
	svc.RestoreAllNudging(context.Background(), 0)
	waitUntil(t, "the nudge to be submitted", func() bool { return strings.Contains(h.file(".in")[len(before):], sessions.RestoreNudge) })
	time.Sleep(2*testwait.Scaled(time.Second) + 500*time.Millisecond) // past two confirm windows: a stray second Enter would show
	if got := h.file(".in")[len(before):]; got != sessions.RestoreNudge+"\n" {
		t.Errorf("want the nudge once and no empty submit after it, got %q", got)
	}
}
