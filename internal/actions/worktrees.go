package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
)

// WorktreeListIn is the input of worktree_list.
type WorktreeListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// WorktreeInfo is one card worktree.
type WorktreeInfo struct {
	sessions.Worktree
	CardID string `json:"card_id" jsonschema:"the card it belongs to; empty when the card was deleted"`
}

// WorktreeRemoveIn is the input of worktree_remove.
type WorktreeRemoveIn struct {
	Card        string `json:"card" jsonschema:"the card (id, nickname or key), or the nickname of a deleted card whose worktree was left on disk"`
	Force       bool   `json:"force,omitempty" jsonschema:"remove it even with uncommitted changes. They are lost; the branch keeps what was committed"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// WorktreeRemoveOut is the result of worktree_remove.
type WorktreeRemoveOut struct {
	Removed string `json:"removed"`
	Branch  string `json:"branch"`
	Note    string `json:"note"`
}

func gitEnv(c *Call) []string {
	if c.Deps.Sessions != nil {
		return c.Deps.Sessions.Env()
	}
	return os.Environ()
}

func registerWorktrees(r *Registry) {
	Register(r, Spec{Name: "worktree_list", Description: "List the git worktrees Hadron made for cards (launch profiles with worktree on), with their branch and whether they have uncommitted changes. Worktrees of deleted cards are listed too: card_delete leaves them on disk."},
		func(ctx context.Context, c *Call, in WorktreeListIn) ([]WorktreeInfo, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			env := gitEnv(c)
			if !sessions.IsGitRepo(env, w.Root) {
				return []WorktreeInfo{}, nil
			}
			list, err := sessions.Worktrees(env, w.Root)
			if err != nil {
				return nil, err
			}
			// Workspaces on the same root keep their worktrees side by side; list only this one's.
			suffix := sessions.WorktreeSuffix(c.Store, w)
			var others []string
			for _, o := range sessions.SharesRoot(c.Store, w) {
				if s := sessions.WorktreeSuffix(c.Store, o); s != "" {
					others = append(others, s)
				}
			}
			out := []WorktreeInfo{}
			for _, wt := range list {
				if suffix != "" && !strings.HasSuffix(wt.Nick, suffix) {
					continue
				}
				if suffix == "" && hasAnySuffix(wt.Nick, others) {
					continue
				}
				wt.Nick = strings.TrimSuffix(wt.Nick, suffix)
				info := WorktreeInfo{Worktree: wt}
				if card, err := c.Store.Cards.ByNick(w.ID, wt.Nick); err == nil {
					info.CardID = card.ID
				}
				out = append(out, info)
			}
			return out, nil
		})

	Register(r, Spec{Name: "worktree_remove", Description: "Remove a card's git worktree. Refused while the tree has uncommitted changes (unless force is true) or while the card's session is running. The branch hadron/<nick> stays, so committed work is kept.",
		Mutates: true, Entities: []string{"cards"}},
		func(ctx context.Context, c *Call, in WorktreeRemoveIn) (WorktreeRemoveOut, error) {
			if err := userOrPlanner(c, "remove worktrees"); err != nil {
				return WorktreeRemoveOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return WorktreeRemoveOut{}, err
			}
			nick := in.Card
			var card *domain.Card
			if cd, err := c.Store.Cards.ByNick(w.ID, in.Card); err == nil {
				card, nick = &cd, cd.Nick
			} else if cd, err := c.Store.Cards.Get(in.Card); err == nil && cd.WorkspaceID == w.ID {
				card, nick = &cd, cd.Nick
			}
			if card != nil {
				if t, err := target(c, w.ID, card); err == nil && t.Live != nil {
					return WorktreeRemoveOut{}, fmt.Errorf("%s has a running session in that worktree. Stop it first (session_stop)", nick)
				}
			}
			env := gitEnv(c)
			if !sessions.IsGitRepo(env, w.Root) {
				return WorktreeRemoveOut{}, errors.New("the workspace root is not a git repository, so there are no worktrees")
			}
			name := sessions.WorktreeName(c.Store, w, nick)
			if err := sessions.RemoveWorktree(env, w.Root, name, in.Force); err != nil {
				return WorktreeRemoveOut{}, err
			}
			return WorktreeRemoveOut{Removed: sessions.WorktreePath(w.Root, name), Branch: "hadron/" + name, Note: "The branch is still there; delete it with git branch -d once it is merged."}, nil
		})
}

func hasAnySuffix(s string, suffixes []string) bool {
	for _, x := range suffixes {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}
