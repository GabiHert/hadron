package actions

import (
	"strings"
	"testing"
)

func TestPanelWidthsAreCheckedAndCanBeReset(t *testing.T) {
	h := newHarness(t)
	h.do("settings_update", m{"values": m{"ui.panel_width.planner": 500, "ui.panel_width.card": 700}}, nil)
	h.do("settings_update", m{"values": m{"ui.panel_width.card": nil}}, nil)
	for _, bad := range []any{100, 5000, "wide", true} {
		if err := h.try("settings_update", m{"values": m{"ui.panel_width.planner": bad}}, nil); err == nil || !strings.Contains(err.Error(), "from 260 to 720") {
			t.Fatalf("planner width %v: %v", bad, err)
		}
	}
	if err := h.try("settings_update", m{"values": m{"ui.panel_width.card": 200}}, nil); err == nil || !strings.Contains(err.Error(), "from 320 to 960") {
		t.Fatalf("card width 200: %v", err)
	}
}
