package store

import (
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
)

// ---- rules ----

// Rules is the repo for rules.
type Rules struct{ s *Store }

const ruleCols = `id, created_at, updated_at, workspace_id, position, name, enabled, "trigger", "condition", actions, builtin`

func scanRule(r scanner) (domain.Rule, error) {
	var x domain.Rule
	var cr, up, trig, act string
	var en, bi int
	err := r.Scan(&x.ID, &cr, &up, &x.WorkspaceID, &x.Position, &x.Name, &en, &trig, &x.Condition, &act, &bi)
	x.CreatedAt, x.UpdatedAt, x.Enabled, x.Builtin = parseTS(cr), parseTS(up), en == 1, bi == 1
	x.Trigger, x.Actions = json.RawMessage(trig), json.RawMessage(act)
	return x, err
}

// Create appends a rule to the end of the workspace's rules.
func (r *Rules) Create(x *domain.Rule) error {
	stamp(&x.Meta)
	if err := r.s.q.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM rules WHERE workspace_id = ?`, x.WorkspaceID).Scan(&x.Position); err != nil {
		return err
	}
	_, err := r.s.q.Exec(`INSERT INTO rules (`+ruleCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, ts(x.CreatedAt), ts(x.UpdatedAt), x.WorkspaceID, x.Position, x.Name, b2i(x.Enabled), rawOrDefault(x.Trigger, "{}"), x.Condition, rawOrDefault(x.Actions, "[]"), b2i(x.Builtin))
	return mapErr(err)
}

// Get returns one rule.
func (r *Rules) Get(id string) (domain.Rule, error) {
	x, err := scanRule(r.s.q.QueryRow(`SELECT `+ruleCols+` FROM rules WHERE id = ?`, id))
	return x, mapErr(err)
}

// List returns a workspace's rules in evaluation order.
func (r *Rules) List(workspaceID string) ([]domain.Rule, error) {
	rows, err := r.s.q.Query(`SELECT `+ruleCols+` FROM rules WHERE workspace_id = ? ORDER BY position, created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Rule{}
	for rows.Next() {
		x, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Update writes every field of x.
func (r *Rules) Update(x *domain.Rule) error {
	stamp(&x.Meta)
	return affected(r.s.q.Exec(`UPDATE rules SET updated_at=?, position=?, name=?, enabled=?, "trigger"=?, "condition"=?, actions=?, builtin=? WHERE id=?`,
		ts(x.UpdatedAt), x.Position, x.Name, b2i(x.Enabled), rawOrDefault(x.Trigger, "{}"), x.Condition, rawOrDefault(x.Actions, "[]"), b2i(x.Builtin), x.ID))
}

// Delete removes a rule.
func (r *Rules) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM rules WHERE id = ?`, id))
}

// ---- views ----

// Views is the repo for saved filter views.
type Views struct{ s *Store }

const viewCols = `id, created_at, updated_at, workspace_id, name, filters, position`

func scanView(r scanner) (domain.View, error) {
	var v domain.View
	var cr, up, f string
	err := r.Scan(&v.ID, &cr, &up, &v.WorkspaceID, &v.Name, &f, &v.Position)
	v.CreatedAt, v.UpdatedAt, v.Filters = parseTS(cr), parseTS(up), json.RawMessage(f)
	return v, err
}

// Create appends a view.
func (r *Views) Create(v *domain.View) error {
	stamp(&v.Meta)
	if err := r.s.q.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM views WHERE workspace_id = ?`, v.WorkspaceID).Scan(&v.Position); err != nil {
		return err
	}
	_, err := r.s.q.Exec(`INSERT INTO views (`+viewCols+`) VALUES (?,?,?,?,?,?,?)`,
		v.ID, ts(v.CreatedAt), ts(v.UpdatedAt), v.WorkspaceID, v.Name, rawOrDefault(v.Filters, "{}"), v.Position)
	return mapErr(err)
}

// Get returns one view.
func (r *Views) Get(id string) (domain.View, error) {
	v, err := scanView(r.s.q.QueryRow(`SELECT `+viewCols+` FROM views WHERE id = ?`, id))
	return v, mapErr(err)
}

// List returns a workspace's views in order.
func (r *Views) List(workspaceID string) ([]domain.View, error) {
	rows, err := r.s.q.Query(`SELECT `+viewCols+` FROM views WHERE workspace_id = ? ORDER BY position, created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.View{}
	for rows.Next() {
		v, err := scanView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Update writes every field of v.
func (r *Views) Update(v *domain.View) error {
	stamp(&v.Meta)
	return affected(r.s.q.Exec(`UPDATE views SET updated_at=?, name=?, filters=?, position=? WHERE id=?`, ts(v.UpdatedAt), v.Name, rawOrDefault(v.Filters, "{}"), v.Position, v.ID))
}

// Delete removes a view.
func (r *Views) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM views WHERE id = ?`, id))
}

// ---- dashboard blocks ----

// Blocks is the repo for dashboard blocks.
type Blocks struct{ s *Store }

const blockCols = `id, created_at, updated_at, workspace_id, position, type, title, span, source, html, schedule, text, text_at`

func scanBlock(r scanner) (domain.Block, error) {
	var b domain.Block
	var cr, up, src string
	var at sql.NullString
	err := r.Scan(&b.ID, &cr, &up, &b.WorkspaceID, &b.Position, &b.Type, &b.Title, &b.Span, &src, &b.HTML, &b.Schedule, &b.Text, &at)
	b.CreatedAt, b.UpdatedAt, b.Source, b.TextAt = parseTS(cr), parseTS(up), json.RawMessage(src), scanTSPtr(at)
	return b, err
}

// Create appends a block. Span defaults to 1.
func (r *Blocks) Create(b *domain.Block) error {
	stamp(&b.Meta)
	if b.Span == 0 {
		b.Span = 1
	}
	if err := r.s.q.QueryRow(`SELECT COALESCE(MAX(position), -1) + 1 FROM dash_blocks WHERE workspace_id = ?`, b.WorkspaceID).Scan(&b.Position); err != nil {
		return err
	}
	_, err := r.s.q.Exec(`INSERT INTO dash_blocks (`+blockCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, ts(b.CreatedAt), ts(b.UpdatedAt), b.WorkspaceID, b.Position, b.Type, b.Title, b.Span, rawOrDefault(b.Source, "{}"), b.HTML, b.Schedule, b.Text, tsPtr(b.TextAt))
	return mapErr(err)
}

// Get returns one block.
func (r *Blocks) Get(id string) (domain.Block, error) {
	b, err := scanBlock(r.s.q.QueryRow(`SELECT `+blockCols+` FROM dash_blocks WHERE id = ?`, id))
	return b, mapErr(err)
}

// List returns a workspace's blocks in order.
func (r *Blocks) List(workspaceID string) ([]domain.Block, error) {
	rows, err := r.s.q.Query(`SELECT `+blockCols+` FROM dash_blocks WHERE workspace_id = ? ORDER BY position, created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Block{}
	for rows.Next() {
		b, err := scanBlock(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Update writes every field of b.
func (r *Blocks) Update(b *domain.Block) error {
	stamp(&b.Meta)
	return affected(r.s.q.Exec(`UPDATE dash_blocks SET updated_at=?, position=?, type=?, title=?, span=?, source=?, html=?, schedule=?, text=?, text_at=? WHERE id=?`,
		ts(b.UpdatedAt), b.Position, b.Type, b.Title, b.Span, rawOrDefault(b.Source, "{}"), b.HTML, b.Schedule, b.Text, tsPtr(b.TextAt), b.ID))
}

// Delete removes a block.
func (r *Blocks) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM dash_blocks WHERE id = ?`, id))
}

// ---- prompts ----

// Prompts is the repo for per-workspace prompt overrides.
type Prompts struct{ s *Store }

const promptCols = `id, created_at, updated_at, workspace_id, kind, template`

func scanPrompt(r scanner) (domain.Prompt, error) {
	var p domain.Prompt
	var cr, up string
	err := r.Scan(&p.ID, &cr, &up, &p.WorkspaceID, &p.Kind, &p.Template)
	p.CreatedAt, p.UpdatedAt = parseTS(cr), parseTS(up)
	return p, err
}

// Get returns a workspace's prompt row of a kind.
func (r *Prompts) Get(workspaceID, kind string) (domain.Prompt, error) {
	p, err := scanPrompt(r.s.q.QueryRow(`SELECT `+promptCols+` FROM prompts WHERE workspace_id = ? AND kind = ?`, workspaceID, kind))
	return p, mapErr(err)
}

// List returns a workspace's prompt rows.
func (r *Prompts) List(workspaceID string) ([]domain.Prompt, error) {
	rows, err := r.s.q.Query(`SELECT `+promptCols+` FROM prompts WHERE workspace_id = ? ORDER BY kind`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Prompt{}
	for rows.Next() {
		p, err := scanPrompt(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Set creates or replaces the row of (workspace, kind). A nil template means the default.
func (r *Prompts) Set(workspaceID, kind string, template *string) error {
	n := ts(now())
	_, err := r.s.q.Exec(`INSERT INTO prompts (id, created_at, updated_at, workspace_id, kind, template) VALUES (?,?,?,?,?,?)
		ON CONFLICT (workspace_id, kind) DO UPDATE SET template = excluded.template, updated_at = excluded.updated_at`,
		newID(), n, n, workspaceID, kind, template)
	return mapErr(err)
}

// ---- launch profiles ----

// LaunchProfiles is the repo for launch profiles.
type LaunchProfiles struct{ s *Store }

const lpCols = `id, created_at, updated_at, workspace_id, name, command, args, first_prompt, env, worktree, prompt, model`

func scanLaunchProfile(r scanner) (domain.LaunchProfile, error) {
	var p domain.LaunchProfile
	var cr, up, args, env string
	var wt int
	err := r.Scan(&p.ID, &cr, &up, &p.WorkspaceID, &p.Name, &p.Command, &args, &p.FirstPrompt, &env, &wt, &p.Prompt, &p.Model)
	p.CreatedAt, p.UpdatedAt, p.Worktree = parseTS(cr), parseTS(up), wt == 1
	p.Args, p.Env = []string{}, map[string]string{}
	fromJSON(args, &p.Args)
	fromJSON(env, &p.Env)
	return p, err
}

// Create inserts a launch profile. Command defaults to claude.
func (r *LaunchProfiles) Create(p *domain.LaunchProfile) error {
	stamp(&p.Meta)
	if p.Command == "" {
		p.Command = "claude"
	}
	if p.Args == nil {
		p.Args = []string{}
	}
	if p.Env == nil {
		p.Env = map[string]string{}
	}
	_, err := r.s.q.Exec(`INSERT INTO launch_profiles (`+lpCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, ts(p.CreatedAt), ts(p.UpdatedAt), p.WorkspaceID, p.Name, p.Command, toJSON(p.Args), p.FirstPrompt, toJSON(p.Env), b2i(p.Worktree), p.Prompt, p.Model)
	return mapErr(err)
}

// Get returns one launch profile.
func (r *LaunchProfiles) Get(id string) (domain.LaunchProfile, error) {
	p, err := scanLaunchProfile(r.s.q.QueryRow(`SELECT `+lpCols+` FROM launch_profiles WHERE id = ?`, id))
	return p, mapErr(err)
}

// List returns a workspace's launch profiles.
func (r *LaunchProfiles) List(workspaceID string) ([]domain.LaunchProfile, error) {
	rows, err := r.s.q.Query(`SELECT `+lpCols+` FROM launch_profiles WHERE workspace_id = ? ORDER BY created_at, rowid`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.LaunchProfile{}
	for rows.Next() {
		p, err := scanLaunchProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Update writes every field of p.
func (r *LaunchProfiles) Update(p *domain.LaunchProfile) error {
	stamp(&p.Meta)
	return affected(r.s.q.Exec(`UPDATE launch_profiles SET updated_at=?, name=?, command=?, args=?, first_prompt=?, env=?, worktree=?, prompt=?, model=? WHERE id=?`,
		ts(p.UpdatedAt), p.Name, p.Command, toJSON(p.Args), p.FirstPrompt, toJSON(p.Env), b2i(p.Worktree), p.Prompt, p.Model, p.ID))
}

// Delete removes a launch profile.
func (r *LaunchProfiles) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM launch_profiles WHERE id = ?`, id))
}

// ---- scripts ----

// Scripts is the repo for integrations and user scripts.
type Scripts struct{ s *Store }

const scriptCols = `id, created_at, updated_at, workspace_id, name, kind, manifest, source, status, approved_by, approved_at, test_result`

func scanScript(r scanner) (domain.Script, error) {
	var x domain.Script
	var cr, up, man, tr string
	var ws, appr sql.NullString
	err := r.Scan(&x.ID, &cr, &up, &ws, &x.Name, &x.Kind, &man, &x.Source, &x.Status, &x.ApprovedBy, &appr, &tr)
	x.CreatedAt, x.UpdatedAt, x.WorkspaceID = parseTS(cr), parseTS(up), ws.String
	x.Manifest, x.TestResult, x.ApprovedAt = json.RawMessage(man), json.RawMessage(tr), scanTSPtr(appr)
	return x, err
}

// Create inserts a script. Status defaults to pending.
func (r *Scripts) Create(x *domain.Script) error {
	stamp(&x.Meta)
	if x.Status == "" {
		x.Status = "pending"
	}
	_, err := r.s.q.Exec(`INSERT INTO scripts (`+scriptCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, ts(x.CreatedAt), ts(x.UpdatedAt), nullable(x.WorkspaceID), x.Name, x.Kind, rawOrDefault(x.Manifest, "{}"), x.Source, x.Status, x.ApprovedBy, tsPtr(x.ApprovedAt), rawOrDefault(x.TestResult, "null"))
	return mapErr(err)
}

// Get returns one script.
func (r *Scripts) Get(id string) (domain.Script, error) {
	x, err := scanScript(r.s.q.QueryRow(`SELECT `+scriptCols+` FROM scripts WHERE id = ?`, id))
	return x, mapErr(err)
}

// List returns the scripts of a workspace plus the global ones.
func (r *Scripts) List(workspaceID string) ([]domain.Script, error) {
	rows, err := r.s.q.Query(`SELECT `+scriptCols+` FROM scripts WHERE workspace_id = ? OR workspace_id IS NULL ORDER BY created_at, rowid`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Script{}
	for rows.Next() {
		x, err := scanScript(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// ListAll returns every script of every workspace.
func (r *Scripts) ListAll() ([]domain.Script, error) {
	rows, err := r.s.q.Query(`SELECT ` + scriptCols + ` FROM scripts ORDER BY created_at, rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Script{}
	for rows.Next() {
		x, err := scanScript(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Update writes every field of x.
func (r *Scripts) Update(x *domain.Script) error {
	stamp(&x.Meta)
	return affected(r.s.q.Exec(`UPDATE scripts SET updated_at=?, name=?, manifest=?, source=?, status=?, approved_by=?, approved_at=?, test_result=? WHERE id=?`,
		ts(x.UpdatedAt), x.Name, rawOrDefault(x.Manifest, "{}"), x.Source, x.Status, x.ApprovedBy, tsPtr(x.ApprovedAt), rawOrDefault(x.TestResult, "null"), x.ID))
}

// Delete removes a script.
func (r *Scripts) Delete(id string) error {
	return affected(r.s.q.Exec(`DELETE FROM scripts WHERE id = ?`, id))
}

// ---- changes ----

// Changes is the repo for the config history.
type Changes struct{ s *Store }

const changeCols = `id, created_at, updated_at, workspace_id, actor, action, args, "before", "after", reverted`

func scanChange(r scanner) (domain.Change, error) {
	var c domain.Change
	var cr, up, args, before, after string
	var rev int
	err := r.Scan(&c.ID, &cr, &up, &c.WorkspaceID, &c.Actor, &c.Action, &args, &before, &after, &rev)
	c.CreatedAt, c.UpdatedAt, c.Reverted = parseTS(cr), parseTS(up), rev == 1
	c.Args, c.Before, c.After = json.RawMessage(args), json.RawMessage(before), json.RawMessage(after)
	return c, err
}

// Create inserts a change.
func (r *Changes) Create(c *domain.Change) error {
	stamp(&c.Meta)
	_, err := r.s.q.Exec(`INSERT INTO changes (`+changeCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ID, ts(c.CreatedAt), ts(c.UpdatedAt), c.WorkspaceID, c.Actor, c.Action, rawOrDefault(c.Args, "null"), rawOrDefault(c.Before, "null"), rawOrDefault(c.After, "null"), b2i(c.Reverted))
	return mapErr(err)
}

// Get returns one change.
func (r *Changes) Get(id string) (domain.Change, error) {
	c, err := scanChange(r.s.q.QueryRow(`SELECT `+changeCols+` FROM changes WHERE id = ?`, id))
	return c, mapErr(err)
}

// List returns the newest changes of a workspace (all workspaces when empty), newest first.
func (r *Changes) List(workspaceID string, limit int) ([]domain.Change, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.s.q.Query(`SELECT `+changeCols+` FROM changes WHERE (? = '' OR workspace_id = ?) ORDER BY created_at DESC, rowid DESC LIMIT ?`, workspaceID, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Change{}
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListByAction returns a workspace's newest changes whose action is one of actions.
func (r *Changes) ListByAction(workspaceID string, actions []string, limit int) ([]domain.Change, error) {
	if limit <= 0 {
		limit = 100
	}
	marks := make([]string, len(actions))
	args := []any{workspaceID, workspaceID}
	for i, a := range actions {
		marks[i] = "?"
		args = append(args, a)
	}
	args = append(args, limit)
	rows, err := r.s.q.Query(`SELECT `+changeCols+` FROM changes WHERE (? = '' OR workspace_id = ?) AND action IN (`+strings.Join(marks, ",")+`) ORDER BY created_at DESC, rowid DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Change{}
	for rows.Next() {
		c, err := scanChange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkReverted flags a change as reverted.
func (r *Changes) MarkReverted(id string) error {
	return affected(r.s.q.Exec(`UPDATE changes SET reverted = 1, updated_at = ? WHERE id = ?`, ts(now()), id))
}

// ---- mcp calls ----

// MCPCalls is the repo for the tool-call log.
type MCPCalls struct{ s *Store }

const callCols = `id, created_at, updated_at, workspace_id, actor, tool, args, ok, error, duration_ms`

func scanCall(r scanner) (domain.MCPCall, error) {
	var c domain.MCPCall
	var cr, up, args string
	var ok int
	err := r.Scan(&c.ID, &cr, &up, &c.WorkspaceID, &c.Actor, &c.Tool, &args, &ok, &c.Error, &c.DurationMS)
	c.CreatedAt, c.UpdatedAt, c.OK, c.Args = parseTS(cr), parseTS(up), ok == 1, json.RawMessage(args)
	return c, err
}

const maxCallArgs = 2048

// Create inserts a call. Args longer than 2 KB are replaced by a JSON string holding the first 2 KB.
func (r *MCPCalls) Create(c *domain.MCPCall) error {
	stamp(&c.Meta)
	args := rawOrDefault(c.Args, "null")
	if len(args) > maxCallArgs {
		args = toJSON(args[:maxCallArgs])
		c.Args = json.RawMessage(args)
	}
	_, err := r.s.q.Exec(`INSERT INTO mcp_calls (`+callCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ID, ts(c.CreatedAt), ts(c.UpdatedAt), c.WorkspaceID, c.Actor, c.Tool, args, b2i(c.OK), c.Error, c.DurationMS)
	return mapErr(err)
}

// List returns the newest calls of a workspace (all when empty), newest first.
func (r *MCPCalls) List(workspaceID string, limit int) ([]domain.MCPCall, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.s.q.Query(`SELECT `+callCols+` FROM mcp_calls WHERE (? = '' OR workspace_id = ?) ORDER BY created_at DESC, rowid DESC LIMIT ?`, workspaceID, workspaceID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MCPCall{}
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- settings ----

// Settings is the repo for global key-value settings.
type Settings struct{ s *Store }

// Get returns the JSON value of key, or ErrNotFound.
func (r *Settings) Get(key string) (json.RawMessage, error) {
	var v string
	if err := r.s.q.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v); err != nil {
		return nil, mapErr(err)
	}
	return json.RawMessage(v), nil
}

// Set stores the JSON value of key.
func (r *Settings) Set(key string, value json.RawMessage) error {
	n := ts(now())
	_, err := r.s.q.Exec(`INSERT INTO settings (id, created_at, updated_at, key, value) VALUES (?,?,?,?,?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, newID(), n, n, key, rawOrDefault(value, "null"))
	return mapErr(err)
}

// All returns every setting.
func (r *Settings) All() (map[string]json.RawMessage, error) {
	rows, err := r.s.q.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = json.RawMessage(v)
	}
	return out, rows.Err()
}

// ---- script logs ----

// ScriptLogs is the repo for what integrations and scripts did.
type ScriptLogs struct{ s *Store }

// Create inserts a log row.
func (r *ScriptLogs) Create(l *domain.ScriptLog) error {
	stamp(&l.Meta)
	_, err := r.s.q.Exec(`INSERT INTO script_logs (id, created_at, updated_at, workspace_id, script_name, op, ok, duration_ms, message) VALUES (?,?,?,?,?,?,?,?,?)`,
		l.ID, ts(l.CreatedAt), ts(l.UpdatedAt), l.WorkspaceID, l.ScriptName, l.Op, b2i(l.OK), l.DurationMS, l.Message)
	return mapErr(err)
}

// List returns the newest rows of one script (or every script when name is empty), newest first.
func (r *ScriptLogs) List(name string, limit int) ([]domain.ScriptLog, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.s.q.Query(`SELECT id, created_at, updated_at, workspace_id, script_name, op, ok, duration_ms, message FROM script_logs WHERE (? = '' OR script_name = ?) ORDER BY created_at DESC, rowid DESC LIMIT ?`, name, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ScriptLog{}
	for rows.Next() {
		var l domain.ScriptLog
		var cr, up string
		var ok int
		if err := rows.Scan(&l.ID, &cr, &up, &l.WorkspaceID, &l.ScriptName, &l.Op, &ok, &l.DurationMS, &l.Message); err != nil {
			return nil, err
		}
		l.CreatedAt, l.UpdatedAt, l.OK = parseTS(cr), parseTS(up), ok == 1
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteByName removes a script's log rows.
func (r *ScriptLogs) DeleteByName(name string) error {
	_, err := r.s.q.Exec(`DELETE FROM script_logs WHERE script_name = ?`, name)
	return err
}
