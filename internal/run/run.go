// Package run holds what every other package needs to know about this build and this machine:
// the version and the folders Hadron keeps its files in.
package run

import (
	"os"
	"path/filepath"
	"testing"
)

// Version is set at build time: -ldflags "-X github.com/GabiHert/hadron/internal/run.Version=v1.2.3".
// A build that is not cut from a tag reports "dev" and counts as older than every release.
var Version = "dev"

// Home is $HADRON_HOME, else ~/.hadron. Tests always set HADRON_HOME to a temp folder.
func Home() string {
	if v := os.Getenv("HADRON_HOME"); v != "" {
		return v
	}
	if testing.Testing() {
		panic("run.Home() was called in a test without HADRON_HOME: the test would touch the real ~/.hadron. Set HADRON_HOME to a temp dir (t.Setenv)")
	}
	return DefaultHome()
}

// DefaultHome is ~/.hadron, the data folder when HADRON_HOME is unset.
func DefaultHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".hadron"
	}
	return filepath.Join(home, ".hadron")
}

// DBPath is the SQLite database.
func DBPath() string { return filepath.Join(Home(), "hadron.db") }

// LogPath is the app log.
func LogPath() string { return filepath.Join(Home(), "hadron.log") }

// RunDir holds per-session files (MCP configs) that are recreated at every start.
func RunDir() string { return filepath.Join(Home(), "run") }

// UpdateDir is where the updater downloads and stages a new version.
func UpdateDir() string { return filepath.Join(Home(), "update") }

// UpdateLogPath is the install script's log from the last update.
func UpdateLogPath() string { return filepath.Join(Home(), "update.log") }

// MCPInfoPath records the port the MCP server listens on.
func MCPInfoPath() string { return filepath.Join(Home(), "mcp.json") }

// MCPTokenPath holds the MCP bearer token.
func MCPTokenPath() string { return filepath.Join(Home(), "mcp-token") }
