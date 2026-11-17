package store

import (
	"database/sql"

	"github.com/GabiHert/hadron/internal/domain"
)

// FreeSessions is the repo for the Sessions space's terminals.
type FreeSessions struct{ s *Store }

const freeCols = `id, created_at, updated_at, name, kind, command, cwd, workspace_id, pid, status, exit_code, claude_session_id, started_at, ended_at, restore`

func scanFree(r scanner) (domain.FreeSession, error) {
	var x domain.FreeSession
	var cr, up, st string
	var ws, ended sql.NullString
	var restore int
	err := r.Scan(&x.ID, &cr, &up, &x.Name, &x.Kind, &x.Command, &x.Cwd, &ws, &x.PID, &x.Status, &x.ExitCode, &x.ClaudeSessionID, &st, &ended, &restore)
	x.CreatedAt, x.UpdatedAt, x.StartedAt = parseTS(cr), parseTS(up), parseTS(st)
	x.WorkspaceID = ws.String
	x.Restore = restore == 1
	x.EndedAt = scanTSPtr(ended)
	return x, err
}

// Create inserts a free session. StartedAt defaults to now and Status to running.
func (r *FreeSessions) Create(x *domain.FreeSession) error {
	stamp(&x.Meta)
	if x.StartedAt.IsZero() {
		x.StartedAt = x.CreatedAt
	}
	if x.Status == "" {
		x.Status = "running"
	}
	_, err := r.s.q.Exec(`INSERT INTO free_sessions (`+freeCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, ts(x.CreatedAt), ts(x.UpdatedAt), x.Name, x.Kind, x.Command, x.Cwd, nullable(x.WorkspaceID), x.PID, x.Status, x.ExitCode, x.ClaudeSessionID, ts(x.StartedAt), tsPtr(x.EndedAt), b2i(x.Restore))
	return mapErr(err)
}

// Get returns one free session.
func (r *FreeSessions) Get(id string) (domain.FreeSession, error) {
	x, err := scanFree(r.s.q.QueryRow(`SELECT `+freeCols+` FROM free_sessions WHERE id = ?`, id))
	return x, mapErr(err)
}

// List returns every free session, oldest first.
func (r *FreeSessions) List() ([]domain.FreeSession, error) {
	return r.query(`SELECT ` + freeCols + ` FROM free_sessions ORDER BY created_at, rowid`)
}

// ListRestorable returns the free sessions meant to be running, oldest first.
func (r *FreeSessions) ListRestorable() ([]domain.FreeSession, error) {
	return r.query(`SELECT ` + freeCols + ` FROM free_sessions WHERE restore = 1 ORDER BY created_at, rowid`)
}

func (r *FreeSessions) query(q string, args ...any) ([]domain.FreeSession, error) {
	rows, err := r.s.q.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.FreeSession{}
	for rows.Next() {
		x, err := scanFree(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Update writes every field of x.
func (r *FreeSessions) Update(x *domain.FreeSession) error {
	stamp(&x.Meta)
	return affected(r.s.q.Exec(`UPDATE free_sessions SET updated_at=?, name=?, kind=?, command=?, cwd=?, workspace_id=?, pid=?, status=?, exit_code=?, claude_session_id=?, started_at=?, ended_at=?, restore=? WHERE id=?`,
		ts(x.UpdatedAt), x.Name, x.Kind, x.Command, x.Cwd, nullable(x.WorkspaceID), x.PID, x.Status, x.ExitCode, x.ClaudeSessionID, ts(x.StartedAt), tsPtr(x.EndedAt), b2i(x.Restore), x.ID))
}

// Delete removes a free session.
func (r *FreeSessions) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM free_sessions WHERE id = ?`, id))
}

// MarkRunningExited marks free sessions a previous run left "running" as exited, keeping their
// restore flag, unless their process is still alive (a second Hadron must not touch the live one's).
func (r *FreeSessions) MarkRunningExited(alive func(pid int) bool) error {
	list, err := r.query(`SELECT ` + freeCols + ` FROM free_sessions WHERE status = 'running'`)
	if err != nil {
		return err
	}
	n := ts(now())
	for _, x := range list {
		if x.PID > 0 && alive(x.PID) {
			continue
		}
		if _, err := r.s.q.Exec(`UPDATE free_sessions SET status='exited', ended_at=?, updated_at=? WHERE id=? AND status='running'`, n, n, x.ID); err != nil {
			return err
		}
	}
	return nil
}
