package testwait

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDeadlineIsThirtySecondsUnlessScaled(t *testing.T) {
	t.Setenv(ScaleEnv, "")
	if Deadline() != 30*time.Second {
		t.Fatalf("Deadline = %s", Deadline())
	}
	t.Setenv(ScaleEnv, "2")
	if Deadline() != time.Minute || Scaled(time.Second) != 2*time.Second {
		t.Fatalf("scaled by 2: %s, %s", Deadline(), Scaled(time.Second))
	}
	for _, bad := range []string{"0", "-1", "fast"} {
		t.Setenv(ScaleEnv, bad)
		if Deadline() != Base {
			t.Fatalf("%q changed the deadline to %s", bad, Deadline())
		}
	}
}

func TestPollReturnsAsSoonAsTheConditionHolds(t *testing.T) {
	var n atomic.Int32
	start := time.Now()
	if !Poll(time.Minute, func() bool { return n.Add(1) >= 3 }) {
		t.Fatal("Poll gave up")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("three checks took %s", d)
	}
	if Poll(20*time.Millisecond, func() bool { return false }) {
		t.Fatal("Poll said a false condition held")
	}
}
