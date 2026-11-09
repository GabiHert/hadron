package run

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// EnsureMCPToken returns the MCP bearer token at path, creating it (mode 0600) when missing.
func EnsureMCPToken(path string) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		tok, err := ensureMCPToken(path)
		if !errors.Is(err, errRetry) {
			return tok, err
		}
	}
	return "", errors.New("could not create the MCP token: " + path + " keeps changing")
}

var errRetry = errors.New("retry")

func ensureMCPToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(b)); tok != "" {
			return tok, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	// O_EXCL so two starters never write different tokens; the loser reads the winner's.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		// Another starter won the race, or the file is empty: drop an empty one and go round again.
		if b, rerr := os.ReadFile(path); rerr == nil && strings.TrimSpace(string(b)) == "" {
			_ = os.Remove(path)
		}
		return "", errRetry
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(tok + "\n"); err != nil {
		return "", err
	}
	return tok, nil
}

// IsTestHome reports whether home is a scratch folder: under the system temp folder, or the process
// is a Go test binary. A test home must never start a real claude.
func IsTestHome(home string) bool {
	if underGoTest() {
		return true
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return p
	}
	h := resolve(home)
	for _, tmp := range []string{os.TempDir(), "/tmp", "/private/tmp", "/var/folders", "/private/var/folders"} {
		t := resolve(tmp)
		if h == t || strings.HasPrefix(h, t+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
