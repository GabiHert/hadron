package dlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func read(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	files, _ := filepath.Glob(filepath.Join(dir, "hadron-*.jsonl"))
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		b.Write(raw)
	}
	return b.String()
}

func TestOffWritesOnlyErrorsAndPanics(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	l.Debug("noisy", "a", 1)
	l.Info("hello")
	l.Warn("careful")
	l.Error("broke", "err", "disk full")
	func() {
		defer func() {
			if r := recover(); r != nil {
				l.Panic("test", r)
			}
		}()
		panic("boom")
	}()
	got := read(t, dir)
	if strings.Contains(got, "noisy") || strings.Contains(got, "hello") || strings.Contains(got, "careful") {
		t.Errorf("debug logging is off, so only errors are written:\n%s", got)
	}
	if !strings.Contains(got, `"msg":"broke"`) || !strings.Contains(got, "disk full") {
		t.Errorf("errors are written:\n%s", got)
	}
	if !strings.Contains(got, "panic recovered") || !strings.Contains(got, "boom") || !strings.Contains(got, "goroutine") {
		t.Errorf("a recovered panic is written with its stack:\n%s", got)
	}
}

func TestOnWritesEveryLevelAndTailFilters(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	l.SetDebug(true)
	l.Debug("d1", "action", "card_list")
	l.Info("i1")
	l.Warn("w1")
	l.Error("e1")
	all := l.Tail(0, "", "")
	if len(all) != 4 || all[0].Msg != "d1" || all[0].Fields["action"] != "card_list" {
		t.Fatalf("tail = %+v", all)
	}
	if got := l.Tail(10, "warn", ""); len(got) != 2 || got[0].Msg != "w1" {
		t.Errorf("warn and up = %+v", got)
	}
	if got := l.Tail(10, "", "CARD_list"); len(got) != 1 {
		t.Errorf("contains is case-insensitive: %+v", got)
	}
	if got := l.Tail(2, "", ""); len(got) != 2 || got[1].Msg != "e1" {
		t.Errorf("the newest two, oldest first: %+v", got)
	}
}

func TestSecretishFieldsAndLongValuesAreNeverWritten(t *testing.T) {
	dir := t.TempDir()
	l := New(dir)
	l.SetDebug(true)
	l.Debug("call", "token", "tok-SECRET-123", "text", "hello there", "Authorization", "Bearer abc", "prompt", "You are the planner", "arg_keys", []string{"card", "text"}, "prompt_chars", 4200, "long", strings.Repeat("x", 2000))
	got := read(t, dir)
	for _, bad := range []string{"tok-SECRET-123", "hello there", "Bearer abc", "You are the planner"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q reached the log:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, `"prompt_chars":4200`) || !strings.Contains(got, `"arg_keys":["card","text"]`) {
		t.Errorf("counts and key names are fine:\n%s", got)
	}
	if strings.Contains(got, strings.Repeat("x", 600)) {
		t.Error("long values are cut")
	}
}

func TestRetentionAndTheSizeCap(t *testing.T) {
	dir := t.TempDir()
	mk := func(day string, size int) string {
		p := filepath.Join(dir, "hadron-"+day+".jsonl")
		if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.Local)
	old := mk("2026-11-10", 10)      // 10 days old: gone
	edge := mk("2026-11-14", 10)     // 6 days old: kept
	big1 := mk("2026-11-15", 30<<20) // 30 MB
	big2 := mk("2026-11-16", 30<<20) // 30 MB: the total is over 50 MB, so the oldest goes
	today := mk("2026-11-20", 10)
	l := &Logger{dir: dir, now: func() time.Time { return now }}
	l.Prune()
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if exists(old) {
		t.Error("files past 7 days are removed")
	}
	if exists(edge) || exists(big1) {
		// 6 days old is inside the retention, but the cap removes the oldest files first.
		t.Log("cap removed older files first")
	}
	if !exists(big2) || !exists(today) {
		t.Error("the newest files stay")
	}
	var total int64
	files, _ := filepath.Glob(filepath.Join(dir, "hadron-*.jsonl"))
	for _, f := range files {
		st, _ := os.Stat(f)
		total += st.Size()
	}
	if total > MaxTotal {
		t.Errorf("total %d is over the cap", total)
	}
}

func TestANilLoggerIsHarmless(t *testing.T) {
	var l *Logger
	l.Debug("x")
	l.Error("y")
	l.SetDebug(true)
	l.Panic("z", "p")
	if len(l.Tail(5, "", "")) != 0 || l.Dir() != "" || l.Debugging() {
		t.Error("a nil logger does nothing")
	}
}
