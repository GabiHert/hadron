package store

import (
	"encoding/json"

	"github.com/GabiHert/hadron/internal/domain"
)

// Feedback is the repo for the feedback inbox.
type Feedback struct{ s *Store }

const fbCols = `id, created_at, updated_at, kind, text, area, version, diagnostics, status, note, github_url`

func scanFeedback(r scanner) (domain.Feedback, error) {
	var f domain.Feedback
	var cr, up, diag string
	err := r.Scan(&f.ID, &cr, &up, &f.Kind, &f.Text, &f.Area, &f.Version, &diag, &f.Status, &f.Note, &f.GithubURL)
	f.CreatedAt, f.UpdatedAt, f.Diagnostics = parseTS(cr), parseTS(up), json.RawMessage(diag)
	return f, err
}

// Create inserts a note. Status defaults to new.
func (r *Feedback) Create(f *domain.Feedback) error {
	stamp(&f.Meta)
	if f.Status == "" {
		f.Status = "new"
	}
	_, err := r.s.q.Exec(`INSERT INTO feedback (`+fbCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		f.ID, ts(f.CreatedAt), ts(f.UpdatedAt), f.Kind, f.Text, f.Area, f.Version, rawOrDefault(f.Diagnostics, "null"), f.Status, f.Note, f.GithubURL)
	return mapErr(err)
}

// Get returns one note.
func (r *Feedback) Get(id string) (domain.Feedback, error) {
	f, err := scanFeedback(r.s.q.QueryRow(`SELECT `+fbCols+` FROM feedback WHERE id = ?`, id))
	return f, mapErr(err)
}

// List returns notes newest first, optionally only those with a status.
func (r *Feedback) List(status string) ([]domain.Feedback, error) {
	rows, err := r.s.q.Query(`SELECT `+fbCols+` FROM feedback WHERE (? = '' OR status = ?) ORDER BY created_at DESC, rowid DESC`, status, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Feedback{}
	for rows.Next() {
		f, err := scanFeedback(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Update writes the status, note and issue link of a note.
func (r *Feedback) Update(f *domain.Feedback) error {
	stamp(&f.Meta)
	return affected(r.s.q.Exec(`UPDATE feedback SET updated_at=?, status=?, note=?, github_url=? WHERE id=?`, ts(f.UpdatedAt), f.Status, f.Note, f.GithubURL, f.ID))
}
