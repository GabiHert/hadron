package actions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/dlog"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
)

func logHarness(t *testing.T) (*harness, *dlog.Logger) {
	t.Helper()
	deps, rec := testDeps(t)
	l := dlog.New(filepath.Join(deps.Home, "logs"))
	deps.Log = l
	r := NewRegistry(deps)
	RegisterAll(r)
	Register(r, Spec{Name: "test_boom", Description: "Panics, to prove the registry survives it."},
		func(ctx context.Context, c *Call, in struct{}) (struct{}, error) { panic("kaboom") })
	return &harness{t: t, reg: r, rec: rec}, l
}

func allLogText(t *testing.T, l *dlog.Logger) string {
	t.Helper()
	var b strings.Builder
	files, _ := filepath.Glob(filepath.Join(l.Dir(), "*"))
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		b.Write(raw)
	}
	return b.String()
}

func TestWithDebugLoggingOffOnlyErrorsArePlacedInTheLog(t *testing.T) {
	h, l := logHarness(t)
	w := h.workspace("W")
	h.do("card_create", m{"workspace_id": w.ID, "nick": "alpha", "start": false}, nil)
	h.fails("card_get", m{"card_id": "nope"}, "does not exist")
	if text := allLogText(t, l); text != "" {
		t.Fatalf("off means nothing but errors, got:\n%s", text)
	}
	h.do("client_log", m{"level": "error", "message": "TypeError: x is undefined", "stack": "at App.tsx:10"}, nil)
	h.do("client_log", m{"level": "debug", "message": "chatter"}, nil)
	var lines []dlog.Line
	h.do("logs_tail", m{"lines": 10}, &lines)
	if len(lines) != 1 || lines[0].Msg != "window" || lines[0].Level != "error" || lines[0].Fields["detail"] != "TypeError: x is undefined" || lines[0].Fields["stack"] != "at App.tsx:10" {
		t.Fatalf("tail = %+v", lines)
	}
	h.fails("client_log", m{"level": "loud", "message": "x"}, "must be error, warn, info or debug")
}

func TestTurningDebugLoggingOnLogsActionsByNameAndArgumentKeysOnly(t *testing.T) {
	h, l := logHarness(t)
	w := h.workspace("W")
	var out DebugLoggingOut
	h.do("debug_logging_set", m{"enabled": true}, &out)
	if !out.Enabled || out.LogDir != l.Dir() || !l.Debugging() {
		t.Fatalf("out = %+v", out)
	}
	h.do("card_create", m{"workspace_id": w.ID, "nick": "alpha", "title": "PLANTED-card-title", "start": false}, nil)
	var msg domain.Message
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	if err := h.as(planner, "chat_post", m{"channel": "planner", "text": "PLANTED message text with a secret sk-live-123"}, &msg); err != nil {
		t.Fatal(err)
	}
	h.do("settings_update", m{"values": m{"my.api_token": "PLANTED-token-value"}}, nil)

	text := allLogText(t, l)
	for _, planted := range []string{"PLANTED-card-title", "PLANTED message text", "sk-live-123", "PLANTED-token-value"} {
		if strings.Contains(text, planted) {
			t.Errorf("%q must never reach the log:\n%s", planted, text)
		}
	}
	var found *dlog.Line
	for _, ln := range l.Tail(500, "", "") {
		ln := ln
		if ln.Msg == "action" && ln.Fields["action"] == "card_create" {
			found = &ln
		}
	}
	if found == nil || found.Fields["actor"] != "you" || found.Fields["ms"] == nil {
		t.Fatalf("card_create is logged with its actor and duration: %+v", found)
	}
	keys, _ := found.Fields["arg_keys"].([]any)
	if len(keys) != 4 {
		t.Errorf("only the argument names: %v", keys)
	}
	if !strings.Contains(text, `"actor":"planner"`) {
		t.Error("the planner's call is attributed to the planner")
	}
	h.fails("debug_logging_set", m{"enabled": "yes"}, "enabled")
	h.do("debug_logging_set", m{"enabled": false}, nil)
	before := len(l.Tail(2000, "", ""))
	h.do("card_list", m{"workspace_id": w.ID}, nil)
	if len(l.Tail(2000, "", "")) != before {
		t.Error("off again, so nothing more is written")
	}
}

func TestSettingsUpdateAlsoTogglesLogging(t *testing.T) {
	h, l := logHarness(t)
	h.workspace("W")
	h.do("settings_update", m{"values": m{"debug.logging": true}}, nil)
	if !l.Debugging() {
		t.Error("the setting turns it on")
	}
	h.fails("settings_update", m{"values": m{"debug.logging": "on"}}, "true or false")
	h.do("settings_update", m{"values": m{"debug.logging": false}}, nil)
	if l.Debugging() {
		t.Error("and off")
	}
}

func TestAPanickingActionIsRecoveredWithItsStack(t *testing.T) {
	h, l := logHarness(t)
	err := h.try("test_boom", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "internal error") {
		t.Fatalf("err = %v", err)
	}
	text := allLogText(t, l)
	if !strings.Contains(text, "panic recovered") || !strings.Contains(text, "kaboom") || !strings.Contains(text, "goroutine") || !strings.Contains(text, "test_boom") {
		t.Errorf("a recovered panic is logged with a stack even with debug logging off:\n%s", text)
	}
	h.workspace("still works")
}

func TestDiagnosticsHoldWhatABugReportNeedsAndNoSecrets(t *testing.T) {
	h, l := logHarness(t)
	h.workspace("W")
	h.do("settings_update", m{"values": m{"ui.density": "compact", "my.api_token": "PLANTED-token", "github.password": "pw", "update.key": "k"}}, nil)
	h.do("debug_logging_set", m{"enabled": true}, nil)
	h.do("client_log", m{"level": "error", "message": "something broke"}, nil)
	var d DiagnosticsOut
	h.do("diagnostics_get", nil, &d)
	if d.Version != "test" || d.MacOS == "" || d.Arch == "" || d.Home == "" || d.LogDir != l.Dir() || !d.DebugLogging || d.Actions < 100 {
		t.Errorf("d = %+v", d)
	}
	if d.Settings["ui.density"] != "compact" {
		t.Errorf("settings = %v", d.Settings)
	}
	for k := range d.Settings {
		if strings.Contains(k, "token") || strings.Contains(k, "password") || strings.HasSuffix(k, ".key") {
			t.Errorf("setting %s must not be in the diagnostics", k)
		}
	}
	if len(d.RecentLogLines) == 0 || len(d.RecentLogLines) > 300 || !strings.Contains(strings.Join(d.RecentLogLines, "\n"), "something broke") {
		t.Errorf("recent lines = %d", len(d.RecentLogLines))
	}
	var lines []dlog.Line
	h.do("logs_tail", m{"lines": 500, "level": "error", "contains": "BROKE"}, &lines)
	if len(lines) != 1 {
		t.Errorf("filters: %+v", lines)
	}
	h.fails("logs_tail", m{"lines": 5000}, "between 1 and 2000")
	h.fails("logs_tail", m{"level": "loud"}, "must be debug")
}

func TestSessionStartsAreLoggedWithoutThePromptText(t *testing.T) {
	h := newSessionHarness(t, false)
	l := dlog.New(filepath.Join(t.TempDir(), "logs"))
	l.SetDebug(true)
	h.svc.SetLogForTest(l)
	w := h.workspace("W")
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "the planner", func() bool { return h.file(".started") != "" })
	text := allLogText(t, l)
	if !strings.Contains(text, "pty start") || !strings.Contains(text, "prompt 1") || strings.Contains(text, "You are the planner") {
		t.Errorf("argv is logged with the prompt replaced by its length:\n%s", text)
	}
	h.do("planner_restart", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "restart", func() bool { return strings.Contains(allLogText(t, l), "pty exit") })
	_ = time.Second
	args := sessions.RedactArgs([]string{"--append-system-prompt", "secret prompt body", "--mcp-config", "/x.json"})
	if args[1] != "<prompt 18 chars>" || args[3] != "/x.json" {
		t.Errorf("args = %v", args)
	}
}
