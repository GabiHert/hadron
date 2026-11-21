// Package menutest builds the fake claude that draws Claude Code's menus (fakemenu), for tests.
package menutest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

var (
	once  sync.Once
	built string
	err   error
)

// Build compiles fakemenu once per test binary and returns its path.
func Build(t testing.TB) string {
	t.Helper()
	once.Do(func() {
		_, here, _, _ := runtime.Caller(0)
		dir, e := os.MkdirTemp("", "hadron-fakemenu-")
		if e != nil {
			err = e
			return
		}
		built = filepath.Join(dir, "fakemenu")
		cmd := exec.Command("go", "build", "-o", built, "./fakemenu")
		cmd.Dir = filepath.Dir(here)
		if out, e := cmd.CombinedOutput(); e != nil {
			err = &buildError{out: string(out), err: e}
		}
	})
	if err != nil {
		t.Fatalf("building the fake menu claude: %v", err)
	}
	return built
}

type buildError struct {
	out string
	err error
}

func (e *buildError) Error() string { return e.err.Error() + ": " + e.out }
