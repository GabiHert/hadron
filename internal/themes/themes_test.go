package themes

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestBuiltinsAreCompleteAndReadable(t *testing.T) {
	seen := map[string]bool{}
	for _, th := range Builtins {
		if seen[th.ID] {
			t.Fatalf("duplicate id %s", th.ID)
		}
		seen[th.ID] = true
		for _, k := range TokenNames {
			if _, err := parseHex(th.Tokens[k]); err != nil {
				t.Errorf("%s: token %s: %v", th.ID, k, err)
			}
		}
		if len(th.Tokens) != len(TokenNames) {
			t.Errorf("%s has %d tokens, want %d", th.ID, len(th.Tokens), len(TokenNames))
		}
		c := func(a, b string) float64 {
			x, _ := parseHex(th.Tokens[a])
			y, _ := parseHex(th.Tokens[b])
			return contrast(x, y)
		}
		pairs := [][2]string{{"fg", "bg"}, {"fg", "card"}, {"fg2", "bg"}, {"fg3", "card"}, {"on", "acc"}, {"ok", "okb"}, {"warn", "warnb"}, {"bad", "badb"}, {"tfg", "term"}}
		for _, n := range TagNames {
			pairs = append(pairs, [2]string{"tag-" + n, "tag-" + n + "-b"})
		}
		for _, p := range pairs {
			// 4.4: Classic's green pills sit at 4.49 and have shipped like that.
			if r := c(p[0], p[1]); r < 4.4 {
				t.Errorf("%s: %s on %s has contrast %.2f, want at least 4.4", th.ID, p[0], p[1], r)
			}
		}
	}
}

// The Classic themes must match tokens.css, which paints the app before a theme is applied.
func TestClassicMatchesTokensCSS(t *testing.T) {
	raw, err := os.ReadFile("../../frontend/src/styles/tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(raw)
	decl := regexp.MustCompile(`--([a-z0-9-]+)\s*:\s*(#[0-9a-fA-F]{3,8})\s*;`)
	block := func(start string) map[string]string {
		i := strings.Index(css, start)
		if i < 0 {
			t.Fatalf("no %q block in tokens.css", start)
		}
		body := css[i:]
		body = body[:strings.Index(body, "}")]
		m := map[string]string{}
		for _, x := range decl.FindAllStringSubmatch(body, -1) {
			m[x[1]] = strings.ToLower(x[2])
		}
		return m
	}
	light := block(":root {")
	dark := block(`:root[data-theme="dark"] {`)
	for _, k := range TokenNames {
		if light[k] != ClassicLight.Tokens[k] {
			t.Errorf("classic-light %s = %s, tokens.css has %s", k, ClassicLight.Tokens[k], light[k])
		}
		want := dark[k]
		if want == "" {
			want = light[k] // the dark block only redefines what differs
		}
		if want != ClassicDark.Tokens[k] {
			t.Errorf("classic-dark %s = %s, tokens.css has %s", k, ClassicDark.Tokens[k], want)
		}
	}
}

func TestStripJSONC(t *testing.T) {
	in := `{
  // a comment
  "a": "http://x//y", /* block
  comment */ "b": [1, 2,],
  "c": "quote \" // not a comment",
}`
	got := string(StripJSONC([]byte(in)))
	for _, want := range []string{`"http://x//y"`, `[1, 2]`, `"quote \" // not a comment"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if strings.Contains(got, "comment */") || strings.Contains(got, "// a comment") {
		t.Errorf("comments left in %s", got)
	}
}

const sampleVSCode = `{
	// Acme Midnight, a dark theme
	"name": "Acme Midnight",
	"type": "dark",
	"colors": {
		"editor.background": "#1a1b26",
		"editor.foreground": "#c0caf5",
		"focusBorder": "#7aa2f7",
		"button.background": "#3d59a1",
		"button.foreground": "#ffffff",
		"editor.selectionBackground": "#515c7e4d",
		"terminal.background": "#16161e",
		"terminal.foreground": "#a9b1d6",
		"terminal.ansiRed": "#f7768e",
		"terminal.ansiGreen": "#9ece6a",
		"terminal.ansiYellow": "#e0af68",
		"terminal.ansiBlue": "#7aa2f7",
		"terminal.ansiMagenta": "#bb9af7",
		"terminal.ansiCyan": "#7dcfff",
		"terminal.ansiBrightBlack": "#414868",
	},
	"tokenColors": [],
}`

func TestFromVSCode(t *testing.T) {
	th, err := FromVSCode([]byte(sampleVSCode), "")
	if err != nil {
		t.Fatal(err)
	}
	if th.Name != "Acme Midnight" || th.Mode != "dark" {
		t.Fatalf("got name %q mode %q", th.Name, th.Mode)
	}
	want := map[string]string{"bg": "#1a1b26", "fg": "#c0caf5", "term": "#16161e", "tfg": "#a9b1d6", "tr": "#f7768e", "tb": "#7aa2f7", "tm": "#bb9af7", "tc": "#7dcfff", "on": "#ffffff"}
	for k, v := range want {
		if th.Tokens[k] != v {
			t.Errorf("%s = %s, want %s", k, th.Tokens[k], v)
		}
	}
	if len(th.Tokens) != len(TokenNames) {
		t.Errorf("got %d tokens, want all %d", len(th.Tokens), len(TokenNames))
	}
	if th.Tokens["sel"] == "#515c7e" {
		t.Error("the selection's alpha was not flattened onto the background")
	}
	// Tags come from the ANSI colours and stay readable on their backgrounds.
	for _, n := range TagNames {
		f, _ := parseHex(th.Tokens["tag-"+n])
		b, _ := parseHex(th.Tokens["tag-"+n+"-b"])
		if r := contrast(f, b); r < 4.5 {
			t.Errorf("tag %s contrast %.2f", n, r)
		}
	}
	named, _ := FromVSCode([]byte(sampleVSCode), "My Night")
	if named.Name != "My Night" {
		t.Errorf("name override: %q", named.Name)
	}
}

func TestFromVSCodeLightWithoutType(t *testing.T) {
	th, err := FromVSCode([]byte(`{"colors":{"editor.background":"#ffffff","editor.foreground":"#333333"}}`), "Plain")
	if err != nil {
		t.Fatal(err)
	}
	if th.Mode != "light" {
		t.Errorf("mode %q, want light from the background", th.Mode)
	}
	if err := Validate(&th); err != nil {
		t.Errorf("an imported theme must validate: %v", err)
	}
}

func TestFromVSCodeRejectsOtherFiles(t *testing.T) {
	for _, in := range []string{`{"name":"x","version":"1.0.0"}`, `{"colors":{}}`, `not json`, `[1,2]`} {
		_, err := FromVSCode([]byte(in), "")
		if err == nil {
			t.Errorf("%s: want an error", in)
			continue
		}
		if !strings.Contains(err.Error(), "VS Code colour theme") && !strings.Contains(err.Error(), "not valid JSON") {
			t.Errorf("%s: unclear error %q", in, err)
		}
	}
}

func TestStore(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	list, _ := s.List()
	if len(list) != len(Builtins) {
		t.Fatalf("a fresh store lists %d themes", len(list))
	}
	d := clone(Builtins[3])
	d.ID, d.Name = "", "Deep Space"
	saved, err := s.Save(d)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ID != "deep-space-2" || saved.Builtin {
		t.Errorf("a copy named like a built-in got id %q", saved.ID)
	}
	saved.Name = "Renamed"
	saved.Tokens["acc"] = "#FF0000"
	if _, err := s.Save(saved); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("deep-space-2")
	if err != nil || got.Name != "Renamed" || got.Tokens["acc"] != "#ff0000" {
		t.Fatalf("get after update: %+v %v", got.Name, err)
	}
	if _, err := s.Save(Builtins[0]); err == nil {
		t.Error("saving over a built-in must fail")
	}
	if err := s.Delete("nucleus"); err == nil {
		t.Error("deleting a built-in must fail")
	}
	if err := s.Delete("deep-space-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("deep-space-2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
	bad := Theme{Name: "X", Mode: "dark", Tokens: map[string]string{"nope": "#000"}}
	if _, err := s.Save(bad); err == nil || !strings.Contains(err.Error(), "not a theme token") {
		t.Errorf("unknown token: %v", err)
	}
}
