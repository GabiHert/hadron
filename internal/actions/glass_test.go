package actions

import (
	"strings"
	"testing"
)

func TestGlassTransparencyIsASettingReadBackByUIDisplay(t *testing.T) {
	h := newHarness(t)
	var d UIDisplayOut
	h.do("ui_display_get", m{}, &d)
	if d.GlassTransparency != 60 || d.GlassScale != 0.64 {
		t.Fatalf("default: transparency %v scale %v, want 60 and 0.64", d.GlassTransparency, d.GlassScale)
	}
	h.do("settings_update", m{"values": m{SettingGlassTransparency: 100}}, nil)
	h.do("ui_display_get", m{}, &d)
	if d.GlassTransparency != 100 || d.GlassScale != 0.4 {
		t.Fatalf("at 100: transparency %v scale %v", d.GlassTransparency, d.GlassScale)
	}
	h.do("settings_update", m{"values": m{SettingGlassTransparency: 0}}, nil)
	h.do("ui_display_get", m{}, &d)
	if d.GlassScale != 1 {
		t.Fatalf("at 0 the glass is the theme's own: scale %v", d.GlassScale)
	}
	for _, bad := range []any{-1, 101, "60", true} {
		if err := h.try("settings_update", m{"values": m{SettingGlassTransparency: bad}}, nil); err == nil || !strings.Contains(err.Error(), "0 (the theme's own glass) to 100") {
			t.Fatalf("%v was accepted: %v", bad, err)
		}
	}
}
