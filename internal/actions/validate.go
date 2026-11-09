package actions

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
)

var (
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// DefaultColors are the workspace swatches, in the order a new workspace picks from.
var DefaultColors = []string{"#24408e", "#2f7a4b", "#b07a2a", "#b4322a", "#6b4fa0", "#4b5263"}

func checkName(what, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("%s name must not be empty", what)
	}
	if len([]rune(name)) > 60 {
		return "", fmt.Errorf("%s name is %d characters; keep it to 60 or fewer", what, len([]rune(name)))
	}
	return name, nil
}

func checkRole(role string) error {
	if role == "" {
		return nil
	}
	for _, r := range domain.Roles {
		if r == role {
			return nil
		}
	}
	return fmt.Errorf("role %q is not one of %s (or empty for no role)", role, strings.Join(domain.Roles, ", "))
}

func checkColor(color string) error {
	if color == "" || colorRe.MatchString(color) {
		return nil
	}
	return fmt.Errorf("color %q must look like #24408e", color)
}

func checkNick(nick string) error { return board.CheckNick(nick) }

// resolveFolder checks the folder a session runs in (the field named field): ~ is expanded, and
// the result must be an absolute path to a folder that exists. Empty stays empty, meaning the
// workspace root.
func resolveFolder(field, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	p := raw
	if home, err := os.UserHomeDir(); err == nil {
		switch {
		case p == "~":
			p = home
		case strings.HasPrefix(p, "~/"):
			p = filepath.Join(home, p[2:])
		}
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%s %q is not an absolute path. Start it with / or ~/, such as ~/code/acme-web", field, raw)
	}
	p = filepath.Clean(p)
	st, err := os.Stat(p)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s %s does not exist. Pass a folder that is there, or empty for the workspace root", field, p)
	case !st.IsDir():
		return "", fmt.Errorf("%s %s is a file, not a folder", field, p)
	}
	return p, nil
}

// storedFolder is what a card or planner keeps for a folder: empty when it is the workspace root,
// so the session follows the root if the root changes.
func storedFolder(w domain.Workspace, folder string) string {
	if folder == "" || filepath.Clean(folder) == filepath.Clean(w.Root) {
		return ""
	}
	a, aerr := filepath.EvalSymlinks(folder)
	b, berr := filepath.EvalSymlinks(w.Root)
	if aerr == nil && berr == nil && a == b {
		return ""
	}
	return folder
}

// joinErrs returns nil when no check failed, else every failure, one per line.
func joinErrs(errs ...error) error {
	var msgs []string
	for _, e := range errs {
		if e != nil {
			msgs = append(msgs, e.Error())
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return errors.New(strings.Join(msgs, "\n"))
}

// resolveRoot expands ~ and makes the root absolute. An empty root means the home folder. The
// folder need not exist yet, but if something is there it must be a folder.
func resolveRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	home, _ := os.UserHomeDir()
	switch {
	case root == "":
		return home, nil
	case root == "~":
		root = home
	case strings.HasPrefix(root, "~/"):
		root = filepath.Join(home, root[2:])
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("root folder %q is not a valid path", root)
	}
	if st, err := os.Stat(abs); err == nil && !st.IsDir() {
		return "", errors.New("root folder " + abs + " is a file, not a folder")
	}
	return abs, nil
}
