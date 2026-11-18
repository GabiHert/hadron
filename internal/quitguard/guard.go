// Package quitguard keeps Hadron from quitting by accident.
//
// ⌘Q has to be held for a second, as in Chrome: a quick tap only shows "Hold ⌘Q to quit" with a
// ring that fills while the keys stay down, and letting go early cancels it. Quit from the app
// menu or the Dock asks for confirmation instead (the window shows it, with the running sessions).
// The update installer, a confirmed quit, and macOS logging out or shutting down pass straight
// through, and so does everything when the user turns the guard off (the app.quit_guard setting).
package quitguard

import (
	"sync"
	"sync/atomic"
	"time"
)

// SettingEnabled turns the guard on or off. On unless set to false.
const SettingEnabled = "app.quit_guard"

// Events the window shows.
const (
	// EventHold carries a HoldEvent: the ⌘Q hold started, was let go of, or finished.
	EventHold = "quit:hold"
	// EventConfirm asks the window to confirm a quit from the menu or the Dock.
	EventConfirm = "quit:confirm"
)

// Hold states.
const (
	HoldStart  = "start"
	HoldCancel = "cancel"
	HoldDone   = "done"
)

// HoldFor is how long ⌘Q has to stay down.
const HoldFor = time.Second

// HoldEvent is the payload of EventHold. Ms is the hold's full length, for the ring.
type HoldEvent struct {
	State string `json:"state"`
	Ms    int    `json:"ms"`
}

// Guard decides whether a quit may happen now.
type Guard struct {
	// Enabled reads the setting. Nil means on.
	Enabled func() bool
	// Emit sends an event to the window. It may be nil.
	Emit func(event string, payload any)
	// Show brings the window forward, so a hidden Hadron can show the hold or the question. It
	// may be nil.
	Show func()
	// Quit quits the app for real (Wails' runtime.Quit), which asks BeforeClose again.
	Quit func()
	// SystemQuitting reports that macOS is logging out, restarting or shutting down. It may be nil.
	SystemQuitting func() bool
	// Hold is how long ⌘Q has to stay down; zero means HoldFor.
	Hold time.Duration

	allow   atomic.Bool
	mu      sync.Mutex
	holding bool
	gen     int
}

func (g *Guard) enabled() bool { return g.Enabled == nil || g.Enabled() }

func (g *Guard) hold() time.Duration {
	if g.Hold > 0 {
		return g.Hold
	}
	return HoldFor
}

func (g *Guard) emit(event string, payload any) {
	if g.Emit != nil {
		g.Emit(event, payload)
	}
}

func (g *Guard) show() {
	if g.Show != nil {
		g.Show()
	}
}

// Allow lets the next quit through without asking, for the update installer and a confirmed quit.
func (g *Guard) Allow() { g.allow.Store(true) }

// QuitNow quits past the guard.
func (g *Guard) QuitNow() {
	g.Allow()
	if g.Quit != nil {
		g.Quit()
	}
}

// passes says whether a quit goes through without asking.
func (g *Guard) passes() bool {
	return g.allow.Load() || !g.enabled() || (g.SystemQuitting != nil && g.SystemQuitting())
}

// BeforeClose is Wails' OnBeforeClose: true keeps Hadron running. ⌘Q never reaches it while the
// guard is on (KeyDown takes the key first), so a quit that arrives here came from the app menu,
// the Dock or another app: the window asks.
func (g *Guard) BeforeClose() bool {
	if g.passes() {
		return false
	}
	go func() {
		g.show()
		g.emit(EventConfirm, nil)
	}()
	return true
}

// KeyDown is ⌘Q going down (and its key repeats). It returns true when the guard takes the key,
// so the menu never sees it: the hold starts, and Hadron quits if the keys are still down when it
// ends. It returns false when the quit passes anyway, and the menu quits as usual.
func (g *Guard) KeyDown() bool {
	if g.passes() {
		return false
	}
	g.mu.Lock()
	if g.holding {
		g.mu.Unlock()
		return true
	}
	g.holding = true
	g.gen++
	gen := g.gen
	g.mu.Unlock()
	d := g.hold()
	go func() {
		g.show()
		g.emit(EventHold, HoldEvent{State: HoldStart, Ms: int(d / time.Millisecond)})
	}()
	time.AfterFunc(d, func() {
		g.mu.Lock()
		done := g.holding && g.gen == gen
		if done {
			g.holding = false
		}
		g.mu.Unlock()
		if done {
			g.emit(EventHold, HoldEvent{State: HoldDone, Ms: int(d / time.Millisecond)})
			g.QuitNow()
		}
	})
	return true
}

// KeyUp is ⌘ or Q being let go of. Before the hold ends it cancels the quit.
func (g *Guard) KeyUp() {
	g.mu.Lock()
	was := g.holding
	g.holding = false
	g.gen++
	g.mu.Unlock()
	if was {
		g.emit(EventHold, HoldEvent{State: HoldCancel, Ms: int(g.hold() / time.Millisecond)})
	}
}

// Holding says whether ⌘Q is being held now.
func (g *Guard) Holding() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.holding
}
