package actions

import (
	"strings"
	"testing"
)

func (h *harness) setMode(mode string) {
	h.t.Helper()
	h.do("settings_update", m{"values": m{SettingUserActions: mode}}, nil)
}

func TestUserOnlyActionsFollowTheMcpUserActionsSetting(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("Acme")
	actors := map[string]Actor{
		"you":      You,
		"external": {Kind: KindExternal},
		"planner":  {Kind: KindPlanner, WorkspaceID: w.ID},
		"session":  {Kind: KindSession, Name: "c1", WorkspaceID: w.ID, CardID: "c1"},
	}
	// mode -> actor -> allowed
	matrix := map[string]map[string]bool{
		"external": {"you": true, "external": true, "planner": false, "session": false},
		"all":      {"you": true, "external": true, "planner": true, "session": true},
		"off":      {"you": true, "external": false, "planner": false, "session": false},
	}
	for mode, row := range matrix {
		h.setMode(mode)
		for name, want := range row {
			err := h.as(actors[name], "planner_policy_set", m{"policy": "decisions_only"}, nil)
			if (err == nil) != want {
				t.Errorf("mode %s, %s: planner_policy_set err = %v, want allowed=%v", mode, name, err, want)
			}
			if err != nil && !strings.Contains(err.Error(), SettingUserActions) {
				t.Errorf("mode %s, %s: the refusal should name the setting: %v", mode, name, err)
			}
			err = h.as(actors[name], "settings_update", m{"values": m{"widgets.scripts": false}}, nil)
			if (err == nil) != want {
				t.Errorf("mode %s, %s: widgets.scripts err = %v, want allowed=%v", mode, name, err, want)
			}
			err = h.as(actors[name], "models_set", m{"models": []m{{"id": "opus", "label": "Opus"}}}, nil)
			if !want && (err == nil || !strings.Contains(err.Error(), SettingUserActions)) {
				t.Errorf("mode %s, %s: models_set should be refused naming the setting, got %v", mode, name, err)
			}
			if want && err != nil && strings.Contains(err.Error(), SettingUserActions) {
				t.Errorf("mode %s, %s: models_set was gated: %v", mode, name, err)
			}
		}
	}
}

func TestUnsetSettingMeansExternal(t *testing.T) {
	h := newHarness(t)
	if err := h.as(Actor{Kind: KindExternal}, "planner_policy_set", m{"policy": "user_only"}, nil); err != nil {
		t.Fatalf("external by default: %v", err)
	}
	w := h.workspace("Acme")
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: w.ID}, "planner_policy_set", m{"policy": "allow_all"}, nil); err == nil {
		t.Fatal("a planner must not widen its own permissions by default")
	}
}

func TestOnlyAllowedActorsCanChangeTheSettingItself(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("Acme")
	ext := Actor{Kind: KindExternal}
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	session := Actor{Kind: KindSession, Name: "c1", WorkspaceID: w.ID, CardID: "c1"}
	set := func(a Actor, v string) error {
		return h.as(a, "settings_update", m{"values": m{SettingUserActions: v}}, nil)
	}
	if err := set(planner, "all"); err == nil {
		t.Fatal("a planner must not grant itself user-only actions")
	}
	if err := set(session, "all"); err == nil {
		t.Fatal("a session must not grant itself user-only actions")
	}
	if err := set(You, "bogus"); err == nil || !strings.Contains(err.Error(), "external, all, off") {
		t.Fatalf("an unknown value is refused with the choices: %v", err)
	}
	if err := set(ext, "all"); err != nil {
		t.Fatalf("external may change it while it is not off: %v", err)
	}
	if err := set(planner, "external"); err != nil {
		t.Fatalf("with all, a planner may change it: %v", err)
	}
	if err := set(You, "off"); err != nil {
		t.Fatal(err)
	}
	if err := set(ext, "all"); err == nil {
		t.Fatal("with off, external may not change it")
	}
	if err := set(planner, "all"); err == nil {
		t.Fatal("with off, a planner may not change it")
	}
	if err := set(You, "all"); err != nil {
		t.Fatalf("the UI always may: %v", err)
	}
}

func TestAUserOnlyCallOverMcpIsInTheConfigHistoryWithTheActor(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("Acme")
	if err := h.as(Actor{Kind: KindExternal}, "planner_policy_set", m{"policy": "decisions_only"}, nil); err != nil {
		t.Fatal(err)
	}
	h.setMode("all")
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: w.ID}, "planner_policy_set", m{"policy": "user_only"}, nil); err != nil {
		t.Fatal(err)
	}
	// The UI's own call adds no user-only entry.
	h.do("planner_policy_set", m{"policy": "allow_all"}, nil)

	got := map[string]string{}
	for _, e := range historyIDs(h, m{"limit": 100}) {
		if e.Action == "planner_policy_set" {
			if _, dup := got[e.Actor]; dup {
				t.Errorf("two history rows for %s: the UI shows none and each MCP call shows one", e.Actor)
			}
			got[e.Actor] = e.Summary
		}
	}
	if _, ok := got["you"]; ok {
		t.Error("the UI's own call needs no user-only note")
	}
	for _, who := range []string{"external", "planner"} {
		if s, ok := got[who]; !ok || !strings.Contains(s, "used over MCP by "+who) || !strings.Contains(s, "planner_policy_set") {
			t.Errorf("history for %s = %q", who, s)
		}
	}
}

func TestPlannerKeysPressesKeysInThePlannerLikeSessionKeys(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	h.do("workspace_switch", m{"workspace_id": w.ID}, nil)
	h.fails("planner_keys", m{"keys": []string{"Enter"}}, "no session yet", "planner_start")
	h.do("planner_start", m{}, nil)
	waitUntil(t, "the planner to start", func() bool { return h.file(".started") != "" })

	// planner_send without Enter types the text; the line is not submitted yet.
	h.do("planner_send", m{"text": "approve it", "enter": false}, nil)
	h.do("planner_send", m{"text": "typed and submitted"}, nil)
	waitUntil(t, "planner_send submits with Enter", func() bool { return strings.Contains(h.file(".in"), "approve ittyped and submitted\n") })
	h.do("planner_keys", m{"keys": []string{"Enter"}}, nil)
	h.do("planner_send", m{"text": "second", "enter": false}, nil)
	h.do("planner_keys", m{"keys": []string{"Enter"}}, nil)
	waitUntil(t, "Enter pressed by planner_keys", func() bool { return strings.Contains(h.file(".in"), "second\n") })
	h.fails("planner_keys", m{"keys": []string{"Hyper-Z"}}, "unknown key")

	// The same gate: external is fine, the planner itself is not unless the setting is all.
	if err := h.as(Actor{Kind: KindExternal}, "planner_keys", m{"keys": []string{"Escape"}}, nil); err != nil {
		t.Fatalf("external: %v", err)
	}
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	if err := h.as(planner, "planner_keys", m{"keys": []string{"Escape"}}, nil); err == nil {
		t.Fatal("the planner may not press keys in itself by default")
	}
	h.setMode("all")
	if err := h.as(planner, "planner_keys", m{"keys": []string{"Escape"}}, nil); err != nil {
		t.Fatalf("with all the planner may: %v", err)
	}
	h.setMode("off")
	if err := h.as(planner, "planner_keys", m{"keys": []string{"Escape"}}, nil); err == nil {
		t.Fatal("with off the planner may not")
	}
}
