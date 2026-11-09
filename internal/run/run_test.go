package run

import (
	"path/filepath"
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
	t.Setenv("HADRON_HOME", "")
	t.Setenv("HOME", "/tmp/somebody")
	if got, want := Home(), "/tmp/somebody/.hadron"; got != want {
		t.Fatalf("Home() = %q, want %q", got, want)
	}
}
