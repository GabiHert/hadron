package sessions

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// WorktreeDir is the folder under a workspace root that holds the card worktrees.
const WorktreeDir = ".hadron-worktrees"

// Worktree is one card worktree on disk.
type Worktree struct {
	Nick   string `json:"nick"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Dirty  bool   `json:"dirty"`
}

func git(env []string, dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if len(env) > 0 {
		cmd.Env = env
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// IsGitRepo says whether root is the top of (or inside) a git working tree.
func IsGitRepo(env []string, root string) bool {
	out, err := git(env, root, "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// WorktreePath is where a card's worktree lives.
func WorktreePath(root, nick string) string { return filepath.Join(root, WorktreeDir, nick) }

// EnsureWorktree creates the card's worktree on branch hadron/<nick>, or reuses it when it is
// already there. The folder is added to .git/info/exclude so the root stays clean.
func EnsureWorktree(env []string, root, nick string) (path string, created bool, err error) {
	path = WorktreePath(root, nick)
	if st, serr := os.Stat(filepath.Join(path, ".git")); serr == nil && st != nil {
		return path, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, err
	}
	if exclude, gerr := git(env, root, "rev-parse", "--git-path", "info/exclude"); gerr == nil {
		if !filepath.IsAbs(exclude) {
			exclude = filepath.Join(root, exclude)
		}
		if b, _ := os.ReadFile(exclude); !strings.Contains(string(b), WorktreeDir+"/") {
			_ = os.MkdirAll(filepath.Dir(exclude), 0o755)
			if f, ferr := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); ferr == nil {
				_, _ = f.WriteString("\n" + WorktreeDir + "/\n")
				f.Close()
			}
		}
	}
	branch := "hadron/" + nick
	if _, err := git(env, root, "worktree", "add", path, "-b", branch); err != nil {
		// The branch survives a removed worktree: check it out again instead of failing.
		if _, berr := git(env, root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); berr == nil {
			if _, err2 := git(env, root, "worktree", "add", path, branch); err2 != nil {
				return "", false, fmt.Errorf("git worktree add: %w", err2)
			}
			return path, true, nil
		}
		return "", false, fmt.Errorf("git worktree add: %w", err)
	}
	return path, true, nil
}

// Worktrees lists the card worktrees of a repo.
func Worktrees(env []string, root string) ([]Worktree, error) {
	out, err := git(env, root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	prefix := filepath.Join(root, WorktreeDir) + string(filepath.Separator)
	resolved, _ := filepath.EvalSymlinks(root)
	prefixResolved := filepath.Join(resolved, WorktreeDir) + string(filepath.Separator)
	list := []Worktree{}
	var cur Worktree
	flush := func() {
		if cur.Path != "" && (strings.HasPrefix(cur.Path, prefix) || strings.HasPrefix(cur.Path, prefixResolved)) {
			cur.Nick = filepath.Base(cur.Path)
			status, _ := git(env, cur.Path, "status", "--porcelain")
			cur.Dirty = status != ""
			list = append(list, cur)
		}
		cur = Worktree{}
	}
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return list, nil
}

// RemoveWorktree removes a card's worktree. A dirty tree is refused unless force is set. The
// branch stays, so committed work is never lost.
func RemoveWorktree(env []string, root, nick string, force bool) error {
	path := WorktreePath(root, nick)
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("there is no worktree for %q at %s", nick, path)
	}
	if !force {
		if status, _ := git(env, path, "status", "--porcelain"); status != "" {
			return fmt.Errorf("the worktree for %q has uncommitted changes:\n%s\nCommit or discard them, or pass force to remove it anyway", nick, status)
		}
	}
	args := []string{"worktree", "remove", path}
	if force {
		args = append(args, "--force")
	}
	_, err := git(env, root, args...)
	return err
}

// WorktreeSuffix keeps two workspaces that share a root folder from sharing worktrees. The oldest
// workspace on a root uses plain card nicknames; every later one adds "-<first 6 of its id>", so
// the same nickname in two workspaces gets two folders and two branches.
func WorktreeSuffix(st *store.Store, ws domain.Workspace) string {
	all, err := st.Workspaces.List(true)
	if err != nil {
		return ""
	}
	root := cleanRoot(ws.Root)
	for _, other := range all {
		if other.ID == ws.ID || cleanRoot(other.Root) != root {
			continue
		}
		if other.CreatedAt.Before(ws.CreatedAt) || (other.CreatedAt.Equal(ws.CreatedAt) && other.ID < ws.ID) {
			id := ws.ID
			if len(id) > 6 {
				id = id[:6]
			}
			return "-" + id
		}
	}
	return ""
}

// WorktreeName is the folder (and, after hadron/, the branch) of a card's worktree in ws.
func WorktreeName(st *store.Store, ws domain.Workspace, nick string) string {
	return nick + WorktreeSuffix(st, ws)
}

// SharesRoot lists the other workspaces that use the same root folder as ws.
func SharesRoot(st *store.Store, ws domain.Workspace) []domain.Workspace {
	all, err := st.Workspaces.List(true)
	if err != nil {
		return nil
	}
	var out []domain.Workspace
	for _, other := range all {
		if other.ID != ws.ID && cleanRoot(other.Root) == cleanRoot(ws.Root) {
			out = append(out, other)
		}
	}
	return out
}

func cleanRoot(root string) string {
	root = filepath.Clean(root)
	if r, err := filepath.EvalSymlinks(root); err == nil {
		return r
	}
	return root
}
