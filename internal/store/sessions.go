package store

import (
	"database/sql"
	"syscall"

	"github.com/GabiHert/hadron/internal/domain"
)

// Sessions is the repo for PTY sessions.
type Sessions struct{ s *Store }

const sessCols = `id, created_at, updated_at, workspace_id, card_id, kind, command, cwd, pid, status, exit_code, claude_session_id, started_at, ended_at, restore`

func scanSession(r scanner) (domain.Session, error) {
	var x domain.Session
	var cr, up, st string
	var card, ended sql.NullString
	var restore int
	err := r.Scan(&x.ID, &cr, &up, &x.WorkspaceID, &card, &x.Kind, &x.Command, &x.Cwd, &x.PID, &x.Status, &x.ExitCode, &x.ClaudeSessionID, &st, &ended, &restore)
	x.CreatedAt, x.UpdatedAt, x.StartedAt = parseTS(cr), parseTS(up), parseTS(st)
	x.CardID = card.String
	x.Restore = restore == 1
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
	_, err := r.s.q.Exec(`INSERT INTO sessions (`+sessCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, ts(x.CreatedAt), ts(x.UpdatedAt), x.WorkspaceID, nullable(x.CardID), x.Kind, x.Command, x.Cwd, x.PID, x.Status, x.ExitCode, x.ClaudeSessionID, ts(x.StartedAt), tsPtr(x.EndedAt), b2i(x.Restore))
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

// MarkRunningExited marks sessions left "running" by a previous run as exited, and returns how
// many. It never touches a row whose process is still alive: such a session belongs to a Hadron
// that is running (a second launch of the binary must not flip the live app's sessions), so only
// a row with no pid, or a pid that no longer exists, is marked.
func (r *Sessions) MarkRunningExited() (int64, error) {
	return r.MarkRunningExitedIf(ProcessAlive)
}

// MarkRunningExitedIf is MarkRunningExited with the liveness check supplied, for tests.
func (r *Sessions) MarkRunningExitedIf(alive func(pid int) bool) (int64, error) {
	running, err := r.ListRunning()
	if err != nil {
		return 0, err
	}
	n := ts(now())
	var marked int64
	for _, x := range running {
		if x.PID > 0 && alive(x.PID) {
			continue
		}
		res, err := r.s.q.Exec(`UPDATE sessions SET status='exited', ended_at=?, updated_at=? WHERE id=? AND status='running'`, n, n, x.ID)
		if err != nil {
			return marked, err
		}
		c, _ := res.RowsAffected()
		marked += c
	}
	return marked, nil
}

// MarkExited marks one running session exited and leaves its restore flag as it is.
func (r *Sessions) MarkExited(id string) error {
	n := ts(now())
	_, err := r.s.q.Exec(`UPDATE sessions SET status='exited', ended_at=?, updated_at=? WHERE id=? AND status='running'`, n, n, id)
	return err
}

// ProcessAlive says whether a process with this pid exists (signal 0).
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
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

// SetRestore flags or unflags a session for restoring after a restart.
func (r *Sessions) SetRestore(id string, restore bool) error {
	_, err := r.s.q.Exec(`UPDATE sessions SET restore=? WHERE id=?`, b2i(restore), id)
	return err
}

// ClearRestore unflags every session.
func (r *Sessions) ClearRestore() error {
	_, err := r.s.q.Exec(`UPDATE sessions SET restore=0 WHERE restore=1`)
	return err
}

// ListRestorable returns the sessions flagged for restoring, the newest per planner or card, the
// planners first and then the cards by when they started.
func (r *Sessions) ListRestorable() ([]domain.Session, error) {
	rows, err := r.s.q.Query(`SELECT ` + sessCols + ` FROM sessions WHERE restore = 1 ORDER BY (card_id IS NOT NULL), started_at DESC, rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []domain.Session
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, x)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []domain.Session
	for _, x := range all { // newest first within each group, so the first of an owner wins
		key := x.CardID
		if key == "" {
			key = "planner:" + x.WorkspaceID
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, x)
	}
	// Planners first (already), then cards oldest first so they come back in their original order.
	cards := out[:0:0]
	var planners []domain.Session
	for _, x := range out {
		if x.CardID == "" {
			planners = append(planners, x)
		} else {
			cards = append(cards, x)
		}
	}
	for i, j := 0, len(cards)-1; i < j; i, j = i+1, j-1 {
		cards[i], cards[j] = cards[j], cards[i]
	}
	return append(planners, cards...), nil
}
