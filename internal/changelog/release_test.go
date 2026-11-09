package changelog

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func sh(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append([]string{"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"}, env...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func must(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	out, err := sh(t, dir, env, name, args...)
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return out
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
}

// releaseRepo builds a throwaway repository with the real release scripts, a stubbed gh and build
// step, and a local bare remote, so make release can run end to end without publishing anything.
func releaseRepo(t *testing.T, buildFails bool) (repo string, env []string, ghLog string) {
	t.Helper()
	for _, tool := range []string{"git", "bash", "awk"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " is not installed")
		}
	}
	root := t.TempDir()
	repo, remote, bin := filepath.Join(root, "repo"), filepath.Join(root, "remote.git"), filepath.Join(root, "bin")
	must(t, root, nil, "git", "init", "-q", "--bare", "-b", "main", remote)
	must(t, root, nil, "git", "init", "-q", "-b", "main", repo)
	for _, f := range []string{"release.sh", "changelog.sh"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "scripts", f))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(repo, "scripts", f), string(b))
	}
	build := "#!/usr/bin/env bash\nmkdir -p build/release\necho zip > build/release/Hadron-darwin-arm64.zip\n"
	if buildFails {
		build = "#!/usr/bin/env bash\necho 'build exploded' >&2\nexit 1\n"
	}
	write(t, filepath.Join(repo, "scripts", "build-release.sh"), build)
	write(t, filepath.Join(repo, "release.conf"), "APP_NAME=Hadron\nREPO=acme/hadron\nGH_ACCOUNT=acme\nBRANCH=main\nAPP_PATH=build/bin/Hadron.app\nTEST_CMD='true'\n")
	write(t, filepath.Join(repo, "CHANGELOG.md"), "# Changelog\n\n## [Unreleased]\n\n## [0.1.0] - 2026-01-01\n\n### Added\n- First.\n")
	write(t, filepath.Join(repo, ".gitignore"), "build/\n")
	ghLog = filepath.Join(root, "gh.log")
	write(t, filepath.Join(bin, "gh"), `#!/usr/bin/env bash
case "$1 $2" in
  "auth token") echo token ;;
  "release view") exit 1 ;;
  "release create")
    echo "$@" >> "$GH_LOG"
    while [ $# -gt 0 ]; do [ "$1" = "--notes-file" ] && cp "$2" "$GH_LOG.notes"; shift; done ;;
esac
`)
	env = []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "GH_LOG=" + ghLog}
	must(t, repo, nil, "git", "add", ".")
	must(t, repo, nil, "git", "commit", "-q", "-m", "init")
	must(t, repo, nil, "git", "tag", "-a", "v0.1.0", "-m", "v0.1.0")
	must(t, repo, nil, "git", "remote", "add", "origin", remote)
	must(t, repo, nil, "git", "push", "-q", "origin", "main", "--tags")
	return repo, env, ghLog
}

func TestReleaseIsRefusedWithoutChangelogEntries(t *testing.T) {
	repo, env, ghLog := releaseRepo(t, false)
	out, err := sh(t, repo, env, "bash", "scripts/release.sh", "v0.2.0")
	if err == nil || !strings.Contains(out, "no entries under [Unreleased]") || !strings.Contains(out, "CHANGELOG.md is not ready for v0.2.0") {
		t.Fatalf("a release with nothing in [Unreleased] must fail loudly: err=%v\n%s", err, out)
	}
	if tags := must(t, repo, nil, "git", "tag", "-l", "v0.2.0"); strings.TrimSpace(tags) != "" {
		t.Error("no tag may be made")
	}
	if _, err := os.Stat(ghLog); err == nil {
		t.Error("nothing may be published")
	}
	if st := must(t, repo, nil, "git", "status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Errorf("the tree stays clean: %s", st)
	}
}

func TestReleasePromotesTheChangelogAndUsesItAsTheNotes(t *testing.T) {
	repo, env, ghLog := releaseRepo(t, false)
	cl := filepath.Join(repo, "CHANGELOG.md")
	write(t, cl, "# Changelog\n\n## [Unreleased]\n\n### Added\n- Shiny new feature.\n\n### Fixed\n- A crash.\n\n## [0.1.0] - 2026-01-01\n\n### Added\n- First.\n")
	must(t, repo, nil, "git", "commit", "-qam", "docs: changelog")
	out, err := sh(t, repo, env, "bash", "scripts/release.sh", "v0.2.0")
	if err != nil {
		t.Fatalf("release failed: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(cl)
	text := string(b)
	if !strings.Contains(text, "## [Unreleased]\n\n## [0.2.0] - ") || !strings.Contains(text, "- Shiny new feature.") {
		t.Fatalf("[Unreleased] must move under the new version, leaving an empty one:\n%s", text)
	}
	if strings.Index(text, "## [0.2.0]") > strings.Index(text, "## [0.1.0]") {
		t.Error("newest first")
	}
	if last := strings.TrimSpace(must(t, repo, nil, "git", "log", "-1", "--format=%s")); last != "docs: changelog for v0.2.0" {
		t.Errorf("last commit = %q", last)
	}
	if tags := must(t, repo, nil, "git", "tag", "-l", "v0.2.0"); strings.TrimSpace(tags) != "v0.2.0" {
		t.Error("tag missing")
	}
	notes, err := os.ReadFile(ghLog + ".notes")
	if err != nil {
		t.Fatalf("no release notes were given to gh: %v", err)
	}
	if !strings.Contains(string(notes), "- Shiny new feature.") || !strings.Contains(string(notes), "- A crash.") || strings.Contains(string(notes), "First.") || strings.Contains(string(notes), "Unreleased") {
		t.Errorf("the notes are the version's section:\n%s", notes)
	}
	if calls, _ := os.ReadFile(ghLog); strings.Contains(string(calls), "--generate-notes") {
		t.Errorf("generated notes would bury the changelog: %s", calls)
	}
	// The pushed commit carries the changelog.
	if remote := must(t, repo, nil, "git", "ls-remote", "origin", "refs/tags/v0.2.0"); strings.TrimSpace(remote) == "" {
		t.Error("the tag was not pushed")
	}
}

func TestAFailedBuildPutsTheChangelogBack(t *testing.T) {
	repo, env, _ := releaseRepo(t, true)
	cl := filepath.Join(repo, "CHANGELOG.md")
	write(t, cl, "# Changelog\n\n## [Unreleased]\n\n### Added\n- Something.\n\n## [0.1.0] - 2026-01-01\n\n### Added\n- First.\n")
	must(t, repo, nil, "git", "commit", "-qam", "docs: changelog")
	before, _ := os.ReadFile(cl)
	out, err := sh(t, repo, env, "bash", "scripts/release.sh", "v0.2.0")
	if err == nil || !strings.Contains(out, "build exploded") {
		t.Fatalf("expected the build failure: %v\n%s", err, out)
	}
	after, _ := os.ReadFile(cl)
	if string(after) != string(before) {
		t.Errorf("CHANGELOG.md was not restored:\n%s", after)
	}
	if st := must(t, repo, nil, "git", "status", "--porcelain"); strings.TrimSpace(st) != "" {
		t.Errorf("the tree is dirty after a failed release: %s", st)
	}
	if tags := must(t, repo, nil, "git", "tag", "-l", "v0.2.0"); strings.TrimSpace(tags) != "" {
		t.Error("no tag after a failed build")
	}
}
