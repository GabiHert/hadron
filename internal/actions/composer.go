package actions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/GabiHert/hadron/internal/complete"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
)

// ComposerTargetIn names the session whose folder the chat composer completes against: a card,
// or the workspace's planner.
type ComposerTargetIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Card        string `json:"card,omitempty" jsonschema:"the card (id, nickname or key); leave out and pass planner: true for the planner"`
	Planner     bool   `json:"planner,omitempty" jsonschema:"the workspace's planner instead of a card"`
}

// FilesSearchIn is the input of files_search.
type FilesSearchIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Card        string `json:"card,omitempty" jsonschema:"the card (id, nickname or key); leave out and pass planner: true for the planner"`
	Planner     bool   `json:"planner,omitempty" jsonschema:"the workspace's planner instead of a card"`
	Q           string `json:"q,omitempty" jsonschema:"what to look for, matched fuzzily against the path relative to the folder; empty lists the top level first"`
	Limit       int    `json:"limit,omitempty" jsonschema:"the most results (default 50, at most 200)"`
}

// files is shared by every files_search call, so a listing is reused for a few seconds.
var files = complete.NewFiles()

func registerComposer(r *Registry) {
	Register(r, Spec{Name: "session_commands_list", Description: "List the slash commands a card's session or the planner can run, for the chat composer's / menu: Claude Code's built-ins, the project's .claude/commands/**/*.md (a subfolder makes ns:cmd), the user's ~/.claude/commands, and user-invocable skills (.claude/skills/*/SKILL.md) of both. Each has a name (without the slash), a description, a source (built-in, project or user) and a kind (command or skill). The project folder is the card's working folder (its worktree when it runs in one, else its cwd, else the workspace root), or the planner's. Read-only."},
		func(ctx context.Context, c *Call, in ComposerTargetIn) ([]complete.Command, error) {
			dir, err := composerFolder(c, in, "session_commands_list")
			if err != nil {
				return nil, err
			}
			return complete.Commands(dir, sessions.ClaudeConfigDir()), nil
		})

	Register(r, Spec{Name: "files_search", Description: "Search the files and folders of a card's working folder (or the planner's) for the chat composer's @ menu, fuzzily, best first. In a git repository it lists the tracked files and the untracked ones git does not ignore; elsewhere it walks the folder, skipping node_modules, .git and build folders, up to 20,000 entries. Listings are cached for a few seconds. Paths are relative to the folder; a query with a leading / or ~, or with .., is refused, and symlinks that lead out of the folder are never listed. Read-only."},
		func(ctx context.Context, c *Call, in FilesSearchIn) ([]complete.File, error) {
			dir, err := composerFolder(c, ComposerTargetIn{WorkspaceID: in.WorkspaceID, Card: in.Card, Planner: in.Planner}, "files_search")
			if err != nil {
				return nil, err
			}
			return files.Search(dir, in.Q, in.Limit)
		})
}

// composerFolder is the folder a card's session or the planner works in: sessions.Folder, or the
// card's worktree when its latest session ran in one that is still there.
func composerFolder(c *Call, in ComposerTargetIn, what string) (string, error) {
	if (in.Card == "") == !in.Planner {
		return "", errors.New("pass card (an id, nickname or key) or planner: true, not both")
	}
	var card *domain.Card
	var w domain.Workspace
	if in.Card != "" {
		cd, err := cardByRef(c, in.Card)
		if err != nil {
			return "", err
		}
		if in.WorkspaceID != "" && cd.WorkspaceID != in.WorkspaceID {
			return "", errors.New("that card belongs to another workspace than workspace_id")
		}
		card = &cd
		if w, err = c.Workspace(cd.WorkspaceID); err != nil {
			return "", err
		}
	} else {
		var err error
		if w, err = c.Workspace(in.WorkspaceID); err != nil {
			return "", err
		}
	}
	if err := mayControl(c, w.ID, card, false, what); err != nil {
		return "", err
	}
	base := sessions.Folder(w, card)
	if card == nil {
		return base, nil
	}
	// A card that runs in a worktree works in <folder>/.hadron-worktrees/<nick>; a session row
	// from before the card's folder changed is not followed.
	if row, err := c.Store.Sessions.Latest(w.ID, card.ID); err == nil && row.Cwd != "" {
		wt := filepath.Join(base, sessions.WorktreeDir)
		if rel, err := filepath.Rel(wt, row.Cwd); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			if st, err := os.Stat(row.Cwd); err == nil && st.IsDir() {
				return row.Cwd, nil
			}
		}
	}
	return base, nil
}
