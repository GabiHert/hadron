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
