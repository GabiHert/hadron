package pty

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestRingGrowsOnDemandAndKeepsTheTail(t *testing.T) {
	r := newRing(256 << 10)
	r.write([]byte("hello"))
	if len(r.buf) != ringMinGrow {
		t.Fatalf("a quiet ring holds %d bytes, want %d", len(r.buf), ringMinGrow)
	}
	var all []byte
	for i := 0; len(all) < 1<<20; i++ {
		line := []byte("line " + strconv.Itoa(i) + "\r\n")
		all = append(all, line...)
		r.write(line)
	}
	all = append([]byte("hello"), all...)
	if len(r.buf) != 256<<10 {
		t.Fatalf("a full ring holds %d bytes, want the cap", len(r.buf))
	}
	if got, want := r.bytes(), all[len(all)-256<<10:]; !bytes.Equal(got, want) {
		t.Fatal("the ring does not hold the newest bytes in order")
	}
	if !r.wrapped() || r.total != int64(len(all)) {
		t.Fatalf("wrapped=%v total=%d, want true %d", r.wrapped(), r.total, len(all))
	}
	// One write bigger than the cap keeps its tail.
	big := bytes.Repeat([]byte("x"), 300<<10)
	big[len(big)-1] = 'y'
	r.write(big)
	if got := r.bytes(); len(got) != 256<<10 || got[len(got)-1] != 'y' {
		t.Fatalf("after a huge write the ring holds %d bytes ending %q", len(got), got[len(got)-1])
	}
}

func TestEmptyRing(t *testing.T) {
	r := newRing(1024)
	if len(r.bytes()) != 0 || r.wrapped() || len(r.buf) != 0 {
		t.Fatal("a new ring must hold nothing and allocate nothing")
	}
}

func TestLiveEmulatorKeepsNoScrollbackButScreenRebuildsIt(t *testing.T) {
	s := start(t, sh(`seq 1 3000; sleep 5`))
	waitFor(t, "3000 lines", func() bool { return strings.HasSuffix(s.Screen(0, false), "3000") })
	s.mu.Lock()
	kept := s.emu.Scrollback().Len()
	s.mu.Unlock()
	if kept > liveScrollback {
		t.Fatalf("the live emulator keeps %d scrolled-off lines, want at most %d", kept, liveScrollback)
	}
	all := strings.Split(s.Screen(0, true), "\n")
	if len(all) != 3000 || all[0] != "1" || all[2999] != "3000" {
		t.Fatalf("with scrollback: %d lines, first %q, last %q", len(all), all[0], all[len(all)-1])
	}
	if got := s.Screen(2, true); got != "2999\n3000" {
		t.Fatalf("the last two lines with scrollback = %q", got)
	}
}
