package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain lets a copy of the test binary stand in for the app: with UPDATER_TEST_SLEEP set it only sleeps.
func TestMain(m *testing.M) {
	if os.Getenv("UPDATER_TEST_SLEEP") != "" {
		time.Sleep(5 * time.Second)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeApp makes an app bundle whose executable is a copy of the test binary, so ps shows its path.
func fakeApp(t *testing.T, dir, marker string) string {
	t.Helper()
	app := filepath.Join(dir, "Demo.app")
	mac := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(mac, 0o755); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	if out, err := exec.Command("/bin/cp", self, filepath.Join(mac, "Demo")).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
	_ = os.WriteFile(filepath.Join(app, "marker"), []byte(marker), 0o644)
	return app
}

func runInstall(t *testing.T, newApp, dest, open string) string {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "update.log")
	gone := exec.Command("/usr/bin/true")
	_ = gone.Run()
	err := startInstaller(gone.Process.Pid, newApp, dest, "Demo", logPath, []string{
		"UPDATER_OPEN=" + open, "UPDATER_QUIT_WAIT=5", "UPDATER_LAUNCH_WAIT=3", "UPDATER_LAUNCH_SETTLE=1"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(logPath)
		if s := string(b); strings.Contains(s, "install: done") || strings.Contains(s, "restored the previous version") {
			return s
		}
		time.Sleep(100 * time.Millisecond)
	}
	b, _ := os.ReadFile(logPath)
	t.Fatalf("install did not finish:\n%s", b)
	return ""
}

func writeOpen(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "open")
	if err := os.WriteFile(p, []byte("#!/bin/bash\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestInstallSwapsAndRemovesStagedApp(t *testing.T) {
	dest := fakeApp(t, t.TempDir(), "old")
	newApp := fakeApp(t, t.TempDir(), "new")
	// The fake open starts the installed executable, as Launch Services would.
	open := writeOpen(t, `UPDATER_TEST_SLEEP=1 "$1/Contents/MacOS/Demo" >/dev/null 2>&1 &`)
	log := runInstall(t, newApp, dest, open)
	if !strings.Contains(log, "install: done") {
		t.Fatalf("log:\n%s", log)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "marker")); string(b) != "new" {
		t.Errorf("installed marker = %q", b)
	}
	for _, p := range []string{newApp, dest + ".old", dest + ".new"} {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s is left", p)
		}
	}
}

func TestInstallRestoresWhenNewAppDoesNotStart(t *testing.T) {
	dest := fakeApp(t, t.TempDir(), "old")
	newApp := fakeApp(t, t.TempDir(), "new")
	log := runInstall(t, newApp, dest, writeOpen(t, "exit 0"))
	if !strings.Contains(log, "restored the previous version") {
		t.Fatalf("log:\n%s", log)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "marker")); string(b) != "old" {
		t.Errorf("installed marker = %q", b)
	}
	if _, err := os.Lstat(dest + ".old"); err == nil {
		t.Error(".old is left")
	}
}
