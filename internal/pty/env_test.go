package pty

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeShell(out string, err error) shellRunner {
	return func(ctx context.Context, shell string, args ...string) ([]byte, error) { return []byte(out), err }
}

func TestLoginShellEnvParsesTheMarkedOutputAndKeepsTheLastPath(t *testing.T) {
	out := "noise from .zshrc\n" + envMarker + "\nHOME=/h\x00PATH=/first\x00FOO=bar\x00PATH=/second:/bin\x00"
	env := loginShellEnvWith(fakeShell(out, nil), "/nonexistent-home", "/bin/zsh")
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "FOO=bar") || !strings.Contains(joined, "PATH=/second:/bin") || strings.Contains(joined, "PATH=/first") {
		t.Fatalf("env = %q", env)
	}
}

func TestLoginShellEnvFallsBackToOurOwnEnvironment(t *testing.T) {
	t.Setenv("HADRON_ENV_TEST", "yes")
	env := loginShellEnvWith(fakeShell("", errors.New("no shell")), "/nonexistent-home", "/bin/zsh")
	if !strings.Contains(strings.Join(env, "\n"), "HADRON_ENV_TEST=yes") {
		t.Fatal("expected os.Environ() as the fallback")
	}
}

func TestClaudeFolderIsPrependedWhenPathLacksIt(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := withClaudeOnPath([]string{"PATH=/usr/bin"}, home)
	if env[0] != "PATH="+bin+":/usr/bin" {
		t.Fatalf("env = %v", env)
	}
	if p, err := LookPath(env, "claude"); err != nil || p != filepath.Join(bin, "claude") {
		t.Fatalf("LookPath = %q, %v", p, err)
	}
	if _, err := LookPath([]string{"PATH=/usr/bin"}, "definitely-not-a-binary"); err == nil {
		t.Fatal("expected an error")
	}
}
