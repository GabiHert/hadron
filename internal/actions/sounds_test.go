package actions

import (
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/sounds"
)

// soundWatcher listens to the harness's store with a fake player and a clock the test moves.
func soundWatcher(h *harness, now *time.Time, plays *[]string) *sounds.Watcher {
	st := h.reg.Deps().Store
	w := &sounds.Watcher{
		Snapshot: func() (map[string]sounds.CardState, error) { return SoundSnapshot(st) },
		Prefs:    func() sounds.Prefs { return SoundPrefs(st) },
		Play:     func(kind string, _ float64) { *plays = append(*plays, kind) },
		Quiet:    time.Second,
		Now:      func() time.Time { return *now },
	}
	if err := w.Seed(); err != nil {
		h.t.Fatal(err)
	}
	return w
}

func TestSoundsFollowAQuestionRoundTripAndManualMoves(t *testing.T) {
	h := newQHarness(t)
	now := time.Unix(1000, 0)
	var plays []string
	w := soundWatcher(h.harness, &now, &plays)
	step := func(want ...string) {
		t.Helper()
		now = now.Add(5 * time.Second)
		w.Check()
		if len(plays) != len(want) {
			t.Fatalf("plays = %v, want %v", plays, want)
		}
		for i := range want {
			if plays[i] != want[i] {
				t.Fatalf("plays = %v, want %v", plays, want)
			}
		}
	}
	step() // nothing moved since the seed

	q := h.ask(decision) // the built-in rule moves the card to needs-you
	step(sounds.NeedsYou)
	if err := h.as(h.plannerActor(), "question_answer", m{"question_id": q.ID, "selections": []string{"Nearest"}}, nil); err != nil {
		t.Fatal(err)
	}
	step(sounds.NeedsYou, sounds.BackToWork) // and back when it is answered

	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.needs.ID}, nil)
	step(sounds.NeedsYou, sounds.BackToWork, sounds.NeedsYou)
	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.done.ID}, nil)
	step(sounds.NeedsYou, sounds.BackToWork, sounds.NeedsYou, sounds.BackToWork)

	// A move inside other columns, or a refetch, is silent.
	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.working.ID}, nil)
	step(sounds.NeedsYou, sounds.BackToWork, sounds.NeedsYou, sounds.BackToWork)

	// Muting the workspace, then switching sounds off, silences it.
	h.do("settings_update", m{"values": m{SettingSoundsMuted: []string{h.ws.ID}}}, nil)
	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.needs.ID}, nil)
	step(sounds.NeedsYou, sounds.BackToWork, sounds.NeedsYou, sounds.BackToWork)
	h.do("settings_update", m{"values": m{SettingSoundsMuted: []string{}, SettingSoundsEnabled: false}}, nil)
	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.working.ID}, nil)
	step(sounds.NeedsYou, sounds.BackToWork, sounds.NeedsYou, sounds.BackToWork)
}

func TestSoundSettingsAreValidatedAndPreviewPlays(t *testing.T) {
	h := newHarness(t)
	p := SoundPrefs(h.reg.Deps().Store)
	if !p.Enabled || p.Volume != DefaultSoundVolume {
		t.Fatalf("defaults = %+v, want on at %v", p, DefaultSoundVolume)
	}
	h.fails("settings_update", m{"values": m{SettingSoundsVolume: 1.5}}, "from 0 (silent) to 1")
	h.fails("settings_update", m{"values": m{SettingSoundsEnabled: "yes"}}, "true or false")
	h.fails("settings_update", m{"values": m{SettingSoundsMuted: "w1"}}, "list of workspace ids")
	h.do("settings_update", m{"values": m{SettingSoundsVolume: 0.3}}, nil)
	if v := SoundPrefs(h.reg.Deps().Store).Volume; v != 0.3 {
		t.Fatalf("volume = %v", v)
	}

	h.fails("sound_preview", m{"sound": "needs_you"}, "cannot play sounds")
	var got []string
	h.reg.deps.PlaySound = func(kind string, vol float64) error { got = append(got, kind); return nil }
	var out SoundPreviewOut
	h.do("sound_preview", m{"sound": "back_to_work"}, &out)
	if out.Played != "back_to_work" || out.Volume != 0.3 || len(got) != 1 {
		t.Fatalf("preview = %+v, plays %v", out, got)
	}
	h.fails("sound_preview", m{"sound": "beep"}, "does not exist")
	h.fails("sound_preview", m{"sound": "needs_you", "volume": 2}, "from 0 to 1")
}
