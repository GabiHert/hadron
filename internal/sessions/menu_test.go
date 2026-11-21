package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "menus", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func labels(m *Menu) string {
	var l []string
	for _, o := range m.Options {
		l = append(l, o.Label)
	}
	return strings.Join(l, " | ")
}

// The screens in testdata/menus were captured from Claude Code 2.1 (paths made up), except the
// boxed, trust, plan and model ones, which follow its layouts.
func TestParseMenuReadsEachKindOfClaudeCodeMenu(t *testing.T) {
	cases := []struct {
		file, kind, title, labels string
		cursor                    int
		multi, cancel             bool
	}{
		{"mcp.txt", "mcp", "New MCP server found in this project: acme", "Use this MCP server | Use this and all future MCP servers in this project | Continue without using this MCP server", 2, false, true},
		{"mcp_first.txt", "mcp", "New MCP server found in this project: acme", "Use this MCP server | Use this and all future MCP servers in this project | Continue without using this MCP server", 0, false, true},
		{"ask.txt", "question", "Which database should we use?", "Postgres | SQLite | Type something. | Chat about this", 0, false, true},
		{"ask_typed.txt", "question", "Which database should we use?", "Postgres | SQLite | use the one we already run | Chat about this", 2, false, true},
		{"multi.txt", "question", "Which checks should run?", "Lint | Tests | Build | Type something | Submit | Chat about this", 0, true, true},
		{"multi_submit.txt", "question", "Which checks should run?", "Lint | Tests | Build | Type something | Submit | Chat about this", 4, true, true},
		{"review.txt", "review", "Review your answers", "Submit answers | Cancel", 0, false, false},
		{"permission.txt", "permission", "Do you want to proceed?", "Yes | Yes, and always allow access to /Users/acme/projects/billing-fix/services/ledger/exports/monthly from this project | Yes, and switch to auto mode · auto mode handles these prompts for you | No", 0, false, true},
		{"boxed_permission.txt", "permission", "Do you want to proceed?", "Yes | Yes, and don't ask again for rm commands in /work/acme | No, and tell Claude what to do differently (esc)", 0, false, false},
		{"trust.txt", "trust", "Do you trust the files in this folder?", "Yes, proceed | No, exit", 0, false, true},
		{"plan.txt", "plan", "Would you like to proceed?", "Yes, and auto-accept edits | Yes, and manually approve edits | No, keep planning", 0, false, false},
		{"model.txt", "select", "Select model", "Default (recommended) | Sonnet | Haiku", 0, false, true},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			screen := fixture(t, c.file)
			m := ParseMenu(screen, 100)
			if m == nil {
				t.Fatal("no menu found")
			}
			if m.Kind != c.kind || m.Title != c.title || labels(m) != c.labels || m.Cursor != c.cursor || m.Multi != c.multi || m.Cancel != c.cancel {
				t.Fatalf("got kind %q title %q\nlabels %q\ncursor %d multi %v cancel %v", m.Kind, m.Title, labels(m), m.Cursor, m.Multi, m.Cancel)
			}
			if ScreenShowsInput(screen) {
				t.Error("a menu's highlighted row was taken for Claude's input box")
			}
		})
	}
}

func TestParseMenuKeepsTextDescriptionsBoxesAndTheFreeTextRow(t *testing.T) {
	ask := ParseMenu(fixture(t, "ask.txt"), 100)
	if ask.Header != "Database" || ask.Options[0].Description != "Relational and mature" || !ask.Options[2].FreeText || ask.Options[1].FreeText {
		t.Fatalf("ask = %+v", ask)
	}
	typed := ParseMenu(fixture(t, "ask_typed.txt"), 100)
	if !typed.Options[2].FreeText {
		t.Error("the free-text row with typed text in it is no longer known as one")
	}
	multi := ParseMenu(fixture(t, "multi.txt"), 100)
	if multi.Header != "Checks" || multi.Options[0].Checked || !multi.Options[1].Checked || !multi.Options[1].Box || !multi.Options[4].Submit || !multi.Options[3].FreeText {
		t.Fatalf("multi = %+v", multi)
	}
	mcp := ParseMenu(fixture(t, "mcp.txt"), 100)
	if !strings.HasPrefix(mcp.Body, "MCP servers may execute code") || mcp.Footer != "Enter to confirm · Esc to cancel" {
		t.Fatalf("mcp body %q footer %q", mcp.Body, mcp.Footer)
	}
	perm := ParseMenu(fixture(t, "permission.txt"), 100)
	if !strings.Contains(perm.Body, "mkdir demo_dir") || !strings.Contains(perm.Body, "Bash command") || strings.Contains(perm.Body, "Tip:") {
		t.Fatalf("permission body %q", perm.Body)
	}
	model := ParseMenu(fixture(t, "model.txt"), 100)
	if model.Options[0].Description != "current · Opus 5.5 · Most capable for complex work" {
		t.Fatalf("model row 1 description %q", model.Options[0].Description)
	}
}

// The signature names the menu, not its state: moving the highlight, ticking a box or typing into
// the free-text row is the same menu, so the watcher does not ask twice.
func TestTheMenuSignatureIgnoresTheHighlightBoxesAndTypedText(t *testing.T) {
	pairs := [][2]string{{"mcp.txt", "mcp_first.txt"}, {"ask.txt", "ask_typed.txt"}, {"multi.txt", "multi_submit.txt"}}
	for _, p := range pairs {
		a, b := ParseMenu(fixture(t, p[0]), 100), ParseMenu(fixture(t, p[1]), 100)
		if a.Sig != b.Sig {
			t.Errorf("%s and %s are the same menu but their signatures differ", p[0], p[1])
		}
	}
	if ParseMenu(fixture(t, "ask.txt"), 100).Sig == ParseMenu(fixture(t, "multi.txt"), 100).Sig {
		t.Error("two different menus share a signature")
	}
}

func TestNoMenuOnThePromptOrWhileClaudeWorks(t *testing.T) {
	for _, f := range []string{"prompt.txt", "working.txt"} {
		if m := ParseMenu(fixture(t, f), 100); m != nil {
			t.Errorf("%s: found a menu %+v", f, m)
		}
	}
	if !ScreenShowsInput(fixture(t, "prompt.txt")) {
		t.Error("Claude's input box is not seen")
	}
}
