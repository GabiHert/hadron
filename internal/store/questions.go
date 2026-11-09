package store

import (
	"database/sql"
	"encoding/json"

	"github.com/GabiHert/hadron/internal/domain"
)

// Questions is the repo for questions.
type Questions struct{ s *Store }

const qCols = `id, created_at, updated_at, workspace_id, card_id, seq, kind, text, context, options, multi, approval, status, answer, answered_at`

func scanQuestion(r scanner) (domain.Question, error) {
	var q domain.Question
	var cr, up, opts, appr, ans string
	var multi int
	var at sql.NullString
	err := r.Scan(&q.ID, &cr, &up, &q.WorkspaceID, &q.CardID, &q.Seq, &q.Kind, &q.Text, &q.Context, &opts, &multi, &appr, &q.Status, &ans, &at)
	q.CreatedAt, q.UpdatedAt, q.Multi = parseTS(cr), parseTS(up), multi == 1
	q.Options = []domain.QuestionOption{}
	fromJSON(opts, &q.Options)
	q.Approval, q.Answer = json.RawMessage(appr), json.RawMessage(ans)
	q.AnsweredAt = scanTSPtr(at)
	return q, err
}

// Create inserts a question and numbers it Q-01, Q-02, ... per card.
func (r *Questions) Create(q *domain.Question) error {
	stamp(&q.Meta)
	if q.Status == "" {
		q.Status = "open"
	}
	if q.Options == nil {
		q.Options = []domain.QuestionOption{}
	}
	if err := r.s.q.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM questions WHERE card_id = ?`, q.CardID).Scan(&q.Seq); err != nil {
		return err
	}
	_, err := r.s.q.Exec(`INSERT INTO questions (`+qCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		q.ID, ts(q.CreatedAt), ts(q.UpdatedAt), q.WorkspaceID, q.CardID, q.Seq, q.Kind, q.Text, q.Context, toJSON(q.Options), b2i(q.Multi),
		rawOrDefault(q.Approval, "null"), q.Status, rawOrDefault(q.Answer, "null"), tsPtr(q.AnsweredAt))
	return mapErr(err)
}

// Get returns one question.
func (r *Questions) Get(id string) (domain.Question, error) {
	q, err := scanQuestion(r.s.q.QueryRow(`SELECT `+qCols+` FROM questions WHERE id = ?`, id))
	return q, mapErr(err)
}

// List returns a workspace's questions, newest first. status and cardID may be empty.
func (r *Questions) List(workspaceID, cardID, status string) ([]domain.Question, error) {
	rows, err := r.s.q.Query(`SELECT `+qCols+` FROM questions WHERE workspace_id = ? AND (? = '' OR card_id = ?) AND (? = '' OR status = ?) ORDER BY created_at DESC, rowid DESC`,
		workspaceID, cardID, cardID, status, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Question{}
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// Update writes the status and answer of q.
func (r *Questions) Update(q *domain.Question) error {
	stamp(&q.Meta)
	return affected(r.s.q.Exec(`UPDATE questions SET updated_at=?, status=?, answer=?, answered_at=? WHERE id=?`,
		ts(q.UpdatedAt), q.Status, rawOrDefault(q.Answer, "null"), tsPtr(q.AnsweredAt), q.ID))
}
