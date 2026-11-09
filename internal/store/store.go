// Package store keeps Hadron's data in SQLite (WAL) with embedded, versioned migrations.
// Adapted from Deputy's store.go; the inline schema became migrations/*.sql.
package store

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrDuplicate is returned when a unique constraint rejects a row, such as a card nickname that
// the workspace already uses.
var ErrDuplicate = errors.New("already exists")

// Store is the SQLite store. It is safe for concurrent use: it holds one connection, so SQLite's
// single writer never sees contention, and every multi-statement change runs in a transaction.
type Store struct {
	db *sql.DB
	q  queryer // db, or the transaction inside Tx

	Workspaces     *Workspaces
	Columns        *Columns
	Cards          *Cards
	Sessions       *Sessions
	Messages       *Messages
	Questions      *Questions
	Gates          *Gates
	Links          *Links
	Rules          *Rules
	Views          *Views
	Blocks         *Blocks
	Prompts        *Prompts
	LaunchProfiles *LaunchProfiles
	Scripts        *Scripts
	ScriptLogs     *ScriptLogs
	Changes        *Changes
	MCPCalls       *MCPCalls
	Settings       *Settings
}

// queryer is what both *sql.DB and *sql.Tx offer, so a repo method can run in either.
type queryer interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Open opens (and creates) the database at path and applies any pending migrations.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", "file:"+path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on&_synchronous=NORMAL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("could not update the database: %w", err)
	}
	s := &Store{db: db, q: db}
	s.wire()
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) wire() {
	s.Workspaces = &Workspaces{s}
	s.Columns = &Columns{s}
	s.Cards = &Cards{s}
	s.Sessions = &Sessions{s}
	s.Messages = &Messages{s}
	s.Questions = &Questions{s}
	s.Gates = &Gates{s}
	s.Links = &Links{s}
	s.Rules = &Rules{s}
	s.Views = &Views{s}
	s.Blocks = &Blocks{s}
	s.Prompts = &Prompts{s}
	s.LaunchProfiles = &LaunchProfiles{s}
	s.Scripts = &Scripts{s}
	s.ScriptLogs = &ScriptLogs{s}
	s.Changes = &Changes{s}
	s.MCPCalls = &MCPCalls{s}
	s.Settings = &Settings{s}
}

// Tx runs fn in a transaction and commits it, or rolls back when fn returns an error. fn gets a
// Store whose repos all run inside the transaction. Tx cannot be nested.
func (s *Store) Tx(fn func(tx *Store) error) error {
	if _, nested := s.q.(*sql.Tx); nested {
		return errors.New("store: Tx cannot be nested")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	t := &Store{db: s.db, q: tx}
	t.wire()
	if err := fn(t); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SchemaVersion is the newest migration applied.
func (s *Store) SchemaVersion() (int, error) {
	var v sql.NullInt64
	err := s.q.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: the name must start with a number", name)
		}
		var done int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&done); err != nil {
			return err
		}
		if done > 0 {
			continue
		}
		body, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`, version, name, ts(time.Now())); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---- helpers shared by the repos ----

func newID() string { return uuid.NewString() }

// tsLayout is RFC 3339 with all nine fractional digits. RFC3339Nano drops trailing zeros, and then
// "…08.828Z" sorts after "…08.8281Z" as text ('Z' beats a digit), which scrambled ORDER BY created_at.
const tsLayout = "2006-01-02T15:04:05.000000000Z07:00"

func ts(t time.Time) string { return t.UTC().Format(tsLayout) }

func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func tsPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func scanTSPtr(s sql.NullString) *time.Time {
	if !s.Valid || s.String == "" {
		return nil
	}
	t := parseTS(s.String)
	return &t
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullable stores an empty string as NULL, so a foreign key can be absent.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func toJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// rawOrDefault stores a RawMessage, or def when it is empty.
func rawOrDefault(raw json.RawMessage, def string) string {
	if len(raw) == 0 {
		return def
	}
	return string(raw)
}

func fromJSON(s string, v any) {
	if s != "" {
		_ = json.Unmarshal([]byte(s), v)
	}
}

// stamp gives a new row its id and timestamps.
func stamp(m *domainMeta) {
	n := time.Now().UTC()
	if m.ID == "" {
		m.ID = newID()
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = n
	}
	m.UpdatedAt = n
}

// mapErr turns driver errors into the store's sentinel errors.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var se sqlite3.Error
	if errors.As(err, &se) && (se.ExtendedCode == sqlite3.ErrConstraintUnique || se.ExtendedCode == sqlite3.ErrConstraintPrimaryKey) {
		return fmt.Errorf("%w: %v", ErrDuplicate, err)
	}
	return err
}

// affected returns ErrNotFound when a write touched no row.
func affected(res sql.Result, err error) error {
	if err != nil {
		return mapErr(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
