package complete

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func byName(cmds []Command) map[string]Command {
	out := map[string]Command{}
	for _, c := range cmds {
		out[c.Name] = c
	}
	return out
}

func TestCommandsFindBuiltinsProjectUserAndSkills(t *testing.T) {
	proj, cfg := t.TempDir(), t.TempDir()
	write(t, filepath.Join(proj, ".claude/commands/deploy.md"), "---\ndescription: Ship the current branch to staging\nallowed-tools: Bash\n---\nRun the deploy.\n")
	write(t, filepath.Join(proj, ".claude/commands/frontend/component.md"), "---\ndescription: \"Scaffold a \\\"component\\\"\"\n---\nbody\n")
	write(t, filepath.Join(proj, ".claude/commands/notes.md"), "# Take release notes\n\nMore text.\n")
	write(t, filepath.Join(proj, ".claude/commands/README.txt"), "not a command")
	write(t, filepath.Join(proj, ".claude/skills/billing-audit/SKILL.md"), "---\nname: billing-audit\ndescription: >\n  Audit the invoices\n  of last month\n---\n")
	write(t, filepath.Join(proj, ".claude/skills/internal-only/SKILL.md"), "---\nname: internal-only\ndescription: Claude only\nuser-invocable: false\n---\n")
	write(t, filepath.Join(cfg, "commands/standup.md"), "---\ndescription: 'Write today''s standup'\n---\n")
	write(t, filepath.Join(cfg, "commands/deploy.md"), "---\ndescription: the user's deploy\n---\n")
	write(t, filepath.Join(cfg, "skills/weekly/SKILL.md"), "---\ndescription: Weekly report # a comment\n---\n")

	got := Commands(proj, cfg)
	names := byName(got)
	want := map[string]Command{
		"deploy":             {Name: "deploy", Description: "Ship the current branch to staging", Source: SourceProject, Kind: KindCommand},
		"frontend:component": {Name: "frontend:component", Description: `Scaffold a "component"`, Source: SourceProject, Kind: KindCommand},
		"notes":              {Name: "notes", Description: "Take release notes", Source: SourceProject, Kind: KindCommand},
		"billing-audit":      {Name: "billing-audit", Description: "Audit the invoices of last month", Source: SourceProject, Kind: KindSkill},
		"standup":            {Name: "standup", Description: "Write today's standup", Source: SourceUser, Kind: KindCommand},
		"weekly":             {Name: "weekly", Description: "Weekly report", Source: SourceUser, Kind: KindSkill},
		"compact":            {Name: "compact", Description: "Compact the conversation, with optional focus instructions", Source: SourceBuiltin, Kind: KindCommand},
	}
	for name, w := range want {
		if names[name] != w {
			t.Errorf("%s = %+v, want %+v", name, names[name], w)
		}
	}
	if _, ok := names["internal-only"]; ok {
		t.Error("a skill with user-invocable: false is Claude's alone")
	}
	if _, ok := names["README"]; ok {
		t.Error("only .md files are commands")
	}
	// The project's deploy shadows the user's, and project entries come before user and built-ins.
	idx := map[string]int{}
	for i, c := range got {
		if _, dup := idx[c.Name]; dup {
			t.Fatalf("%s is listed twice", c.Name)
		}
		idx[c.Name] = i
	}
	if !(idx["deploy"] < idx["standup"] && idx["standup"] < idx["compact"]) {
		t.Errorf("order = %v", got)
	}
	for _, b := range []string{"model", "clear", "compact", "cost", "status", "context", "resume", "memory", "agents", "mcp", "permissions", "review", "init", "help", "exit", "effort"} {
		if c, ok := names[b]; !ok || c.Source != SourceBuiltin || c.Description == "" {
			t.Errorf("built-in /%s = %+v", b, c)
		}
	}
}

func TestCommandsWithNoFoldersAreTheBuiltins(t *testing.T) {
	got := Commands("", "")
	if len(got) != len(Builtins) {
		t.Fatalf("got %d, want %d built-ins", len(got), len(Builtins))
	}
	got = Commands(filepath.Join(t.TempDir(), "missing"), filepath.Join(t.TempDir(), "missing"))
	if len(got) != len(Builtins) {
		t.Fatalf("missing folders add nothing: %d", len(got))
	}
}

func TestCommandsSkipSymlinksThatLeaveTheFolder(t *testing.T) {
	proj, outside := t.TempDir(), t.TempDir()
	write(t, filepath.Join(outside, "secret.md"), "---\ndescription: outside\n---\n")
	write(t, filepath.Join(outside, "skill/SKILL.md"), "---\ndescription: outside skill\n---\n")
	write(t, filepath.Join(proj, ".claude/commands/inside.md"), "---\ndescription: inside\n---\n")
	if err := os.Symlink(filepath.Join(outside, "secret.md"), filepath.Join(proj, ".claude/commands/secret.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(proj, ".claude/commands/linked")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(proj, ".claude/skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "skill"), filepath.Join(proj, ".claude/skills/skill")); err != nil {
		t.Fatal(err)
	}
	names := byName(Commands(proj, ""))
	if _, ok := names["inside"]; !ok {
		t.Fatal("the plain command is listed")
	}
	for _, n := range []string{"secret", "linked:secret", "skill"} {
		if _, ok := names[n]; ok {
			t.Errorf("%s points outside the folder and must not be read", n)
		}
	}
}

func paths(fs []File) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Path
		if f.IsDir {
			out[i] += "/"
		}
	}
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-qm", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
}

func TestSearchAGitRepoListsTrackedAndUntrackedButNotIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, ".gitignore"), "secret.env\nbuild/\n")
	write(t, filepath.Join(root, "src/billing/invoice.go"), "package billing")
	write(t, filepath.Join(root, "README.md"), "# Acme")
	gitInit(t, root)
	write(t, filepath.Join(root, "src/billing/new_draft.go"), "untracked")
	write(t, filepath.Join(root, "secret.env"), "ignored")
	write(t, filepath.Join(root, "build/out.bin"), "ignored")

	f := NewFiles()
	all, err := f.Search(root, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	got := paths(all)
	for _, want := range []string{"src/", "src/billing/", "src/billing/invoice.go", "src/billing/new_draft.go", "README.md", ".gitignore"} {
		if !has(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	for _, not := range []string{"secret.env", "build/out.bin", "build/", ".git/"} {
		if has(got, not) {
			t.Errorf("%s is ignored but listed", not)
		}
	}
	if got[0] != "src/" && got[0] != ".gitignore" && got[0] != "README.md" {
		t.Errorf("an empty query lists the top level first: %v", got)
	}

	hits, err := f.Search(root, "invoice", 10)
	if err != nil || len(hits) == 0 || hits[0].Path != "src/billing/invoice.go" {
		t.Fatalf("invoice = %v, %v", paths(hits), err)
	}
	hits, _ = f.Search(root, "sbi", 10)
	if len(hits) == 0 || !strings.HasPrefix(hits[0].Path, "src/billing") {
		t.Fatalf("a fuzzy query matches in order: %v", paths(hits))
	}
	hits, _ = f.Search(root, "billing", 10)
	if len(hits) == 0 || hits[0].Path != "src/billing" || !hits[0].IsDir {
		t.Fatalf("a folder whose name matches comes first: %v", paths(hits))
	}
	if hits, _ := f.Search(root, "zzz", 10); len(hits) != 0 {
		t.Fatalf("no match: %v", paths(hits))
	}
}

func TestSearchAPlainFolderSkipsDependencyAndBuildFolders(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "notes/plan.md"), "plan")
	write(t, filepath.Join(root, "node_modules/left-pad/index.js"), "x")
	write(t, filepath.Join(root, "dist/app.js"), "x")
	write(t, filepath.Join(root, ".git/HEAD"), "x")
	write(t, filepath.Join(root, "my docs/spec draft.md"), "x")
	got, err := NewFiles().Search(root, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	ps := paths(got)
	for _, want := range []string{"notes/", "notes/plan.md", "my docs/", "my docs/spec draft.md"} {
		if !has(ps, want) {
			t.Errorf("missing %s in %v", want, ps)
		}
	}
	for _, p := range ps {
		if strings.HasPrefix(p, "node_modules") || strings.HasPrefix(p, "dist") || strings.HasPrefix(p, ".git") {
			t.Errorf("%s should be skipped", p)
		}
	}
}

func TestSearchRefusesPathsOutsideAndNeverFollowsSymlinksOut(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	write(t, filepath.Join(outside, "passwords.txt"), "x")
	write(t, filepath.Join(outside, "deep/more.txt"), "x")
	write(t, filepath.Join(root, "ok.txt"), "x")
	if err := os.Symlink(filepath.Join(outside, "passwords.txt"), filepath.Join(root, "link-out.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dir-out")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "ok.txt"), filepath.Join(root, "link-in.txt")); err != nil {
		t.Fatal(err)
	}
	f := NewFiles()
	for _, q := range []string{"../", "../outside", "/etc/passwd", "~/secrets", "a/../../b"} {
		if _, err := f.Search(root, q, 10); !errors.Is(err, ErrOutside) {
			t.Errorf("%q: err = %v, want ErrOutside", q, err)
		}
	}
	got, err := f.Search(root, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	ps := paths(got)
	if !has(ps, "ok.txt") || !has(ps, "link-in.txt") {
		t.Errorf("files and links inside are listed: %v", ps)
	}
	for _, p := range ps {
		if strings.Contains(p, "passwords") || strings.Contains(p, "more.txt") || strings.HasPrefix(p, "link-out") || strings.HasPrefix(p, "dir-out") {
			t.Errorf("%s leads outside the folder", p)
		}
	}
	if _, err := f.Search(filepath.Join(root, "missing"), "", 10); err == nil {
		t.Error("a missing folder is an error")
	}
}

func TestSearchCapsResultsAndEntriesAndCachesBriefly(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 300; i++ {
		write(t, filepath.Join(root, fmt.Sprintf("f%03d.txt", i)), "x")
	}
	f := NewFiles()
	now := time.Unix(1000, 0)
	f.Now = func() time.Time { return now }
	got, _ := f.Search(root, "", 0)
	if len(got) != DefaultLimit {
		t.Errorf("default limit: %d", len(got))
	}
	got, _ = f.Search(root, "", 10000)
	if len(got) != MaxLimit {
		t.Errorf("max limit: %d", len(got))
	}
	write(t, filepath.Join(root, "zz-new.txt"), "x")
	if got, _ := f.Search(root, "zz-new", 5); len(got) != 0 {
		t.Errorf("a listing is reused for a few seconds: %v", paths(got))
	}
	now = now.Add(cacheFor + time.Second)
	if got, _ := f.Search(root, "zz-new", 5); len(got) != 1 {
		t.Errorf("then read again: %v", paths(got))
	}
}

func TestWalkStopsAtMaxEntries(t *testing.T) {
	root := t.TempDir()
	for d := 0; d < 21; d++ {
		dir := filepath.Join(root, fmt.Sprintf("d%02d", d))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 1000; i++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%04d", i)), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := len(walkList(root)); n != MaxEntries {
		t.Fatalf("walk listed %d entries, want the cap %d", n, MaxEntries)
	}
}

func TestScore(t *testing.T) {
	if _, ok := Score("xyz", "src/app.go"); ok {
		t.Error("no match")
	}
	a, _ := Score("app", "src/app.go")
	b, _ := Score("app", "apps/other/thing.go")
	if a <= b {
		t.Errorf("a basename match (%d) beats a folder match (%d)", a, b)
	}
	c, _ := Score("sag", "src/app.go")
	d, _ := Score("sag", "docs/messages/again.md")
	if c <= 0 || d <= 0 {
		t.Errorf("fuzzy scores: %d %d", c, d)
	}
}
