package sessions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

type orphanFixture struct {
	st      *store.Store
	stopped []int
	ops     ProcOps
}

// newOrphanFixture has an Acme workspace, and a fake process table: pid -> (ppid, command).
func newOrphanFixture(t *testing.T, table map[int][2]any) (*orphanFixture, domain.Workspace) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "hadron.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ws := domain.Workspace{Name: "Acme", Root: t.TempDir()}
	if err := st.Workspaces.Create(&ws); err != nil {
		t.Fatal(err)
	}
	f := &orphanFixture{st: st}
	f.ops = ProcOps{
		Inspect: func(pid int) (int, string, bool) {
			e, ok := table[pid]
			if !ok {
				return 0, "", false
			}
			return e[0].(int), e[1].(string), true
		},
		Stop: func(pid int) bool { f.stopped = append(f.stopped, pid); return true },
	}
	return f, ws
}

func (f *orphanFixture) running(t *testing.T, ws domain.Workspace, pid int, claudeID string) domain.Session {
	t.Helper()
	s := domain.Session{WorkspaceID: ws.ID, Kind: "planner", Command: "claude", PID: pid, Status: "running", Restore: true, ClaudeSessionID: claudeID}
	if err := f.st.Sessions.Create(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *orphanFixture) status(t *testing.T, id string) domain.Session {
	t.Helper()
	s, err := f.st.Sessions.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAConfirmedOrphanIsStoppedMarkedExitedAndRestoredOnce(t *testing.T) {
	self := os.Getpid() // a pid that is alive, standing in for the orphan
	f, ws := newOrphanFixture(t, nil)
	s := f.running(t, ws, self, "claude-abc")
	table := map[int][2]any{self: {1, "claude --mcp-config /h/run/" + s.ID + ".mcp.json --session-id claude-abc"}}
	f.ops.Inspect = func(pid int) (int, string, bool) { e, ok := table[pid]; return e[0].(int), e[1].(string), ok }

	stopped, marked := ReapOrphans(f.st, f.ops, 4242, nil)
	if stopped != 1 || marked != 0 || len(f.stopped) != 1 || f.stopped[0] != self {
		t.Fatalf("stopped %d, marked %d, stop calls %v", stopped, marked, f.stopped)
	}
	got := f.status(t, s.ID)
	if got.Status != "exited" || !got.Restore {
		t.Fatalf("after the reap: %+v; want exited with its restore flag kept", got)
	}
	rest, _ := f.st.Sessions.ListRestorable()
	if len(rest) != 1 || rest[0].ID != s.ID {
		t.Fatalf("restorable = %+v, want the session once", rest)
	}
	// Reaping again finds nothing to do.
	if stopped, marked := ReapOrphans(f.st, f.ops, 4242, nil); stopped+marked != 0 || len(f.stopped) != 1 {
		t.Fatalf("second reap: %d %d %v", stopped, marked, f.stopped)
	}
}

func TestAnOrphanIsAlsoRecognisedByItsClaudeSessionID(t *testing.T) {
	self := os.Getpid()
	f, ws := newOrphanFixture(t, map[int][2]any{self: {1, "claude --resume claude-xyz"}})
	f.running(t, ws, self, "claude-xyz")
	if stopped, _ := ReapOrphans(f.st, f.ops, 4242, nil); stopped != 1 {
		t.Fatalf("stopped = %d", stopped)
	}
}

func TestAReusedPidIsNotKilledAndItsRowIsMarkedExited(t *testing.T) {
	self := os.Getpid()
	f, ws := newOrphanFixture(t, map[int][2]any{self: {1, "/Applications/Music.app/Contents/MacOS/Music"}})
	s := f.running(t, ws, self, "claude-abc")
	stopped, marked := ReapOrphans(f.st, f.ops, 4242, nil)
	if stopped != 0 || marked != 1 || len(f.stopped) != 0 {
		t.Fatalf("stopped %d, marked %d, stop calls %v; another program must never be killed", stopped, marked, f.stopped)
	}
	if got := f.status(t, s.ID); got.Status != "exited" {
		t.Fatalf("row = %+v", got)
	}
}

func TestALiveChildOfThisHadronIsLeftAlone(t *testing.T) {
	self := os.Getpid()
	f, ws := newOrphanFixture(t, nil)
	s := f.running(t, ws, self, "")
	f.ops.Inspect = func(pid int) (int, string, bool) {
		return 4242, "claude --mcp-config /h/run/" + s.ID + ".mcp.json", true // parent is this Hadron (4242)
	}
	stopped, marked := ReapOrphans(f.st, f.ops, 4242, nil)
	if stopped+marked != 0 || len(f.stopped) != 0 {
		t.Fatalf("stopped %d, marked %d, stop calls %v", stopped, marked, f.stopped)
	}
	if got := f.status(t, s.ID); got.Status != "running" {
		t.Fatalf("row = %+v", got)
	}
}

func TestADeadPidIsLeftToMarkRunningExited(t *testing.T) {
	f, ws := newOrphanFixture(t, nil)
	f.running(t, ws, 2147483646, "")
	if stopped, marked := ReapOrphans(f.st, f.ops, 4242, nil); stopped+marked != 0 {
		t.Fatalf("stopped %d, marked %d", stopped, marked)
	}
}

func TestTheRealProcOpsInspectAndStopAProcessGroup(t *testing.T) {
	cmd := exec.Command("sleep", "61")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start sleep:", err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }() // reap, or a zombie still answers kill -0
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	ops := RealProcOps()
	ppid, command, ok := ops.Inspect(cmd.Process.Pid)
	if !ok || ppid != os.Getpid() || !strings.Contains(command, "sleep 61") {
		t.Fatalf("Inspect = %d, %q, %v", ppid, command, ok)
	}
	start := time.Now()
	if !ops.Stop(cmd.Process.Pid) {
		t.Fatal("Stop did not end the process")
	}
	<-done
	if time.Since(start) > 3*time.Second {
		t.Errorf("Stop took %v", time.Since(start))
	}
	if _, _, ok := ops.Inspect(cmd.Process.Pid); ok {
		t.Error("the process is still there")
	}
}
