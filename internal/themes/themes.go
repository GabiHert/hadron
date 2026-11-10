// Package themes holds Hadron's colour themes: the built-in ones, custom ones saved as JSON files
// under $HADRON_HOME/themes, and an importer for VS Code colour themes. A theme sets every colour
// token of frontend/src/styles/tokens.css (without the leading --).
package themes

import (
	"fmt"
	"regexp"
	"strings"
)

// Theme is one colour theme.
type Theme struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Mode    string            `json:"mode" jsonschema:"light or dark"`
	Builtin bool              `json:"builtin"`
	Tokens  map[string]string `json:"tokens" jsonschema:"colour per token name, such as bg, fg, acc, tag-blue or tb, as #rrggbb"`
}

// TagNames are the tag palette's colours, the same list as actions.ChipPalette.
var TagNames = []string{"grey", "blue", "teal", "green", "lime", "amber", "orange", "red", "pink", "purple"}

// TokenGroup is a named list of tokens, in the order the editor shows them.
type TokenGroup struct {
	Name   string   `json:"name"`
	Tokens []string `json:"tokens"`
}

// Groups lists every colour token of a theme.
var Groups = func() []TokenGroup {
	tags := []string{}
	for _, n := range TagNames {
		tags = append(tags, "tag-"+n, "tag-"+n+"-b")
	}
	return []TokenGroup{
		{Name: "Surfaces", Tokens: []string{"bg", "side", "card", "muted", "row", "sel", "border", "dot"}},
		{Name: "Text", Tokens: []string{"fg", "fg2", "fg3"}},
		{Name: "Accent and states", Tokens: []string{"acc", "on", "ok", "okb", "warn", "warnb", "bad", "badb"}},
		{Name: "Terminal", Tokens: []string{"term", "termbar", "tfg", "tdim", "tr", "tg", "ty", "tb", "tm", "tc"}},
		{Name: "Tags", Tokens: tags},
	}
}()

// TokenNames is every token, flattened from Groups.
var TokenNames = func() []string {
	var out []string
	for _, g := range Groups {
		out = append(out, g.Tokens...)
	}
	return out
}()

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,47}$`)

// Slug turns a name into an id: lowercase letters, digits and dashes.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if b.Len() > 0 && !dash {
			b.WriteRune('-')
			dash = true
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		s = "theme"
	}
	return s
}

// Validate checks a theme and normalises its colours to #rrggbb. Missing tokens are derived.
func Validate(t *Theme) error {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return fmt.Errorf("a theme needs a name")
	}
	if len(t.Name) > 60 {
		return fmt.Errorf("a theme name may have at most 60 characters")
	}
	if t.Mode != "light" && t.Mode != "dark" {
		return fmt.Errorf("mode must be light or dark, got %q", t.Mode)
	}
	known := map[string]bool{}
	for _, n := range TokenNames {
		known[n] = true
	}
	bg := t.Tokens["bg"]
	out := map[string]string{}
	for k, v := range t.Tokens {
		if !known[k] {
			return fmt.Errorf("%q is not a theme token; tokens are %s", k, strings.Join(TokenNames, ", "))
		}
		h, err := NormalizeHex(v, bg)
		if err != nil {
			return fmt.Errorf("token %s: %w", k, err)
		}
		out[k] = h
	}
	t.Tokens = out
	Complete(t)
	return nil
}

// Complete fills every missing token from the ones that are set, starting from the Classic theme
// of the same mode for the base surfaces.
func Complete(t *Theme) {
	if t.Tokens == nil {
		t.Tokens = map[string]string{}
	}
	base := ClassicLight
	if t.Mode == "dark" {
		base = ClassicDark
	}
	get := func(k string) rgba {
		c, err := parseHex(t.Tokens[k])
		if err != nil {
			c, _ = parseHex(base.Tokens[k])
		}
		return c
	}
	has := func(k string) bool { _, ok := t.Tokens[k]; return ok }
	set := func(k string, c rgba) {
		if !has(k) {
			t.Tokens[k] = c.hex()
		}
	}
	bg, fg := get("bg"), get("fg")
	// Surfaces and text from bg and fg.
	if has("bg") || has("fg") {
		set("side", mix(bg, fg, 0.04))
		set("card", mix(bg, fg, 0.025))
		set("muted", mix(bg, fg, 0.02))
		set("row", mix(bg, fg, 0.06))
		set("border", mix(bg, fg, 0.13))
		set("dot", mix(bg, fg, 0.2))
		set("fg2", mix(fg, bg, 0.28))
		set("fg3", readable(mix(fg, bg, 0.45), bg, fg, 4.5))
	}
	acc := get("acc")
	if has("acc") {
		set("sel", mix(bg, acc, 0.16))
		if contrast(rgba{255, 255, 255, 1}, acc) >= contrast(rgba{10, 12, 20, 1}, acc) {
			set("on", rgba{255, 255, 255, 1})
		} else {
			set("on", mix(rgba{0, 0, 0, 1}, acc, 0.15))
		}
	}
	for _, st := range [][2]string{{"ok", "okb"}, {"warn", "warnb"}, {"bad", "badb"}} {
		if has(st[0]) {
			set(st[1], mix(bg, get(st[0]), 0.16))
		}
	}
	// Terminal.
	if has("term") || has("tfg") {
		set("termbar", mix(get("term"), get("tfg"), 0.06))
		set("tdim", mix(get("tfg"), get("term"), 0.4))
	}
	// Tags from the terminal's ANSI colours when they were given.
	tagFrom := map[string]func() rgba{
		"blue": func() rgba { return get("tb") }, "teal": func() rgba { return get("tc") },
		"green": func() rgba { return get("tg") }, "red": func() rgba { return get("tr") },
		"amber": func() rgba { return get("ty") }, "purple": func() rgba { return get("tm") },
		"lime":   func() rgba { return mix(get("tg"), get("ty"), 0.5) },
		"orange": func() rgba { return mix(get("tr"), get("ty"), 0.5) },
		"pink":   func() rgba { return mix(get("tm"), get("tr"), 0.5) },
		"grey":   func() rgba { return get("fg3") },
	}
	derivedTags := has("tb") || has("tg") || has("tr")
	for _, n := range TagNames {
		f, b := "tag-"+n, "tag-"+n+"-b"
		if derivedTags && !has(f) {
			c := tagFrom[n]()
			tint := 0.16
			if t.Mode == "light" {
				tint = 0.13
			}
			bb := mix(bg, c, tint)
			set(b, bb)
			set(f, readable(c, get(b), fg, 4.5))
		}
		if has(f) && !has(b) {
			set(b, mix(bg, get(f), 0.15))
		}
	}
	for _, k := range TokenNames {
		if !has(k) {
			t.Tokens[k] = base.Tokens[k]
		}
	}
}
