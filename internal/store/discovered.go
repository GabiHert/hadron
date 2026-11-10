package store

import "time"

// Discovered remembers the items an integration's discover op already turned into cards.
type Discovered struct{ s *Store }

// Has says whether an item was discovered in the workspace before.
func (r *Discovered) Has(workspaceID, integration, itemID string) (bool, error) {
	var n int
	err := r.s.q.QueryRow(`SELECT COUNT(*) FROM discovered WHERE workspace_id = ? AND integration = ? AND item_id = ?`, workspaceID, integration, itemID).Scan(&n)
	return n > 0, err
}

// Add records an item and the card that holds it. Adding it again keeps the first record.
func (r *Discovered) Add(workspaceID, integration, itemID, cardID string) error {
	_, err := r.s.q.Exec(`INSERT OR IGNORE INTO discovered (workspace_id, integration, item_id, card_id, created_at) VALUES (?,?,?,?,?)`,
		workspaceID, integration, itemID, cardID, ts(time.Now()))
	return err
}

// Count is how many items of an integration a workspace has discovered.
func (r *Discovered) Count(workspaceID, integration string) (int, error) {
	var n int
	err := r.s.q.QueryRow(`SELECT COUNT(*) FROM discovered WHERE workspace_id = ? AND integration = ?`, workspaceID, integration).Scan(&n)
	return n, err
}

// Forget drops every discovered item of an integration in a workspace, so they may come back.
func (r *Discovered) Forget(workspaceID, integration string) error {
	_, err := r.s.q.Exec(`DELETE FROM discovered WHERE workspace_id = ? AND integration = ?`, workspaceID, integration)
	return err
}
