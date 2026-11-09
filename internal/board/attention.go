package board

import (
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// Attention counts what a card waits on the user for: its open questions and its gates.
func Attention(tx *store.Store, card domain.Card) (int, error) {
	qs, err := tx.Questions.List(card.WorkspaceID, card.ID, "open")
	if err != nil {
		return 0, err
	}
	gs, err := tx.Gates.List(card.WorkspaceID, card.ID)
	if err != nil {
		return 0, err
	}
	return len(qs) + len(gs), nil
}
