package quitguard

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/testwait"
)

type rec struct {
	mu     sync.Mutex
	events []string
	quits  atomic.Int32
	shown  atomic.Int32
}

func (r *rec) emit(event string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := payload.(HoldEvent); ok {
		event += ":" + h.State
	}
	r.events = append(r.events, event)
}

func (r *rec) has(e string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.events {
		if x == e {
			return true
		}
	}
	return false
}

func newGuard(on *atomic.Bool, hold time.Duration) (*Guard, *rec) {
	r := &rec{}
	g := &Guard{
		Enabled: func() bool { return on.Load() },
		Emit:    r.emit,
		Show:    func() { r.shown.Add(1) },
		Hold:    hold,
	}
	g.Quit = func() {
		// Wails' Quit asks OnBeforeClose again; it must let the quit through now.
		if !g.BeforeClose() {
			r.quits.Add(1)
		}
	}
	return g, r
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	testwait.Until(t, what, ok)
}

func on() *atomic.Bool {
	b := &atomic.Bool{}
	b.Store(true)
	return b
}

func TestHoldingCmdQForTheWholeHoldQuits(t *testing.T) {
	g, r := newGuard(on(), 60*time.Millisecond)
	if !g.KeyDown() {
		t.Fatal("the guard should take ⌘Q")
	}
	// Key repeats while held are taken too, and do not restart the hold.
	for i := 0; i < 3; i++ {
		if !g.KeyDown() {
			t.Fatal("a key repeat should be taken")
		}
	}
	waitFor(t, "the quit", func() bool { return r.quits.Load() == 1 })
	if !r.has(EventHold+":start") || !r.has(EventHold+":done") {
		t.Fatalf("events = %v", r.events)
	}
	if r.shown.Load() == 0 {
		t.Fatal("the window should come forward to show the hold")
	}
}

func TestLettingGoEarlyCancels(t *testing.T) {
	g, r := newGuard(on(), 150*time.Millisecond)
	g.KeyDown()
	time.Sleep(20 * time.Millisecond)
	g.KeyUp()
	time.Sleep(250 * time.Millisecond)
	if r.quits.Load() != 0 {
		t.Fatal("a tap must not quit")
	}
	if !r.has(EventHold + ":cancel") {
		t.Fatalf("events = %v", r.events)
	}
	if g.Holding() {
		t.Fatal("still holding after the key up")
	}
	// A key up with no hold says nothing.
	n := len(r.events)
	g.KeyUp()
	if len(r.events) != n {
		t.Fatal("a stray key up sent an event")
	}
}

func TestATapThenAHoldQuitsOnTheSecondHoldOnly(t *testing.T) {
	g, r := newGuard(on(), 80*time.Millisecond)
	g.KeyDown()
	g.KeyUp()
	time.Sleep(40 * time.Millisecond)
	g.KeyDown() // the first hold's timer must not quit this one early
	time.Sleep(60 * time.Millisecond)
	if r.quits.Load() != 0 {
		t.Fatal("the first tap's timer quit the second hold early")
	}
	waitFor(t, "the quit", func() bool { return r.quits.Load() == 1 })
}

func TestMenuOrDockQuitAsks(t *testing.T) {
	g, r := newGuard(on(), 0)
	if !g.BeforeClose() {
		t.Fatal("a menu quit should wait for the confirmation")
	}
	waitFor(t, "the confirmation", func() bool { return r.has(EventConfirm) })
	// The user confirms.
	g.QuitNow()
	if r.quits.Load() != 1 {
		t.Fatal("a confirmed quit should go through")
	}
}

func TestTheGuardOffQuitsAtOnce(t *testing.T) {
	enabled := &atomic.Bool{}
	g, r := newGuard(enabled, 0)
	if g.KeyDown() {
		t.Fatal("with the guard off, ⌘Q should reach the menu")
	}
	if g.BeforeClose() {
		t.Fatal("with the guard off, a quit should not wait")
	}
	if len(r.events) != 0 {
		t.Fatalf("events = %v", r.events)
	}
}

func TestTheUpdaterAndShutdownPass(t *testing.T) {
	g, _ := newGuard(on(), 0)
	g.Allow() // what the update installer does before it quits
	if g.BeforeClose() || g.KeyDown() {
		t.Fatal("an allowed quit should pass")
	}
	g2, _ := newGuard(on(), 0)
	g2.SystemQuitting = func() bool { return true }
	if g2.BeforeClose() {
		t.Fatal("logging out or shutting down must never wait")
	}
}

func TestNilEnabledMeansOn(t *testing.T) {
	g := &Guard{}
	if !g.BeforeClose() {
		t.Fatal("the guard is on by default")
	}
}
