package actions

import (
	"encoding/json"
	"testing"
)

func TestKeyBindingsAreValidatedAndStored(t *testing.T) {
	h := newHarness(t)
	h.do("settings_update", m{"values": m{"keys.bindings": m{"view.dashboard": []any{"Shift+Mod+D"}, "card.start": []any{}, "board.filter": []any{"?", "Mod+F"}}}}, nil)
	var got map[string]json.RawMessage
	h.do("settings_get", m{"key": "keys.bindings"}, &got)
	var b map[string][]string
	if err := json.Unmarshal(got["keys.bindings"], &b); err != nil {
		t.Fatal(err)
	}
	if len(b["view.dashboard"]) != 1 || b["view.dashboard"][0] != "Shift+Mod+D" || len(b["card.start"]) != 0 || len(b["board.filter"]) != 2 {
		t.Fatalf("stored %v", b)
	}
	h.fails("settings_update", m{"values": m{"keys.bindings": "Mod+D"}}, "object of command id")
	h.fails("settings_update", m{"values": m{"keys.bindings": m{"View Dashboard": []any{"Mod+D"}}}}, "not a command id")
	h.fails("settings_update", m{"values": m{"keys.bindings": m{"view.dashboard": "Mod+D"}}}, "list of chords")
	h.fails("settings_update", m{"values": m{"keys.bindings": m{"view.dashboard": []any{"cmd-d"}}}}, "not a chord")
	h.fails("settings_update", m{"values": m{"keys.bindings": m{"view.dashboard": []any{"Mod+Shift+D"}}}}, "order Ctrl, Alt, Shift, Mod")
}
