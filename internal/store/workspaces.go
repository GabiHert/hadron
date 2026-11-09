package store

import (
	"github.com/GabiHert/hadron/internal/domain"
)

// Workspaces is the repo for workspaces.
type Workspaces struct{ s *Store }

const wsCols = `id, created_at, updated_at, name, color, root, position, archived, integrations, filter_bar, ui, default_model, planner_model`

type scanner interface{ Scan(dest ...any) error }

func scanWorkspace(r scanner) (domain.Workspace, error) {
	var w domain.Workspace
	var c, u, integ, fb, ui string
	var arch int
	if err := r.Scan(&w.ID, &c, &u, &w.Name, &w.Color, &w.Root, &w.Position, &arch, &integ, &fb, &ui, &w.DefaultModel, &w.PlannerModel); err != nil {
		return w, err
	}
	w.CreatedAt, w.UpdatedAt, w.Archived = parseTS(c), parseTS(u), arch == 1
	w.Integrations, w.FilterBar, w.UI = []string{}, []string{}, map[string]any{}
	fromJSON(integ, &w.Integrations)
	fromJSON(fb, &w.FilterBar)
	fromJSON(ui, &w.UI)
	return w, nil
}

// Create inserts w at the end of the workspace list.
func (r *Workspaces) Create(w *domain.Workspace) error {
	stamp(&w.Meta)
	if err := r.s.q.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM workspaces`).Scan(&w.Position); err != nil {
		return err
	}
	if w.Integrations == nil {
		w.Integrations = []string{}
	}
	if w.FilterBar == nil {
		w.FilterBar = []string{}
	}
	if w.UI == nil {
		w.UI = map[string]any{}
	}
	_, err := r.s.q.Exec(`INSERT INTO workspaces (`+wsCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, ts(w.CreatedAt), ts(w.UpdatedAt), w.Name, w.Color, w.Root, w.Position, b2i(w.Archived),
		toJSON(w.Integrations), toJSON(w.FilterBar), toJSON(w.UI), w.DefaultModel, w.PlannerModel)
	return mapErr(err)
}

// Get returns one workspace.
func (r *Workspaces) Get(id string) (domain.Workspace, error) {
	w, err := scanWorkspace(r.s.q.QueryRow(`SELECT `+wsCols+` FROM workspaces WHERE id = ?`, id))
	return w, mapErr(err)
}

// List returns workspaces in order. Archived ones only when includeArchived.
func (r *Workspaces) List(includeArchived bool) ([]domain.Workspace, error) {
	rows, err := r.s.q.Query(`SELECT `+wsCols+` FROM workspaces WHERE archived = 0 OR ? ORDER BY position, created_at`, b2i(includeArchived))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Workspace{}
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// Update writes every field of w.
func (r *Workspaces) Update(w *domain.Workspace) error {
	stamp(&w.Meta)
	return affected(r.s.q.Exec(`UPDATE workspaces SET updated_at=?, name=?, color=?, root=?, position=?, archived=?, integrations=?, filter_bar=?, ui=?, default_model=?, planner_model=? WHERE id=?`,
		ts(w.UpdatedAt), w.Name, w.Color, w.Root, w.Position, b2i(w.Archived), toJSON(w.Integrations), toJSON(w.FilterBar), toJSON(w.UI), w.DefaultModel, w.PlannerModel, w.ID))
}

// Delete removes a workspace and, by cascade, everything in it.
func (r *Workspaces) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM workspaces WHERE id = ?`, id))
}
