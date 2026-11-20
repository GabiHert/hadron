// Package testwait is the one wait the Go tests use for something that happens in the background:
// a process starting or ending, a file appearing, an event arriving. It polls, so a fast machine
// returns as soon as the condition holds, and its deadline is generous, so a Mac under heavy load
// (a release build, the whole suite with -race) is not mistaken for a broken feature.
//
// The deadline is 30 seconds, multiplied by HADRON_TEST_WAIT_SCALE when that is set to a positive
// number (2 doubles it, 0.5 halves it).
package testwait

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// Base is the deadline before scaling.
const Base = 30 * time.Second

// ScaleEnv names the variable that scales every deadline.
const ScaleEnv = "HADRON_TEST_WAIT_SCALE"

// Deadline is how long a wait may take before the test fails: Base times HADRON_TEST_WAIT_SCALE.
func Deadline() time.Duration { return Scaled(Base) }

// Scaled multiplies d by HADRON_TEST_WAIT_SCALE, for a wait that has its own base.
func Scaled(d time.Duration) time.Duration {
	if f, err := strconv.ParseFloat(os.Getenv(ScaleEnv), 64); err == nil && f > 0 {
		return time.Duration(float64(d) * f)
	}
	return d
}

// Until polls cond until it holds, failing the test with "timed out waiting for <what>" after
// Deadline. It checks at once, then every few milliseconds, backing off to 20 ms.
func Until(t testing.TB, what string, cond func() bool) {
	t.Helper()
	if !Poll(Deadline(), cond) {
		t.Fatalf("timed out waiting for %s (after %s; set %s to wait longer)", what, Deadline(), ScaleEnv)
	}
}

// Poll polls cond until it holds or within passes, and says whether it held.
func Poll(within time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(within)
	pause := 2 * time.Millisecond
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pause)
		if pause < 20*time.Millisecond {
			pause *= 2
		}
	}
}

// After is time.After(Deadline()), for a select on a channel.
func After() <-chan time.Time { return time.After(Deadline()) }
