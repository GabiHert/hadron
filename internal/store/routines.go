package store

import (
	"database/sql"

	"github.com/GabiHert/hadron/internal/domain"
)

// Routines is the repo for scheduled routines.
type Routines struct{ s *Store }

const routineCols = `id, created_at, updated_at, workspace_id, name, enabled, cron, every_minutes, target, card_id, prompt, catch_up, next_run_at, last_run_at, last_result`

func scanRoutine(r scanner) (domain.Routine, error) {
	var x domain.Routine
	var cr, up string
	var en, cu int
	var card, next, last sql.NullString
	err := r.Scan(&x.ID, &cr, &up, &x.WorkspaceID, &x.Name, &en, &x.Cron, &x.EveryMinutes, &x.Target, &card, &x.Prompt, &cu, &next, &last, &x.LastResult)
	x.CreatedAt, x.UpdatedAt, x.Enabled, x.CatchUp = parseTS(cr), parseTS(up), en == 1, cu == 1
	x.CardID, x.NextRunAt, x.LastRunAt = card.String, scanTSPtr(next), scanTSPtr(last)
	return x, err
}

// Create adds a routine.
func (r *Routines) Create(x *domain.Routine) error {
	stamp(&x.Meta)
	_, err := r.s.q.Exec(`INSERT INTO routines (`+routineCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, ts(x.CreatedAt), ts(x.UpdatedAt), x.WorkspaceID, x.Name, b2i(x.Enabled), x.Cron, x.EveryMinutes, x.Target, nullable(x.CardID), x.Prompt, b2i(x.CatchUp), tsPtr(x.NextRunAt), tsPtr(x.LastRunAt), x.LastResult)
	return mapErr(err)
}

// Get returns one routine.
func (r *Routines) Get(id string) (domain.Routine, error) {
	x, err := scanRoutine(r.s.q.QueryRow(`SELECT `+routineCols+` FROM routines WHERE id = ?`, id))
	return x, mapErr(err)
}

// List returns a workspace's routines, oldest first; an empty workspace id lists every workspace's.
func (r *Routines) List(workspaceID string) ([]domain.Routine, error) {
	q, args := `SELECT `+routineCols+` FROM routines ORDER BY created_at`, []any{}
	if workspaceID != "" {
		q, args = `SELECT `+routineCols+` FROM routines WHERE workspace_id = ? ORDER BY created_at`, []any{workspaceID}
	}
	rows, err := r.s.q.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Routine{}
	for rows.Next() {
		x, err := scanRoutine(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Update writes every field of x.
func (r *Routines) Update(x *domain.Routine) error {
	stamp(&x.Meta)
	return affected(r.s.q.Exec(`UPDATE routines SET updated_at=?, name=?, enabled=?, cron=?, every_minutes=?, target=?, card_id=?, prompt=?, catch_up=?, next_run_at=?, last_run_at=?, last_result=? WHERE id=?`,
		ts(x.UpdatedAt), x.Name, b2i(x.Enabled), x.Cron, x.EveryMinutes, x.Target, nullable(x.CardID), x.Prompt, b2i(x.CatchUp), tsPtr(x.NextRunAt), tsPtr(x.LastRunAt), x.LastResult, x.ID))
}

// Delete removes a routine.
func (r *Routines) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM routines WHERE id = ?`, id))
}
