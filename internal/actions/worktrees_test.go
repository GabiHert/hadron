package actions

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// worktreeWorkspace makes a workspace whose root is a temp git repo with one commit, and a launch
// profile "wt" with worktree on.
func worktreeWorkspace(t *testing.T, h *sessionHarness, git bool) (domain.Workspace, string) {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	if git {
		gitIn(t, root, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("hi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitIn(t, root, "add", ".")
		gitIn(t, root, "commit", "-q", "-m", "init")
	}
	var w domain.Workspace
	h.do("workspace_create", m{"name": "W", "root": root}, &w)
	h.do("launch_profile_create", m{"workspace_id": w.ID, "name": "wt", "worktree": true}, nil)
	return w, root
}

func TestSessionStartInAWorktreeCreatesAndReusesIt(t *testing.T) {
	h := newSessionHarness(t, false)
	w, root := worktreeWorkspace(t, h, true)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "launch_profile": "wt", "start": false}, &card)

	var st SessionStatus
	h.do("session_start", m{"card": card.ID}, &st)
	want := filepath.Join(root, ".hadron-worktrees", "billing-fix")
	waitUntil(t, "the session to start", func() bool { return h.file(".pwd") != "" })
	pwd, _ := filepath.EvalSymlinks(strings.TrimSpace(h.file(".pwd")))
	if pwd != want {
		t.Fatalf("the session ran in %s, want %s", pwd, want)
	}
	if st.Session == nil || st.Session.Cwd != want || len(st.Warnings) != 0 {
		t.Errorf("status = %+v", st)
	}
	if got := gitIn(t, want, "rev-parse", "--abbrev-ref", "HEAD"); got != "hadron/billing-fix" {
		t.Errorf("branch = %s", got)
	}
	if status := gitIn(t, root, "status", "--porcelain"); status != "" {
		t.Errorf("the worktree folder must not dirty the root: %q", status)
	}

	// A restart reuses the same tree.
	if err := os.WriteFile(filepath.Join(want, "work.txt"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.do("session_restart", m{"card": card.ID}, nil)
	waitUntil(t, "the restart", func() bool { return h.latest(w.ID, card.ID).ID != st.Session.ID })
	if _, err := os.Stat(filepath.Join(want, "work.txt")); err != nil {
		t.Error("the existing worktree was not reused")
	}
	if got := h.latest(w.ID, card.ID).Cwd; got != want {
		t.Errorf("cwd after restart = %s", got)
	}
}

func TestWorktreeFlagOutsideAGitRepoWarnsAndStartsInTheRoot(t *testing.T) {
	h := newSessionHarness(t, false)
	w, root := worktreeWorkspace(t, h, false)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "plain", "launch_profile": "wt", "start": false}, &card)
	var st SessionStatus
	h.do("session_start", m{"card": card.ID}, &st)
	if len(st.Warnings) != 1 || !strings.Contains(st.Warnings[0], "not a git repository") {
		t.Fatalf("warnings = %v", st.Warnings)
	}
	if st.Session.Cwd != root {
		t.Errorf("cwd = %s, want the root %s", st.Session.Cwd, root)
	}
	if _, err := os.Stat(filepath.Join(root, ".hadron-worktrees")); err == nil {
		t.Error("no worktree folder outside a repo")
	}
}

func TestCardDeleteLeavesTheWorktreeAndWorktreeRemoveGuardsDirtyTrees(t *testing.T) {
	h := newSessionHarness(t, false)
	w, root := worktreeWorkspace(t, h, true)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "keeper", "launch_profile": "wt", "start": false}, &card)
	h.do("session_start", m{"card": card.ID}, nil)
	waitUntil(t, "the session", func() bool { return h.file(".pwd") != "" })
	tree := filepath.Join(root, ".hadron-worktrees", "keeper")

	var list []WorktreeInfo
	h.do("worktree_list", m{"workspace_id": w.ID}, &list)
	if len(list) != 1 || list[0].Nick != "keeper" || list[0].Branch != "hadron/keeper" || list[0].Dirty || list[0].CardID != card.ID {
		t.Fatalf("list = %+v", list)
	}

	h.fails("worktree_remove", m{"card": "keeper"}, "running session")
	h.do("session_stop", m{"card": card.ID}, nil)
	waitUntil(t, "the session to end", func() bool { return !h.status("keeper").Running })

	if err := os.WriteFile(filepath.Join(tree, "unsaved.txt"), []byte("work"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.do("worktree_list", m{"workspace_id": w.ID}, &list)
	if !list[0].Dirty {
		t.Error("an untracked file makes the tree dirty")
	}

	var del CardDeleteOut
	h.do("card_delete", m{"card_id": card.ID}, &del)
	if del.Worktree == nil || del.Worktree.Path != tree || !strings.Contains(del.Worktree.Note, "worktree_remove") {
		t.Fatalf("card_delete should report the worktree it left: %+v", del)
	}
	if del.Nick != "keeper" {
		t.Errorf("the response still carries the card: %+v", del.Card)
	}
	if _, err := os.Stat(filepath.Join(tree, "unsaved.txt")); err != nil {
		t.Fatal("card_delete must not touch the worktree")
	}
	h.do("worktree_list", m{"workspace_id": w.ID}, &list)
	if len(list) != 1 || list[0].CardID != "" {
		t.Errorf("a deleted card's worktree is still listed: %+v", list)
	}

	h.fails("worktree_remove", m{"card": "keeper"}, "uncommitted changes", "unsaved.txt", "force")
	if _, err := os.Stat(tree); err != nil {
		t.Fatal("a refused removal leaves the tree")
	}
	var out WorktreeRemoveOut
	h.do("worktree_remove", m{"card": "keeper", "force": true}, &out)
	if _, err := os.Stat(tree); err == nil {
		t.Error("force removes the tree")
	}
	if out.Branch != "hadron/keeper" || gitIn(t, root, "branch", "--list", "hadron/keeper") == "" {
		t.Errorf("the branch stays: %+v", out)
	}
	h.fails("worktree_remove", m{"card": "keeper"}, "no worktree")

	// A clean tree goes without force, and a new card with the same nick gets it back from the branch.
	var again domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "keeper", "launch_profile": "wt", "start": false}, &again)
	h.do("session_start", m{"card": again.ID}, nil)
	waitUntil(t, "the second session", func() bool { return strings.Contains(h.file(".pwd"), "keeper") })
	h.do("session_stop", m{"card": again.ID}, nil)
	waitUntil(t, "stopped", func() bool { return !h.status("keeper").Running })
	h.do("worktree_remove", m{"card": "keeper"}, nil)
}

func TestWorktreeListOutsideAGitRepoIsEmpty(t *testing.T) {
	h := newSessionHarness(t, false)
	w, _ := worktreeWorkspace(t, h, false)
	var list []WorktreeInfo
	h.do("worktree_list", m{"workspace_id": w.ID}, &list)
	if len(list) != 0 {
		t.Errorf("%+v", list)
	}
	h.fails("worktree_remove", m{"card": "x", "workspace_id": w.ID}, "not a git repository")
}
