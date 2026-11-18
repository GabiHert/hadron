package actions

import (
	"strings"
	"testing"
)

func TestTutorialShowAsksTheWindow(t *testing.T) {
	h := newHarness(t)
	before := h.rec.count()
	h.do("tutorial_show", nil, nil)
	h.rec.mu.Lock()
	got := h.rec.got[before:]
	h.rec.mu.Unlock()
	found := false
	for _, e := range got {
		found = found || e.event == EventTutorial
	}
	if !found {
		t.Fatalf("no %s event in %+v", EventTutorial, got)
	}
	err := h.as(Actor{Kind: KindSession, Name: "alpha"}, "tutorial_show", m{}, nil)
	if err == nil || !strings.Contains(err.Error(), "card session cannot") {
		t.Errorf("card session: %v", err)
	}
}

func TestTutorialShowStartsAtAChapter(t *testing.T) {
	h := newHarness(t)
	var out TutorialEvent
	h.do("tutorial_show", m{"chapter": "mcp"}, &out)
	if out.Chapter != "mcp" {
		t.Errorf("out = %+v", out)
	}
	h.rec.mu.Lock()
	last := h.rec.got[len(h.rec.got)-1]
	h.rec.mu.Unlock()
	if ev, ok := last.payload.(TutorialEvent); last.event != EventTutorial || !ok || ev.Chapter != "mcp" {
		t.Errorf("event = %+v", last)
	}
	h.fails("tutorial_show", m{"chapter": "advanced"}, "must be one of basics, planner, automate, mcp")
}

func TestSoundPlayFollowsTheSoundsSettings(t *testing.T) {
	deps, rec := testDeps(t)
	var played []string
	var vols []float64
	deps.PlaySound = func(kind string, v float64) error { played = append(played, kind); vols = append(vols, v); return nil }
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &harness{t: t, reg: r, rec: rec}
	var out SoundPlayOut
	h.do("sound_play", m{"sound": "tour_glide"}, &out)
	if !out.Played || len(played) != 1 || played[0] != "tour_glide" || vols[0] != DefaultSoundVolume {
		t.Fatalf("played %v at %v: %+v", played, vols, out)
	}
	h.do("settings_update", m{"values": m{SettingSoundsVolume: 0.3}}, nil)
	h.do("sound_play", m{"sound": "tour_chime"}, &out)
	if vols[len(vols)-1] != 0.3 {
		t.Errorf("volume = %v", vols)
	}
	h.do("settings_update", m{"values": m{SettingSoundsEnabled: false}}, nil)
	h.do("sound_play", m{"sound": "tour_tick"}, &out)
	if out.Played || len(played) != 2 {
		t.Errorf("sounds off still played: %v", played)
	}
	h.fails("sound_play", m{"sound": "trumpet"}, "does not exist")
}
