package store

import (
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

// Messages is the repo for chat messages.
type Messages struct{ s *Store }

const msgCols = `id, created_at, updated_at, workspace_id, channel, author, kind, report_kind, text, widget_title, widget_html, delivered, reason, question_id`

func scanMessage(r scanner) (domain.Message, error) {
	var m domain.Message
	var cr, up string
	var del int
	err := r.Scan(&m.ID, &cr, &up, &m.WorkspaceID, &m.Channel, &m.Author, &m.Kind, &m.ReportKind, &m.Text, &m.WidgetTitle, &m.WidgetHTML, &del, &m.Reason, &m.QuestionID)
	m.CreatedAt, m.UpdatedAt, m.Delivered = parseTS(cr), parseTS(up), del == 1
	return m, err
}

// Create inserts a message. Kind defaults to text.
func (r *Messages) Create(m *domain.Message) error {
	stamp(&m.Meta)
	if m.Kind == "" {
		m.Kind = "text"
	}
	_, err := r.s.q.Exec(`INSERT INTO messages (`+msgCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.ID, ts(m.CreatedAt), ts(m.UpdatedAt), m.WorkspaceID, m.Channel, m.Author, m.Kind, m.ReportKind, m.Text, m.WidgetTitle, m.WidgetHTML, b2i(m.Delivered), m.Reason, m.QuestionID)
	return mapErr(err)
}

// Get returns one message.
func (r *Messages) Get(id string) (domain.Message, error) {
	m, err := scanMessage(r.s.q.QueryRow(`SELECT `+msgCols+` FROM messages WHERE id = ?`, id))
	return m, mapErr(err)
}

// List returns a channel's messages, oldest first. since (RFC 3339, may be empty) skips older ones.
func (r *Messages) List(workspaceID, channel, since string) ([]domain.Message, error) {
	rows, err := r.s.q.Query(`SELECT `+msgCols+` FROM messages WHERE workspace_id = ? AND channel = ? AND created_at > ? ORDER BY created_at, rowid`, workspaceID, channel, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Update writes the delivery fields and text of m.
func (r *Messages) Update(m *domain.Message) error {
	stamp(&m.Meta)
	return affected(r.s.q.Exec(`UPDATE messages SET updated_at=?, text=?, delivered=?, reason=? WHERE id=?`, ts(m.UpdatedAt), m.Text, b2i(m.Delivered), m.Reason, m.ID))
}

// ChannelSummary is one channel's size and newest message.
type ChannelSummary struct {
	Channel    string    `json:"channel"`
	Count      int       `json:"count"`
	LastAt     time.Time `json:"last_at"`
	LastAuthor string    `json:"last_author"`
	LastKind   string    `json:"last_kind"`
	LastText   string    `json:"last_text"`
}

// Channels summarizes every channel of a workspace that has messages, newest activity first.
func (r *Messages) Channels(workspaceID string) ([]ChannelSummary, error) {
	rows, err := r.s.q.Query(`
SELECT m.channel, (SELECT COUNT(*) FROM messages c WHERE c.workspace_id = m.workspace_id AND c.channel = m.channel), m.created_at, m.author, m.kind, m.text
FROM messages m
WHERE m.workspace_id = ? AND m.rowid = (SELECT rowid FROM messages l WHERE l.workspace_id = m.workspace_id AND l.channel = m.channel ORDER BY l.created_at DESC, l.rowid DESC LIMIT 1)
ORDER BY m.created_at DESC, m.rowid DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelSummary{}
	for rows.Next() {
		var c ChannelSummary
		var at string
		if err := rows.Scan(&c.Channel, &c.Count, &at, &c.LastAuthor, &c.LastKind, &c.LastText); err != nil {
			return nil, err
		}
		c.LastAt = parseTS(at)
		out = append(out, c)
	}
	return out, rows.Err()
}
