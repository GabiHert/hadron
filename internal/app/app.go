package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/GabiHert/hadron/internal/actions"
	"github.com/GabiHert/hadron/internal/changelog"
	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/dlog"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/integrations"
	"github.com/GabiHert/hadron/internal/mcpserver"
	"github.com/GabiHert/hadron/internal/pty"
	"github.com/GabiHert/hadron/internal/run"
	"github.com/GabiHert/hadron/internal/script"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/GabiHert/hadron/internal/update"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the one struct Wails binds to the frontend. Its bound methods are Invoke and the three
// PTY bridge methods (PtyAttach, PtyWrite, PtyResize): every operation lives in the action
// registry, so the UI can do nothing MCP can't. Anything else on App must stay unexported, or
// Wails would bind it; TestOnlyInvokeAndThePTYBridgeAreBound fails if it isn't.
type App struct {
	reg    *actions.Registry
	st     *store.Store
	upd    *update.Service
	sess   *sessions.Service
	mcp    *mcpserver.Server
	chat   *chat.Service
	fields *fields.Registry
	integ  *integrations.Manager
	log    *dlog.Logger
	feeds  *ptyFeeds
	cancel context.CancelFunc

	mu  sync.Mutex
	ctx context.Context
}

// New builds an App over an open store.
func New(st *store.Store) *App {
	a := &App{st: st}
	a.log = dlog.New(filepath.Join(run.Home(), "logs"))
	if raw, err := st.Settings.Get(actions.SettingDebugLogging); err == nil {
		var on bool
		if json.Unmarshal(raw, &on) == nil {
			a.log.SetDebug(on)
		}
	}
	env := cachedEnv()
	a.feeds = &ptyFeeds{emit: a.emit, byID: map[string]*ptyFeed{}}
	a.sess = sessions.New(sessions.Config{
		Store: st, Home: run.Home(), Env: env, Log: a.log,
		Changed: func(workspaceID string, entities ...string) {
			a.emit(actions.EventChanged, actions.ChangedEvent{WorkspaceID: workspaceID, Entities: entities})
		},
		OnData: a.feeds.add,
		MCPPort: func() int {
			if a.mcp == nil {
				return 0
			}
			return a.mcp.Port()
		},
	})
	a.sess.CleanRunDir()
	a.upd = update.New(update.Deps{
		Version: run.Version, AppName: "Hadron", Repo: "GabiHert/hadron",
		Env:  env,
		Emit: a.emit,
		Dir:  run.UpdateDir(), LogPath: run.UpdateLogPath(),
		Quit: func() {
			if c := a.context(); c != nil {
				wruntime.Quit(c)
			}
		},
	})
	a.upd.CleanLeftovers()
	a.fields = fields.NewRegistry()
	a.chat = chat.New(chat.SessionsPlanner{Sessions: a.sess}, st)
	a.chat.Log = a.log
	a.integ = integrations.New(st, a.fields, script.NewSecrets(run.Home()), script.Live{Env: env})
	a.integ.Log = a.log
	a.integ.Changed = func(workspaceID string, entities ...string) {
		a.emit(actions.EventChanged, actions.ChangedEvent{WorkspaceID: workspaceID, Entities: entities})
	}
	a.integ.Notify = func(workspaceID, title, body string) {
		a.emit(actions.EventNotify, actions.NotifyEvent{WorkspaceID: workspaceID, Title: title, Body: body})
	}
	a.reg = actions.NewRegistry(actions.Deps{Store: st, Emit: a.emit, Version: run.Version, Update: a.upd, Home: run.Home(), Sessions: a.sess, MCP: a.mcpInfo, Chat: a.chat, Fields: a.fields, Integrations: a.integ, Log: a.log, Changelog: changelog.Embedded()})
	a.integ.FieldChanged = a.reg.FieldChanged
	if err := a.integ.Sync(); err != nil {
		log.Printf("integrations: %v", err)
	}
	actions.RegisterAll(a.reg)
	a.mcp = mcpserver.New(mcpserver.Options{Registry: a.reg, Store: st, Home: run.Home(), Version: run.Version, Log: a.log})
	return a
}

// showWindow brings the window forward, for a second launch of the app.
func (a *App) showWindow() {
	c := a.context()
	if c == nil {
		return
	}
	wruntime.WindowUnminimise(c)
	wruntime.Show(c)
}

// loginEnv reads the login shell's environment. Tests replace it.
var loginEnv = pty.LoginShellEnv

// cachedEnv reads the login shell's environment once, off the startup path; it is slow.
func cachedEnv() func() []string {
	var once sync.Once
	var env []string
	return func() []string {
		once.Do(func() { env = loginEnv() })
		return append([]string(nil), env...)
	}
}

func (a *App) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	watchCtx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.mu.Unlock()
	a.log.Info("startup", "version", run.Version)
	reconcile(a.st)
	// The MCP server must be listening before any planner starts, so the config files it writes
	// name the port the server really got.
	if err := a.mcp.Start(); err != nil {
		log.Printf("mcp server: %v", err)
	}
	// Check for a new release a minute after launch, then every six hours.
	go a.upd.Watch(watchCtx, time.Minute, 6*time.Hour)
	go a.ruleTimer(watchCtx)
	go a.restoreSessions(ctx)
}

// ruleTimer fires the idle_for and cron rules that come due. The first tick only starts the clock.
func (a *App) ruleTimer(ctx context.Context) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		if err := a.reg.TickRules(ctx, time.Now().UTC()); err != nil {
			log.Printf("rule timer: %v", err)
		}
		if err := a.reg.TickDashboard(ctx, time.Now().UTC()); err != nil {
			log.Printf("dashboard timer: %v", err)
		}
		if err := a.integ.Poll(ctx, time.Now().UTC()); err != nil {
			log.Printf("integration poll: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// restoreSessions brings back the sessions that were running when Hadron last quit, crashed or
// updated (the planners first), one second apart, then lets the planner autostart cover a
// workspace that had none.
func (a *App) restoreSessions(ctx context.Context) {
	n := a.sess.RestoreAll(ctx, restoreStagger)
	if n > 0 {
		a.log.Info("restored sessions", "count", n)
	}
	a.autostartPlanner(ctx)
}

// restoreStagger is the pause between two restored sessions, so a dozen claude processes do not
// start at once.
var restoreStagger = time.Second

// autostartPlanner starts the active workspace's planner when planner.autostart allows it. The
// login shell environment is read first, off the UI path, because it is slow.
func (a *App) autostartPlanner(ctx context.Context) {
	if !a.sess.PlannerAutostart(a.st) {
		return
	}
	raw, err := a.st.Settings.Get(actions.SettingActiveWorkspace)
	if err != nil {
		return
	}
	var id string
	if json.Unmarshal(raw, &id) != nil || id == "" {
		return
	}
	if t, err := a.sess.Find(a.st, id, ""); err == nil && t.Live != nil {
		return // a restored planner is already running
	}
	if _, err := a.reg.Invoke(ctx, actions.You, "planner_start", `{"workspace_id":`+strconv.Quote(id)+`}`); err != nil {
		log.Printf("planner autostart: %v", err)
	}
}

func (a *App) shutdown() {
	a.log.Info("shutdown")
	a.mu.Lock()
	if a.cancel != nil {
		a.cancel()
	}
	a.mu.Unlock()
	a.mcp.Close()
	a.sess.StopAll()
	_ = a.st.Close()
	a.log.Close()
}

func (a *App) context() context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ctx
}

// emitEvent is Wails' event call; tests replace it, because Wails insists on its own context.
var emitEvent = wruntime.EventsEmit

// emit sends a Wails event once the window exists.
func (a *App) emit(event string, data any) {
	if c := a.context(); c != nil {
		emitEvent(c, event, data)
	}
}

// Invoke runs the named action as the user and returns its JSON result.
func (a *App) Invoke(name string, argsJSON string) (string, error) {
	ctx := a.context()
	if ctx == nil {
		ctx = context.Background()
	}
	return a.reg.Invoke(ctx, actions.You, name, argsJSON)
}

// PtyAttach returns what a terminal replays when it mounts, as "<end>:<base64>". end is the byte
// offset the replay stops at; pty:data events carry their own start and end, so the terminal drops
// what the replay already holds and no byte shows twice.
func (a *App) PtyAttach(sessionID string) string {
	a.feeds.flush(sessionID)
	data, end := a.sess.Attach(sessionID)
	return strconv.FormatInt(end, 10) + ":" + base64.StdEncoding.EncodeToString(data)
}

// PtyWrite sends the user's keystrokes (base64) to a session.
func (a *App) PtyWrite(sessionID, b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("PtyWrite: the data is not base64: %w", err)
	}
	return a.sess.Write(sessionID, data)
}

// PtyResize sets a session's terminal size.
func (a *App) PtyResize(sessionID string, cols, rows int) error {
	return a.sess.Resize(sessionID, cols, rows)
}

// PtyData is the payload of the pty:data:<sessionId> event: output bytes [Start, End) in base64.
type PtyData struct {
	Start int64  `json:"start"`
	End   int64  `json:"end"`
	Data  string `json:"data"`
}

// ptyFeeds batches a session's output for a few milliseconds before it becomes an event, so a
// flood of output costs a handful of events rather than thousands.
type ptyFeeds struct {
	emit func(event string, data any)
	mu   sync.Mutex
	byID map[string]*ptyFeed
}

type ptyFeed struct {
	buf   []byte
	end   int64
	timer *time.Timer
}

const ptyBatch = 8 * time.Millisecond

func (f *ptyFeeds) add(id string, data []byte, end int64) {
	f.mu.Lock()
	feed := f.byID[id]
	if feed == nil {
		feed = &ptyFeed{}
		f.byID[id] = feed
	}
	feed.buf = append(feed.buf, data...)
	feed.end = end
	if feed.timer == nil {
		feed.timer = time.AfterFunc(ptyBatch, func() { f.flush(id) })
	}
	f.mu.Unlock()
}

// flush emits what a session has buffered, now.
func (f *ptyFeeds) flush(id string) {
	f.mu.Lock()
	feed := f.byID[id]
	if feed == nil || len(feed.buf) == 0 {
		if feed != nil {
			feed.timer = nil
		}
		f.mu.Unlock()
		return
	}
	if feed.timer != nil {
		feed.timer.Stop()
		feed.timer = nil
	}
	p := PtyData{Start: feed.end - int64(len(feed.buf)), End: feed.end, Data: base64.StdEncoding.EncodeToString(feed.buf)}
	feed.buf = nil
	f.mu.Unlock()
	f.emit("pty:data:"+id, p)
}

// mcpInfo is what the mcp_info action reports. It reads the token each time, so a token replaced
// on disk shows at once.
func (a *App) mcpInfo() actions.MCPInfoOut {
	out := actions.MCPInfoOut{Port: a.mcp.Port(), URL: a.mcp.URL()}
	if tok, err := a.mcp.Token(); err == nil {
		out.Token = tok
	}
	out.Running = out.Port != 0
	return out
}
