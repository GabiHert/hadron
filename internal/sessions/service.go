// Package sessions starts, stops and talks to the Claude Code sessions behind cards and workspace
// planners. It joins the store (session rows, launch profiles, prompts), the PTY manager and the
// per-session MCP config. Actions call it; it knows nothing of actions.
package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GabiHert/hadron/internal/dlog"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/pty"
	"github.com/GabiHert/hadron/internal/run"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/google/uuid"
)

// Settings keys.
const (
	SettingCommand          = "session.command"
	SettingPlannerAutostart = "planner.autostart"
	SettingPlannerPolicy    = "planner.policy"
	SettingMCPPort          = "mcp.port"
)

// DefaultMCPPort is where the MCP server listens unless told otherwise (spec section 10).
const DefaultMCPPort = 47820

// DefaultFirstPromptDelay is how long to wait for Claude before typing the first prompt, when the
// screen has not shown its input box by then.
const DefaultFirstPromptDelay = 1500 * time.Millisecond

// DefaultReadyTimeout is how long after a session starts a send waits for Claude's input box.
const DefaultReadyTimeout = 20 * time.Second

// Config is what a Service needs from the app.
type Config struct {
	// Store is the root store. Actions pass their transaction store to Prepare; the callbacks that
	// fire from a session's own goroutines use this one.
	Store *store.Store
	// Home is the data folder; MCP configs go to <Home>/run and the token to <Home>/mcp-token.
	Home string
	// Env returns the login shell environment. Nil means the app's own.
	Env func() []string
	// Changed tells the frontend which entities of a workspace changed.
	Changed func(workspaceID string, entities ...string)
	// OnData forwards each output chunk to the frontend (the pty:data:<id> event).
	OnData func(sessionID string, data []byte, end int64)
	// FirstPromptDelay overrides DefaultFirstPromptDelay. Tests shorten it.
	FirstPromptDelay time.Duration
	// ReadyTimeout overrides DefaultReadyTimeout. Tests shorten it.
	ReadyTimeout time.Duration
	// MCPPort returns the port the MCP server listens on, or 0 while it is not listening. Nil or 0
	// reads HADRON_MCP_PORT, then the setting, then the default.
	MCPPort func() int
	// Window overrides the working window of new sessions. Tests shorten it.
	Window time.Duration
	// Log is Hadron's own log. It may be nil.
	Log *dlog.Logger
}

// Service owns the live sessions.
type Service struct {
	cfg Config
	pty *pty.Manager

	mu       sync.Mutex
	failures map[string]string // session id -> why it never started
	ready    map[string]bool   // session id -> Claude's input box has been seen
	// closing is set when Hadron is quitting: sessions that end after that are meant to come back.
	closing atomic.Bool
}

// New returns a Service.
func New(cfg Config) *Service {
	if cfg.FirstPromptDelay <= 0 {
		cfg.FirstPromptDelay = DefaultFirstPromptDelay
	}
	if cfg.ReadyTimeout <= 0 {
		cfg.ReadyTimeout = DefaultReadyTimeout
	}
	return &Service{cfg: cfg, pty: pty.NewManager(cfg.Env), failures: map[string]string{}, ready: map[string]bool{}}
}

// Request says what to start.
type Request struct {
	Workspace domain.Workspace
	// Handoff, when set, is typed into the session once it is ready, instead of a launch profile's
	// first prompt. Adopted conversations get it.
	Handoff string
	// Warnings, when set, receives notes the start should show (a worktree that could not be made).
	Warnings *[]string
	// Card is nil for the workspace planner.
	Card *domain.Card
	// Profile overrides the card's launch profile by name or id.
	Profile string
	// Fresh ignores a known Claude session id and starts a new conversation.
	Fresh bool
	// Strict returns an error now, instead of recording a failed start, when the command cannot be found.
	Strict bool
	// Restart stops the target's running session first instead of refusing to start.
	Restart bool
	// Restore is a restart after Hadron itself restarted: no first prompt and no handoff.
	Restore bool
}

// Prepare writes the session row inside tx and returns it with the function that spawns the
// process. Call that function only after tx has committed (Call.AfterCommit), so a rolled-back call
// leaves no process behind.
func (s *Service) Prepare(tx *store.Store, req Request) (domain.Session, func(), error) {
	ws, card := req.Workspace, req.Card
	kind, cardID := "planner", ""
	if card != nil {
		kind, cardID = "card", card.ID
	}
	prev, prevErr := tx.Sessions.Latest(ws.ID, cardID)
	var old *pty.Session
	if prevErr == nil {
		if live, ok := s.pty.Get(prev.ID); ok {
			select {
			case <-live.Done():
			default:
				if !req.Restart {
					return domain.Session{}, nil, fmt.Errorf("%s already has a running session. Use session_restart to start it over", s.label(card, ws))
				}
				old = live
			}
		}
	} else if !errors.Is(prevErr, store.ErrNotFound) {
		return domain.Session{}, nil, prevErr
	}
	base := Folder(ws, card)
	if st, err := os.Stat(base); err != nil || !st.IsDir() {
		switch {
		case card != nil && card.Cwd != "":
			return domain.Session{}, nil, fmt.Errorf("card %q runs in %s, which is not a folder any more. Change it with card_update cwd (empty means the workspace root)", card.Nick, base)
		case card == nil && ws.PlannerCwd != "":
			return domain.Session{}, nil, fmt.Errorf("the planner of %q runs in %s, which is not a folder any more. Change it with workspace_update planner_cwd (empty means the workspace root)", ws.Name, base)
		}
		return domain.Session{}, nil, fmt.Errorf("workspace %q has no folder at %s. Fix its root with workspace_update", ws.Name, ws.Root)
	}

	profile, err := s.profile(tx, ws, card, req.Profile)
	if err != nil {
		return domain.Session{}, nil, err
	}
	command := profile.Command
	if v := s.setting(tx, SettingCommand); v != "" {
		command = v
	}
	if command == "" {
		command = "claude"
	}
	env := s.baseEnv()
	if req.Strict && !strings.Contains(command, "/") {
		if _, err := pty.LookPath(env, command); err != nil {
			return domain.Session{}, nil, fmt.Errorf("%s was not found on PATH. Install it, or set the setting session.command to its full path", command)
		}
	}

	cwd := base
	if card != nil && profile.Worktree {
		switch {
		case !IsGitRepo(env, base):
			if req.Warnings != nil {
				*req.Warnings = append(*req.Warnings, fmt.Sprintf("the launch profile asks for a worktree but %s is not a git repository, so the session started in that folder", base))
			}
		default:
			path, _, err := EnsureWorktree(env, base, WorktreeName(tx, ws, card.Nick))
			if err != nil {
				return domain.Session{}, nil, fmt.Errorf("could not make the worktree for %s: %w", card.Nick, err)
			}
			cwd = path
		}
	}

	row := domain.Session{WorkspaceID: ws.ID, CardID: cardID, Kind: kind, Command: command, Cwd: cwd, Status: "running", Restore: true}
	resume := ""
	if prevErr == nil && !req.Fresh && prev.ClaudeSessionID != "" && isClaude(command) {
		resume = prev.ClaudeSessionID
		row.ClaudeSessionID = resume
	}
	row.ID = uuid.NewString() // known before the row exists, so nothing fallible follows the insert
	// Every claude launch has a conversation id Hadron knows: a fresh one is given to claude with
	// --session-id and stored, so a restart can always resume it. Claude writes the transcript only
	// after the first message, so an id whose transcript is missing (it was never used) cannot be
	// resumed: it is given to claude again with --session-id, keeping the same id.
	newConversation := ""
	if resume != "" && !HasConversation(ClaudeConfigDir(), cwd, resume) {
		s.cfg.Log.Debug("no transcript for the stored conversation, starting it again with the same id", "session", resume, "cwd", cwd)
		newConversation, resume = resume, ""
	}
	if resume == "" && newConversation == "" && isClaude(command) {
		newConversation = uuid.NewString()
		row.ClaudeSessionID = newConversation
	}

	prompt, err := RenderPromptWith(tx, ws, card, &profile, kindOf(card))
	if err != nil {
		return row, nil, err
	}
	cfgPath, err := s.writeMCPConfig(tx, row.ID)
	if err != nil {
		return row, nil, err
	}
	args := []string{}
	if isClaude(command) {
		args = append(args, "--append-system-prompt", prompt, "--mcp-config", cfgPath)
		if resume != "" {
			args = append(args, "--resume", resume)
		}
		if newConversation != "" {
			args = append(args, "--session-id", newConversation)
		}
		profileArgs := profile.Args
		if model, _ := ResolveModel(ws, card, profile); model != "" {
			args = append(args, "--model", model)
			profileArgs = dropModelArg(profileArgs) // the model setting wins; never pass --model twice
		}
		args = append(args, profileArgs...)
	} else {
		args = append(args, profile.Args...)
	}

	env = withEnv(env, "HADRON_SESSION_ID="+row.ID, "HADRON_WORKSPACE_ID="+ws.ID, "HADRON_CARD_ID="+cardID)
	for k, v := range profile.Env {
		env = withEnv(env, k+"="+v)
	}
	first := ""
	if card != nil && resume == "" {
		first = renderFirstPrompt(profile.FirstPrompt, *card)
	}
	if req.Handoff != "" {
		first = req.Handoff
	}
	if req.Restore {
		first = "" // a restore brings back a conversation; it never types anything into it
	}
	if err := tx.Sessions.Create(&row); err != nil {
		return row, nil, err
	}
	if card != nil {
		if err := tx.Cards.SetLiveness(card.ID, domain.LivenessIdle); err != nil {
			return row, nil, err
		}
	}

	opts := pty.Options{
		Command: command, Args: args, Cwd: cwd, Env: env, WorkingWindow: s.cfg.Window,
		OnData:     func(data []byte, end int64) { s.data(row.ID, data, end) },
		OnLiveness: func(l string) { s.onLiveness(row, cardID, l) },
		OnExit:     func(code int) { s.onExit(row, cardID, code) },
	}
	launch := func() { s.launch(row, cardID, old, opts, first) }
	return row, launch, nil
}

func (s *Service) data(id string, data []byte, end int64) {
	if s.cfg.OnData != nil {
		s.cfg.OnData(id, data, end)
	}
}

func (s *Service) launch(row domain.Session, cardID string, old *pty.Session, opts pty.Options, first string) {
	if old != nil {
		old.Stop()
		select {
		case <-old.Done():
		case <-time.After(5 * time.Second):
		}
	}
	s.cfg.Log.Debug("pty start", "session", row.ID, "kind", row.Kind, "command", opts.Command, "argv", RedactArgs(opts.Args), "cwd", opts.Cwd)
	ps, err := s.pty.Start(row.ID, opts)
	if err != nil {
		s.cfg.Log.Error("pty could not start", "session", row.ID, "command", opts.Command, "error", err.Error())
		s.mu.Lock()
		s.failures[row.ID] = err.Error()
		s.mu.Unlock()
		s.markExited(row, cardID, -1)
		return
	}
	if cur, err := s.cfg.Store.Sessions.Get(row.ID); err == nil {
		cur.PID = ps.PID()
		_ = s.cfg.Store.Sessions.Update(&cur)
	}
	// A card that restarts with a question still open is still waiting on the user.
	if cardID != "" {
		if open, err := s.cfg.Store.Questions.List(row.WorkspaceID, cardID, "open"); err == nil && len(open) > 0 {
			ps.SetWaiting(true)
		}
	}
	s.changed(row.WorkspaceID, "sessions")
	if first != "" {
		go s.deliverFirstPrompt(ps, first)
	}
}

// inputBox matches the line Claude draws its prompt on: an optional box border, then > or the
// prompt arrow. A numbered menu choice ("❯ 1. Yes") is a dialog, not the prompt.
var inputBox = regexp.MustCompile(`^\s*[│|]?\s*[>❯](\s*[│|]?\s*$|\s+[^\d\s])`)

// ScreenShowsInput reports whether a screen looks like Claude's input box.
func ScreenShowsInput(screen string) bool {
	for _, line := range strings.Split(screen, "\n") {
		if inputBox.MatchString(line) {
			return true
		}
	}
	return false
}

// deliverFirstPrompt types the first prompt once Claude shows its input box, or after the delay,
// whichever comes first.
func (s *Service) deliverFirstPrompt(ps *pty.Session, text string) {
	deadline := time.After(s.cfg.FirstPromptDelay)
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
wait:
	for {
		select {
		case <-ps.Done():
			return
		case <-deadline:
			break wait
		case <-tick.C:
			if ScreenShowsInput(ps.Screen(0, false)) {
				break wait
			}
		}
	}
	_ = ps.Send(text, true)
}

// isLatest reports whether row is still the newest session of its card. A restarted card's old
// process reports its exit after the new one has started, and must not overwrite the card.
func (s *Service) isLatest(row domain.Session, cardID string) bool {
	cur, err := s.cfg.Store.Sessions.Latest(row.WorkspaceID, cardID)
	return err == nil && cur.ID == row.ID
}

func (s *Service) onLiveness(row domain.Session, cardID, liveness string) {
	if !s.isLatest(row, cardID) {
		return
	}
	if cardID != "" {
		_ = s.cfg.Store.Cards.SetLiveness(cardID, liveness)
		s.changed(row.WorkspaceID, "cards", "sessions")
		return
	}
	s.changed(row.WorkspaceID, "sessions")
}

func (s *Service) onExit(row domain.Session, cardID string, code int) {
	s.markExited(row, cardID, code)
}

func (s *Service) markExited(row domain.Session, cardID string, code int) {
	s.cfg.Log.Debug("pty exit", "session", row.ID, "kind", row.Kind, "exit_code", code)
	s.mu.Lock()
	delete(s.ready, row.ID)
	s.mu.Unlock()
	// A session that ends while Hadron runs ended on purpose or on its own, so it stays down.
	// Quitting Hadron (and an update, which quits it) keeps the flag: those sessions come back.
	if !s.closing.Load() {
		_ = s.cfg.Store.Sessions.SetRestore(row.ID, false)
	}
	if cur, err := s.cfg.Store.Sessions.Get(row.ID); err == nil {
		now := time.Now().UTC()
		cur.Status, cur.ExitCode, cur.EndedAt = "exited", code, &now
		_ = s.cfg.Store.Sessions.Update(&cur)
	}
	if !s.isLatest(row, cardID) {
		return
	}
	if cardID != "" {
		_ = s.cfg.Store.Cards.SetLiveness(cardID, domain.LivenessEnded)
		s.changed(row.WorkspaceID, "cards", "sessions")
		return
	}
	s.changed(row.WorkspaceID, "sessions")
}

func (s *Service) changed(workspaceID string, entities ...string) {
	if s.cfg.Changed != nil {
		s.cfg.Changed(workspaceID, entities...)
	}
}

// Live returns a session's process, if the manager has it.
func (s *Service) Live(sessionID string) (*pty.Session, bool) { return s.pty.Get(sessionID) }

// Target is the latest session of a card, or of a workspace's planner (card nil), with its process
// when it has one and the process is still running.
type Target struct {
	Row  domain.Session
	Live *pty.Session // nil unless running
	// Err says why the session never started.
	Err string
}

// Find returns the latest session of a card or planner. ErrNoSession when there is none.
func (s *Service) Find(st *store.Store, workspaceID, cardID string) (Target, error) {
	row, err := st.Sessions.Latest(workspaceID, cardID)
	if errors.Is(err, store.ErrNotFound) {
		return Target{}, ErrNoSession
	}
	if err != nil {
		return Target{}, err
	}
	t := Target{Row: row}
	if ps, ok := s.pty.Get(row.ID); ok {
		select {
		case <-ps.Done():
		default:
			t.Live = ps
		}
	}
	s.mu.Lock()
	t.Err = s.failures[row.ID]
	s.mu.Unlock()
	return t, nil
}

// ErrNoSession means a card or planner has never had a session.
var ErrNoSession = errors.New("no session has been started yet")

// Stop hangs up a card's or planner's running session. It reports whether one was running.
func (s *Service) Stop(st *store.Store, workspaceID, cardID string) (bool, error) {
	t, err := s.Find(st, workspaceID, cardID)
	if errors.Is(err, ErrNoSession) {
		return false, nil
	}
	if err != nil || t.Live == nil {
		return false, err
	}
	t.Live.Stop()
	return true, nil
}

// StopCard stops a card's session by card id, for use after the card is gone.
func (s *Service) StopCard(workspaceID, cardID string) {
	_, _ = s.Stop(s.cfg.Store, workspaceID, cardID)
}

// Running returns a stop function for each running session of a workspace, to call after the
// transaction that removes or archives the workspace has committed.
func (s *Service) Running(st *store.Store, workspaceID string) []func() {
	rows, err := st.Sessions.ListRunning()
	if err != nil {
		return nil
	}
	var out []func()
	for _, r := range rows {
		if r.WorkspaceID != workspaceID {
			continue
		}
		if ps, ok := s.pty.Get(r.ID); ok {
			out = append(out, ps.Stop)
		}
	}
	return out
}

// StopAll hangs up every running session. Call it when the app quits.
func (s *Service) StopAll() {
	s.closing.Store(true)
	s.pty.StopAll()
}

// SetWaiting marks a card's session as waiting on an open question, or clears it. The chat layer
// calls it when a question opens or is answered.
func (s *Service) SetWaiting(workspaceID, cardID string, waiting bool) {
	if t, err := s.Find(s.cfg.Store, workspaceID, cardID); err == nil && t.Live != nil {
		t.Live.SetWaiting(waiting)
	}
}

// Attach returns what a terminal replays when it mounts: the scrollback, and the byte offset after
// it. A session that never started replays its error.
func (s *Service) Attach(sessionID string) ([]byte, int64) {
	if ps, ok := s.pty.Get(sessionID); ok {
		return ps.Scrollback()
	}
	s.mu.Lock()
	msg := s.failures[sessionID]
	s.mu.Unlock()
	if msg != "" {
		return []byte("\x1b[31mCould not start the session: " + msg + "\x1b[0m\r\n"), 0
	}
	return nil, 0
}

// Write sends the user's keystrokes to a session.
func (s *Service) Write(sessionID string, data []byte) error {
	ps, ok := s.pty.Get(sessionID)
	if !ok {
		return errors.New("that session is not running")
	}
	return ps.Write(data)
}

// Resize sets a session's terminal size.
func (s *Service) Resize(sessionID string, cols, rows int) error {
	ps, ok := s.pty.Get(sessionID)
	if !ok {
		return errors.New("that session is not running")
	}
	return ps.Resize(cols, rows)
}

// PlannerAutostart reports whether planners start with their workspace: the setting, else true
// unless Home is a scratch folder (tests must never start a real claude).
func (s *Service) PlannerAutostart(st *store.Store) bool {
	if raw, err := st.Settings.Get(SettingPlannerAutostart); err == nil {
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return b
		}
	}
	return !run.IsTestHome(s.cfg.Home)
}

// CleanRunDir removes MCP configs left by earlier runs. Call it at startup.
func (s *Service) CleanRunDir() {
	files, _ := filepath.Glob(filepath.Join(s.cfg.Home, "run", "*.mcp.json"))
	for _, f := range files {
		_ = os.Remove(f)
	}
}

func (s *Service) label(card *domain.Card, ws domain.Workspace) string {
	if card != nil {
		return fmt.Sprintf("card %q", card.Nick)
	}
	return fmt.Sprintf("the planner of %q", ws.Name)
}

// Env is the environment sessions and git run in.
func (s *Service) Env() []string { return s.baseEnv() }

func (s *Service) baseEnv() []string {
	if s.cfg.Env != nil {
		return s.cfg.Env()
	}
	return os.Environ()
}

func (s *Service) setting(st *store.Store, key string) string {
	raw, err := st.Settings.Get(key)
	if err != nil {
		return ""
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

func (s *Service) port(st *store.Store) int {
	if s.cfg.MCPPort != nil {
		if n := s.cfg.MCPPort(); n > 0 {
			return n
		}
	}
	if v := os.Getenv("HADRON_MCP_PORT"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	if raw, err := st.Settings.Get(SettingMCPPort); err == nil {
		var n int
		if json.Unmarshal(raw, &n) == nil && n > 0 {
			return n
		}
	}
	return DefaultMCPPort
}

// dropModelArg removes "--model X" and "--model=X" from a profile's arguments.
func dropModelArg(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--model":
			i++ // and its value
		case strings.HasPrefix(args[i], "--model="):
		default:
			out = append(out, args[i])
		}
	}
	return out
}

// IsClaude says whether command is the claude CLI, the only program Hadron gives its prompt and model flags, and types slash commands into.
func IsClaude(command string) bool { return filepath.Base(command) == "claude" }
func isClaude(command string) bool { return IsClaude(command) }

func kindOf(card *domain.Card) string {
	if card == nil {
		return "planner"
	}
	return "session"
}

// withEnv sets KEY=VALUE pairs on env, replacing earlier values of the same key.
func withEnv(env []string, set ...string) []string {
	skip := map[string]bool{}
	for _, kv := range set {
		k, _, _ := strings.Cut(kv, "=")
		skip[k] = true
	}
	out := make([]string, 0, len(env)+len(set))
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !skip[k] {
			out = append(out, kv)
		}
	}
	return append(out, set...)
}

// RedactArgs copies a command's arguments with the prompt text replaced by its length, so a log of
// how a session started never holds the prompt.
func RedactArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if i > 0 && args[i-1] == "--append-system-prompt" {
			out[i] = fmt.Sprintf("<prompt %d chars>", len([]rune(a)))
			continue
		}
		out[i] = a
	}
	return out
}

// SetLogForTest swaps the log. Tests use it.
func (s *Service) SetLogForTest(l *dlog.Logger) { s.cfg.Log = l }

// LiveSessions lists the sessions whose process is alive.
func (s *Service) LiveSessions() []domain.Session {
	rows, err := s.cfg.Store.Sessions.ListRunning()
	if err != nil {
		return nil
	}
	var out []domain.Session
	for _, r := range rows {
		if _, ok := s.pty.Get(r.ID); ok {
			out = append(out, r)
		}
	}
	return out
}

// ErrNotReady says a session did not show Claude's input box in time, so nothing was typed.
var ErrNotReady = errors.New("not ready")

// WaitReady waits until a session that is still starting shows Claude's input box, so text sent
// to it is not lost before Claude reads its input. A session that showed the box once, or that
// started more than ReadyTimeout ago, is ready at once. It fails (wrapping ErrNotReady) when the
// box does not show by ReadyTimeout after the start, and when the session ends meanwhile.
func (s *Service) WaitReady(ctx context.Context, row domain.Session, ps *pty.Session) error {
	s.mu.Lock()
	seen := s.ready[row.ID]
	s.mu.Unlock()
	if seen {
		return nil
	}
	deadline := row.StartedAt.Add(s.cfg.ReadyTimeout)
	if row.StartedAt.IsZero() || !time.Now().Before(deadline) {
		return nil // an established session: its screen may show anything (a dialog, a pager)
	}
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		if ScreenShowsInput(ps.Screen(0, false)) {
			s.mu.Lock()
			s.ready[row.ID] = true
			s.mu.Unlock()
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("%w: Claude's prompt did not show within %s of the session starting", ErrNotReady, s.cfg.ReadyTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ps.Done():
			return fmt.Errorf("%w: the session ended before Claude was ready", ErrNotReady)
		case <-tick.C:
		}
	}
}
