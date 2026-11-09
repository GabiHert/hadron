package board

import (
	"fmt"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// MoveCard places a card in a column at a position (0 is the top; a negative or too-large value
// means the bottom) and renumbers the positions of the columns it touches. Run it inside a
// transaction. It returns the card as saved.
func MoveCard(tx *store.Store, cardID, columnID string, position int) (domain.Card, error) {
	card, err := tx.Cards.Get(cardID)
	if err != nil {
		return card, err
	}
	target, err := tx.Columns.Get(columnID)
	if err != nil {
		return card, fmt.Errorf("column %q does not exist. Call column_list for the ids", columnID)
	}
	if target.WorkspaceID != card.WorkspaceID {
		return card, fmt.Errorf("column %q belongs to another workspace than card %q", columnID, card.Nick)
	}
	from := card.ColumnID
	dest, err := tx.Cards.List(card.WorkspaceID, store.CardFilter{ColumnID: columnID, IncludeArchived: true})
	if err != nil {
		return card, err
	}
	ordered := make([]domain.Card, 0, len(dest)+1)
	for _, c := range dest {
		if c.ID != card.ID {
			ordered = append(ordered, c)
		}
	}
	if position < 0 || position > len(ordered) {
		position = len(ordered)
	}
	card.ColumnID = columnID
	ordered = append(ordered[:position], append([]domain.Card{card}, ordered[position:]...)...)
	for i := range ordered {
		if ordered[i].Position == i && ordered[i].ID != card.ID {
			continue
		}
		ordered[i].Position = i
		ordered[i].ColumnID = columnID
		if err := tx.Cards.Update(&ordered[i]); err != nil {
			return card, err
		}
	}
	if from != columnID {
		rest, err := tx.Cards.List(card.WorkspaceID, store.CardFilter{ColumnID: from, IncludeArchived: true})
		if err != nil {
			return card, err
		}
		for i := range rest {
			if rest[i].Position != i {
				rest[i].Position = i
				if err := tx.Cards.Update(&rest[i]); err != nil {
					return card, err
				}
			}
		}
	}
	return tx.Cards.Get(card.ID)
}
