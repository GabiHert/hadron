package actions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

// CardListIn is the input of card_list.
type CardListIn struct {
	WorkspaceID     string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	ColumnID        string `json:"column_id,omitempty" jsonschema:"only cards in this column"`
	Role            string `json:"role,omitempty" jsonschema:"only cards in columns with this role"`
	Liveness        string `json:"liveness,omitempty" jsonschema:"working, idle, waiting or ended"`
	Expr            string `json:"expr,omitempty" jsonschema:"a CEL filter expression over the card, custom and integration fields, such as card.stage == 'review'; see field_schema_get"`
	IncludeArchived bool   `json:"include_archived,omitempty" jsonschema:"also list archived cards"`
}

// CardIDIn is the input of the actions that need only a card.
type CardIDIn struct {
	CardID string `json:"card_id" jsonschema:"the card id"`
}

// CardCreateIn is the input of card_create.
type CardCreateIn struct {
	WorkspaceID   string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Nick          string `json:"nick" jsonschema:"a kebab-case nickname, unique in the workspace, such as billing-fix"`
	Key           string `json:"key,omitempty" jsonschema:"an optional key such as ACME-508"`
	Title         string `json:"title,omitempty" jsonschema:"an optional longer title"`
	ColumnID      string `json:"column_id,omitempty" jsonschema:"the column; defaults to the first active column"`
	Assignee      string `json:"assignee,omitempty" jsonschema:"who owns the card; defaults to You"`
	LaunchProfile string `json:"launch_profile,omitempty" jsonschema:"the launch profile for its session"`
	Start         *bool  `json:"start,omitempty" jsonschema:"start the card's session (default true)"`
	Model         string `json:"model,omitempty" jsonschema:"the Claude model for this card's session: opus, sonnet, haiku or a full model name. Empty uses the launch profile's, then the workspace default"`
	Prompt        string `json:"prompt,omitempty" jsonschema:"extra instructions for this card's session, added to its system prompt after the workspace and launch profile prompts; may use {{variables}}"`
}

// CardUpdateIn is the input of card_update. Only the fields you pass change.
type CardUpdateIn struct {
	CardID        string  `json:"card_id" jsonschema:"the card id"`
	Nick          *string `json:"nick,omitempty" jsonschema:"new kebab-case nickname"`
	Key           *string `json:"key,omitempty" jsonschema:"new key"`
	Title         *string `json:"title,omitempty" jsonschema:"new title"`
	Assignee      *string `json:"assignee,omitempty" jsonschema:"new assignee"`
	Stage         *string `json:"stage,omitempty" jsonschema:"a short stage label"`
	LaunchProfile *string `json:"launch_profile,omitempty" jsonschema:"new launch profile"`
	Model         *string `json:"model,omitempty" jsonschema:"new Claude model; typed as /model into a running session. Empty goes back to the launch profile's or the workspace default"`
	Prompt        *string `json:"prompt,omitempty" jsonschema:"new card prompt: extra instructions added to the session's system prompt. Applies when the session next starts. Empty removes it"`
}

// CardMoveIn is the input of card_move.
type CardMoveIn struct {
	CardID   string `json:"card_id" jsonschema:"the card id"`
	ColumnID string `json:"column_id" jsonschema:"the destination column"`
	Position *int   `json:"position,omitempty" jsonschema:"0 is the top of the column; default is the bottom"`
}

// CardArchiveIn is the input of card_archive.
type CardArchiveIn struct {
	CardID   string `json:"card_id" jsonschema:"the card id"`
	Archived *bool  `json:"archived,omitempty" jsonschema:"true (default) to archive, false to restore"`
}

func registerCards(r *Registry) {
	Register(r, Spec{Name: "card_list", Description: "List a workspace's cards in board order, optionally by column, role or liveness."},
		func(ctx context.Context, c *Call, in CardListIn) ([]domain.Card, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			if err := checkRole(in.Role); err != nil {
				return nil, err
			}
			list, err := c.Store.Cards.List(w.ID, store.CardFilter{ColumnID: in.ColumnID, Role: in.Role, Liveness: in.Liveness, IncludeArchived: in.IncludeArchived})
			if err != nil || in.Expr == "" {
				return list, err
			}
			ids, err := fieldRegistry(c).Filter(c.Store, w, in.Expr, list)
			if err != nil {
				return nil, err
			}
			keep := make(map[string]bool, len(ids))
			for _, id := range ids {
				keep[id] = true
			}
			out := make([]domain.Card, 0, len(ids))
			for _, card := range list {
				if keep[card.ID] {
					out = append(out, card)
				}
			}
			return out, nil
		})

	Register(r, Spec{Name: "card_get", Description: "Get one card by id, with its column, liveness and fields."},
		func(ctx context.Context, c *Call, in CardIDIn) (domain.Card, error) { return getCard(c, in.CardID) })

	Register(r, Spec{Name: "card_create", Description: "Create a card at the bottom of a column and start its Claude Code session, unless start is false. The nickname must be kebab-case and unique in the workspace. The launch profile decides the session's first prompt; none means no first prompt.",
		Mutates: true, Entities: []string{"cards"}},
		func(ctx context.Context, c *Call, in CardCreateIn) (domain.Card, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return domain.Card{}, err
			}
			if err := checkNick(in.Nick); err != nil {
				return domain.Card{}, err
			}
			cols, err := c.Store.Columns.List(w.ID)
			if err != nil {
				return domain.Card{}, err
			}
			colID := in.ColumnID
			if colID == "" {
				colID = defaultColumn(cols)
			}
			found := false
			for _, col := range cols {
				found = found || col.ID == colID
			}
			if !found {
				return domain.Card{}, fmt.Errorf("column %q is not in workspace %q. Call column_list for the ids", colID, w.Name)
			}
			assignee := in.Assignee
			if assignee == "" {
				assignee = "You"
			}
			if err := checkLayerPrompt("card prompt", in.Prompt); err != nil {
				return domain.Card{}, err
			}
			if err := sessions.CheckModelID(strings.TrimSpace(in.Model)); err != nil {
				return domain.Card{}, err
			}
			card := domain.Card{WorkspaceID: w.ID, ColumnID: colID, Nick: in.Nick, Key: strings.TrimSpace(in.Key), Title: strings.TrimSpace(in.Title),
				Assignee: assignee, LaunchProfile: in.LaunchProfile, Prompt: strings.TrimSpace(in.Prompt), Model: strings.TrimSpace(in.Model), Liveness: domain.LivenessIdle}
			if err := c.Store.Cards.Create(&card); err != nil {
				return card, nickError(err, in.Nick, w.Name)
			}
			if err := runEvent(c, board.Event{Trigger: board.OnCardCreated, CardID: card.ID}); err != nil {
				return card, err
			}
			if (in.Start == nil || *in.Start) && c.Deps.Sessions != nil {
				_, launch, err := c.Deps.Sessions.Prepare(c.Store, sessions.Request{Workspace: w, Card: &card})
				if err != nil {
					return card, err
				}
				c.AfterCommit(launch)
				c.Touch("sessions")
				card.Liveness = domain.LivenessIdle
			}
			return card, nil
		})

	Register(r, Spec{Name: "card_update", Description: "Change a card's nickname, key, title, assignee, stage, launch profile, prompt or model. Use card_move to change its column.",
		Mutates: true, Entities: []string{"cards"}},
		func(ctx context.Context, c *Call, in CardUpdateIn) (domain.Card, error) {
			card, err := getCard(c, in.CardID)
			if err != nil {
				return card, err
			}
			if in.Nick != nil {
				if err := checkNick(*in.Nick); err != nil {
					return card, err
				}
				card.Nick = *in.Nick
			}
			was := card
			set := func(dst *string, src *string) {
				if src != nil {
					*dst = strings.TrimSpace(*src)
				}
			}
			set(&card.Key, in.Key)
			set(&card.Title, in.Title)
			set(&card.Assignee, in.Assignee)
			set(&card.Stage, in.Stage)
			set(&card.LaunchProfile, in.LaunchProfile)
			if in.Prompt != nil {
				if err := checkLayerPrompt("card prompt", *in.Prompt); err != nil {
					return card, err
				}
			}
			set(&card.Prompt, in.Prompt)
			if in.Model != nil {
				model := strings.TrimSpace(*in.Model)
				if err := sessions.CheckModelID(model); err != nil {
					return card, err
				}
				if model != card.Model {
					if err := mayChooseModel(c, card.WorkspaceID, &card); err != nil {
						return card, err
					}
				}
				card.Model = model
			}
			if err := c.Store.Cards.Update(&card); err != nil {
				return card, nickError(err, card.Nick, "")
			}
			if card.Model != was.Model {
				if w, err := c.Workspace(card.WorkspaceID); err == nil {
					c.RecordChange(w.ID, was, card)
					announceModel(c, w, &was, &card)
				}
			}
			for _, ch := range []struct{ path, old, now string }{
				{"card.nick", was.Nick, card.Nick}, {"card.key", was.Key, card.Key},
				{"card.assignee", was.Assignee, card.Assignee}, {"card.stage", was.Stage, card.Stage},
			} {
				if ch.old != ch.now {
					if err := runEvent(c, board.Event{Trigger: board.OnFieldChanged, CardID: card.ID, Field: ch.path, Old: ch.old, New: ch.now}); err != nil {
						return card, err
					}
				}
			}
			return c.Store.Cards.Get(card.ID)
		})

	Register(r, Spec{Name: "card_move", Description: "Move a card to a column, at a position (0 is the top; default is the bottom). Also reorders within a column. A card that waits on a question or gate remembers where it should return to: moving it into a needs-you column remembers the column it left, and moving it out of one forgets the old return column, because you put it somewhere on purpose.",
		Mutates: true, Entities: []string{"cards"}},
		func(ctx context.Context, c *Call, in CardMoveIn) (domain.Card, error) {
			before, err := getCard(c, in.CardID)
			if err != nil {
				return domain.Card{}, err
			}
			pos := -1
			if in.Position != nil {
				pos = *in.Position
				if pos < 0 {
					return domain.Card{}, errors.New("position must be 0 (top) or more")
				}
			}
			moved, err := board.MoveCard(c.Store, in.CardID, in.ColumnID, pos)
			if err != nil || moved.ColumnID == before.ColumnID {
				return moved, err
			}
			from, _ := c.Store.Columns.Get(before.ColumnID)
			to, _ := c.Store.Columns.Get(moved.ColumnID)
			if err := updateReturnColumn(c, moved, before.ColumnID, from, to); err != nil {
				return moved, err
			}
			for _, ev := range board.ColumnEvents(moved.ID, moved.ColumnID, from.Name, to.Name) {
				if err := runEvent(c, ev); err != nil {
					return moved, err
				}
			}
			return c.Store.Cards.Get(moved.ID)
		})

	Register(r, Spec{Name: "card_archive", Description: "Archive a card so it leaves the board, or restore it with archived: false.",
		Mutates: true, Entities: []string{"cards"}},
		func(ctx context.Context, c *Call, in CardArchiveIn) (domain.Card, error) {
			card, err := getCard(c, in.CardID)
			if err != nil {
				return card, err
			}
			card.Archived = in.Archived == nil || *in.Archived
			return card, c.Store.Cards.Update(&card)
		})

	Register(r, Spec{Name: "card_delete", Description: "Delete a card with its sessions, questions, gates and links. This cannot be undone; prefer card_archive.",
		Mutates: true, Entities: []string{"cards"}},
		func(ctx context.Context, c *Call, in CardIDIn) (CardDeleteOut, error) {
			card, err := getCard(c, in.CardID)
			if err != nil {
				return CardDeleteOut{}, err
			}
			if t, err := target(c, card.WorkspaceID, &card); err == nil && t.Live != nil {
				c.AfterCommit(t.Live.Stop)
			}
			out := CardDeleteOut{Card: card}
			if w, err := c.Workspace(card.WorkspaceID); err == nil {
				path := sessions.WorktreePath(w.Root, card.Nick)
				if st, serr := os.Stat(path); serr == nil && st.IsDir() {
					out.Worktree = &WorktreeKept{Path: path, Branch: "hadron/" + card.Nick, Note: "The card's worktree is still on disk so no work is lost. worktree_remove with card=" + card.Nick + " deletes it once it is clean."}
				}
			}
			return out, c.Store.Cards.Delete(card.ID)
		})
}

// updateReturnColumn keeps a card's return_column_id true after a move by hand. Into a needs-you
// column with something still waiting, it remembers the column the card came from, so answering
// sends it back there. Out of a needs-you column, or anywhere else, the old pointer is dropped.
func updateReturnColumn(c *Call, moved domain.Card, fromColumnID string, from, to domain.Column) error {
	fresh, err := c.Store.Cards.Get(moved.ID)
	if err != nil {
		return err
	}
	want := fresh.ReturnColumnID
	switch {
	case to.Role == domain.RoleNeedsYou && from.Role != domain.RoleNeedsYou:
		attention, err := board.Attention(c.Store, fresh)
		if err != nil {
			return err
		}
		if attention > 0 {
			want = fromColumnID
		}
	case to.Role != domain.RoleNeedsYou:
		want = ""
	}
	if want == fresh.ReturnColumnID {
		return nil
	}
	fresh.ReturnColumnID = want
	return c.Store.Cards.Update(&fresh)
}

func getCard(c *Call, id string) (domain.Card, error) {
	card, err := c.Store.Cards.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return card, fmt.Errorf("card %q does not exist. Call card_list for the ids", id)
	}
	if err == nil {
		c.SetWorkspace(card.WorkspaceID)
	}
	return card, err
}

// defaultColumn is the first active column, else the first column.
func defaultColumn(cols []domain.Column) string {
	for _, col := range cols {
		if col.Role == domain.RoleActive {
			return col.ID
		}
	}
	if len(cols) > 0 {
		return cols[0].ID
	}
	return ""
}

func nickError(err error, nick, workspace string) error {
	if errors.Is(err, store.ErrDuplicate) {
		if workspace != "" {
			return fmt.Errorf("a card named %q already exists in workspace %q. Pick another nickname", nick, workspace)
		}
		return fmt.Errorf("a card named %q already exists in this workspace. Pick another nickname", nick)
	}
	return err
}

// WorktreeKept says a deleted card's worktree was left on disk.
type WorktreeKept struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
	Note   string `json:"note"`
}

// CardDeleteOut is the result of card_delete: the card, and its worktree if one was left behind.
type CardDeleteOut struct {
	domain.Card
	Worktree *WorktreeKept `json:"worktree,omitempty"`
}
