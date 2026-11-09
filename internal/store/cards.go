package store

import (
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

func now() time.Time { return time.Now().UTC() }

// Cards is the repo for cards.
type Cards struct{ s *Store }

const cardCols = `id, created_at, updated_at, workspace_id, column_id, position, nick, key, title, assignee, stage, liveness, return_column_id, launch_profile, fields, archived`

func scanCard(r scanner) (domain.Card, error) {
	var c domain.Card
	var cr, up, fields string
	var ret *string
	var arch int
	err := r.Scan(&c.ID, &cr, &up, &c.WorkspaceID, &c.ColumnID, &c.Position, &c.Nick, &c.Key, &c.Title, &c.Assignee, &c.Stage, &c.Liveness, &ret, &c.LaunchProfile, &fields, &arch)
	c.CreatedAt, c.UpdatedAt, c.Archived = parseTS(cr), parseTS(up), arch == 1
	if ret != nil {
		c.ReturnColumnID = *ret
	}
	c.Fields = map[string]any{}
	fromJSON(fields, &c.Fields)
	return c, err
}

// Create appends c to the end of its column. The nickname must be unique in the workspace.
func (r *Cards) Create(c *domain.Card) error {
	stamp(&c.Meta)
	if c.Liveness == "" {
		c.Liveness = domain.LivenessIdle
	}
	if c.Fields == nil {
		c.Fields = map[string]any{}
	}
	if err := r.s.q.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM cards WHERE column_id = ?`, c.ColumnID).Scan(&c.Position); err != nil {
		return err
	}
	_, err := r.s.q.Exec(`INSERT INTO cards (`+cardCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, ts(c.CreatedAt), ts(c.UpdatedAt), c.WorkspaceID, c.ColumnID, c.Position, c.Nick, c.Key, c.Title, c.Assignee, c.Stage, c.Liveness,
		nullable(c.ReturnColumnID), c.LaunchProfile, toJSON(c.Fields), b2i(c.Archived))
	return mapErr(err)
}

// Get returns one card.
func (r *Cards) Get(id string) (domain.Card, error) {
	c, err := scanCard(r.s.q.QueryRow(`SELECT `+cardCols+` FROM cards WHERE id = ?`, id))
	return c, mapErr(err)
}

// ByNick returns the card with the nickname or key in a workspace.
func (r *Cards) ByNick(workspaceID, nickOrKey string) (domain.Card, error) {
	c, err := scanCard(r.s.q.QueryRow(`SELECT `+cardCols+` FROM cards WHERE workspace_id = ? AND (nick = ? OR (key != '' AND key = ?)) ORDER BY nick = ? DESC LIMIT 1`,
		workspaceID, nickOrKey, nickOrKey, nickOrKey))
	return c, mapErr(err)
}

// CardFilter narrows Cards.List. Zero fields match everything. Archived cards are hidden unless
// IncludeArchived is set.
type CardFilter struct {
	ColumnID        string
	Role            string
	Liveness        string
	IncludeArchived bool
}

// List returns a workspace's cards in column order, then position.
func (r *Cards) List(workspaceID string, f CardFilter) ([]domain.Card, error) {
	q := `SELECT ` + prefixed("c.", cardCols) + ` FROM cards c JOIN columns col ON col.id = c.column_id WHERE c.workspace_id = ?`
	args := []any{workspaceID}
	if f.ColumnID != "" {
		q += ` AND c.column_id = ?`
		args = append(args, f.ColumnID)
	}
	if f.Role != "" {
		q += ` AND col.role = ?`
		args = append(args, f.Role)
	}
	if f.Liveness != "" {
		q += ` AND c.liveness = ?`
		args = append(args, f.Liveness)
	}
	if !f.IncludeArchived {
		q += ` AND c.archived = 0`
	}
	q += ` ORDER BY col.position, c.position, c.created_at`
	rows, err := r.s.q.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Update writes every field of c.
func (r *Cards) Update(c *domain.Card) error {
	stamp(&c.Meta)
	return mapErr(affected(r.s.q.Exec(`UPDATE cards SET updated_at=?, column_id=?, position=?, nick=?, key=?, title=?, assignee=?, stage=?, liveness=?, return_column_id=?, launch_profile=?, fields=?, archived=? WHERE id=?`,
		ts(c.UpdatedAt), c.ColumnID, c.Position, c.Nick, c.Key, c.Title, c.Assignee, c.Stage, c.Liveness, nullable(c.ReturnColumnID), c.LaunchProfile, toJSON(c.Fields), b2i(c.Archived), c.ID)))
}

// SetLiveness records a card's liveness. A card that is gone is ignored: a session can outlive it by a moment.
func (r *Cards) SetLiveness(id, liveness string) error {
	_, err := r.s.q.Exec(`UPDATE cards SET liveness=?, updated_at=? WHERE id=?`, liveness, ts(now()), id)
	return err
}

// Delete removes a card and, by cascade, its sessions, questions, gates and links.
func (r *Cards) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM cards WHERE id = ?`, id))
}

func prefixed(p, cols string) string {
	out := ""
	start := 0
	for i := 0; i <= len(cols); i++ {
		if i == len(cols) || cols[i] == ',' {
			name := cols[start:i]
			for len(name) > 0 && name[0] == ' ' {
				name = name[1:]
			}
			if out != "" {
				out += ", "
			}
			out += p + name
			start = i + 1
		}
	}
	return out
}
