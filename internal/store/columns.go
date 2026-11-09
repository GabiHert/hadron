package store

import (
	"fmt"

	"github.com/GabiHert/hadron/internal/domain"
)

// Columns is the repo for board columns.
type Columns struct{ s *Store }

const colCols = `id, created_at, updated_at, workspace_id, name, role, position, wip, fold, color`

func scanColumn(r scanner) (domain.Column, error) {
	var c domain.Column
	var cr, up string
	var fold int
	err := r.Scan(&c.ID, &cr, &up, &c.WorkspaceID, &c.Name, &c.Role, &c.Position, &c.WIP, &fold, &c.Color)
	c.CreatedAt, c.UpdatedAt, c.Fold = parseTS(cr), parseTS(up), fold == 1
	return c, err
}

// Create appends c to the end of its workspace's columns.
func (r *Columns) Create(c *domain.Column) error {
	stamp(&c.Meta)
	if err := r.s.q.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM columns WHERE workspace_id = ?`, c.WorkspaceID).Scan(&c.Position); err != nil {
		return err
	}
	_, err := r.s.q.Exec(`INSERT INTO columns (`+colCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ID, ts(c.CreatedAt), ts(c.UpdatedAt), c.WorkspaceID, c.Name, c.Role, c.Position, c.WIP, b2i(c.Fold), c.Color)
	return mapErr(err)
}

// Get returns one column.
func (r *Columns) Get(id string) (domain.Column, error) {
	c, err := scanColumn(r.s.q.QueryRow(`SELECT `+colCols+` FROM columns WHERE id = ?`, id))
	return c, mapErr(err)
}

// List returns a workspace's columns in board order.
func (r *Columns) List(workspaceID string) ([]domain.Column, error) {
	rows, err := r.s.q.Query(`SELECT `+colCols+` FROM columns WHERE workspace_id = ? ORDER BY position, created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Column{}
	for rows.Next() {
		c, err := scanColumn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Update writes every field of c except its workspace. The id never changes, so a rename keeps it.
func (r *Columns) Update(c *domain.Column) error {
	stamp(&c.Meta)
	return affected(r.s.q.Exec(`UPDATE columns SET updated_at=?, name=?, role=?, position=?, wip=?, fold=?, color=? WHERE id=?`,
		ts(c.UpdatedAt), c.Name, c.Role, c.Position, c.WIP, b2i(c.Fold), c.Color, c.ID))
}

// Reorder sets the positions of a workspace's columns to the order of ids. ids must name every
// column of the workspace exactly once.
func (r *Columns) Reorder(workspaceID string, ids []string) error {
	cols, err := r.List(workspaceID)
	if err != nil {
		return err
	}
	if len(cols) != len(ids) {
		return fmt.Errorf("column order must list all %d columns, got %d", len(cols), len(ids))
	}
	known := map[string]bool{}
	for _, c := range cols {
		known[c.ID] = true
	}
	for i, id := range ids {
		if !known[id] {
			return fmt.Errorf("column %q is not in this workspace or is listed twice", id)
		}
		delete(known, id)
		if _, err := r.s.q.Exec(`UPDATE columns SET position = ?, updated_at = ? WHERE id = ?`, i, ts(now()), id); err != nil {
			return err
		}
	}
	return nil
}

// DeleteMoving deletes a column after moving its cards to target, appended after target's cards.
// Cards whose return_column_id is the deleted column lose it. Run it inside Store.Tx.
func (r *Columns) DeleteMoving(id, target string) error {
	if id == target {
		return fmt.Errorf("cannot move a column's cards into the column being deleted")
	}
	var base int
	if err := r.s.q.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM cards WHERE column_id = ?`, target).Scan(&base); err != nil {
		return err
	}
	rows, err := r.s.q.Query(`SELECT id FROM cards WHERE column_id = ? ORDER BY position`, id)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var cid string
		if err := rows.Scan(&cid); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, cid)
	}
	rows.Close()
	for i, cid := range ids {
		if _, err := r.s.q.Exec(`UPDATE cards SET column_id = ?, position = ?, updated_at = ? WHERE id = ?`, target, base+i, ts(now()), cid); err != nil {
			return err
		}
	}
	if _, err := r.s.q.Exec(`UPDATE cards SET return_column_id = NULL WHERE return_column_id = ?`, id); err != nil {
		return err
	}
	return affected(r.s.q.Exec(`DELETE FROM columns WHERE id = ?`, id))
}
