package dash

import (
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// TypeGroup is a card group: cards the user keeps an eye on, in the order source.cards lists them.
const TypeGroup = "group"

// MaxGroupCards is the most cards one group holds.
const MaxGroupCards = 40

// MaxLastRunes is how much of a card's last chat line a group row carries.
const MaxLastRunes = 200

// QueryGroup names the data of a group block in its Result.
const QueryGroup = "group.cards"

// checkGroup returns the problems with a group block's cards.
func checkGroup(cards []string) []string {
	var errs []string
	if len(cards) > MaxGroupCards {
		errs = append(errs, fmt.Sprintf("a group holds at most %d cards, got %d", MaxGroupCards, len(cards)))
	}
	seen := map[string]bool{}
	for _, id := range cards {
		if strings.TrimSpace(id) == "" {
			errs = append(errs, "a card id is empty")
			continue
		}
		if seen[id] {
			errs = append(errs, fmt.Sprintf("card %q is in the group twice", id))
		}
		seen[id] = true
	}
	return errs
}

// GroupRows resolves a group's card ids into one row per card, in the group's order. A card that is
// archived, deleted or in another workspace is left out. Each row has the card's id, nick, key,
// title, column, column_id, stage, liveness, needs_you (an open question or gate, or a needs-you
// column), question (the open question's text, if any), and the channel's last line: last,
// last_kind, last_author and last_at.
func GroupRows(env Env, ids []string) ([]map[string]any, error) {
	rows := []map[string]any{}
	if len(ids) == 0 {
		return rows, nil
	}
	cols, err := env.Store.Columns.List(env.Workspace.ID)
	if err != nil {
		return nil, err
	}
	colByID := map[string]domain.Column{}
	for _, c := range cols {
		colByID[c.ID] = c
	}
	chans, err := env.Store.Messages.Channels(env.Workspace.ID)
	if err != nil {
		return nil, err
	}
	last := map[string]store.ChannelSummary{}
	for _, ch := range chans {
		last[ch.Channel] = ch
	}
	for _, id := range ids {
		card, err := env.Store.Cards.Get(id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if card.Archived || card.WorkspaceID != env.Workspace.ID {
			continue
		}
		col := colByID[card.ColumnID]
		qs, err := env.Store.Questions.List(card.WorkspaceID, card.ID, "open")
		if err != nil {
			return nil, err
		}
		gs, err := env.Store.Gates.List(card.WorkspaceID, card.ID)
		if err != nil {
			return nil, err
		}
		row := map[string]any{
			"id": card.ID, "nick": card.Nick, "key": card.Key, "title": card.Title,
			"column": col.Name, "column_id": card.ColumnID, "stage": card.Stage, "liveness": card.Liveness,
			"needs_you": len(qs)+len(gs) > 0 || col.Role == domain.RoleNeedsYou,
			"question":  "", "last": "", "last_kind": "", "last_author": "", "last_at": "",
		}
		if len(qs) > 0 {
			row["question"] = qs[0].Text
		}
		if s, ok := last["card:"+card.ID]; ok {
			row["last"] = clip(oneLine(s.LastText), MaxLastRunes)
			row["last_kind"], row["last_author"] = s.LastKind, s.LastAuthor
			row["last_at"] = s.LastAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// WithCard returns cards with id placed at pos (clamped; a negative pos means the end). A card
// already there moves.
func WithCard(cards []string, id string, pos int) []string {
	rest := WithoutCard(cards, id)
	if pos < 0 || pos > len(rest) {
		pos = len(rest)
	}
	out := make([]string, 0, len(rest)+1)
	out = append(out, rest[:pos]...)
	out = append(out, id)
	return append(out, rest[pos:]...)
}

// WithoutCard returns cards without id.
func WithoutCard(cards []string, id string) []string {
	out := make([]string, 0, len(cards))
	for _, c := range cards {
		if c != id {
			out = append(out, c)
		}
	}
	return out
}

// HasCard says whether the group lists id.
func HasCard(cards []string, id string) bool {
	for _, c := range cards {
		if c == id {
			return true
		}
	}
	return false
}
