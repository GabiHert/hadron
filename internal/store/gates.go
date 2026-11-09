package store

import "github.com/GabiHert/hadron/internal/domain"

// Gates is the repo for gates.
type Gates struct{ s *Store }

const gateCols = `id, created_at, updated_at, workspace_id, card_id, kind, summary, since`

func scanGate(r scanner) (domain.Gate, error) {
	var g domain.Gate
	var cr, up, since string
	err := r.Scan(&g.ID, &cr, &up, &g.WorkspaceID, &g.CardID, &g.Kind, &g.Summary, &since)
	g.CreatedAt, g.UpdatedAt, g.Since = parseTS(cr), parseTS(up), parseTS(since)
	return g, err
}

// Create inserts a gate. Since defaults to now.
func (r *Gates) Create(g *domain.Gate) error {
	stamp(&g.Meta)
	if g.Since.IsZero() {
		g.Since = g.CreatedAt
	}
	_, err := r.s.q.Exec(`INSERT INTO gates (`+gateCols+`) VALUES (?,?,?,?,?,?,?,?)`,
		g.ID, ts(g.CreatedAt), ts(g.UpdatedAt), g.WorkspaceID, g.CardID, g.Kind, g.Summary, ts(g.Since))
	return mapErr(err)
}

// Get returns one gate.
func (r *Gates) Get(id string) (domain.Gate, error) {
	g, err := scanGate(r.s.q.QueryRow(`SELECT `+gateCols+` FROM gates WHERE id = ?`, id))
	return g, mapErr(err)
}

// List returns a workspace's gates, oldest first. cardID may be empty.
func (r *Gates) List(workspaceID, cardID string) ([]domain.Gate, error) {
	rows, err := r.s.q.Query(`SELECT `+gateCols+` FROM gates WHERE workspace_id = ? AND (? = '' OR card_id = ?) ORDER BY since, rowid`, workspaceID, cardID, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Gate{}
	for rows.Next() {
		g, err := scanGate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Delete clears a gate.
func (r *Gates) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM gates WHERE id = ?`, id))
}

// Update writes the summary of g.
func (r *Gates) Update(g *domain.Gate) error {
	stamp(&g.Meta)
	return affected(r.s.q.Exec(`UPDATE gates SET updated_at=?, summary=? WHERE id=?`, ts(g.UpdatedAt), g.Summary, g.ID))
}
