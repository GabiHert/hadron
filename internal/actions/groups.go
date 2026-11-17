package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/dash"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// GroupView is a card group: its dashboard block and the cards it watches, resolved.
type GroupView struct {
	ID       string           `json:"id" jsonschema:"the group's block id"`
	Title    string           `json:"title"`
	Span     int              `json:"span"`
	Position int              `json:"position"`
	Cards    []map[string]any `json:"cards" jsonschema:"one row per card in the group's order: id, nick, key, title, column, stage, liveness, needs_you, question, last (the card's last chat line), last_kind, last_at"`
}

// CardGroupListIn is the input of card_group_list.
type CardGroupListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Card        string `json:"card,omitempty" jsonschema:"only the groups that hold this card (id, nickname or key)"`
}

// CardGroupCreateIn is the input of card_group_create.
type CardGroupCreateIn struct {
	WorkspaceID string   `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Title       string   `json:"title" jsonschema:"the group's name, such as In flight"`
	Cards       []string `json:"cards,omitempty" jsonschema:"cards to start with (ids, nicknames or keys), in order"`
	Span        int      `json:"span,omitempty" jsonschema:"1 for half width, 2 for full width; default 1"`
	Position    *int     `json:"position,omitempty" jsonschema:"where the block goes on the dashboard, 0 for first; default last"`
}

// CardGroupAddIn is the input of card_group_add.
type CardGroupAddIn struct {
	WorkspaceID string   `json:"workspace_id,omitempty" jsonschema:"the workspace, for a group named by title; defaults to the active one"`
	Group       string   `json:"group" jsonschema:"the group: its block id or its title"`
	Cards       []string `json:"cards" jsonschema:"cards to add (ids, nicknames or keys). A card already in the group stays once"`
	Position    *int     `json:"position,omitempty" jsonschema:"where in the group the cards go, 0 for first; default the end"`
}

// CardGroupRemoveIn is the input of card_group_remove.
type CardGroupRemoveIn struct {
	WorkspaceID string   `json:"workspace_id,omitempty" jsonschema:"the workspace, for a group named by title; defaults to the active one"`
	Group       string   `json:"group" jsonschema:"the group: its block id or its title"`
	Cards       []string `json:"cards" jsonschema:"cards to take out (ids, nicknames or keys)"`
}

// CardGroupMoveIn is the input of card_group_move.
type CardGroupMoveIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace, for groups named by title; defaults to the active one"`
	Card        string `json:"card" jsonschema:"the card (id, nickname or key)"`
	From        string `json:"from,omitempty" jsonschema:"the group it leaves (block id or title); empty just adds it to the target"`
	To          string `json:"to" jsonschema:"the group it goes to (block id or title); the same as from reorders it"`
	Position    *int   `json:"position,omitempty" jsonschema:"its place in the target group, 0 for first; default the end"`
	Copy        bool   `json:"copy,omitempty" jsonschema:"true keeps it in from as well"`
}

func groupCards(b domain.Block) []string {
	src, _ := dash.ParseSource(b.Source)
	return append([]string{}, src.Cards...)
}

func groupView(env dash.Env, b domain.Block) (GroupView, error) {
	rows, err := dash.GroupRows(env, groupCards(b))
	if err != nil {
		return GroupView{}, err
	}
	return GroupView{ID: b.ID, Title: b.Title, Span: b.Span, Position: b.Position, Cards: rows}, nil
}

// findGroup finds a group block by id, else by title (any case) in the workspace.
func findGroup(c *Call, workspaceID, ref string) (domain.Block, domain.Workspace, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return domain.Block{}, domain.Workspace{}, errors.New("name the group by its block id or title. card_group_list lists them")
	}
	if b, err := c.Store.Blocks.Get(ref); err == nil {
		if b.Type != dash.TypeGroup {
			return b, domain.Workspace{}, fmt.Errorf("block %q is a %s block, not a card group", b.Title, b.Type)
		}
		w, err := c.Workspace(b.WorkspaceID)
		return b, w, err
	} else if !errors.Is(err, store.ErrNotFound) {
		return b, domain.Workspace{}, err
	}
	w, err := c.Workspace(workspaceID)
	if err != nil {
		return domain.Block{}, w, err
	}
	blocks, err := c.Store.Blocks.List(w.ID)
	if err != nil {
		return domain.Block{}, w, err
	}
	var found []domain.Block
	var names []string
	for _, b := range blocks {
		if b.Type != dash.TypeGroup {
			continue
		}
		names = append(names, b.Title)
		if strings.EqualFold(b.Title, ref) {
			found = append(found, b)
		}
	}
	switch len(found) {
	case 1:
		return found[0], w, nil
	case 0:
		if len(names) == 0 {
			return domain.Block{}, w, fmt.Errorf("no card group %q: this workspace has none yet. card_group_create makes one", ref)
		}
		return domain.Block{}, w, fmt.Errorf("no card group %q. The groups are: %s", ref, strings.Join(names, ", "))
	}
	return domain.Block{}, w, fmt.Errorf("%d groups are called %q; pass the block id from card_group_list", len(found), ref)
}

// groupCard resolves a card to put in a group of workspace w.
func groupCard(c *Call, w domain.Workspace, ref string) (domain.Card, error) {
	card, err := c.Store.Cards.Get(ref)
	if errors.Is(err, store.ErrNotFound) {
		card, err = c.Store.Cards.ByNick(w.ID, ref)
	}
	if errors.Is(err, store.ErrNotFound) {
		return card, fmt.Errorf("no card %q in workspace %q. Pass a card id, nickname or key; card_list shows them", ref, w.Name)
	}
	if err != nil {
		return card, err
	}
	if card.WorkspaceID != w.ID {
		return card, fmt.Errorf("card %q is in another workspace; a group holds cards of its own workspace", card.Nick)
	}
	if card.Archived {
		return card, fmt.Errorf("card %q is archived; restore it before watching it", card.Nick)
	}
	return card, nil
}

// saveGroup writes a group's new card list, checked as a whole, and records the change.
func saveGroup(c *Call, w domain.Workspace, b domain.Block, cards []string) (domain.Block, error) {
	before := b
	src, err := dash.ParseSource(b.Source)
	if err != nil {
		return b, err
	}
	src.Cards = cards
	spec := dash.SpecOf(b)
	spec.Source = &src
	if err := checkSpec(dashEnv(c, w), spec); err != nil {
		return b, err
	}
	nb, err := spec.Domain(w.ID)
	if err != nil {
		return b, err
	}
	b.Source = nb.Source
	if err := c.Store.Blocks.Update(&b); err != nil {
		return b, err
	}
	c.RecordChange(w.ID, before, b)
	return b, nil
}

// pruneGroups takes a card out of every group of its workspace, as it is archived or deleted.
func pruneGroups(c *Call, workspaceID, cardID string) error {
	blocks, err := c.Store.Blocks.List(workspaceID)
	if err != nil {
		return err
	}
	for _, b := range blocks {
		if b.Type != dash.TypeGroup {
			continue
		}
		cards := groupCards(b)
		if !dash.HasCard(cards, cardID) {
			continue
		}
		src, _ := dash.ParseSource(b.Source)
		src.Cards = dash.WithoutCard(cards, cardID)
		raw, err := json.Marshal(src)
		if err != nil {
			return err
		}
		b.Source = raw
		if err := c.Store.Blocks.Update(&b); err != nil {
			return err
		}
		c.Touch(EntityDash)
	}
	return nil
}

func registerCardGroups(r *Registry) {
	Register(r, Spec{Name: "card_group_list", Description: "List the card groups on the workspace's dashboard: each group's block id, title and its cards in order, each with column, stage, liveness, whether it needs the user, and its last chat line. With card, only the groups that hold it."},
		func(ctx context.Context, c *Call, in CardGroupListIn) ([]GroupView, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			only := ""
			if in.Card != "" {
				card, err := c.Store.Cards.Get(in.Card)
				if errors.Is(err, store.ErrNotFound) {
					card, err = c.Store.Cards.ByNick(w.ID, in.Card)
				}
				if err != nil {
					return nil, fmt.Errorf("no card %q in workspace %q. card_list shows them", in.Card, w.Name)
				}
				only = card.ID
			}
			blocks, err := c.Store.Blocks.List(w.ID)
			if err != nil {
				return nil, err
			}
			env := dashEnv(c, w)
			out := []GroupView{}
			for _, b := range blocks {
				if b.Type != dash.TypeGroup || (only != "" && !dash.HasCard(groupCards(b), only)) {
					continue
				}
				v, err := groupView(env, b)
				if err != nil {
					return nil, err
				}
				out = append(out, v)
			}
			return out, nil
		})

	Register(r, Spec{Name: "card_group_create", Description: "Make a card group: a dashboard block that shows the cards the user keeps an eye on, live, and opens each one's chat in place. Rename it with dash_block_update {block_id, title}, resize it with span, delete it with dash_block_remove.",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in CardGroupCreateIn) (GroupView, error) {
			if err := userOrPlanner(c, "change card groups"); err != nil {
				return GroupView{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return GroupView{}, err
			}
			var cards []string
			for _, ref := range in.Cards {
				card, err := groupCard(c, w, ref)
				if err != nil {
					return GroupView{}, err
				}
				if !dash.HasCard(cards, card.ID) {
					cards = append(cards, card.ID)
				}
			}
			env := dashEnv(c, w)
			spec := dash.Spec{Type: dash.TypeGroup, Title: in.Title, Span: in.Span, Source: &dash.Source{Cards: cards}}
			if err := checkSpec(env, spec); err != nil {
				return GroupView{}, err
			}
			existing, err := c.Store.Blocks.List(w.ID)
			if err != nil {
				return GroupView{}, err
			}
			if len(existing) >= dash.MaxBlocks {
				return GroupView{}, fmt.Errorf("the dashboard already has %d blocks, the most it holds. Remove one first", dash.MaxBlocks)
			}
			_ = dash.ValidateOne(&spec, env.Lookup)
			b, err := spec.Domain(w.ID)
			if err != nil {
				return GroupView{}, err
			}
			if err := c.Store.Blocks.Create(&b); err != nil {
				return GroupView{}, err
			}
			if in.Position != nil {
				if b, err = placeBlock(c, w.ID, b.ID, *in.Position); err != nil {
					return GroupView{}, err
				}
			}
			c.RecordChange(w.ID, nil, b)
			return groupView(env, b)
		})

	Register(r, Spec{Name: "card_group_add", Description: "Add cards to a card group, at the end or at a position. A card already in the group moves to that position instead of appearing twice. The card's board column does not change.",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in CardGroupAddIn) (GroupView, error) {
			if err := userOrPlanner(c, "change card groups"); err != nil {
				return GroupView{}, err
			}
			b, w, err := findGroup(c, in.WorkspaceID, in.Group)
			if err != nil {
				return GroupView{}, err
			}
			if len(in.Cards) == 0 {
				return GroupView{}, errors.New("cards is empty: name at least one card")
			}
			cards := groupCards(b)
			pos := -1
			if in.Position != nil {
				pos = *in.Position
			}
			for _, ref := range in.Cards {
				card, err := groupCard(c, w, ref)
				if err != nil {
					return GroupView{}, err
				}
				cards = dash.WithCard(cards, card.ID, pos)
				if pos >= 0 {
					pos = indexOf(cards, card.ID) + 1
				}
			}
			if b, err = saveGroup(c, w, b, cards); err != nil {
				return GroupView{}, err
			}
			return groupView(dashEnv(c, w), b)
		})

	Register(r, Spec{Name: "card_group_remove", Description: "Take cards out of a card group. The cards stay on the board as they are.",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in CardGroupRemoveIn) (GroupView, error) {
			if err := userOrPlanner(c, "change card groups"); err != nil {
				return GroupView{}, err
			}
			b, w, err := findGroup(c, in.WorkspaceID, in.Group)
			if err != nil {
				return GroupView{}, err
			}
			cards := groupCards(b)
			for _, ref := range in.Cards {
				id := ref
				if !dash.HasCard(cards, id) {
					if card, err := c.Store.Cards.ByNick(w.ID, ref); err == nil {
						id = card.ID
					}
				}
				if !dash.HasCard(cards, id) {
					return GroupView{}, fmt.Errorf("card %q is not in the group %q", ref, b.Title)
				}
				cards = dash.WithoutCard(cards, id)
			}
			if b, err = saveGroup(c, w, b, cards); err != nil {
				return GroupView{}, err
			}
			return groupView(dashEnv(c, w), b)
		})

	Register(r, Spec{Name: "card_group_move", Description: "Move a card from one card group to another (what dragging does), or copy it with copy: true. When from and to are the same group it reorders the card. Only group membership changes, never the card's board column.",
		Mutates: true, Entities: []string{EntityDash}},
		func(ctx context.Context, c *Call, in CardGroupMoveIn) ([]GroupView, error) {
			if err := userOrPlanner(c, "change card groups"); err != nil {
				return nil, err
			}
			to, w, err := findGroup(c, in.WorkspaceID, in.To)
			if err != nil {
				return nil, err
			}
			card, err := groupCard(c, w, in.Card)
			if err != nil {
				return nil, err
			}
			pos := -1
			if in.Position != nil {
				pos = *in.Position
			}
			var from domain.Block
			if in.From != "" {
				if from, _, err = findGroup(c, w.ID, in.From); err != nil {
					return nil, err
				}
				if from.WorkspaceID != to.WorkspaceID {
					return nil, errors.New("both groups must be in the same workspace")
				}
				if !dash.HasCard(groupCards(from), card.ID) {
					return nil, fmt.Errorf("card %q is not in the group %q", card.Nick, from.Title)
				}
			}
			if to, err = saveGroup(c, w, to, dash.WithCard(groupCards(to), card.ID, pos)); err != nil {
				return nil, err
			}
			env := dashEnv(c, w)
			tv, err := groupView(env, to)
			if err != nil {
				return nil, err
			}
			out := []GroupView{tv}
			if from.ID == "" || from.ID == to.ID {
				return out, nil
			}
			if !in.Copy {
				if from, err = saveGroup(c, w, from, dash.WithoutCard(groupCards(from), card.ID)); err != nil {
					return nil, err
				}
			}
			fv, err := groupView(env, from)
			if err != nil {
				return nil, err
			}
			return append([]GroupView{fv}, out...), nil
		})
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}
