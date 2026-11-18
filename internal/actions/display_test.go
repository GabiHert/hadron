package actions

import (
	"testing"

	"github.com/GabiHert/hadron/internal/accessibility"
)

func TestUIDisplayFollowsTheSettingAndReduceTransparency(t *testing.T) {
	deps, rec := testDeps(t)
	reduce := false
	deps.Display = func() accessibility.Display { return accessibility.Display{ReduceTransparency: reduce} }
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &harness{t: t, reg: r, rec: rec}
	var out UIDisplayOut
	h.do("ui_display_get", nil, &out)
	if !out.GlassChats || out.Glass != "on" {
		t.Fatalf("default = %+v", out)
	}
	reduce = true
	h.do("ui_display_get", nil, &out)
	if out.Glass != "solid" || !out.ReduceTransparency {
		t.Errorf("reduce transparency = %+v", out)
	}
	h.do("settings_update", m{"values": m{SettingGlassChats: false}}, nil)
	h.do("ui_display_get", nil, &out)
	if out.GlassChats || out.Glass != "off" {
		t.Errorf("switched off = %+v", out)
	}
	plain := newHarness(t)
	plain.do("ui_display_get", nil, &out)
	if out.Glass != "on" {
		t.Errorf("no display reader = %+v", out)
	}
}
