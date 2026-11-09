package app

import (
	"encoding/json"
	"strings"

	"github.com/GabiHert/hadron/internal/actions"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// EventMenuCommand asks the frontend to run a keyboard-registry command (frontend/src/keys.ts) by
// its id, so a menu click and its accelerator do exactly what the key press does.
const EventMenuCommand = "menu:command"

// menuCmd is one item of the View, Go and Card menus. Chord is the command's default ⌘ chord in
// keys.ts spelling ("Shift+Mod+P"), or "" when its keys are single letters, which a menu must not
// take (they would stop typing). menu_test.go checks this table against keys.ts.
type menuCmd struct {
	ID, Title, Menu, Chord string
	// Sep puts a separator before the item.
	Sep bool
}

var menuCommands = []menuCmd{
	{ID: "palette.toggle", Title: "Command Palette…", Menu: "View", Chord: "Mod+K"},
	{ID: "help.keys", Title: "Keyboard Shortcuts", Menu: "View", Chord: "Mod+/"},
	{ID: "zoom.in", Title: "Zoom In", Menu: "View", Chord: "Mod+=", Sep: true},
	{ID: "zoom.out", Title: "Zoom Out", Menu: "View", Chord: "Mod+-"},
	{ID: "zoom.reset", Title: "Actual Size", Menu: "View", Chord: "Mod+0"},
	{ID: "panel.planner.toggle", Title: "Collapse or Expand the Planner", Menu: "View", Chord: "Alt+Mod+ArrowLeft", Sep: true},
	{ID: "panel.card.toggle", Title: "Collapse or Expand the Card Panel", Menu: "View", Chord: "Alt+Mod+ArrowRight"},
	{ID: "view.terminal", Title: "Chat ⇄ Terminal", Menu: "View", Chord: "Mod+J"},
	{ID: "view.full", Title: "Full Screen Panel", Menu: "View", Chord: "Shift+Mod+F"},
	{ID: "view.dashboard", Title: "Board ⇄ Dashboard", Menu: "View", Chord: "Mod+D"},
	{ID: "theme.toggle", Title: "Switch Light or Dark", Menu: "View", Sep: true},

	{ID: "zone.next", Title: "Next Zone", Menu: "Go", Chord: "Mod+]"},
	{ID: "zone.prev", Title: "Previous Zone", Menu: "Go", Chord: "Mod+["},
	{ID: "focus.planner", Title: "Planner", Menu: "Go", Chord: "Shift+Mod+P", Sep: true},
	{ID: "focus.board", Title: "Board", Menu: "Go", Chord: "Shift+Mod+B"},
	{ID: "focus.card", Title: "Card Panel", Menu: "Go", Chord: "Shift+Mod+C"},
	{ID: "focus.workspaces", Title: "Workspace Bar", Menu: "Go", Chord: "Shift+Mod+W"},
	{ID: "terminal.leave", Title: "Leave the Terminal", Menu: "Go", Chord: "Mod+Escape"},
	{ID: "workspace.1", Title: "Workspace 1", Menu: "Go", Chord: "Mod+1", Sep: true},
	{ID: "workspace.2", Title: "Workspace 2", Menu: "Go", Chord: "Mod+2"},
	{ID: "workspace.3", Title: "Workspace 3", Menu: "Go", Chord: "Mod+3"},
	{ID: "workspace.4", Title: "Workspace 4", Menu: "Go", Chord: "Mod+4"},
	{ID: "workspace.5", Title: "Workspace 5", Menu: "Go", Chord: "Mod+5"},
	{ID: "workspace.6", Title: "Workspace 6", Menu: "Go", Chord: "Mod+6"},
	{ID: "workspace.7", Title: "Workspace 7", Menu: "Go", Chord: "Mod+7"},
	{ID: "workspace.8", Title: "Workspace 8", Menu: "Go", Chord: "Mod+8"},
	{ID: "workspace.9", Title: "Workspace 9", Menu: "Go", Chord: "Mod+9"},
	{ID: "settings.open", Title: "Settings…", Menu: "Go", Chord: "Mod+,", Sep: true},

	{ID: "card.new", Title: "New Session…", Menu: "Card", Chord: "Mod+N"},
	{ID: "card.start", Title: "Start Session", Menu: "Card", Sep: true},
	{ID: "card.stop", Title: "Stop Session…", Menu: "Card"},
	{ID: "card.restart", Title: "Restart Session…", Menu: "Card"},
	{ID: "card.edit", Title: "Edit Card", Menu: "Card", Sep: true},
	{ID: "card.model", Title: "Change Model", Menu: "Card"},
	{ID: "card.archive", Title: "Archive Card…", Menu: "Card"},
}

// buildMenu is the standard Mac menu (the app menu, Edit and Window), View, Go and Card with the
// keyboard registry's commands, and Help, whose "What's New in Hadron" opens the changelog window.
// bindings are the user's rebindings (the keys.bindings setting), so the accelerators match.
func buildMenu(a *App) *menu.Menu {
	bindings := map[string][]string{}
	if a != nil && a.st != nil {
		raw, err := a.st.Settings.Get(actions.SettingKeyBindings)
		if err == nil {
			_ = json.Unmarshal(raw, &bindings)
		}
		a.mu.Lock()
		a.menuBindings = string(raw)
		a.mu.Unlock()
	}
	m := menu.NewMenu()
	m.Append(menu.AppMenu())
	m.Append(menu.EditMenu())
	subs := map[string]*menu.Menu{}
	for _, name := range []string{"View", "Go", "Card"} {
		subs[name] = m.AddSubmenu(name)
	}
	for _, c := range menuCommands {
		sub := subs[c.Menu]
		if c.Sep && len(sub.Items) > 0 {
			sub.AddSeparator()
		}
		id := c.ID
		sub.AddText(c.Title, accelerator(menuChord(c, bindings)), func(*menu.CallbackData) { a.emit(EventMenuCommand, id) })
	}
	m.Append(menu.WindowMenu())
	help := m.AddSubmenu("Help")
	help.AddText("What's New in Hadron", nil, func(*menu.CallbackData) { a.emit(EventWhatsNew, nil) })
	return m
}

// menuChord is the chord a menu item shows: the user's first ⌘ chord for the command when they
// rebound it, else the default.
func menuChord(c menuCmd, bindings map[string][]string) string {
	list, ok := bindings[c.ID]
	if !ok {
		return c.Chord
	}
	for _, k := range list {
		if strings.Contains(k, "Mod+") {
			return k
		}
	}
	return ""
}

var menuKeyName = map[string]string{
	"ArrowLeft": "left", "ArrowRight": "right", "ArrowUp": "up", "ArrowDown": "down", "Escape": "escape", "Enter": "return",
	"Space": "space", "Tab": "tab", "Backspace": "backspace", "Delete": "delete", "Home": "home", "End": "end", "PageUp": "page up", "PageDown": "page down",
}

// accelerator turns a keys.ts chord into a Wails accelerator, or nil when the chord has no ⌘ (a
// menu item never takes a plain key).
func accelerator(chord string) *keys.Accelerator {
	if chord == "" || !strings.Contains(chord, "Mod+") {
		return nil
	}
	key := chord[strings.LastIndex(chord, "+")+1:]
	mods := chord[:strings.LastIndex(chord, "+")]
	if strings.HasSuffix(chord, "++") {
		key, mods = "+", strings.TrimSuffix(chord, "++")
	}
	if n, ok := menuKeyName[key]; ok {
		key = n
	} else {
		key = strings.ToLower(key)
	}
	acc := &keys.Accelerator{Key: key}
	for _, p := range strings.Split(mods, "+") {
		switch p {
		case "Mod":
			acc.Modifiers = append(acc.Modifiers, keys.CmdOrCtrlKey)
		case "Shift":
			acc.Modifiers = append(acc.Modifiers, keys.ShiftKey)
		case "Alt":
			acc.Modifiers = append(acc.Modifiers, keys.OptionOrAltKey)
		case "Ctrl":
			acc.Modifiers = append(acc.Modifiers, keys.ControlKey)
		}
	}
	return acc
}

// refreshMenu rebuilds the menus when the user's key bindings changed, so the accelerators follow them.
func (a *App) refreshMenu() {
	if a.st == nil {
		return
	}
	raw, _ := a.st.Settings.Get(actions.SettingKeyBindings)
	a.mu.Lock()
	same := string(raw) == a.menuBindings
	a.mu.Unlock()
	if same {
		return
	}
	if c := a.context(); c != nil {
		wruntime.MenuSetApplicationMenu(c, buildMenu(a))
		wruntime.MenuUpdateApplicationMenu(c)
	}
}
