package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

// noProcs inspects nothing: every live pid looks like a process of this Hadron.
var noProcs = sessions.ProcOps{
	Inspect: func(int) (int, string, bool) { return os.Getpid(), "claude", true },
	Stop:    func(int) bool { return false },
}

func seedRunning(t *testing.T, home string, pids ...int) []string {
	t.Helper()
	st, err := store.Open(filepath.Join(home, "hadron.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ws := domain.Workspace{Name: "Acme", Root: t.TempDir()}
	if err := st.Workspaces.Create(&ws); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, pid := range pids {
		s := domain.Session{WorkspaceID: ws.ID, Kind: "planner", Command: "claude", PID: pid, Status: "running", Restore: true}
		if err := st.Sessions.Create(&s); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	return ids
}

func statusOf(t *testing.T, home, id string) string {
	t.Helper()
	st, err := store.Open(filepath.Join(home, "hadron.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s, err := st.Sessions.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return s.Status
}

func TestASecondLaunchLeavesTheRunningSessionsAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HADRON_HOME", home)
	ids := seedRunning(t, home, os.Getpid(), os.Getpid())

	// The first launch is the primary instance and reconciles.
	st, lock, err := prepare(false, home)
	if err != nil || st == nil {
		t.Fatalf("primary prepare = %v, %v", st, err)
	}
	reconcile(st, noProcs, nil)
	defer func() { st.Close(); lock.Close() }()

	// A second launch, the way `wails build` runs the binary, opens nothing.
	st2, lock2, err := prepare(false, home)
	if err != nil || st2 != nil || lock2 != nil {
		t.Fatalf("second prepare = %v, %v, %v; want a shell", st2, lock2, err)
	}
	for _, id := range ids {
		if got := statusOf(t, home, id); got != "running" {
			t.Errorf("session %s is %q after a second launch, want running", id, got)
		}
	}
}

func TestReconcileMarksOnlyRowsWhoseProcessIsGone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HADRON_HOME", home)
	// pid 2147483646 does not exist; ours does.
	ids := seedRunning(t, home, os.Getpid(), 2147483646, 0)
	st, lock, err := prepare(false, home)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close(); lock.Close() }()
	reconcile(st, noProcs, nil)
	reconcile(st, noProcs, nil) // calling it twice is safe
	for i, want := range []string{"running", "exited", "exited"} {
		s, _ := st.Sessions.Get(ids[i])
		if s.Status != want {
			t.Errorf("row %d = %q, want %q", i, s.Status, want)
		}
	}
}

func TestBindingGenerationOpensNothingInTheDataFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HADRON_HOME", home)
	sentinel := filepath.Join(home, "hadron.db")
	want := []byte("not a database: a binding-generation run must not open, migrate or replace this")
	if err := os.WriteFile(sentinel, want, 0o600); err != nil {
		t.Fatal(err)
	}
	st, lock, err := prepare(true, home)
	if err != nil || st != nil || lock != nil {
		t.Fatalf("prepare(bindings) = %v, %v, %v", st, lock, err)
	}
	got, _ := os.ReadFile(sentinel)
	if !bytes.Equal(got, want) {
		t.Error("the sentinel database was modified")
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 1 {
		t.Errorf("the data folder gained files: %v", entries)
	}
}

func TestOnlyOneProcessIsPrimary(t *testing.T) {
	home := t.TempDir()
	a, err := lockPrimary(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockPrimary(home); err != errNotPrimary {
		t.Fatalf("second lock = %v, want errNotPrimary", err)
	}
	a.Close()
	b, err := lockPrimary(home)
	if err != nil {
		t.Fatalf("after the first exits: %v", err)
	}
	b.Close()
}
