package notifications

import (
	"sync"
	"time"
)

// Watcher notices cards arriving in a needs-you column. Call Seed once at startup, then Changed
// whenever cards may have moved; it looks a moment later, so a burst of changes is one look.
type Watcher struct {
	// Snapshot reads every card: id to whether its column has the needs-you role.
	Snapshot func() (map[string]bool, error)
	// Entered is called for each card that arrived there.
	Entered  func(cardID string)
	Debounce time.Duration

	mu     sync.Mutex
	known  map[string]bool
	seeded bool
	timer  *time.Timer
}

// Seed takes the board as it is, so nothing notifies at startup.
func (w *Watcher) Seed() error {
	snap, err := w.Snapshot()
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.known, w.seeded = snap, true
	w.mu.Unlock()
	return nil
}

// Changed schedules a look at the board.
func (w *Watcher) Changed() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.timer != nil {
		return
	}
	w.timer = time.AfterFunc(w.Debounce, func() {
		w.mu.Lock()
		w.timer = nil
		w.mu.Unlock()
		w.Check()
	})
}

// Check compares the board with the last look and reports the cards that arrived.
func (w *Watcher) Check() {
	snap, err := w.Snapshot()
	if err != nil {
		return
	}
	w.mu.Lock()
	if !w.seeded {
		w.known, w.seeded = snap, true
		w.mu.Unlock()
		return
	}
	in := Entered(w.known, snap)
	w.known = snap
	w.mu.Unlock()
	for _, id := range in {
		w.Entered(id)
	}
}
