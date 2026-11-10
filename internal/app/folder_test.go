package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFolderStartOpensAtTheTypedPathOrItsNearestParent(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "Projects", "acme")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "notes.txt")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]string{
		"":                           home,
		"  ":                         home,
		"~":                          home,
		"~/Projects/acme":            proj,
		"~/Projects/acme/":           proj,
		proj:                         proj,
		"~/Projects/acme/new/deeper": proj,
		"~/Projects/beta":            filepath.Join(home, "Projects"),
		"relative/path":              home,
		file:                         home,
	} {
		if got := folderStart(in, home); got != want {
			t.Errorf("folderStart(%q) = %q, want %q", in, got, want)
		}
	}
}
