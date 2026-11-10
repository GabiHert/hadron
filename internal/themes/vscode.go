package themes

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// StripJSONC removes // and /* */ comments and trailing commas, leaving strings intact, so VS Code's
// JSON-with-comments files parse as JSON.
func StripJSONC(src []byte) []byte {
	out := make([]byte, 0, len(src))
	inStr, esc := false, false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inStr {
			out = append(out, c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
			out = append(out, c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			if i < len(src) {
				out = append(out, '\n')
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++
		default:
			out = append(out, c)
		}
	}
	// Trailing commas: a comma followed only by whitespace and then } or ].
	res := make([]byte, 0, len(out))
	inStr, esc = false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inStr {
			res = append(res, c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
		}
		if c == ',' {
			j := i + 1
			for j < len(out) && strings.ContainsRune(" \t\r\n", rune(out[j])) {
				j++
			}
			if j < len(out) && (out[j] == '}' || out[j] == ']') {
				continue
			}
		}
		res = append(res, c)
	}
	return res
}

// ErrNotTheme is returned for a file that is not a VS Code colour theme.
var ErrNotTheme = errors.New("this file is not a VS Code colour theme: it needs a \"colors\" object with keys such as editor.background and editor.foreground")

type vscodeTheme struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Colors map[string]string `json:"colors"`
}

// FromVSCode maps a VS Code colour theme onto Hadron's tokens. name overrides the theme's own name.
// The terminal palette comes from terminal.ansi*, and the tag palette and anything else missing
// are derived.
func FromVSCode(data []byte, name string) (Theme, error) {
	var v vscodeTheme
	if err := json.Unmarshal(StripJSONC(data), &v); err != nil {
		var se *json.SyntaxError
		if errors.As(err, &se) {
			return Theme{}, fmt.Errorf("this file is not valid JSON (at byte %d): %v", se.Offset, err)
		}
		return Theme{}, ErrNotTheme
	}
	if len(v.Colors) == 0 || (v.Colors["editor.background"] == "" && v.Colors["editor.foreground"] == "" && v.Colors["foreground"] == "") {
		return Theme{}, ErrNotTheme
	}
	t := Theme{Name: strings.TrimSpace(name), Tokens: map[string]string{}}
	if t.Name == "" {
		t.Name = strings.TrimSpace(v.Name)
	}
	if t.Name == "" {
		t.Name = "Imported theme"
	}
	pick := func(keys ...string) (string, bool) {
		for _, k := range keys {
			if s := strings.TrimSpace(v.Colors[k]); s != "" {
				if _, err := parseHex(s); err == nil {
					return s, true
				}
			}
		}
		return "", false
	}
	bg, ok := pick("editor.background", "sideBar.background", "panel.background")
	if !ok {
		bg = "#1e1e1e"
		if strings.Contains(strings.ToLower(v.Type), "light") {
			bg = "#ffffff"
		}
	}
	bgc, _ := parseHex(bg)
	bg = bgc.over(rgba{255, 255, 255, 1}).hex()
	switch strings.ToLower(v.Type) {
	case "light", "hclight", "vs":
		t.Mode = "light"
	case "dark", "hc", "hcdark", "hc-black", "vs-dark":
		t.Mode = "dark"
	default:
		if bgc.luminance() > 0.4 {
			t.Mode = "light"
		} else {
			t.Mode = "dark"
		}
	}
	flat := func(s string) string {
		h, _ := NormalizeHex(s, bg)
		return h
	}
	t.Tokens["bg"] = bg
	set := func(token string, keys ...string) {
		if s, ok := pick(keys...); ok {
			t.Tokens[token] = flat(s)
		}
	}
	set("fg", "editor.foreground", "foreground")
	set("side", "sideBar.background", "activityBar.background", "titleBar.activeBackground")
	set("card", "editorWidget.background", "dropdown.background", "editorHoverWidget.background")
	set("border", "panel.border", "editorGroup.border", "sideBar.border", "widget.border", "contrastBorder")
	set("row", "list.hoverBackground")
	set("sel", "list.activeSelectionBackground", "editor.selectionBackground")
	set("fg2", "descriptionForeground", "sideBarTitle.foreground")
	set("fg3", "editorLineNumber.foreground", "disabledForeground")
	set("acc", "button.background", "focusBorder", "textLink.foreground", "activityBarBadge.background")
	set("on", "button.foreground", "activityBarBadge.foreground")
	set("ok", "terminal.ansiGreen", "gitDecoration.addedResourceForeground", "testing.iconPassed")
	set("warn", "editorWarning.foreground", "terminal.ansiYellow", "list.warningForeground")
	set("bad", "errorForeground", "editorError.foreground", "terminal.ansiRed")
	set("term", "terminal.background", "panel.background", "editor.background")
	set("tfg", "terminal.foreground", "editor.foreground", "foreground")
	set("tdim", "terminal.ansiBrightBlack")
	set("tr", "terminal.ansiRed", "terminal.ansiBrightRed")
	set("tg", "terminal.ansiGreen", "terminal.ansiBrightGreen")
	set("ty", "terminal.ansiYellow", "terminal.ansiBrightYellow")
	set("tb", "terminal.ansiBlue", "terminal.ansiBrightBlue")
	set("tm", "terminal.ansiMagenta", "terminal.ansiBrightMagenta")
	set("tc", "terminal.ansiCyan", "terminal.ansiBrightCyan")
	if _, ok := t.Tokens["fg"]; !ok {
		if t.Mode == "light" {
			t.Tokens["fg"] = "#1d2330"
		} else {
			t.Tokens["fg"] = "#e6e6e6"
		}
	}
	// Keep text readable when a theme gives a low-contrast description colour.
	b, _ := parseHex(t.Tokens["bg"])
	f, _ := parseHex(t.Tokens["fg"])
	for _, k := range []string{"fg2", "fg3", "acc"} {
		if s, ok := t.Tokens[k]; ok {
			c, _ := parseHex(s)
			min := 4.5
			if k == "acc" {
				min = 3
			}
			t.Tokens[k] = readable(c, b, f, min).hex()
		}
	}
	if s, ok := t.Tokens["on"]; ok {
		on, _ := parseHex(s)
		acc, _ := parseHex(t.Tokens["acc"])
		if contrast(on, acc) < 3 {
			delete(t.Tokens, "on")
		}
	}
	Complete(&t)
	return t, nil
}
