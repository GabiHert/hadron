package pty

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/charmbracelet/x/ansi"
)

// parserCap reads the size of an emulator parser's data buffer, the way trimParser reaches it.
func parserCap(t *testing.T, s *Session) int {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	f := reflect.ValueOf(s.emu).Elem().FieldByName("parser")
	p := *(**ansi.Parser)(unsafe.Pointer(f.UnsafeAddr()))
	d := reflect.ValueOf(p).Elem().FieldByName("data")
	return d.Cap()
}

// vt gives every emulator a 4 MB parser buffer; Hadron keeps 64 KB, and a vt update that moves the
// field must not silently bring the 4 MB back.
func TestEmulatorParserIsTrimmed(t *testing.T) {
	s := start(t, sh(`printf '\033]0;billing-fix\007\033]8;;https://example.com\033\\link\033]8;;\033\\ ok\n'; sleep 5`))
	if !trimParser(newEmulator(80, 24, 0)) {
		t.Fatal("trimParser could not reach vt's parser: check vt's Emulator for a renamed field")
	}
	if c := parserCap(t, s); c != parserData {
		t.Fatalf("the live emulator's parser holds %d bytes, want %d", c, parserData)
	}
	// Titles and hyperlinks still go through.
	waitFor(t, "the line after the OSC sequences", func() bool { return strings.Contains(s.Screen(0, false), "link ok") })
}

// A read of the last 60 lines with scrollback rebuilds the screen with 60 lines of history, not
// 5000: it used to allocate about 67 MB each time for a full 2 MB ring.
func TestScreenWithScrollbackOnlyBuildsTheLinesAsked(t *testing.T) {
	if rebuildScrollback(60) != 60 || rebuildScrollback(0) != emuScrollback || rebuildScrollback(99999) != emuScrollback {
		t.Fatal("rebuildScrollback does not follow the request")
	}
	s := start(t, sh(`seq 1 20000 | sed 's/$/ build output from the billing reconcile job, long enough to look like Claude/'; sleep 10`))
	waitFor(t, "20000 lines", func() bool { return strings.Contains(s.Screen(0, false), "20000 build output") })
	got := strings.Split(s.Screen(60, true), "\n")
	if len(got) != 60 || !strings.HasPrefix(got[59], "20000 ") || !strings.HasPrefix(got[0], "19941 ") {
		t.Fatalf("Screen(60, true) = %d lines, %q … %q", len(got), got[0], got[len(got)-1])
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range 5 {
		_ = s.Screen(60, true)
	}
	runtime.ReadMemStats(&after)
	per := (after.TotalAlloc - before.TotalAlloc) / 5
	t.Logf("one 60-line read with scrollback allocates %.1f MB", float64(per)/(1<<20))
	if per > 24<<20 {
		t.Fatalf("one 60-line read with scrollback allocated %d MB; it should stay well under the 67 MB of a 5000-line rebuild", per>>20)
	}
	// Asking for everything still gives the long history.
	if all := strings.Split(s.Screen(0, true), "\n"); len(all) < emuScrollback {
		t.Fatalf("Screen(0, true) gave %d lines, want at least %d", len(all), emuScrollback)
	}
}

func TestRingTailIsTheNewestBytes(t *testing.T) {
	r := newRing(1000)
	var all []byte
	for i := 0; len(all) < 5000; i++ {
		b := []byte(strings.Repeat(string(rune('a'+i%26)), 37))
		all = append(all, b...)
		r.write(b)
	}
	held := all[len(all)-1000:]
	for _, n := range []int{1, 10, 999, 1000, 5000} {
		want := held[max(0, len(held)-n):]
		if got := r.tail(n); string(got) != string(want) {
			t.Fatalf("tail(%d) = %d bytes, not the newest", n, len(got))
		}
	}
}
