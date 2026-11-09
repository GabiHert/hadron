package sessions

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/GabiHert/hadron/internal/store"
)

// ProcOps is how orphan handling looks at and stops processes. Tests replace it.
type ProcOps struct {
	// Inspect returns a process's parent pid and full command line; ok is false when no such
	// process exists.
	Inspect func(pid int) (ppid int, command string, ok bool)
	// Stop hangs up the process group, waits for the grace period and kills what is left. It
	// returns whether the process is gone.
	Stop func(pid int) bool
}

// RealProcOps inspects with ps and stops the way a normal stop does: SIGHUP to the process group,
// then SIGKILL after the grace period.
func RealProcOps() ProcOps {
	return ProcOps{
		Inspect: func(pid int) (int, string, bool) {
			out, err := exec.Command("ps", "-ww", "-o", "ppid=,command=", "-p", strconv.Itoa(pid)).Output()
			if err != nil {
				return 0, "", false
			}
			f := strings.Fields(strings.TrimSpace(string(out)))
			if len(f) < 2 {
				return 0, "", false
			}
			ppid, err := strconv.Atoi(f[0])
			if err != nil {
				return 0, "", false
			}
			return ppid, strings.Join(f[1:], " "), true
		},
		Stop: func(pid int) bool {
			_ = syscall.Kill(-pid, syscall.SIGHUP)
			_ = syscall.Kill(pid, syscall.SIGHUP)
			deadline := time.Now().Add(orphanGrace)
			for time.Now().Before(deadline) {
				if !store.ProcessAlive(pid) {
					return true
				}
				time.Sleep(50 * time.Millisecond)
			}
			_ = syscall.Kill(-pid, syscall.SIGKILL)
			_ = syscall.Kill(pid, syscall.SIGKILL)
			time.Sleep(200 * time.Millisecond)
			return !store.ProcessAlive(pid)
		},
	}
}

// orphanGrace is how long an orphan gets after SIGHUP, the same as a normal stop.
const orphanGrace = 2 * time.Second

// ReapOrphans deals with sessions the database still calls running whose pid is alive, at
// startup of the primary instance and before any restore. After a crash such a process is an
// orphan of the old Hadron; restoring its conversation beside it would put two claude processes
// on one transcript.
//
//   - A process of this Hadron (its parent is self) is left alone.
//   - A process that is that session's (its command line names the session's MCP config, or its
//     Claude session id) is stopped and its row marked exited. The restore flag stays, so the
//     restore resumes it under this Hadron.
//   - Any other process, the pid having been reused by another program, is left alone and the row
//     is marked exited.
//
// It returns how many orphans it stopped and how many stale rows it marked.
func ReapOrphans(st *store.Store, ops ProcOps, self int, logf func(msg string, kv ...any)) (stopped, marked int) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	rows, err := st.Sessions.ListRunning()
	if err != nil {
		logf("orphans: could not list running sessions", "error", err.Error())
		return
	}
	for _, x := range rows {
		if x.PID <= 0 || !store.ProcessAlive(x.PID) {
			continue // a dead row: MarkRunningExited handles it
		}
		ppid, cmd, ok := ops.Inspect(x.PID)
		switch {
		case !ok:
			continue // gone between the two checks
		case ppid == self:
			logf("orphans: session belongs to this Hadron, left alone", "session", x.ID, "pid", x.PID)
		case isSessionProcess(x.ID, x.ClaudeSessionID, cmd):
			if ops.Stop(x.PID) {
				_ = st.Sessions.MarkExited(x.ID)
				stopped++
				logf("orphans: stopped a session left running by an earlier Hadron", "session", x.ID, "pid", x.PID, "ppid", ppid)
			} else {
				_ = st.Sessions.SetRestore(x.ID, false)
				logf("orphans: could not stop the process; not restoring it", "session", x.ID, "pid", x.PID)
			}
		default:
			_ = st.Sessions.MarkExited(x.ID)
			marked++
			logf("orphans: the pid now belongs to another program; row marked exited", "session", x.ID, "pid", x.PID, "command", fmt.Sprint(truncate(cmd, 80)))
		}
	}
	return
}

// isSessionProcess says whether a command line is the session's claude: it names the session's
// MCP config file, or --session-id / --resume with its Claude session id.
func isSessionProcess(id, claudeID, cmd string) bool {
	if id != "" && strings.Contains(cmd, id+".mcp.json") {
		return true
	}
	return claudeID != "" && strings.Contains(cmd, claudeID)
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
