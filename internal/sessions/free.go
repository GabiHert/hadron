package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/pty"
	"github.com/GabiHert/hadron/internal/run"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/google/uuid"
)

// Free sessions are the terminals of the Sessions space: a login shell or a claude in a folder,
// owned by no card. They are global (a workspace is only a tag), keep running while the user
// switches workspaces or views, and come back after Hadron restarts: a shell in the folder it was
// last in, a claude resuming its conversation. "Make a card" (FreeToCard) hands the running process
// to a new card without stopping it.

// SettingFreeLastCwd is the folder the last free session started in, the default for the next.
const SettingFreeLastCwd = "sessions.last_cwd"

// EntityFree is the change entity of free sessions. Its events carry no workspace.
const EntityFree = "free_sessions"

// FreeRequest says what free session to start.
type FreeRequest struct {
	Kind string // domain.FreeShell (default) or domain.FreeClaude
	Name string
	// Cwd is an absolute folder. Empty means the folder the last free session started in, else home.
	Cwd         string
	WorkspaceID string
}

// FreeStatus is a free session with what its process is doing now.
type FreeStatus struct {
	domain.FreeSession
	Running  bool   `json:"running"`
	Liveness string `json:"liveness"`
	// Program is what runs in the foreground of the terminal: the shell, or vim, htop, claude.
	Program string `json:"program"`
	// Error says why it never started.
	Error string `json:"error,omitempty"`
}

// freeOwner is the card a free session was handed to: its process now reports to the card.
type freeOwner struct {
	row    domain.Session
	cardID string
}

// freeInfo is what the service remembers of a live free session.
type freeInfo struct {
	kind         string
	program, cwd string
	owner        *freeOwner
	removed      bool
}

func (s *Service) info(id string) *freeInfo {
	fi := s.free[id]
	if fi == nil {
		fi = &freeInfo{}
		s.free[id] = fi
	}
	return fi
}

// loginShell is the user's shell: the Config override (tests), else SHELL of the login
// environment, else /bin/zsh.
func (s *Service) loginShell(env []string) string {
	if s.cfg.Shell != "" {
		return s.cfg.Shell
	}
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], "SHELL="); ok && v != "" {
			return v
		}
	}
	if v := os.Getenv("SHELL"); v != "" {
		return v
	}
	return "/bin/zsh"
}

// FreeDefaultCwd is where a free session starts when no folder is given: the folder the last one
// started in when it still exists, else the home folder.
func (s *Service) FreeDefaultCwd(st *store.Store) string {
	if v := s.setting(st, SettingFreeLastCwd); v != "" {
		if fi, err := os.Stat(v); err == nil && fi.IsDir() {
			return v
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

// StartFree writes a free session's row inside tx and returns it with the function that spawns
// its process, to call after tx commits.
func (s *Service) StartFree(tx *store.Store, req FreeRequest) (domain.FreeSession, func(), error) {
	kind := req.Kind
	if kind == "" {
		kind = domain.FreeShell
	}
	if kind != domain.FreeShell && kind != domain.FreeClaude {
		return domain.FreeSession{}, nil, fmt.Errorf("kind is %q; use shell or claude", kind)
	}
	cwd := req.Cwd
	if cwd == "" {
		cwd = s.FreeDefaultCwd(tx)
	}
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return domain.FreeSession{}, nil, fmt.Errorf("%s is not a folder", cwd)
	}
	env := s.baseEnv()
	command := s.loginShell(env)
	if kind == domain.FreeClaude {
		command = s.setting(tx, SettingCommand)
		if command == "" {
			command = "claude"
		}
		if !strings.Contains(command, "/") {
			if _, err := pty.LookPath(env, command); err != nil {
				return domain.FreeSession{}, nil, fmt.Errorf("%s was not found on PATH. Install it, or set the setting session.command to its full path", command)
			}
		}
	}
	row := domain.FreeSession{Name: strings.TrimSpace(req.Name), Kind: kind, Command: command, Cwd: cwd, WorkspaceID: req.WorkspaceID, Status: "running", Restore: true}
	row.ID = uuid.NewString()
	if kind == domain.FreeClaude {
		row.ClaudeSessionID = uuid.NewString()
	}
	if err := tx.FreeSessions.Create(&row); err != nil {
		return row, nil, err
	}
	_ = tx.Settings.Set(SettingFreeLastCwd, json.RawMessage(mustJSON(cwd)))
	opts := s.freeOptions(row, false)
	return row, func() { s.launchFree(row, opts) }, nil
}

// RestartFree starts an ended free session again under the same id, so its tab keeps it: a shell
// in its folder, a claude resuming its conversation (or starting it under the same id when it never
// got a first message).
func (s *Service) RestartFree(tx *store.Store, id string) (domain.FreeSession, func(), error) {
	row, err := tx.FreeSessions.Get(id)
	if err != nil {
		return row, nil, err
	}
	if ps, ok := s.pty.Get(id); ok {
		select {
		case <-ps.Done():
		default:
			return row, nil, errors.New("that session is still running")
		}
	}
	if st, err := os.Stat(row.Cwd); err != nil || !st.IsDir() {
		home, _ := os.UserHomeDir()
		row.Cwd = home
	}
	resume := row.Kind == domain.FreeClaude && row.ClaudeSessionID != "" && HasConversation(ClaudeConfigDir(), row.Cwd, row.ClaudeSessionID)
	if row.Kind == domain.FreeClaude && row.ClaudeSessionID == "" {
		row.ClaudeSessionID = uuid.NewString()
	}
	now := time.Now().UTC()
	row.Status, row.ExitCode, row.EndedAt, row.StartedAt, row.Restore, row.PID = "running", 0, nil, now, true, 0
	if err := tx.FreeSessions.Update(&row); err != nil {
		return row, nil, err
	}
	s.mu.Lock()
	delete(s.failures, id)
	s.mu.Unlock()
	opts := s.freeOptions(row, resume)
	return row, func() { s.launchFree(row, opts) }, nil
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// freeOptions builds the PTY options of a free session. A shell is a login shell (-l) with the
// user's full environment; claude gets its conversation id, and nothing of Hadron's prompt.
func (s *Service) freeOptions(row domain.FreeSession, resume bool) pty.Options {
	env := withEnv(s.baseEnv(), "TERM_PROGRAM=Hadron", "TERM_PROGRAM_VERSION="+run.Version)
	if !hasEnv(env, "LANG") && !hasEnv(env, "LC_ALL") && !hasEnv(env, "LC_CTYPE") {
		env = withEnv(env, "LANG=en_US.UTF-8") // a GUI app has no locale; without one, Unicode breaks
	}
	var args []string
	if row.Kind == domain.FreeClaude {
		if resume {
			args = []string{"--resume", row.ClaudeSessionID}
		} else {
			args = []string{"--session-id", row.ClaudeSessionID}
		}
	} else {
		args = []string{"-l"}
	}
	id := row.ID
	return pty.Options{
		Command: row.Command, Args: args, Cwd: row.Cwd, Env: env, WorkingWindow: s.cfg.Window,
		OnData:     func(data []byte, end int64) { s.data(id, data, end) },
		OnLiveness: func(l string) { s.onFreeLiveness(id, l) },
		OnExit:     func(code int) { s.onFreeExit(id, code) },
	}
}

func hasEnv(env []string, key string) bool {
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == key && v != "" {
			return true
		}
	}
	return false
}

func (s *Service) launchFree(row domain.FreeSession, opts pty.Options) {
	s.cfg.Log.Debug("free session start", "session", row.ID, "kind", row.Kind, "command", opts.Command, "cwd", opts.Cwd)
	s.mu.Lock()
	s.info(row.ID).kind = row.Kind
	s.free[row.ID].removed = false
	s.mu.Unlock()
	ps, err := s.pty.Start(row.ID, opts)
	if err != nil {
		s.cfg.Log.Error("free session could not start", "session", row.ID, "error", err.Error())
		s.mu.Lock()
		s.failures[row.ID] = err.Error()
		s.mu.Unlock()
		s.markFreeExited(row.ID, -1, false)
		return
	}
	if cur, err := s.cfg.Store.FreeSessions.Get(row.ID); err == nil {
		cur.PID = ps.PID()
		_ = s.cfg.Store.FreeSessions.Update(&cur)
	}
	s.changed("", EntityFree)
}

// owner returns the card a free session was handed to, or nil.
func (s *Service) owner(id string) *freeOwner {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fi := s.free[id]; fi != nil {
		return fi.owner
	}
	return nil
}

// onFreeLiveness refreshes what the session runs and where: a change of program (vim starting) or
// of folder (cd) is told to the frontend, and the shell's folder is kept for a restore.
func (s *Service) onFreeLiveness(id, liveness string) {
	if o := s.owner(id); o != nil {
		s.onLiveness(o.row, o.cardID, liveness)
		return
	}
	ps, ok := s.pty.Get(id)
	if !ok {
		return
	}
	program, _ := ps.Foreground()
	s.mu.Lock()
	fi := s.info(id)
	kind := fi.kind
	s.mu.Unlock()
	cwd := ""
	if kind == domain.FreeShell { // claude resumes only in the folder it started in
		cwd = ps.Cwd()
	}
	s.mu.Lock()
	moved := cwd != "" && cwd != fi.cwd
	fi.program = program
	if cwd != "" {
		fi.cwd = cwd
	}
	s.mu.Unlock()
	if moved {
		if cur, err := s.cfg.Store.FreeSessions.Get(id); err == nil && cur.Cwd != cwd {
			cur.Cwd = cwd
			_ = s.cfg.Store.FreeSessions.Update(&cur)
		}
	}
	s.changed("", EntityFree)
}

func (s *Service) onFreeExit(id string, code int) {
	if o := s.owner(id); o != nil {
		s.markExited(o.row, o.cardID, code)
		return
	}
	s.markFreeExited(id, code, true)
}

// markFreeExited records the end of a free session. A shell that exits cleanly while Hadron runs
// closes, as a terminal tab does; anything else stays, ended, so its output can be read and it can
// be started again.
func (s *Service) markFreeExited(id string, code int, ran bool) {
	s.mu.Lock()
	removed := s.free[id] != nil && s.free[id].removed
	s.mu.Unlock()
	if removed {
		return
	}
	cur, err := s.cfg.Store.FreeSessions.Get(id)
	if err != nil {
		return
	}
	closing := s.closing.Load()
	if ran && !closing && cur.Kind == domain.FreeShell && code == 0 {
		s.dropFree(id)
		_ = s.cfg.Store.FreeSessions.Delete(id)
		s.changed("", EntityFree)
		return
	}
	now := time.Now().UTC()
	cur.Status, cur.ExitCode, cur.EndedAt = "exited", code, &now
	if !closing {
		cur.Restore = false
	}
	_ = s.cfg.Store.FreeSessions.Update(&cur)
	s.changed("", EntityFree)
}

// dropFree forgets a free session's process and what the service knew of it.
func (s *Service) dropFree(id string) {
	s.mu.Lock()
	s.info(id).removed = true
	delete(s.failures, id)
	s.mu.Unlock()
	s.pty.Forget(id)
	s.mu.Lock()
	delete(s.free, id)
	s.mu.Unlock()
}

// ListFree returns the free sessions with what their processes are doing.
func (s *Service) ListFree(st *store.Store) ([]FreeStatus, error) {
	rows, err := st.FreeSessions.List()
	if err != nil {
		return nil, err
	}
	out := make([]FreeStatus, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.freeStatus(r))
	}
	return out, nil
}

// FreeStatusOf returns one free session with its process state.
func (s *Service) FreeStatusOf(st *store.Store, id string) (FreeStatus, error) {
	r, err := st.FreeSessions.Get(id)
	if err != nil {
		return FreeStatus{}, err
	}
	return s.freeStatus(r), nil
}

// FreeStatusOfRow returns a free session row with its process state.
func (s *Service) FreeStatusOfRow(r domain.FreeSession) FreeStatus { return s.freeStatus(r) }

func (s *Service) freeStatus(r domain.FreeSession) FreeStatus {
	f := FreeStatus{FreeSession: r, Liveness: domain.LivenessEnded}
	if ps, ok := s.FreeLive(r.ID); ok {
		f.Running, f.Liveness = true, ps.Liveness()
		s.mu.Lock()
		if fi := s.free[r.ID]; fi != nil {
			f.Program = fi.program
			if fi.cwd != "" {
				f.Cwd = fi.cwd
			}
		}
		s.mu.Unlock()
		if f.Program == "" {
			f.Program, _ = ps.Foreground()
		}
	}
	s.mu.Lock()
	f.Error = s.failures[r.ID]
	s.mu.Unlock()
	return f
}

// FreeLive returns a free session's process while it runs.
func (s *Service) FreeLive(id string) (*pty.Session, bool) {
	ps, ok := s.pty.Get(id)
	if !ok {
		return nil, false
	}
	select {
	case <-ps.Done():
		return nil, false
	default:
		return ps, true
	}
}

// FreeScreen returns what a free session's terminal shows, also after it ended.
func (s *Service) FreeScreen(id string, lines int, scrollback bool) (string, bool) {
	ps, ok := s.pty.Get(id)
	if !ok {
		return "", false
	}
	return ps.Screen(lines, scrollback), true
}

// StopFree hangs up a free session. With remove it is also closed: its row and its output go.
func (s *Service) StopFree(st *store.Store, id string, remove bool) error {
	if _, err := st.FreeSessions.Get(id); err != nil {
		return err
	}
	if remove {
		s.dropFree(id)
		return st.FreeSessions.Delete(id)
	}
	if ps, ok := s.FreeLive(id); ok {
		ps.Stop()
	}
	return nil
}

// FreeToCard hands a free session to a card: a card session row with the same id takes over the
// running process, which keeps running and from now on reports to the card. The free session's row
// goes. Call the returned function after tx commits.
func (s *Service) FreeToCard(tx *store.Store, id string, card domain.Card) (domain.Session, func(), error) {
	f, err := tx.FreeSessions.Get(id)
	if err != nil {
		return domain.Session{}, nil, err
	}
	ps, live := s.FreeLive(id)
	cwd := f.Cwd
	if live {
		if c := ps.Cwd(); c != "" {
			cwd = c
		}
	}
	row := domain.Session{WorkspaceID: card.WorkspaceID, CardID: card.ID, Kind: "card", Command: f.Command, Cwd: cwd, PID: f.PID,
		Status: "exited", ExitCode: f.ExitCode, ClaudeSessionID: f.ClaudeSessionID, StartedAt: f.StartedAt, EndedAt: f.EndedAt}
	row.ID = f.ID
	liveness := domain.LivenessEnded
	if live {
		row.Status, row.EndedAt, row.Restore = "running", nil, true
		liveness = ps.Liveness()
	}
	if err := tx.Sessions.Create(&row); err != nil {
		return row, nil, err
	}
	if err := tx.Cards.SetLiveness(card.ID, liveness); err != nil {
		return row, nil, err
	}
	if err := tx.FreeSessions.Delete(id); err != nil {
		return row, nil, err
	}
	commit := func() {
		s.mu.Lock()
		s.info(id).owner = &freeOwner{row: row, cardID: card.ID}
		s.mu.Unlock()
		s.changed("", EntityFree)
		s.changed(card.WorkspaceID, "cards", "sessions")
	}
	return row, commit, nil
}

// RestoreFree brings back the free sessions that were running when Hadron last quit, crashed or
// updated. One whose process is still alive (an orphan another Hadron runs) is left alone. With the
// restore setting off, nothing comes back. It returns how many it started.
func (s *Service) RestoreFree(ctx context.Context) int {
	st := s.cfg.Store
	rows, err := st.FreeSessions.ListRestorable()
	if err != nil {
		return 0
	}
	on := RestoreOnLaunch(st)
	started := 0
	for _, r := range rows {
		if ctx.Err() != nil {
			return started
		}
		if r.Status == "running" {
			continue
		}
		if !on {
			r.Restore = false
			_ = st.FreeSessions.Update(&r)
			continue
		}
		var launch func()
		err := st.Tx(func(tx *store.Store) error {
			var e error
			_, launch, e = s.RestartFree(tx, r.ID)
			return e
		})
		if err != nil {
			s.cfg.Log.Error("restore: could not restart a free session", "session", r.ID, "error", err.Error())
			continue
		}
		launch()
		started++
	}
	return started
}

// keepFreeFolders writes each running free shell's current folder to its row, so a restore starts
// it there. StopAll calls it before hanging up.
func (s *Service) keepFreeFolders() {
	rows, err := s.cfg.Store.FreeSessions.List()
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.Kind != domain.FreeShell {
			continue
		}
		if ps, ok := s.FreeLive(r.ID); ok {
			if c := ps.Cwd(); c != "" && c != r.Cwd {
				r.Cwd = c
				_ = s.cfg.Store.FreeSessions.Update(&r)
			}
		}
	}
}

// SetShellForTest sets the shell free sessions run. Tests use it.
func (s *Service) SetShellForTest(path string) { s.cfg.Shell = path }
