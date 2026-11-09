package pty

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func init() {
	// Keep the suite fast: the real pauses exist for the Claude CLI, not for sh and cat.
	submitDelay = 20 * time.Millisecond
	stopGrace = 500 * time.Millisecond
}

// waitFor polls cond for up to five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func start(t *testing.T, o Options) *Session {
	t.Helper()
	s, err := Start(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Stop(); <-s.Done() })
	return s
}

func sh(script string) Options {
	return Options{Command: "/bin/sh", Args: []string{"-c", script}}
}

func readFile(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestSendAndReadScreen(t *testing.T) {
	s := start(t, Options{Command: "/bin/cat"})
	if err := s.Send("hello hadron", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the echoed line", func() bool { return strings.Count(s.Screen(10, false), "hello hadron") >= 2 })
}

func TestSendFramesAsBracketedPasteOnlyWhenTheChildAsked(t *testing.T) {
	out := filepath.Join(t.TempDir(), "in")
	cases := []struct {
		name, prelude, want string
	}{
		{"mode on", `printf '\033[?2004h'; `, "\x1b[200~hello world\x1b[201~\r"},
		{"mode off", ``, "hello world\r"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_ = os.Remove(out)
			s := start(t, sh(c.prelude+`stty raw -echo; cat > `+out))
			if c.prelude != "" {
				waitFor(t, "bracketed paste mode", func() bool {
					s.mu.Lock()
					defer s.mu.Unlock()
					return s.modes.enabled(modeBracketedPaste)
				})
			}
			time.Sleep(100 * time.Millisecond) // let stty raw land
			if err := s.Send("hello world", true); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the child to read the input", func() bool { return readFile(out) == c.want })
		})
	}
}

func TestConcurrentSendsNeverInterleave(t *testing.T) {
	out := filepath.Join(t.TempDir(), "in")
	s := start(t, sh(`stty raw -echo; cat > `+out))
	time.Sleep(100 * time.Millisecond)
	a, b := strings.Repeat("A", 1800), strings.Repeat("B", 1800)
	var wg sync.WaitGroup
	for _, text := range []string{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Send(text, true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	waitFor(t, "both lines", func() bool { return len(readFile(out)) == 2*1801 })
	got := readFile(out)
	if got != a+"\r"+b+"\r" && got != b+"\r"+a+"\r" {
		t.Fatalf("the two sends interleaved: %q", got)
	}
}

func TestResizeReachesTheChildAndTheScreenModel(t *testing.T) {
	s := start(t, sh(`read x; stty size; sleep 5`))
	if c, r := s.Size(); c != DefaultCols || r != DefaultRows {
		t.Fatalf("default size = %dx%d", c, r)
	}
	if err := s.Resize(100, 50); err != nil {
		t.Fatal(err)
	}
	if err := s.Resize(0, 10); err == nil {
		t.Fatal("a zero size should be refused")
	}
	if err := s.Send("go", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "stty size to report 50 100", func() bool { return strings.Contains(s.Screen(10, false), "50 100") })
	if c, r := s.Size(); c != 100 || r != 50 {
		t.Fatalf("size = %dx%d", c, r)
	}
}

func TestExitCodeNoticeAndWritesAfterExit(t *testing.T) {
	var mu sync.Mutex
	var codes []int
	s := start(t, Options{Command: "/bin/sh", Args: []string{"-c", "echo bye; exit 3"}, OnExit: func(c int) { mu.Lock(); codes = append(codes, c); mu.Unlock() }})
	<-s.Done()
	if s.ExitCode() != 3 || s.Liveness() != Ended {
		t.Fatalf("exit = %d, liveness = %s", s.ExitCode(), s.Liveness())
	}
	screen := s.Screen(10, false)
	if !strings.Contains(screen, "bye") || !strings.Contains(screen, "[process exited with code 3]") {
		t.Fatalf("screen = %q", screen)
	}
	if err := s.Send("late", true); err != ErrEnded {
		t.Fatalf("Send after exit = %v, want ErrEnded", err)
	}
	waitFor(t, "OnExit", func() bool { mu.Lock(); defer mu.Unlock(); return len(codes) == 1 && codes[0] == 3 })
	replay, _ := s.Scrollback()
	if !strings.Contains(string(replay), "bye") {
		t.Fatalf("scrollback lost the output: %q", replay)
	}
}

func TestSignalExitCodeIs128PlusSignal(t *testing.T) {
	s := start(t, sh(`sleep 30`))
	s.Stop()
	<-s.Done()
	if s.ExitCode() != 128+1 { // SIGHUP
		t.Fatalf("exit code = %d, want 129", s.ExitCode())
	}
}

func TestRestartUnderTheSameID(t *testing.T) {
	m := NewManager(nil)
	first, err := m.Start("s1", Options{Command: "/bin/sh", Args: []string{"-c", "echo one"}})
	if err != nil {
		t.Fatal(err)
	}
	<-first.Done()
	second, err := m.Start("s1", Options{Command: "/bin/sh", Args: []string{"-c", "echo two; sleep 5"}})
	if err != nil {
		t.Fatalf("restart after exit: %v", err)
	}
	waitFor(t, "second run output", func() bool { return strings.Contains(second.Screen(5, false), "two") })
	if got, _ := m.Get("s1"); got != second {
		t.Fatal("Get should return the newest session")
	}
	if _, err := m.Start("s1", Options{Command: "/bin/cat"}); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("starting over a running session = %v", err)
	}
	m.StopAll()
	<-second.Done()
}

func TestLivenessTransitions(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	s := start(t, Options{Command: "/bin/cat", WorkingWindow: 150 * time.Millisecond, OnLiveness: func(l string) { mu.Lock(); seen = append(seen, l); mu.Unlock() }})
	if s.Liveness() != Idle {
		t.Fatalf("a quiet session is %s, want idle", s.Liveness())
	}
	_ = s.Send("ping", true)
	waitFor(t, "working", func() bool { return s.Liveness() == Working })
	waitFor(t, "idle again", func() bool { return s.Liveness() == Idle })
	s.SetWaiting(true)
	waitFor(t, "waiting", func() bool { return s.Liveness() == Waiting })
	// Output while a question is open does not hide it.
	_ = s.Send("more", true)
	time.Sleep(50 * time.Millisecond)
	if s.Liveness() != Waiting {
		t.Fatalf("liveness = %s, want waiting", s.Liveness())
	}
	s.SetWaiting(false)
	waitFor(t, "idle after the answer", func() bool { return s.Liveness() == Idle })
	s.Stop()
	<-s.Done()
	waitFor(t, "the ended notification", func() bool { mu.Lock(); defer mu.Unlock(); return len(seen) > 0 && seen[len(seen)-1] == Ended })
	mu.Lock()
	defer mu.Unlock()
	// Idle is derived from the clock, so the callback may or may not report it between two other
	// states (a timer that a later change supersedes never fires). What must hold is the order of
	// the states that need a cause: working, then waiting, optionally working again after the
	// answer, then ended.
	var causes []string
	for _, l := range seen {
		if l != Idle {
			causes = append(causes, l)
		}
	}
	got := dedupeRuns(causes)
	ok := reflect.DeepEqual(got, []string{Working, Waiting, Ended}) ||
		reflect.DeepEqual(got, []string{Working, Waiting, Working, Ended})
	if !ok {
		t.Fatalf("liveness changes = %v", seen)
	}
}

func dedupeRuns(in []string) []string {
	var out []string
	for _, s := range in {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

func TestScreenTailAndScrollback(t *testing.T) {
	s := start(t, sh(`seq 1 100; sleep 5`))
	waitFor(t, "100 lines", func() bool { return strings.HasSuffix(s.Screen(0, false), "100") })
	if got := s.Screen(3, false); got != "98\n99\n100" {
		t.Fatalf("last three lines = %q", got)
	}
	all := strings.Split(s.Screen(0, true), "\n")
	if all[0] != "1" || all[len(all)-1] != "100" || len(all) != 100 {
		t.Fatalf("with scrollback: %d lines, first %q, last %q", len(all), all[0], all[len(all)-1])
	}
	if len(strings.Split(s.Screen(0, false), "\n")) > DefaultRows {
		t.Fatal("the plain screen is taller than the terminal")
	}
}

func TestKeys(t *testing.T) {
	got, err := keyBytes([]string{"Up", "Ctrl-C", "c-d", "Enter", "y", "Esc"})
	if err != nil || got != "\x1b[A\x03\x04\ry\x1b" {
		t.Fatalf("keyBytes = %q, %v", got, err)
	}
	if _, err := keyBytes([]string{"Hyper-Z"}); err == nil || !strings.Contains(err.Error(), "Ctrl-<letter>") {
		t.Fatalf("an unknown key should name the valid ones, got %v", err)
	}
	s := start(t, Options{Command: "/bin/cat"})
	if err := s.Keys([]string{"Ctrl-D"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Ctrl-D did not end cat")
	}
}

func TestInterruptStopsASleepingShell(t *testing.T) {
	s := start(t, sh(`trap 'echo got-int; exit 0' INT; echo ready; while :; do sleep 0.05; done`))
	waitFor(t, "ready", func() bool { return strings.Contains(s.Screen(5, false), "ready") })
	if err := s.Interrupt(); err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	if !strings.Contains(s.Screen(10, false), "got-int") {
		t.Fatalf("screen = %q", s.Screen(10, false))
	}
}

func TestCwdAndEnv(t *testing.T) {
	dir := t.TempDir()
	s := start(t, Options{Command: "/bin/sh", Args: []string{"-c", `pwd; echo "$FOO $TERM"`}, Cwd: dir, Env: []string{"PATH=/usr/bin:/bin", "FOO=bar", "TERM=dumb"}})
	<-s.Done()
	screen := s.Screen(10, false)
	real, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(screen, real) || !strings.Contains(screen, "bar xterm-256color") {
		t.Fatalf("screen = %q", screen)
	}
}

func TestManagerFindsBareCommandsOnTheSessionPath(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "mytool")
	_ = os.WriteFile(script, []byte("#!/bin/sh\necho found-it\n"), 0o755)
	m := NewManager(func() []string { return []string{"PATH=" + dir + ":/usr/bin:/bin"} })
	s, err := m.Start("a", Options{Command: "mytool"})
	if err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	if !strings.Contains(s.Screen(5, false), "found-it") {
		t.Fatalf("screen = %q", s.Screen(5, false))
	}
	if _, err := m.Start("b", Options{Command: "no-such-tool-xyz"}); err == nil || !strings.Contains(err.Error(), "session.command") {
		t.Fatalf("a missing command should point at session.command, got %v", err)
	}
}

func TestRingKeepsTheNewestBytesAndCountsAll(t *testing.T) {
	r := newRing(8)
	r.write([]byte("abcdef"))
	if string(r.bytes()) != "abcdef" || r.wrapped() {
		t.Fatalf("before wrap: %q wrapped=%v", r.bytes(), r.wrapped())
	}
	r.write([]byte("ghij"))
	if string(r.bytes()) != "cdefghij" || !r.wrapped() || r.total != 10 {
		t.Fatalf("after wrap: %q wrapped=%v total=%d", r.bytes(), r.wrapped(), r.total)
	}
	r.write([]byte("0123456789XY")) // longer than the ring
	if string(r.bytes()) != "456789XY" || r.total != 22 {
		t.Fatalf("after a long write: %q total=%d", r.bytes(), r.total)
	}
}

func TestScrollbackReplayRestoresModesAfterTheRingWrapped(t *testing.T) {
	s := start(t, sh(`printf '\033[?2004h\033[?1049h\033[?25l'; head -c 30 /dev/zero | tr '\0' x; sleep 5`))
	waitFor(t, "output", func() bool { _, end := s.Scrollback(); return end > 30 })
	s.mu.Lock()
	s.ring = newRing(16) // shrink so the early sequences fall out
	s.ring.write([]byte("tail-of-output!!"))
	s.ring.write([]byte("more"))
	s.mu.Unlock()
	data, _ := s.Scrollback()
	if !strings.HasPrefix(string(data), "\x1b[?25l\x1b[?2004h") {
		t.Fatalf("replay should open with the tracked modes, got %q", data)
	}
	if strings.Contains(string(data), "1049") {
		t.Fatal("the alternate-screen switch must not be replayed")
	}
}

func TestDecModesAcrossSplitChunks(t *testing.T) {
	d := newDecModes()
	d.feed([]byte("abc\x1b[?20"))
	d.feed([]byte("04h and \x1b[?25"))
	d.feed([]byte(";1000l"))
	if !d.enabled(2004) || d.enabled(25) || d.enabled(1000) {
		t.Fatalf("modes = %v", d.modes)
	}
	if string(d.preamble()) != "\x1b[?25l\x1b[?1000l\x1b[?2004h" {
		t.Fatalf("preamble = %q", d.preamble())
	}
	d.feed([]byte("\x1b[?2004l"))
	if d.enabled(2004) {
		t.Fatal("2004 should be off")
	}
}

func TestOnDataGetsEveryChunkWithOffsets(t *testing.T) {
	var mu sync.Mutex
	var got []byte
	var last int64
	s := start(t, Options{Command: "/bin/sh", Args: []string{"-c", "printf abc; printf def"}, OnData: func(d []byte, end int64) {
		mu.Lock()
		got = append(got, d...)
		last = end
		mu.Unlock()
	}})
	<-s.Done()
	mu.Lock()
	defer mu.Unlock()
	data, end := s.Scrollback()
	if string(got) != string(data) || last != end || !strings.HasPrefix(string(got), "abcdef") {
		t.Fatalf("OnData saw %q (end %d); Scrollback is %q (end %d)", got, last, data, end)
	}
}
