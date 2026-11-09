package run

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureMCPTokenCreatesOnceWithMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "mcp-token")
	a, err := EnsureMCPToken(path)
	if err != nil || len(a) < 32 {
		t.Fatalf("token = %q, %v", a, err)
	}
	b, _ := EnsureMCPToken(path)
	if a != b {
		t.Fatal("the token changed on the second call")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}
	// An empty file is replaced rather than used as a token.
	_ = os.WriteFile(path, []byte("\n"), 0o600)
	c, err := EnsureMCPToken(path)
	if err != nil || c == "" {
		t.Fatalf("an empty token file must be replaced, got %q, %v", c, err)
	}
}

func TestIsTestHome(t *testing.T) {
	if !IsTestHome(t.TempDir()) {
		t.Error("a temp dir is a test home")
	}
	if !IsTestHome("/Users/someone/.hadron") { // this process is a go test binary
		t.Error("under go test, every home is a test home")
	}
}
