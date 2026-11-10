package sounds

import (
	"sync"
	"time"
)

// CardState is where one card is: its workspace, and whether its column has the needs-you role.
type CardState struct {
	WorkspaceID string
	NeedsYou    bool
}

// Diff counts the cards that moved into a needs-you column and out of one between two snapshots
// (card id → state). A new card that starts in needs-you counts as entering; a card that is gone
// (archived or deleted) counts as nothing. Cards of muted workspaces are skipped.
func Diff(prev, cur map[string]CardState, muted map[string]bool) (entered, left int) {
	for id, c := range cur {
		if muted[c.WorkspaceID] {
			continue
		}
		p, known := prev[id]
		switch {
		case !known && c.NeedsYou:
			entered++
		case known && !p.NeedsYou && c.NeedsYou:
			entered++
		case known && p.NeedsYou && !c.NeedsYou:
			left++
		}
	}
	return entered, left
}

// Prefs are the sound settings, read when a sound is due.
type Prefs struct {
	Enabled bool
	Volume  float64
	Muted   map[string]bool
}

// Watcher turns board changes into sounds. Call Seed once at startup, then Changed whenever cards
// may have moved; it looks at the board a moment later, so a burst of changes plays one sound.
type Watcher struct {
	// Snapshot reads every card of every workspace.
	Snapshot func() (map[string]CardState, error)
	// Prefs reads the settings.
	Prefs func() Prefs
	// Play plays a sound at a volume from 0 to 1.
	Play func(kind string, volume float64)
	// Debounce is how long Changed waits before looking; Quiet is the least time between two sounds.
	Debounce, Quiet time.Duration
	// Now is the clock; tests replace it.
	Now func() time.Time

	mu       sync.Mutex
	known    map[string]CardState
	seeded   bool
	timer    *time.Timer
	lastPlay time.Time
}

// Seed takes the board as it is, silently, so nothing plays at startup.
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

// Changed schedules a look at the board. Calls within Debounce of the first one share that look.
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

// Check compares the board with the last look and plays at most one sound: the needs-you chime
// when any card arrived there, else the back-to-work note when any left. Within Quiet of the last
// sound it stays silent, but still takes the new board as known.
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
	prefs := w.Prefs()
	entered, left := Diff(w.known, snap, prefs.Muted)
	w.known = snap
	now := w.now()
	kind := ""
	switch {
	case entered > 0:
		kind = NeedsYou
	case left > 0:
		kind = BackToWork
	}
	if kind == "" || !prefs.Enabled || prefs.Volume <= 0 || (!w.lastPlay.IsZero() && now.Sub(w.lastPlay) < w.Quiet) {
		w.mu.Unlock()
		return
	}
	w.lastPlay = now
	w.mu.Unlock()
	w.Play(kind, prefs.Volume)
}

func (w *Watcher) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}
