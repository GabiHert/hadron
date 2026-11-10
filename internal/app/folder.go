package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ChooseFolder opens the macOS folder picker and returns the chosen folder, or "" when the user
// cancels. It starts at start (~ expanded) or, when that does not exist yet, at its nearest
// existing parent, else at the home folder. The picker can make new folders. It is a window
// binding, not an action: a session has no window to show a picker in.
func (a *App) ChooseFolder(start string) (string, error) {
	ctx := a.context()
	if ctx == nil {
		return "", errors.New("the window is not ready yet; try again in a moment")
	}
	home, _ := os.UserHomeDir()
	return wruntime.OpenDirectoryDialog(ctx, wruntime.OpenDialogOptions{
		Title:                "Choose a folder",
		DefaultDirectory:     folderStart(start, home),
		CanCreateDirectories: true,
	})
}

// folderStart is where the picker opens for a typed path: the path itself, its nearest existing
// parent, or home.
func folderStart(start, home string) string {
	p := strings.TrimSpace(start)
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	if p == "" || !filepath.IsAbs(p) {
		return home
	}
	for p = filepath.Clean(p); ; p = filepath.Dir(p) {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
		if p == filepath.Dir(p) {
			return home
		}
	}
}
