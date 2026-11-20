package actions

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/GabiHert/hadron/internal/testwait"
)

func TestAppQuitIsTheUsersOnly(t *testing.T) {
	deps, _ := testDeps(t)
	var quits atomic.Int32
	deps.Quit = func() { quits.Add(1) }
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &harness{t: t, reg: r}
	for _, who := range []Actor{{Kind: KindSession, Name: "alpha"}, {Kind: KindPlanner}, {Kind: KindExternal}} {
		if err := h.as(who, "app_quit", m{}, nil); err == nil || !strings.Contains(err.Error(), "only the user") {
			t.Fatalf("%s quit Hadron: %v", who.Kind, err)
		}
	}
	var out AppQuitOut
	h.do("app_quit", m{}, &out)
	if !out.Quitting {
		t.Fatal("the user's quit was not done")
	}
	testwait.Until(t, "Quit to be called", func() bool { return quits.Load() == 1 })
}

func TestQuitGuardIsOnUnlessTurnedOff(t *testing.T) {
	h := newHarness(t)
	st := h.reg.deps.Store
	if !QuitGuardOn(st) {
		t.Fatal("the guard should be on by default")
	}
	h.do("settings_update", m{"values": m{"app.quit_guard": false}}, nil)
	if QuitGuardOn(st) {
		t.Fatal("the guard should be off")
	}
	h.do("settings_update", m{"values": m{"app.quit_guard": true}}, nil)
	if !QuitGuardOn(st) {
		t.Fatal("the guard should be on again")
	}
}
