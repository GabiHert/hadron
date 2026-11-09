package run

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeHonoursHadronHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HADRON_HOME", dir)
	if Home() != dir {
		t.Fatalf("Home() = %q, want %q", Home(), dir)
	}
	if got, want := DBPath(), filepath.Join(dir, "hadron.db"); got != want {
		t.Fatalf("DBPath() = %q, want %q", got, want)
	}
}

func TestHomeDefaultsToDotHadron(t *testing.T) {
	t.Setenv("HOME", "/tmp/somebody")
	if got, want := DefaultHome(), "/tmp/somebody/.hadron"; got != want {
		t.Fatalf("DefaultHome() = %q, want %q", got, want)
	}
}

func TestATestThatResolvesHomeWithoutHadronHomePanics(t *testing.T) {
	t.Setenv("HADRON_HOME", "")
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), "HADRON_HOME") {
			t.Fatalf("recover = %v, want a panic naming HADRON_HOME", r)
		}
	}()
	Home()
}
