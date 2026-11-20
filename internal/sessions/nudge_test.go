package sessions

import "testing"

func TestPendingInBoxLooksOnlyAtTheLastPromptLine(t *testing.T) {
	probe := nudgeProbe(RestoreNudge)
	typed := "│ > " + RestoreNudge
	sent := "> " + RestoreNudge + "\n\n✻ Working…\n│ > │"
	cases := map[string]bool{
		typed:               true,
		"notice\n" + typed:  true,
		sent:                false, // echoed above an empty box: submitted
		"│ > │":             false,
		"❯ 1. Yes\n❯ 2. No": false,
		"":                  false,
	}
	for screen, want := range cases {
		if got := pendingInBox(screen, probe); got != want {
			t.Errorf("pendingInBox(%q) = %v, want %v", screen, got, want)
		}
	}
}
