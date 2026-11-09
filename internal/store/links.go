package store

import (
	"database/sql"

	"github.com/GabiHert/hadron/internal/domain"
)

// Links is the repo for card links.
type Links struct{ s *Store }

const linkCols = `id, created_at, updated_at, workspace_id, card_id, integration, item_id, title, url, fields, synced_at`

func scanLink(r scanner) (domain.Link, error) {
	var l domain.Link
	var cr, up, fields string
	var synced sql.NullString
	err := r.Scan(&l.ID, &cr, &up, &l.WorkspaceID, &l.CardID, &l.Integration, &l.ItemID, &l.Title, &l.URL, &fields, &synced)
	l.CreatedAt, l.UpdatedAt = parseTS(cr), parseTS(up)
	l.Fields = map[string]any{}
	fromJSON(fields, &l.Fields)
	l.SyncedAt = scanTSPtr(synced)
	return l, err
}

// Create inserts a link. A card links an item once.
func (r *Links) Create(l *domain.Link) error {
	stamp(&l.Meta)
	if l.Fields == nil {
		l.Fields = map[string]any{}
	}
	_, err := r.s.q.Exec(`INSERT INTO links (`+linkCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		l.ID, ts(l.CreatedAt), ts(l.UpdatedAt), l.WorkspaceID, l.CardID, l.Integration, l.ItemID, l.Title, l.URL, toJSON(l.Fields), tsPtr(l.SyncedAt))
	return mapErr(err)
}

// Get returns one link.
func (r *Links) Get(id string) (domain.Link, error) {
	l, err := scanLink(r.s.q.QueryRow(`SELECT `+linkCols+` FROM links WHERE id = ?`, id))
	return l, mapErr(err)
}

// ListByCard returns a card's links, oldest first.
func (r *Links) ListByCard(cardID string) ([]domain.Link, error) {
	rows, err := r.s.q.Query(`SELECT `+linkCols+` FROM links WHERE card_id = ? ORDER BY created_at, rowid`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Link{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Update writes the title, URL, fields and sync time of l.
func (r *Links) Update(l *domain.Link) error {
	stamp(&l.Meta)
	return affected(r.s.q.Exec(`UPDATE links SET updated_at=?, title=?, url=?, fields=?, synced_at=? WHERE id=?`,
		ts(l.UpdatedAt), l.Title, l.URL, toJSON(l.Fields), tsPtr(l.SyncedAt), l.ID))
}

// Delete removes a link.
func (r *Links) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM links WHERE id = ?`, id))
}

// ListByIntegration returns every link of an integration across workspaces, oldest first.
func (r *Links) ListByIntegration(integration string) ([]domain.Link, error) {
	rows, err := r.s.q.Query(`SELECT `+linkCols+` FROM links WHERE integration = ? ORDER BY created_at, rowid`, integration)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Link{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListByWorkspace returns every link in a workspace, oldest first.
func (r *Links) ListByWorkspace(workspaceID string) ([]domain.Link, error) {
	rows, err := r.s.q.Query(`SELECT `+linkCols+` FROM links WHERE workspace_id = ? ORDER BY created_at, rowid`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Link{}
	for rows.Next() {
		l, err := scanLink(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
