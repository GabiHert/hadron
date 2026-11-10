package actions

import "testing"

func TestChipColorsSetListAutoAndValidate(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("Acme")
	var out ChipColorListOut
	h.do("chip_color_list", m{"workspace_id": w.ID}, &out)
	if len(out.Palette) != 10 || len(out.Rules) != 0 || len(out.Plain) != 0 {
		t.Fatalf("empty = %+v", out)
	}
	h.do("chip_color_set", m{"workspace_id": w.ID, "path": "card.stage", "value": " review ", "color": "Blue"}, &out)
	h.do("chip_color_set", m{"workspace_id": w.ID, "path": "card.key", "value": "acme-508", "color": "purple"}, &out)
	h.do("chip_color_set", m{"workspace_id": w.ID, "path": "card.model", "value": "opus", "color": "teal"}, &out)
	want := []ChipColorRule{{"card.key", "ACME", "purple"}, {"card.model", "opus", "teal"}, {"card.stage", "review", "blue"}}
	if len(out.Rules) != 3 {
		t.Fatalf("rules = %+v", out.Rules)
	}
	for i, r := range want {
		if out.Rules[i] != r {
			t.Errorf("rule %d = %+v, want %+v", i, out.Rules[i], r)
		}
	}
	// Removing a colour and switching automatic colours off and on again.
	h.do("chip_color_set", m{"workspace_id": w.ID, "path": "card.model", "value": "opus", "color": ""}, &out)
	h.do("chip_color_auto_set", m{"workspace_id": w.ID, "path": "card.stage", "auto": false}, &out)
	h.do("chip_color_auto_set", m{"workspace_id": w.ID, "path": "card.stage", "auto": false}, &out)
	if len(out.Rules) != 2 || len(out.Plain) != 1 || out.Plain[0] != "card.stage" {
		t.Fatalf("after remove and plain = %+v", out)
	}
	h.do("chip_color_auto_set", m{"workspace_id": w.ID, "path": "card.stage", "auto": true}, &out)
	if len(out.Plain) != 0 {
		t.Fatalf("plain = %v", out.Plain)
	}
	// Per workspace.
	other := h.workspace("Beta")
	h.do("chip_color_list", m{"workspace_id": other.ID}, &out)
	if len(out.Rules) != 0 {
		t.Fatalf("another workspace sees the colours: %+v", out.Rules)
	}

	h.fails("chip_color_set", m{"workspace_id": w.ID, "path": "card.stage", "value": "x", "color": "#ff0000"}, "not in the palette")
	h.fails("chip_color_set", m{"workspace_id": w.ID, "path": "jira.status", "value": "x", "color": "red"}, "not a field")
	h.fails("chip_color_set", m{"workspace_id": w.ID, "path": "card.stage", "value": " ", "color": "red"}, "1 to 200")
	h.do("custom_field_create", m{"workspace_id": w.ID, "name": "team", "type": "text"}, nil)
	h.do("chip_color_set", m{"workspace_id": w.ID, "path": "custom.team", "value": "Platform", "color": "green"}, nil)
	if err := h.as(Actor{Kind: KindSession, Name: "x", WorkspaceID: w.ID}, "chip_color_set", m{"path": "card.stage", "value": "a", "color": "red"}, nil); err == nil {
		t.Fatal("a session changed tag colours")
	}
}

func TestChipKeyPrefix(t *testing.T) {
	for in, want := range map[string]string{"ACME-508": "ACME", "acme": "ACME", "PROJ-CORE-12": "PROJ-CORE", " beta-1 ": "BETA"} {
		if got := ChipKeyPrefix(in); got != want {
			t.Errorf("ChipKeyPrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
