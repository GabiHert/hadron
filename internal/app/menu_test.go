package app

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

type registryCmd struct {
	id, menu string
	keys     []string
}

var (
	idRe    = regexp.MustCompile(`id: "([^"]+)"`)
	keysRe  = regexp.MustCompile(`keys: \[((?:"[^"]*"(?:, )?)*)\]`)
	menuRe  = regexp.MustCompile(`menu: "([A-Za-z]+)"`)
	quoteRe = regexp.MustCompile(`"([^"]*)"`)
)

// readRegistry reads the commands with a menu from frontend/src/keys.ts: one DEFAULTS entry per
// line, plus the workspace.1–9 family that the ws() helper builds.
func readRegistry(t *testing.T) map[string]registryCmd {
	t.Helper()
	b, err := os.ReadFile("../../frontend/src/keys.ts")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]registryCmd{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "const ws = ") {
			mm := menuRe.FindStringSubmatch(line)
			if mm == nil {
				t.Fatal("the ws() helper lost its menu")
			}
			for n := 1; n <= 9; n++ {
				id := "workspace." + strconv.Itoa(n)
				out[id] = registryCmd{id: id, menu: mm[1], keys: []string{"Mod+" + strconv.Itoa(n)}}
			}
			continue
		}
		im, mm := idRe.FindStringSubmatch(line), menuRe.FindStringSubmatch(line)
		if im == nil || mm == nil || !strings.HasPrefix(strings.TrimSpace(line), "{ id:") {
			continue
		}
		km := keysRe.FindStringSubmatch(line)
		if km == nil {
			t.Fatalf("cannot read the keys of %s", im[1])
		}
		var ks []string
		for _, q := range quoteRe.FindAllStringSubmatch(km[1], -1) {
			ks = append(ks, q[1])
		}
		out[im[1]] = registryCmd{id: im[1], menu: mm[1], keys: ks}
	}
	if len(out) < 20 {
		t.Fatalf("read only %d menu commands from keys.ts; did its format change?", len(out))
	}
	return out
}

// The native View, Go and Card menus carry exactly the registry's menu commands, each with its
// default ⌘ chord (single-letter keys never become accelerators).
func TestMenusMatchTheKeyboardRegistry(t *testing.T) {
	reg := readRegistry(t)
	seen := map[string]bool{}
	for _, c := range menuCommands {
		r, ok := reg[c.ID]
		if !ok {
			t.Errorf("menu item %s is not a menu command in keys.ts", c.ID)
			continue
		}
		seen[c.ID] = true
		if r.menu != c.Menu {
			t.Errorf("%s: keys.ts puts it in %s, the menu in %s", c.ID, r.menu, c.Menu)
		}
		want := ""
		for _, k := range r.keys {
			if strings.Contains(k, "Mod+") {
				want = k
				break
			}
		}
		if c.Chord != want {
			t.Errorf("%s: menu chord %q, keys.ts's first ⌘ chord is %q", c.ID, c.Chord, want)
		}
	}
	for id := range reg {
		if !seen[id] {
			t.Errorf("keys.ts puts %s in a menu, but buildMenu has no item for it", id)
		}
	}
}

func TestMenuItemsSendTheirCommandAndKeepTheStandardMenus(t *testing.T) {
	a := newTestApp(t)
	m := buildMenu(a)
	var labels []string
	var view *menu.Menu
	for _, it := range m.Items {
		labels = append(labels, it.Label)
		if it.Label == "View" {
			view = it.SubMenu
		}
	}
	got := strings.Join(labels, ",")
	if !strings.Contains(got, ",View,Go,Card,") || !strings.HasSuffix(got, "Help") {
		t.Fatalf("menus = %s", got)
	}
	var zoom *menu.MenuItem
	for _, it := range view.Items {
		if it.Label == "Zoom In" {
			zoom = it
		}
	}
	if zoom == nil || zoom.Accelerator == nil || zoom.Accelerator.Key != "=" || len(zoom.Accelerator.Modifiers) != 1 || zoom.Accelerator.Modifiers[0] != keys.CmdOrCtrlKey {
		t.Fatalf("Zoom In = %+v", zoom)
	}
	zoom.Click(&menu.CallbackData{}) // before the window exists this must be harmless
}

func TestAccelerators(t *testing.T) {
	cases := map[string]string{
		"Mod+K":              "k cmdorctrl",
		"Shift+Mod+P":        "p shift cmdorctrl",
		"Alt+Mod+ArrowLeft":  "left optionoralt cmdorctrl",
		"Mod+Escape":         "escape cmdorctrl",
		"Mod+,":              ", cmdorctrl",
		"Mod+]":              "] cmdorctrl",
		"Mod++":              "+ cmdorctrl",
		"Ctrl+Shift+Mod+Tab": "tab ctrl shift cmdorctrl",
	}
	for chord, want := range cases {
		a := accelerator(chord)
		if a == nil {
			t.Errorf("%s: no accelerator", chord)
			continue
		}
		parts := []string{a.Key}
		for _, m := range a.Modifiers {
			parts = append(parts, string(m))
		}
		if got := strings.Join(parts, " "); got != want {
			t.Errorf("%s = %q, want %q", chord, got, want)
		}
	}
	for _, plain := range []string{"S", "Backspace", "", "Ctrl+Tab"} {
		if accelerator(plain) != nil {
			t.Errorf("%q must not become a menu accelerator", plain)
		}
	}
}

func TestMenuFollowsRebinding(t *testing.T) {
	c := menuCmd{ID: "view.dashboard", Chord: "Mod+D"}
	if got := menuChord(c, nil); got != "Mod+D" {
		t.Errorf("default = %q", got)
	}
	if got := menuChord(c, map[string][]string{"view.dashboard": {"G", "Shift+Mod+D"}}); got != "Shift+Mod+D" {
		t.Errorf("rebound = %q", got)
	}
	if got := menuChord(c, map[string][]string{"view.dashboard": {}}); got != "" {
		t.Errorf("unbound = %q", got)
	}
}
