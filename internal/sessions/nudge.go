package sessions

import (
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/pty"
)

// DefaultNudgeTimeout is how long a restored session may take to settle (its prompt showing, and
// no output for its working window) before the nudge is typed anyway.
const DefaultNudgeTimeout = 60 * time.Second

// DefaultNudgeConfirm is how long after Enter the nudge must have left the input box; if it has
// not, Enter is pressed again.
const DefaultNudgeConfirm = 3 * time.Second

// nudgeEnters is how many times Enter is pressed at most: the first and two retries.
const nudgeEnters = 3

// nudgeProbe is the start of the text, which is what shows on the input box's first line.
func nudgeProbe(text string) string {
	r := []rune(strings.TrimSpace(text))
	if len(r) > 24 {
		r = r[:24]
	}
	return string(r)
}

// pendingInBox reports whether the input box (the last prompt line on the screen) still holds the
// text. Claude echoes a sent message above the box with the same "> " mark, so only the last such
// line is the box.
func pendingInBox(screen, probe string) bool {
	last := ""
	for _, line := range strings.Split(screen, "\n") {
		if inputBox.MatchString(line) {
			last = line
		}
	}
	return probe != "" && strings.Contains(last, probe)
}

// deliverNudge types a restored session's nudge and makes sure it is submitted. Right after a
// resume Claude is still drawing (a resume notice, its history), and an Enter pressed then can be
// lost, leaving the nudge sitting in the box. So it waits for the prompt to show and the output to
// stop, types the text, waits until the box shows it, presses Enter, and checks that the box let
// go of the text (or, when the text never showed, that Claude started working); if not, it waits
// for the output to settle again and presses Enter once more, up to nudgeEnters times.
func (s *Service) deliverNudge(id string, ps *pty.Session, text string) {
	settled := func() bool { return ScreenShowsInput(ps.Screen(0, false)) && ps.Liveness() == pty.Idle }
	if !s.waitFor(ps, s.cfg.NudgeTimeout, settled) && !ScreenShowsInput(ps.Screen(0, false)) {
		s.cfg.Log.Debug("nudge: the prompt never showed; typing it anyway", "session", id)
	}
	if ps.Send(text, false) != nil {
		return
	}
	probe := nudgeProbe(text)
	shown := s.waitQuiet(ps, func() bool {
		return pendingInBox(ps.Screen(0, false), probe) && ps.Liveness() == pty.Idle
	})
	submitted := func() bool {
		if shown {
			return !pendingInBox(ps.Screen(0, false), probe)
		}
		return ps.Liveness() == pty.Working
	}
	for try := 1; try <= nudgeEnters; try++ {
		if ps.Keys([]string{"Enter"}) != nil {
			return
		}
		if s.waitQuiet(ps, submitted) {
			if try > 1 {
				s.cfg.Log.Info("nudge: submitted after pressing Enter again", "session", id, "enters", try)
			}
			return
		}
		// The Enter was lost: the output has settled (waitQuiet waited for that), so press again.
	}
	s.cfg.Log.Error("nudge: still in the input box after pressing Enter; left for the user", "session", id, "enters", nudgeEnters)
}

// waitQuiet waits NudgeConfirm for cond, and longer while the session is still printing: on a
// busy Mac, or while Claude draws a long resume, the screen can lag the input by seconds, and
// pressing Enter again then would send an empty line, or give up on a nudge that was about to
// land. It keeps waiting until cond holds or the output has been quiet for the working window,
// up to NudgeTimeout, and then says whether cond holds.
func (s *Service) waitQuiet(ps *pty.Session, cond func() bool) bool {
	if s.waitFor(ps, s.cfg.NudgeConfirm, cond) {
		return true
	}
	s.waitFor(ps, s.cfg.NudgeTimeout, func() bool { return cond() || ps.Liveness() == pty.Idle })
	return cond()
}

// waitFor polls cond every 40 ms until it holds (true), the time is up or the session ends (false).
func (s *Service) waitFor(ps *pty.Session, within time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(within)
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ps.Done():
			return false
		case <-tick.C:
		}
	}
}
