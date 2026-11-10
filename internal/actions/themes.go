package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GabiHert/hadron/internal/themes"
)

// EntityThemes is the changed-event entity of the custom themes.
const EntityThemes = "themes"

// Settings that choose the theme. SettingTheme is a theme id, or "system" to follow macOS with
// SettingThemeLight in light mode and SettingThemeDark in dark mode.
const (
	SettingTheme      = "ui.theme"
	SettingThemeLight = "ui.theme.light"
	SettingThemeDark  = "ui.theme.dark"
)

func themeStore(c *Call) themes.Store {
	if c.Deps.Home == "" {
		return themes.Store{}
	}
	return themes.Store{Dir: filepath.Join(c.Deps.Home, "themes")}
}

// ThemeChoice is which theme is in use.
type ThemeChoice struct {
	Theme string `json:"theme" jsonschema:"a theme id, or system to follow macOS"`
	Light string `json:"light" jsonschema:"the theme used in System mode while macOS is light"`
	Dark  string `json:"dark" jsonschema:"the theme used in System mode while macOS is dark"`
}

func settingString(c *Call, key string) string {
	raw, err := c.Store.Settings.Get(key)
	if err != nil {
		return ""
	}
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// themeChoice reads the settings, falling back to the older theme key (light, dark or system).
func themeChoice(c *Call) ThemeChoice {
	ch := ThemeChoice{Theme: settingString(c, SettingTheme), Light: settingString(c, SettingThemeLight), Dark: settingString(c, SettingThemeDark)}
	if ch.Theme == "" {
		switch settingString(c, "theme") {
		case "light":
			ch.Theme = themes.ClassicLight.ID
		case "dark":
			ch.Theme = themes.ClassicDark.ID
		default:
			ch.Theme = "system"
		}
	}
	if ch.Light == "" {
		ch.Light = themes.ClassicLight.ID
	}
	if ch.Dark == "" {
		ch.Dark = themes.ClassicDark.ID
	}
	return ch
}

// checkThemeSetting validates ui.theme, ui.theme.light and ui.theme.dark for settings_update.
func checkThemeSetting(c *Call, k string, v any) error {
	if k != SettingTheme && k != SettingThemeLight && k != SettingThemeDark {
		return nil
	}
	id, ok := v.(string)
	if !ok {
		return fmt.Errorf("%s must be a theme id; theme_list lists them", k)
	}
	if k == SettingTheme && id == "system" {
		return nil
	}
	if _, err := themeStore(c).Get(id); err != nil {
		return fmt.Errorf("%s: %w; theme_list lists the themes", k, err)
	}
	return nil
}

// ThemeListOut is the result of theme_list.
type ThemeListOut struct {
	Themes []themes.Theme      `json:"themes"`
	Groups []themes.TokenGroup `json:"groups"`
	Active ThemeChoice         `json:"active"`
}

// ThemeSetIn is the input of theme_set.
type ThemeSetIn struct {
	Theme string `json:"theme,omitempty" jsonschema:"a theme id to use always, or system to follow macOS"`
	Light string `json:"light,omitempty" jsonschema:"the theme for System mode while macOS is light"`
	Dark  string `json:"dark,omitempty" jsonschema:"the theme for System mode while macOS is dark"`
}

// ThemeSaveIn is the input of theme_save.
type ThemeSaveIn struct {
	Theme themes.Theme `json:"theme" jsonschema:"the theme; leave id empty to create one, or pass a custom theme's id to change it. Missing tokens are derived"`
}

// ThemeImportIn is the input of theme_import.
type ThemeImportIn struct {
	Path string `json:"path,omitempty" jsonschema:"a .json file on this Mac: a VS Code colour theme, or a theme exported from Hadron"`
	JSON string `json:"json,omitempty" jsonschema:"the file's text, instead of path"`
	Name string `json:"name,omitempty" jsonschema:"a name for the theme; defaults to the file's name field"`
}

// ThemeIDIn names a theme.
type ThemeIDIn struct {
	ID string `json:"id" jsonschema:"the theme id"`
}

// ThemeExportIn is the input of theme_export.
type ThemeExportIn struct {
	ID   string `json:"id" jsonschema:"the theme id"`
	Path string `json:"path,omitempty" jsonschema:"where to write the file; defaults to ~/Downloads/<id>.hadron-theme.json"`
}

// ThemeExportOut is the result of theme_export.
type ThemeExportOut struct {
	Path string `json:"path"`
	JSON string `json:"json"`
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// importTheme reads a Hadron theme (with mode and tokens) or a VS Code colour theme.
func importTheme(data []byte, name string) (themes.Theme, error) {
	var h struct {
		Name   string            `json:"name"`
		Mode   string            `json:"mode"`
		Tokens map[string]string `json:"tokens"`
	}
	if json.Unmarshal(themes.StripJSONC(data), &h) == nil && len(h.Tokens) > 0 && h.Mode != "" {
		t := themes.Theme{Name: h.Name, Mode: h.Mode, Tokens: h.Tokens}
		if strings.TrimSpace(name) != "" {
			t.Name = name
		}
		return t, nil
	}
	return themes.FromVSCode(data, name)
}

func registerThemes(r *Registry) {
	Register(r, Spec{Name: "theme_list", Description: "List the colour themes (built-in and custom) with every token's colour, the token groups, and which theme is in use: theme is an id or system (follow macOS, with light and dark naming the theme for each)."},
		func(ctx context.Context, c *Call, _ struct{}) (ThemeListOut, error) {
			list, err := themeStore(c).List()
			if err != nil {
				return ThemeListOut{}, err
			}
			return ThemeListOut{Themes: list, Groups: themes.Groups, Active: themeChoice(c)}, nil
		})

	Register(r, Spec{Name: "theme_set", Description: "Switch the colour theme. theme is a theme id to use always, or system to follow macOS; light and dark pick the themes System mode uses. theme_list lists the ids. Returns the choice.", Mutates: true, Entities: []string{"settings"}},
		func(ctx context.Context, c *Call, in ThemeSetIn) (ThemeChoice, error) {
			if in.Theme == "" && in.Light == "" && in.Dark == "" {
				return ThemeChoice{}, errors.New("pass theme, light or dark")
			}
			for _, kv := range [][2]string{{SettingTheme, in.Theme}, {SettingThemeLight, in.Light}, {SettingThemeDark, in.Dark}} {
				if kv[1] == "" {
					continue
				}
				if err := checkThemeSetting(c, kv[0], kv[1]); err != nil {
					return ThemeChoice{}, err
				}
				if err := c.Store.Settings.Set(kv[0], json.RawMessage(toJSON(kv[1]))); err != nil {
					return ThemeChoice{}, err
				}
			}
			return themeChoice(c), nil
		})

	Register(r, Spec{Name: "theme_save", Description: "Create or change a custom colour theme: a name, a mode (light or dark) and tokens (colour per token, #rrggbb; theme_list shows the token names). Leave id empty to create one. Missing tokens are derived from the others. Built-in themes can't be changed; save a copy.", Mutates: true, Entities: []string{EntityThemes}},
		func(ctx context.Context, c *Call, in ThemeSaveIn) (themes.Theme, error) {
			return themeStore(c).Save(in.Theme)
		})

	Register(r, Spec{Name: "theme_import", Description: "Import a colour theme from a VS Code theme .json file (colors with editor.background, terminal.ansi* and so on; comments and trailing commas are fine) or a theme exported from Hadron, by path or as text. Tokens VS Code doesn't have, such as the tag colours, are derived. Returns the saved theme; use theme_set to switch to it.", Mutates: true, Entities: []string{EntityThemes}},
		func(ctx context.Context, c *Call, in ThemeImportIn) (themes.Theme, error) {
			data := []byte(in.JSON)
			if in.Path != "" {
				p := expandHome(in.Path)
				st, err := os.Stat(p)
				if err != nil {
					return themes.Theme{}, fmt.Errorf("can't read %s: %w", in.Path, err)
				}
				if st.Size() > 4<<20 {
					return themes.Theme{}, fmt.Errorf("%s is larger than 4 MB, so it isn't a colour theme", in.Path)
				}
				if data, err = os.ReadFile(p); err != nil {
					return themes.Theme{}, err
				}
			}
			if len(strings.TrimSpace(string(data))) == 0 {
				return themes.Theme{}, errors.New("pass path or json")
			}
			t, err := importTheme(data, in.Name)
			if err != nil {
				return themes.Theme{}, err
			}
			return themeStore(c).Save(t)
		})

	Register(r, Spec{Name: "theme_export", Description: "Write a theme to a JSON file that theme_import reads back, by default ~/Downloads/<id>.hadron-theme.json. Returns the path and the JSON."},
		func(ctx context.Context, c *Call, in ThemeExportIn) (ThemeExportOut, error) {
			t, err := themeStore(c).Get(in.ID)
			if err != nil {
				return ThemeExportOut{}, err
			}
			raw, _ := json.MarshalIndent(struct {
				Name   string            `json:"name"`
				Mode   string            `json:"mode"`
				Tokens map[string]string `json:"tokens"`
			}{t.Name, t.Mode, t.Tokens}, "", "  ")
			p := expandHome(in.Path)
			if p == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return ThemeExportOut{}, err
				}
				p = filepath.Join(home, "Downloads", t.ID+".hadron-theme.json")
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return ThemeExportOut{}, err
			}
			if err := os.WriteFile(p, append(raw, '\n'), 0o644); err != nil {
				return ThemeExportOut{}, err
			}
			return ThemeExportOut{Path: p, JSON: string(raw)}, nil
		})

	Register(r, Spec{Name: "theme_delete", Description: "Delete a custom colour theme. Where it was in use, Hadron goes back to Classic.", Mutates: true, Entities: []string{EntityThemes, "settings"}},
		func(ctx context.Context, c *Call, in ThemeIDIn) (ThemeChoice, error) {
			if err := themeStore(c).Delete(in.ID); err != nil {
				return ThemeChoice{}, err
			}
			ch := themeChoice(c)
			for _, kv := range [][3]string{{SettingTheme, ch.Theme, "system"}, {SettingThemeLight, ch.Light, themes.ClassicLight.ID}, {SettingThemeDark, ch.Dark, themes.ClassicDark.ID}} {
				if kv[1] == in.ID {
					if err := c.Store.Settings.Set(kv[0], json.RawMessage(toJSON(kv[2]))); err != nil {
						return ThemeChoice{}, err
					}
				}
			}
			return themeChoice(c), nil
		})
}
