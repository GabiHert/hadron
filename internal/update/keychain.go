package update

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Keychain keeps the GitHub token in the login keychain as a generic password: service is the
// app's update service name, account is the repo. The token never goes on a command line, where
// other processes could read it: it is written through security's interactive mode on stdin.
type Keychain struct {
	Service string
	Bin     string // /usr/bin/security; tests replace it
}

// tokenRE matches GitHub tokens (classic, fine-grained and app tokens). It also keeps the token
// safe to place in a security(1) command line read from stdin.
var tokenRE = regexp.MustCompile(`^[A-Za-z0-9_]{20,255}$`)

func (k Keychain) bin() string {
	if k.Bin != "" {
		return k.Bin
	}
	return "/usr/bin/security"
}

// Get returns the saved token for repo, or "" when there is none.
func (k Keychain) Get(ctx context.Context, repo string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, k.bin(), "find-generic-password", "-s", k.Service, "-a", repo, "-w").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// Set saves token for repo, replacing any saved one.
func (k Keychain) Set(ctx context.Context, repo, token string) error {
	token = strings.TrimSpace(token)
	if !tokenRE.MatchString(token) {
		return errors.New("That doesn't look like a GitHub token. Paste the token that starts with github_pat_.")
	}
	if !safeArg(repo) || !safeArg(k.Service) {
		return fmt.Errorf("Cannot save a token for %q.", repo)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, k.bin(), "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %q -a %q -l %q -w %s\n",
		k.Service, repo, k.Service+" ("+repo+")", token))
	if out, err := cmd.CombinedOutput(); err != nil || strings.Contains(string(out), "error") {
		return errors.New("macOS did not save the token in your keychain.")
	}
	if k.Get(ctx, repo) != token {
		return errors.New("macOS did not save the token in your keychain.")
	}
	return nil
}

// Delete removes the saved token for repo; a missing token is not an error.
func (k Keychain) Delete(ctx context.Context, repo string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, k.bin(), "delete-generic-password", "-s", k.Service, "-a", repo).Run()
	if k.Get(ctx, repo) != "" {
		return errors.New("macOS did not remove the token from your keychain.")
	}
	return nil
}

// safeArg allows the characters repo and service names use, so %q quoting is enough for security.
func safeArg(s string) bool {
	return s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._/ -") == ""
}
