package actions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
)

// realDir is a new temp folder with symlinks resolved, the path Claude records as a cwd.
func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (h *sessionHarness) pwd() string {
	p, _ := filepath.EvalSymlinks(strings.TrimSpace(h.file(".pwd")))
	return p
}

func TestAdoptFromAnotherFolderResumesThere(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	other := realDir(t) // such as ~/code/acme-web, outside the workspace root
	id := "88888888-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, other, id, "Ship the landing page", 3000, time.Hour)

	// Without cwd the transcript is looked up under the root, and is not there.
	h.fails("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "landing"}, "has no transcript", "all: true")

	var out AdoptOut
	h.do("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "landing", "cwd": other}, &out)
	if out.Card == nil || out.Card.Cwd != other {
		t.Fatalf("the card keeps the folder: %+v", out.Card)
	}
	waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })
	if h.pwd() != other {
		t.Errorf("the session ran in %s, want %s", h.pwd(), other)
	}
	if !strings.Contains(h.file(".argv"), "--resume\x00"+id) {
		t.Errorf("argv = %q", h.file(".argv"))
	}
	if got := h.latest(w.ID, out.Card.ID); got.Cwd != other || got.ClaudeSessionID != id {
		t.Errorf("session row = %+v", got)
	}
	if c := h.card(out.Card.ID); c.Cwd != other {
		t.Errorf("card_get cwd = %q", c.Cwd)
	}

	// A restart resumes it in the same folder.
	h.do("session_restart", m{"card": "landing"}, nil)
	waitUntil(t, "the restart", func() bool { return strings.Count(h.file(".history"), "--resume "+id) == 2 })
	if h.pwd() != other {
		t.Errorf("restart ran in %s", h.pwd())
	}
}

func TestAdoptFromAnotherFolderIsRefusedWithoutItsTranscript(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	other := realDir(t)
	writeConversation(t, cfg, w.Root, "99999999-aaaa-bbbb-cccc-000000000001", "In the root", 3000, time.Hour)
	// The conversation exists, but in the root, not in the folder given.
	h.fails("session_adopt", m{"workspace_id": w.ID, "claude_session_id": "99999999-aaaa-bbbb-cccc-000000000001", "nick": "elsewhere", "cwd": other}, "has no transcript", other)
	h.fails("planner_adopt", m{"workspace_id": w.ID, "claude_session_id": "99999999-aaaa-bbbb-cccc-000000000001", "cwd": other}, "has no transcript")
	if n := len(h.cards(m{"workspace_id": w.ID})); n != 0 {
		t.Errorf("a refused adopt leaves no card: %d", n)
	}
	var ws domain.Workspace
	h.do("workspace_get", m{"workspace_id": w.ID}, &ws)
	if ws.PlannerCwd != "" {
		t.Errorf("a refused planner adopt keeps the planner folder: %q", ws.PlannerCwd)
	}
}

func TestPlannerAdoptWithCwdKeepsThePlannerInThatFolder(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	other := realDir(t)
	id := "aaaaaaaa-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, other, id, "Plan the quarter", 3000, time.Hour)
	var out AdoptOut
	h.do("planner_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "cwd": other, "handoff": false}, &out)
	if out.Status.Session == nil || out.Status.Session.Cwd != other {
		t.Fatalf("out = %+v", out.Status.Session)
	}
	waitUntil(t, "the planner", func() bool { return h.file(".started") != "" })
	if h.pwd() != other || !strings.Contains(h.file(".argv"), "--resume\x00"+id) {
		t.Fatalf("pwd %s argv %q", h.pwd(), h.file(".argv"))
	}
	var ws domain.Workspace
	h.do("workspace_get", m{"workspace_id": w.ID}, &ws)
	if ws.PlannerCwd != other {
		t.Fatalf("planner_cwd = %q", ws.PlannerCwd)
	}
	// planner_restart uses the stored folder.
	h.do("planner_restart", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "the restart", func() bool { return strings.Count(h.file(".history"), "--resume "+id) == 2 })
	if h.pwd() != other {
		t.Errorf("planner restart ran in %s", h.pwd())
	}
	// Setting it back to empty goes back to the root.
	h.do("workspace_update", m{"workspace_id": w.ID, "planner_cwd": ""}, &ws)
	if ws.PlannerCwd != "" {
		t.Errorf("planner_cwd = %q", ws.PlannerCwd)
	}
}

func TestRestoreBringsACardBackInItsFolder(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	other := realDir(t)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "acme-web", "cwd": other}, &card)
	if card.Cwd != other {
		t.Fatalf("card_create cwd = %q", card.Cwd)
	}
	waitUntil(t, "the start", func() bool { return len(h.history()) == 1 && h.file(".pwd") != "" })
	if h.pwd() != other {
		t.Fatalf("the session ran in %s, want %s", h.pwd(), other)
	}
	row := h.latest(w.ID, card.ID)
	writeConversation(t, cfg, other, row.ClaudeSessionID, "hello", 3000, time.Minute)
	waitUntil(t, "running", func() bool { return len(h.svc.LiveSessions()) == 1 })

	svc := h.relaunch(t)
	_ = os.Remove(h.out + ".pwd")
	if n := svc.RestoreAll(context.Background(), 0); n != 1 {
		t.Fatalf("restored %d", n)
	}
	waitUntil(t, "the restore", func() bool { return len(h.history()) == 2 && h.file(".pwd") != "" })
	if got := h.history()[1]; got != "card --resume "+row.ClaudeSessionID {
		t.Errorf("restore = %q (the transcript is found under the card's folder)", got)
	}
	if h.pwd() != other {
		t.Errorf("the restore ran in %s, want %s", h.pwd(), other)
	}
}

func TestCardUpdateCwdAppliesAtTheNextStart(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	other := realDir(t)
	var created domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix"}, &created)
	waitUntil(t, "the start", func() bool { return h.file(".started") != "" })
	waitUntil(t, "running", func() bool { return len(h.svc.LiveSessions()) == 1 })

	var out CardUpdateOut
	h.do("card_update", m{"card_id": created.ID, "cwd": other}, &out)
	if out.Cwd != other || !strings.Contains(out.Note, "applies when it next starts") {
		t.Fatalf("out = %+v", out)
	}
	h.do("session_restart", m{"card": "billing-fix"}, nil)
	waitUntil(t, "the restart", func() bool { return h.pwd() == other })

	// The root itself is stored as empty, and empty goes back to the root.
	h.do("card_update", m{"card_id": out.ID, "cwd": w.Root}, &out)
	if out.Cwd != "" {
		t.Errorf("the root is stored as empty: %q", out.Cwd)
	}
}

func TestFolderValidationReportsEveryError(t *testing.T) {
	h, w, _ := adoptHarness(t)
	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.fails("session_adopt", m{"workspace_id": w.ID, "claude_session_id": " ", "nick": "Bad Nick", "cwd": "code/acme-web"}, "kebab-case", "not an absolute path", "claude_session_id is required")
	h.fails("card_create", m{"workspace_id": w.ID, "nick": "Bad Nick", "cwd": "/no/such/acme-web", "start": false}, "kebab-case", "does not exist")
	h.fails("card_create", m{"workspace_id": w.ID, "nick": "ok-nick", "cwd": file, "start": false}, "is a file")
	var c domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "ok-nick", "start": false}, &c)
	h.fails("card_update", m{"card_id": c.ID, "nick": "Bad Nick", "cwd": "relative"}, "kebab-case", "not an absolute path")
	h.fails("planner_adopt", m{"workspace_id": w.ID, "claude_session_id": " ", "cwd": "relative"}, "not an absolute path", "claude_session_id is required")
	h.fails("workspace_update", m{"workspace_id": w.ID, "planner_cwd": "/no/such/folder"}, "does not exist")
	h.fails("session_adoptable_list", m{"workspace_id": w.ID, "cwd": "/no/such/folder"}, "does not exist")
	h.fails("session_adoptable_list", m{"workspace_id": w.ID, "cwd": w.Root, "all": true}, "not both")

	// ~ is expanded.
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "code", "acme-web"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.do("card_create", m{"workspace_id": w.ID, "nick": "tilde", "cwd": "~/code/acme-web", "start": false}, &c)
	if c.Cwd != filepath.Join(home, "code", "acme-web") {
		t.Errorf("cwd = %q", c.Cwd)
	}
}

func TestAdoptableListByFolderAndAllReadsTheRealCwd(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	web, api := realDir(t), realDir(t)
	writeConversation(t, cfg, w.Root, "bbbbbbbb-aaaa-bbbb-cccc-000000000001", "In the root", 3000, 3*time.Hour)
	writeConversation(t, cfg, web, "bbbbbbbb-aaaa-bbbb-cccc-000000000002", "In the web app", 3000, 2*time.Hour)
	writeConversation(t, cfg, api, "bbbbbbbb-aaaa-bbbb-cccc-000000000003", "In the api", 3000, time.Hour)
	writeConversation(t, cfg, api, "bbbbbbbb-aaaa-bbbb-cccc-000000000004", "tiny", 0, time.Minute) // under 2 KB
	// A folder whose encoded name would decode wrongly: the cwd comes from the transcript.
	dotted := filepath.Join(realDir(t), "acme.web_v2")
	if err := os.MkdirAll(dotted, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConversation(t, cfg, dotted, "bbbbbbbb-aaaa-bbbb-cccc-000000000005", "Dotted", 3000, 30*time.Minute)
	// A transcript with no cwd at all is left out of the all list.
	bare := filepath.Join(cfg, "projects", "-somewhere-unknown")
	_ = os.MkdirAll(bare, 0o755)
	_ = os.WriteFile(filepath.Join(bare, "bbbbbbbb-aaaa-bbbb-cccc-000000000006.jsonl"), []byte(`{"type":"user","message":{"content":"x"}}`+"\n"+strings.Repeat(`{"type":"assistant"}`+"\n", 200)), 0o644)

	var list []AdoptableOut
	h.do("session_adoptable_list", m{"workspace_id": w.ID, "cwd": web}, &list)
	if len(list) != 1 || list[0].Cwd != web || list[0].FirstUserMessage != "In the web app" {
		t.Fatalf("by folder = %+v", list)
	}
	h.do("session_adoptable_list", m{"workspace_id": w.ID}, &list)
	root, _ := filepath.EvalSymlinks(w.Root)
	if len(list) != 1 || list[0].Cwd != root {
		t.Fatalf("default lists the root only: %+v", list)
	}

	h.do("session_adoptable_list", m{"workspace_id": w.ID, "all": true}, &list)
	var got []string
	for _, a := range list {
		got = append(got, a.Cwd)
	}
	want := []string{dotted, api, web, root} // newest first
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("all cwds:\n got %q\nwant %q", got, want)
	}

	// held_by looks at every workspace.
	h.do("session_adopt", m{"workspace_id": w.ID, "claude_session_id": "bbbbbbbb-aaaa-bbbb-cccc-000000000003", "nick": "api", "cwd": api, "start": false}, nil)
	other := h.workspace("Beta")
	h.do("session_adoptable_list", m{"workspace_id": other.ID, "all": true}, &list)
	for _, a := range list {
		if strings.HasSuffix(a.ClaudeSessionID, "003") {
			if !a.AlreadyInHadron || a.HeldBy != `card api of workspace "W"` {
				t.Errorf("held_by = %+v", a)
			}
		} else if a.AlreadyInHadron {
			t.Errorf("not held: %+v", a)
		}
	}
	h.fails("session_adopt", m{"workspace_id": other.ID, "claude_session_id": "bbbbbbbb-aaaa-bbbb-cccc-000000000003", "nick": "again", "cwd": api}, "already in Hadron", `workspace "W"`)
}

func TestAWorktreeProfileBranchesFromTheCardsFolder(t *testing.T) {
	h := newSessionHarness(t, false)
	w, root := worktreeWorkspace(t, h, false) // the root is not a repository
	repo := realDir(t)
	gitIn(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("acme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "commit", "-q", "-m", "init")

	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "landing", "launch_profile": "wt", "cwd": repo}, &card)
	want := filepath.Join(repo, sessions.WorktreeDir, "landing")
	waitUntil(t, "the start", func() bool { return h.file(".pwd") != "" })
	if h.pwd() != want {
		t.Fatalf("ran in %s, want %s", h.pwd(), want)
	}
	if _, err := os.Stat(filepath.Join(root, sessions.WorktreeDir)); err == nil {
		t.Error("no worktree is made under the workspace root")
	}
	h.do("session_stop", m{"card": "landing"}, nil)
	waitUntil(t, "the stop", func() bool { return len(h.svc.LiveSessions()) == 0 })
	var rm WorktreeRemoveOut
	h.do("worktree_remove", m{"workspace_id": w.ID, "card": "landing"}, &rm)
	if rm.Removed != want {
		t.Errorf("removed %s", rm.Removed)
	}
}
