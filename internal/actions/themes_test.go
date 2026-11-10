package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vscodeSample = `{
  // comment
  "name": "Acme Night",
  "type": "dark",
  "colors": {
    "editor.background": "#1a1b26",
    "editor.foreground": "#c0caf5",
    "button.background": "#3d59a1",
    "terminal.ansiRed": "#f7768e",
    "terminal.ansiGreen": "#9ece6a",
    "terminal.ansiBlue": "#7aa2f7",
  },
}`

func TestThemeListSetAndLegacySetting(t *testing.T) {
	h := newHarness(t)
	var out ThemeListOut
	h.do("theme_list", m{}, &out)
	if len(out.Themes) < 8 || out.Active.Theme != "system" || out.Active.Light != "classic-light" || out.Active.Dark != "classic-dark" {
		t.Fatalf("fresh list: %d themes, active %+v", len(out.Themes), out.Active)
	}
	// The older theme key still decides until ui.theme is set.
	h.do("settings_update", m{"values": m{"theme": "dark"}}, nil)
	h.do("theme_list", m{}, &out)
	if out.Active.Theme != "classic-dark" {
		t.Fatalf("legacy dark: %+v", out.Active)
	}
	var ch ThemeChoice
	h.do("theme_set", m{"theme": "system", "light": "nucleus", "dark": "deep-space"}, &ch)
	if ch != (ThemeChoice{Theme: "system", Light: "nucleus", Dark: "deep-space"}) {
		t.Fatalf("set: %+v", ch)
	}
	h.do("settings_update", m{"values": m{"ui.theme": "synthwave"}}, nil)
	h.do("theme_list", m{}, &out)
	if out.Active.Theme != "synthwave" {
		t.Fatalf("settings_update ui.theme: %+v", out.Active)
	}
	h.fails("theme_set", m{"theme": "nope"}, "no such theme")
	h.fails("settings_update", m{"values": m{"ui.theme.dark": "nope"}}, "no such theme")
	h.fails("theme_set", m{}, "pass theme, light or dark")
}

func TestThemeSaveImportExportDelete(t *testing.T) {
	h := newHarness(t)
	var saved struct {
		ID     string            `json:"id"`
		Name   string            `json:"name"`
		Mode   string            `json:"mode"`
		Tokens map[string]string `json:"tokens"`
	}
	h.do("theme_save", m{"theme": m{"name": "My Lab", "mode": "light", "tokens": m{"bg": "#ffffff", "fg": "#111111", "acc": "#0055ff"}}}, &saved)
	if saved.ID != "my-lab" || saved.Tokens["tag-blue"] == "" || saved.Tokens["border"] == "" {
		t.Fatalf("saved %+v", saved)
	}
	if _, err := os.Stat(filepath.Join(h.reg.Deps().Home, "themes", "my-lab.json")); err != nil {
		t.Fatalf("the theme is not under HADRON_HOME/themes: %v", err)
	}
	h.fails("theme_save", m{"theme": m{"id": "nucleus", "name": "Nucleus", "mode": "light", "tokens": m{}}}, "built-in")

	var imp struct {
		ID     string            `json:"id"`
		Mode   string            `json:"mode"`
		Tokens map[string]string `json:"tokens"`
	}
	h.do("theme_import", m{"json": vscodeSample}, &imp)
	if imp.ID != "acme-night" || imp.Mode != "dark" || imp.Tokens["tr"] != "#f7768e" || imp.Tokens["bg"] != "#1a1b26" {
		t.Fatalf("import %+v", imp)
	}
	file := filepath.Join(t.TempDir(), "acme.json")
	_ = os.WriteFile(file, []byte(vscodeSample), 0o600)
	h.do("theme_import", m{"path": file, "name": "Acme Two"}, &imp)
	if imp.ID != "acme-two" {
		t.Fatalf("import by path: %+v", imp)
	}
	h.fails("theme_import", m{"json": `{"name":"pkg","version":"1.0.0"}`}, "not a VS Code colour theme")
	h.fails("theme_import", m{}, "pass path or json")

	// Export, then import the export back.
	var ex ThemeExportOut
	out := filepath.Join(t.TempDir(), "x.json")
	h.do("theme_export", m{"id": "acme-night", "path": out}, &ex)
	if ex.Path != out || !strings.Contains(ex.JSON, `"mode": "dark"`) {
		t.Fatalf("export %+v", ex)
	}
	h.do("theme_import", m{"path": out, "name": "Round Trip"}, &imp)
	if imp.Tokens["tr"] != "#f7768e" {
		t.Fatalf("round trip lost tokens: %+v", imp.Tokens["tr"])
	}

	// Deleting the theme in use falls back to Classic.
	h.do("theme_set", m{"theme": "acme-night", "dark": "acme-night"}, nil)
	var ch ThemeChoice
	h.do("theme_delete", m{"id": "acme-night"}, &ch)
	if ch.Theme != "system" || ch.Dark != "classic-dark" {
		t.Fatalf("after delete: %+v", ch)
	}
	h.fails("theme_delete", m{"id": "deep-space"}, "built-in")
	h.fails("theme_delete", m{"id": "acme-night"}, "no such theme")
}
