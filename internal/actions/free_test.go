package actions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
)

func newFreeHarness(t *testing.T) *sessionHarness {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	h := newSessionHarness(t, false)
	h.svc.SetShellForTest("/bin/sh")
	return h
}

func (h *sessionHarness) freeList() []sessions.FreeStatus {
	var out FreeListOut
	h.do("free_session_list", m{}, &out)
	return out.Sessions
}

func (h *sessionHarness) free(id string) (sessions.FreeStatus, bool) {
	for _, f := range h.freeList() {
		if f.ID == id {
			return f, true
		}
	}
	return sessions.FreeStatus{}, false
}

func (h *sessionHarness) screen(id string) string {
	var out ScreenOut
	if err := h.try("free_session_read_screen", m{"session": id, "scrollback": true}, &out); err != nil {
		return ""
	}
	return out.Screen
}

func TestFreeShellRunsSendsReadsRenamesStopsAndCloses(t *testing.T) {
	h := newFreeHarness(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	var f sessions.FreeStatus
	h.do("free_session_start", m{"cwd": dir, "name": "build"}, &f)
	if f.Kind != domain.FreeShell || f.Command != "/bin/sh" || !f.Restore {
		t.Fatalf("started %+v, want a shell flagged to restore", f)
	}
	waitUntil(t, "the shell to run", func() bool { st, ok := h.free(f.ID); return ok && st.Running })

	h.do("free_session_send", m{"session": "build", "text": "echo free-$((40+2))"}, nil)
	waitUntil(t, "the echo on screen", func() bool { return strings.Contains(h.screen(f.ID), "free-42") })

	// The folder follows the shell, so a restore starts it where the user left it.
	h.do("free_session_send", m{"session": f.ID, "text": "cd sub && echo moved"}, nil)
	waitUntil(t, "the shell's folder to follow cd", func() bool {
		st, _ := h.free(f.ID)
		return sameDirQuiet(st.Cwd, sub)
	})

	h.do("free_session_update", m{"session": f.ID, "name": "tests"}, nil)
	if st, _ := h.free(f.ID); st.Name != "tests" {
		t.Fatalf("name = %q after the rename", st.Name)
	}
	h.fails("free_session_send", m{"session": "build", "text": "x"}, "no free session is called")

	h.do("free_session_stop", m{"session": f.ID}, nil)
	waitUntil(t, "the stopped shell to stay, ended", func() bool { st, ok := h.free(f.ID); return ok && !st.Running && st.Status == "exited" && !st.Restore })
	h.do("free_session_restart", m{"session": f.ID}, nil)
	waitUntil(t, "the restarted shell", func() bool { st, ok := h.free(f.ID); return ok && st.Running })
	h.do("free_session_send", m{"session": f.ID, "text": "pwd"}, nil)
	waitUntil(t, "the restart to start in the shell's last folder", func() bool { st, _ := h.free(f.ID); return sameDirQuiet(st.Cwd, sub) })
	if row, _ := h.reg.Deps().Store.FreeSessions.Get(f.ID); !sameDirQuiet(row.Cwd, sub) {
		t.Fatalf("stored folder %q, want %q; screen:\n%s", row.Cwd, sub, h.screen(f.ID))
	}

	h.do("free_session_stop", m{"session": f.ID, "close": true}, nil)
	if _, ok := h.free(f.ID); ok {
		t.Fatal("a closed free session is still listed")
	}
	if _, ok := h.svc.FreeLive(f.ID); ok {
		t.Fatal("a closed free session still runs")
	}
}

func sameDirQuiet(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

func TestAShellThatExitsCleanlyClosesItsTab(t *testing.T) {
	h := newFreeHarness(t)
	var f sessions.FreeStatus
	h.do("free_session_start", m{"cwd": t.TempDir()}, &f)
	waitUntil(t, "the shell to run", func() bool { st, ok := h.free(f.ID); return ok && st.Running })
	h.do("free_session_send", m{"session": f.ID, "text": "exit 0"}, nil)
	waitUntil(t, "the exited shell to close", func() bool { _, ok := h.free(f.ID); return !ok })
}

func TestFreeSessionsDefaultToTheLastFolder(t *testing.T) {
	h := newFreeHarness(t)
	dir := t.TempDir()
	h.do("free_session_start", m{"cwd": dir}, nil)
	var out FreeListOut
	h.do("free_session_list", m{}, &out)
	if out.DefaultCwd != dir {
		t.Fatalf("default_cwd = %q, want the last folder %q", out.DefaultCwd, dir)
	}
	var f sessions.FreeStatus
	h.do("free_session_start", m{}, &f)
	if f.Cwd != dir {
		t.Fatalf("a start with no folder ran in %q, want %q", f.Cwd, dir)
	}
	h.fails("free_session_start", m{"kind": "python"}, "use shell or claude")
	h.fails("free_session_start", m{"workspace_id": "nope"}, "does not exist")
}

func TestFreeClaudeHasNoHadronPromptAndResumesAfterARestart(t *testing.T) {
	h := newFreeHarness(t)
	w := h.workspace("Acme")
	var f sessions.FreeStatus
	h.do("free_session_start", m{"kind": "claude", "cwd": w.Root, "workspace_id": w.ID}, &f)
	waitUntil(t, "the fake claude", func() bool { return h.file(".started") != "" })
	argv := h.file(".argv")
	if argv != "--session-id\x00"+f.ClaudeSessionID+"\x00" || f.ClaudeSessionID == "" {
		t.Fatalf("argv = %q, want only --session-id <its id>", argv)
	}
	env := h.file(".env")
	if !strings.Contains(env, "TERM_PROGRAM=Hadron\n") || strings.Contains(env, "HADRON_SESSION_ID=") {
		t.Fatalf("env of a free claude:\n%s", env)
	}

	// Hadron quits: the session is meant to come back.
	h.svc.StopAll()
	waitUntil(t, "the claude to end", func() bool {
		row, err := h.reg.Deps().Store.FreeSessions.Get(f.ID)
		return err == nil && row.Status == "exited" && row.Restore
	})

	// The conversation now has a transcript, so the next Hadron resumes it.
	proj := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", sessions.EncodeProjectDir(w.Root))
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, f.ClaudeSessionID+".jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next := sessions.New(sessions.Config{Store: h.reg.Deps().Store, Home: h.home, Env: func() []string { return h.env }, Window: 150 * time.Millisecond})
	t.Cleanup(next.StopAll)
	_ = os.Remove(h.out + ".started")
	if n := next.RestoreFree(t.Context()); n != 1 {
		t.Fatalf("restored %d free sessions, want 1", n)
	}
	waitUntil(t, "the restored claude", func() bool { return h.file(".started") != "" })
	if got := h.file(".argv"); got != "--resume\x00"+f.ClaudeSessionID+"\x00" {
		t.Fatalf("restored argv = %q, want --resume <its id>", got)
	}
}

func TestMakeACardOfAFreeClaudeKeepsItsProcess(t *testing.T) {
	h := newFreeHarness(t)
	w := h.workspace("Acme")
	var f sessions.FreeStatus
	h.do("free_session_start", m{"kind": "claude", "cwd": w.Root}, &f)
	waitUntil(t, "the free claude to run", func() bool { st, ok := h.free(f.ID); return ok && st.Running })
	ps, _ := h.svc.FreeLive(f.ID)
	pid := ps.PID()

	var out FreeToCardOut
	h.do("free_session_to_card", m{"session": f.ID, "workspace_id": w.ID, "nick": "billing-fix", "key": "ACME-508"}, &out)
	if out.Card.Nick != "billing-fix" || out.Warning == "" {
		t.Fatalf("to_card = %+v", out)
	}
	if _, ok := h.free(f.ID); ok {
		t.Fatal("the free session is still in the Sessions space")
	}
	row := h.latest(w.ID, out.Card.ID)
	if row.ID != f.ID || row.ClaudeSessionID != f.ClaudeSessionID || row.Status != "running" || !row.Restore {
		t.Fatalf("card session row = %+v, want the free session's id, conversation and a running row", row)
	}
	st := h.status("billing-fix")
	if !st.Running || st.Session == nil || st.Session.ID != f.ID {
		t.Fatalf("card status = %+v, want it running on the same process", st)
	}
	if live, _ := h.svc.Live(f.ID); live.PID() != pid {
		t.Fatal("the process was replaced")
	}

	// The card's tools now reach the same process, and its exit reaches the card.
	h.do("session_send", m{"card": "billing-fix", "text": "hello card"}, nil)
	waitUntil(t, "the text in the same claude", func() bool { return strings.Contains(h.file(".in"), "hello card") })
	h.do("session_send", m{"card": "billing-fix", "text": "/exit"}, nil)
	waitUntil(t, "the card to see its session end", func() bool { return h.card(out.Card.ID).Liveness == domain.LivenessEnded })
}

func TestCardSessionsCannotUseFreeSessions(t *testing.T) {
	h := newFreeHarness(t)
	w := h.workspace("Acme")
	err := h.as(Actor{Kind: KindSession, Name: "billing-fix", WorkspaceID: w.ID}, "free_session_list", m{}, nil)
	if err == nil || !strings.Contains(err.Error(), "user's own terminals") {
		t.Fatalf("a card session listed free sessions: %v", err)
	}
}
