package sounds

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

func st(ws string, needs bool) CardState { return CardState{WorkspaceID: ws, NeedsYou: needs} }

func TestDiffCountsEntriesAndExitsOnly(t *testing.T) {
	prev := map[string]CardState{"a": st("w1", false), "b": st("w1", true), "c": st("w2", true), "gone": st("w1", true), "same": st("w1", true)}
	cur := map[string]CardState{"a": st("w1", true), "b": st("w1", false), "c": st("w2", false), "new": st("w1", true), "fresh": st("w1", false), "same": st("w1", true)}
	if e, l := Diff(prev, cur, nil); e != 2 || l != 2 {
		t.Fatalf("entered %d left %d, want 2 and 2 (a and new in; b and c out; gone, fresh and same are nothing)", e, l)
	}
	if e, l := Diff(prev, cur, map[string]bool{"w2": true}); e != 2 || l != 1 {
		t.Fatalf("with w2 muted: entered %d left %d, want 2 and 1", e, l)
	}
}

// board is a fake board the watcher reads.
type board struct {
	mu    sync.Mutex
	cards map[string]CardState
	plays []string
	prefs Prefs
}

func (b *board) set(id string, s CardState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cards[id] = s
}

func (b *board) watcher(now *time.Time) *Watcher {
	return &Watcher{
		Snapshot: func() (map[string]CardState, error) {
			b.mu.Lock()
			defer b.mu.Unlock()
			out := map[string]CardState{}
			for k, v := range b.cards {
				out[k] = v
			}
			return out, nil
		},
		Prefs: func() Prefs { return b.prefs },
		Play: func(kind string, vol float64) {
			b.mu.Lock()
			defer b.mu.Unlock()
			b.plays = append(b.plays, kind)
		},
		Debounce: 30 * time.Millisecond,
		Quiet:    time.Second,
		Now:      func() time.Time { return *now },
	}
}

func (b *board) played() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string{}, b.plays...)
}

func TestWatcherPlaysOncePerTransitionNeverAtStartupOrOnARefetch(t *testing.T) {
	b := &board{cards: map[string]CardState{"a": st("w1", true), "b": st("w1", false)}, prefs: Prefs{Enabled: true, Volume: 0.7}}
	now := time.Unix(1000, 0)
	w := b.watcher(&now)
	if err := w.Seed(); err != nil {
		t.Fatal(err)
	}
	w.Check() // a refetch with nothing moved
	if p := b.played(); len(p) != 0 {
		t.Fatalf("startup or a refetch played %v", p)
	}
	b.set("b", st("w1", true))
	w.Check()
	w.Check() // the same board again: no second sound
	if p := b.played(); len(p) != 1 || p[0] != NeedsYou {
		t.Fatalf("plays = %v, want one needs_you", p)
	}
	now = now.Add(2 * time.Second)
	b.set("a", st("w1", false))
	w.Check()
	if p := b.played(); len(p) != 2 || p[1] != BackToWork {
		t.Fatalf("plays = %v, want needs_you then back_to_work", p)
	}
}

func TestWatcherDebouncesABurstIntoOneSound(t *testing.T) {
	b := &board{cards: map[string]CardState{}, prefs: Prefs{Enabled: true, Volume: 0.5}}
	now := time.Unix(1000, 0)
	w := b.watcher(&now)
	_ = w.Seed()
	for i := 0; i < 5; i++ {
		b.set(string(rune('a'+i)), st("w1", true))
		w.Changed()
	}
	time.Sleep(120 * time.Millisecond)
	if p := b.played(); len(p) != 1 || p[0] != NeedsYou {
		t.Fatalf("5 cards at once played %v, want one needs_you", p)
	}
	// A card that leaves right after still counts as moved, but is silent within Quiet.
	b.set("a", st("w1", false))
	w.Check()
	if p := b.played(); len(p) != 1 {
		t.Fatalf("played again within the quiet time: %v", p)
	}
	// An arrival and a departure in one look: the chime wins.
	now = now.Add(5 * time.Second)
	b.set("a", st("w1", true))
	b.set("b", st("w1", false))
	w.Check()
	if p := b.played(); len(p) != 2 || p[1] != NeedsYou {
		t.Fatalf("plays = %v", p)
	}
}

func TestWatcherFollowsTheSettings(t *testing.T) {
	b := &board{cards: map[string]CardState{"a": st("w1", false), "z": st("w2", false)}, prefs: Prefs{Enabled: false, Volume: 0.7}}
	now := time.Unix(1000, 0)
	w := b.watcher(&now)
	_ = w.Seed()
	b.set("a", st("w1", true))
	w.Check()
	b.prefs = Prefs{Enabled: true, Volume: 0.7, Muted: map[string]bool{"w2": true}}
	b.set("z", st("w2", true))
	w.Check()
	b.prefs.Volume = 0
	b.set("a", st("w1", false))
	w.Check()
	if p := b.played(); len(p) != 0 {
		t.Fatalf("off, muted or silent volume still played %v", p)
	}
	// Turning sounds back on does not replay what moved while they were off.
	b.prefs = Prefs{Enabled: true, Volume: 0.7}
	w.Check()
	if p := b.played(); len(p) != 0 {
		t.Fatalf("replayed old moves: %v", p)
	}
}

func TestWAVIsAShortValidMonoFile(t *testing.T) {
	for _, k := range Kinds {
		b := WAV(k)
		if !bytes.HasPrefix(b, []byte("RIFF")) || string(b[8:16]) != "WAVEfmt " {
			t.Fatalf("%s: not a WAV header", k)
		}
		ch := binary.LittleEndian.Uint16(b[22:24])
		sr := binary.LittleEndian.Uint32(b[24:28])
		n := binary.LittleEndian.Uint32(b[40:44])
		secs := float64(n) / 2 / float64(sr)
		if ch != 1 || sr != 44100 || secs < 0.2 || secs > 0.6 || len(b) > 60_000 {
			t.Fatalf("%s: channels %d rate %d %.2fs %d bytes", k, ch, sr, secs, len(b))
		}
	}
	if bytes.Equal(WAV(NeedsYou), WAV(BackToWork)) {
		t.Fatal("the two sounds are the same")
	}
	p := &Player{Dir: t.TempDir()}
	if _, err := p.File("beep"); err == nil {
		t.Fatal("an unknown sound has a file")
	}
	path, err := p.File(NeedsYou)
	if err != nil || path == "" {
		t.Fatal(err)
	}
}
