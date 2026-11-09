package store

import (
	"database/sql"

	"github.com/GabiHert/hadron/internal/domain"
)

// Sessions is the repo for PTY sessions.
type Sessions struct{ s *Store }

const sessCols = `id, created_at, updated_at, workspace_id, card_id, kind, command, cwd, pid, status, exit_code, claude_session_id, started_at, ended_at`

func scanSession(r scanner) (domain.Session, error) {
	var x domain.Session
	var cr, up, st string
	var card, ended sql.NullString
	err := r.Scan(&x.ID, &cr, &up, &x.WorkspaceID, &card, &x.Kind, &x.Command, &x.Cwd, &x.PID, &x.Status, &x.ExitCode, &x.ClaudeSessionID, &st, &ended)
	x.CreatedAt, x.UpdatedAt, x.StartedAt = parseTS(cr), parseTS(up), parseTS(st)
	x.CardID = card.String
	x.EndedAt = scanTSPtr(ended)
	return x, err
}

// Create inserts a session. StartedAt defaults to now.
func (r *Sessions) Create(x *domain.Session) error {
	stamp(&x.Meta)
	if x.StartedAt.IsZero() {
		x.StartedAt = x.CreatedAt
	}
	if x.Status == "" {
		x.Status = "running"
	}
	_, err := r.s.q.Exec(`INSERT INTO sessions (`+sessCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, ts(x.CreatedAt), ts(x.UpdatedAt), x.WorkspaceID, nullable(x.CardID), x.Kind, x.Command, x.Cwd, x.PID, x.Status, x.ExitCode, x.ClaudeSessionID, ts(x.StartedAt), tsPtr(x.EndedAt))
	return mapErr(err)
}

// Get returns one session.
func (r *Sessions) Get(id string) (domain.Session, error) {
	x, err := scanSession(r.s.q.QueryRow(`SELECT `+sessCols+` FROM sessions WHERE id = ?`, id))
	return x, mapErr(err)
}

// Latest returns the newest session of a card, or of a workspace's planner when cardID is empty.
func (r *Sessions) Latest(workspaceID, cardID string) (domain.Session, error) {
	var row *sql.Row
	if cardID == "" {
		row = r.s.q.QueryRow(`SELECT `+sessCols+` FROM sessions WHERE workspace_id = ? AND kind = 'planner' ORDER BY started_at DESC, rowid DESC LIMIT 1`, workspaceID)
	} else {
		row = r.s.q.QueryRow(`SELECT `+sessCols+` FROM sessions WHERE card_id = ? ORDER BY started_at DESC, rowid DESC LIMIT 1`, cardID)
	}
	x, err := scanSession(row)
	return x, mapErr(err)
}

// ListRunning returns every session that is still marked running.
func (r *Sessions) ListRunning() ([]domain.Session, error) {
	rows, err := r.s.q.Query(`SELECT ` + sessCols + ` FROM sessions WHERE status = 'running' ORDER BY started_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Session{}
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Update writes every field of x.
func (r *Sessions) Update(x *domain.Session) error {
	stamp(&x.Meta)
	return affected(r.s.q.Exec(`UPDATE sessions SET updated_at=?, command=?, cwd=?, pid=?, status=?, exit_code=?, claude_session_id=?, ended_at=? WHERE id=?`,
		ts(x.UpdatedAt), x.Command, x.Cwd, x.PID, x.Status, x.ExitCode, x.ClaudeSessionID, tsPtr(x.EndedAt), x.ID))
}

// MarkRunningExited marks sessions left "running" by a previous run as exited. Call it at startup.
func (r *Sessions) MarkRunningExited() (int64, error) {
	n := ts(now())
	res, err := r.s.q.Exec(`UPDATE sessions SET status='exited', ended_at=?, updated_at=? WHERE status='running'`, n, n)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ByClaudeID returns the sessions that hold a Claude Code session id, newest first.
func (r *Sessions) ByClaudeID(claudeSessionID string) ([]domain.Session, error) {
	rows, err := r.s.q.Query(`SELECT `+sessCols+` FROM sessions WHERE claude_session_id = ? AND claude_session_id != '' ORDER BY created_at DESC, rowid DESC`, claudeSessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Session{}
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
