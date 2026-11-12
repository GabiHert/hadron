package actions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/prompts"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

// fakeClaude is a stand-in for the claude CLI. It records its argv (NUL separated), its environment
// and a copy of the MCP config it was pointed at, draws Claude's input box unless HADRON_TEST_SILENT
// is set, and appends every line typed into it to <out>.in. A typed /exit ends it, as it ends Claude.
const fakeClaude = `#!/bin/sh
out="$HADRON_TEST_OUT"
: > "$out.argv"
prev=""
for a in "$@"; do
  printf '%s\0' "$a" >> "$out.argv"
  if [ "$prev" = "--mcp-config" ]; then cp "$a" "$out.mcp"; fi
  prev="$a"
done
kind=card
case "$*" in *"You are the planner"*) kind=planner ;; esac
prev=""
for a in "$@"; do
  case "$prev" in --resume|--session-id) echo "$kind $prev $a" >> "$out.history" ;; esac
  prev="$a"
done
env > "$out.env"
pwd > "$out.pwd"
echo started > "$out.started"
if [ -n "$HADRON_TEST_BOX_DELAY" ]; then sleep "$HADRON_TEST_BOX_DELAY"; date +%s > "$out.ready"; fi
if [ -z "$HADRON_TEST_SILENT" ]; then printf '│ > \n'; fi
while IFS= read -r line; do printf '%s\n' "$line" >> "$out.in"; [ "$line" = "/exit" ] && exit 0; done
`

type sessionHarness struct {
	*harness
	svc  *sessions.Service
	out  string // path prefix the fake claude writes to
	bin  string // folder holding the fake claude
	home string
	env  []string
}

// newSessionHarness wires a real sessions.Service whose PATH holds only a fake claude.
func newSessionHarness(t *testing.T, silent bool) *sessionHarness {
	t.Helper()
	return newSessionHarnessEnv(t, silent)
}

// harnessReadyTimeout is how long a send waits for a starting fake claude's input box.
const harnessReadyTimeout = time.Second

// newSessionHarnessEnv is newSessionHarness with extra environment for the fake claude.
func newSessionHarnessEnv(t *testing.T, silent bool, extra ...string) *sessionHarness {
	t.Helper()
	deps, rec := testDeps(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(fakeClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "rec")
	env := []string{"PATH=" + bin + ":/usr/bin:/bin", "HADRON_TEST_OUT=" + out, "HOME=" + t.TempDir()}
	if silent {
		env = append(env, "HADRON_TEST_SILENT=1")
	}
	env = append(env, extra...)
	svc := sessions.New(sessions.Config{
		Store: deps.Store, Home: deps.Home, Env: func() []string { return env },
		Changed: func(ws string, entities ...string) {
			rec.emit(EventChanged, ChangedEvent{WorkspaceID: ws, Entities: entities})
		},
		FirstPromptDelay: 200 * time.Millisecond,
		ReadyTimeout:     harnessReadyTimeout,
		Window:           150 * time.Millisecond,
	})
	deps.Sessions = svc
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &sessionHarness{harness: &harness{t: t, reg: r, rec: rec}, svc: svc, out: out, bin: bin, home: deps.Home, env: env}
	t.Cleanup(svc.StopAll)
	return h
}

func (h *sessionHarness) file(suffix string) string {
	b, _ := os.ReadFile(h.out + suffix)
	return string(b)
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	waitFor(t, what, 6*time.Second, cond)
}

// slowWait is the deadline for conditions that hang on processes exiting, which can take long
// when the whole suite runs at once.
const slowWait = 30 * time.Second

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (h *sessionHarness) card(name string) domain.Card {
	var c domain.Card
	h.do("card_get", m{"card_id": name}, &c)
	return c
}

func (h *sessionHarness) latest(workspaceID, cardID string) domain.Session {
	s, err := h.reg.Deps().Store.Sessions.Latest(workspaceID, cardID)
	if err != nil {
		h.t.Fatal(err)
	}
	return s
}

func (h *sessionHarness) status(card string) SessionStatus {
	var st SessionStatus
	h.do("session_status", m{"card": card}, &st)
	return st
}

// TestCardCreateStartsAFakeClaudeWithTheRightArgvEnvAndFirstPrompt is the WI-08 acceptance test: a
// fake claude script records its argv and env, which prove the prompt flag, the MCP config path,
// the env vars and first-prompt delivery.
func TestCardCreateStartsAFakeClaudeWithTheRightArgvEnvAndFirstPrompt(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "key": "ACME-508", "launch_profile": "brief"}, &card)

	waitUntil(t, "the first prompt", func() bool { return strings.Contains(h.file(".in"), "/brief billing-fix") })

	sess := h.latest(w.ID, card.ID)
	mcpPath := filepath.Join(h.home, "run", sess.ID+".mcp.json")

	// argv: exactly the prompt flag and the MCP config flag, in the order of spec section 4.
	argv := strings.Split(strings.TrimSuffix(h.file(".argv"), "\x00"), "\x00")
	wantPrompt, err := sessions.RenderPrompt(h.reg.Deps().Store, w, &card, prompts.KindSession)
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 6 || argv[0] != "--append-system-prompt" || argv[1] != wantPrompt || argv[2] != "--mcp-config" || argv[3] != mcpPath || argv[4] != "--session-id" || argv[5] != sess.ClaudeSessionID || sess.ClaudeSessionID == "" {
		t.Fatalf("argv = %q\nwant [--append-system-prompt <prompt> --mcp-config %s --session-id <the stored id>]", argv, mcpPath)
	}
	for _, want := range []string{`working on the card "billing-fix" (ACME-508) in the "Acme" workspace`, "card_report", "no linked items yet"} {
		if !strings.Contains(argv[1], want) {
			t.Errorf("the rendered prompt lacks %q:\n%s", want, argv[1])
		}
	}
	if strings.Contains(argv[1], "{{") {
		t.Errorf("the rendered prompt still has a variable:\n%s", argv[1])
	}

	// env.
	env := h.file(".env")
	for _, want := range []string{"HADRON_SESSION_ID=" + sess.ID, "HADRON_CARD_ID=" + card.ID, "HADRON_WORKSPACE_ID=" + w.ID, "TERM=xterm-256color"} {
		if !strings.Contains(env, want+"\n") {
			t.Errorf("env lacks %s", want)
		}
	}
	if pwd := strings.TrimSpace(h.file(".pwd")); !sameDir(t, pwd, w.Root) {
		t.Errorf("cwd = %s, want the workspace root %s", pwd, w.Root)
	}

	// the MCP config the script was handed, and its token file.
	token, err := os.ReadFile(filepath.Join(h.home, "mcp-token"))
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(filepath.Join(h.home, "mcp-token")); st.Mode().Perm() != 0o600 {
		t.Errorf("mcp-token mode = %v, want 0600", st.Mode().Perm())
	}
	if st, _ := os.Stat(mcpPath); st.Mode().Perm() != 0o600 {
		t.Errorf("mcp config mode = %v, want 0600", st.Mode().Perm())
	}
	var cfg struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(h.file(".mcp")), &cfg); err != nil {
		t.Fatalf("the MCP config is not JSON: %v\n%s", err, h.file(".mcp"))
	}
	srv := cfg.MCPServers["hadron"]
	if srv.Type != "http" || srv.URL != "http://127.0.0.1:47820/mcp" ||
		srv.Headers["Authorization"] != "Bearer "+strings.TrimSpace(string(token)) || srv.Headers["X-Hadron-Session"] != sess.ID {
		t.Fatalf("mcp config = %+v", srv)
	}
	if strings.TrimSpace(string(token)) == "" {
		t.Fatal("the token is empty")
	}

	// the session row and the card.
	if sess.Kind != "card" || sess.Status != "running" || sess.Command != "claude" {
		t.Errorf("session row = %+v", sess)
	}
	waitUntil(t, "the pid", func() bool { return h.latest(w.ID, card.ID).PID > 0 })
}

func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, _ := filepath.EvalSymlinks(a)
	rb, _ := filepath.EvalSymlinks(b)
	return ra != "" && ra == rb
}

func TestFirstPromptComesFromTheLaunchProfileAndFallsBackToTheDelay(t *testing.T) {
	cases := []struct {
		profile, key, want string
	}{
		{"brief", "", "/brief the-card"},
		{"orchestrate", "ACME-9", "/orchestrate ACME-9"},
		{"orchestrate", "", "/orchestrate the-card"}, // no key: the nickname
		{"", "", ""}, // no profile: no first prompt
		{"plain", "", ""},
	}
	for _, c := range cases {
		t.Run(c.profile+"/"+c.key, func(t *testing.T) {
			h := newSessionHarness(t, true) // silent: no input box, so only the delay can trigger it
			w := h.workspace("W")
			args := m{"workspace_id": w.ID, "nick": "the-card", "key": c.key}
			if c.profile != "" {
				args["launch_profile"] = c.profile
			}
			start := time.Now()
			h.do("card_create", args, nil)
			waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })
			if c.want == "" {
				time.Sleep(600 * time.Millisecond) // well past the 200 ms delay
				if got := h.file(".in"); got != "" {
					t.Fatalf("typed %q, want nothing", got)
				}
				return
			}
			waitUntil(t, "the first prompt", func() bool { return strings.TrimSpace(h.file(".in")) == c.want })
			if time.Since(start) < 150*time.Millisecond {
				t.Errorf("the prompt arrived after %v: the delay fallback should wait for the 200 ms delay when no input box shows", time.Since(start))
			}
		})
	}
}

func TestFirstPromptIsSentAsSoonAsTheInputBoxShows(t *testing.T) {
	h := newSessionHarness(t, false)
	// A long delay proves the screen, not the timer, triggered the prompt.
	h.svc = sessions.New(sessions.Config{Store: h.reg.Deps().Store, Home: h.home, Env: func() []string { return h.env }, FirstPromptDelay: 30 * time.Second})
	deps := h.reg.Deps()
	deps.Sessions = h.svc
	h.reg = NewRegistry(deps)
	RegisterAll(h.reg)
	t.Cleanup(h.svc.StopAll)
	w := h.workspace("W")
	start := time.Now()
	h.do("card_create", m{"workspace_id": w.ID, "nick": "fast", "launch_profile": "brief"}, nil)
	waitUntil(t, "the first prompt", func() bool { return strings.TrimSpace(h.file(".in")) == "/brief fast" })
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v with a 30 s delay: the input box was not detected", d)
	}
}

func TestInputBoxDetection(t *testing.T) {
	for screen, want := range map[string]bool{
		"╭──────╮\n│ >    │\n╰──────╯": true,
		"welcome\n> ":             true,
		"❯ ":                      true,
		"│ > try \"fix the bug\"": true,
		"Do you trust this folder?\n❯ 1. Yes, proceed\n  2. No": false,
		"loading...": false,
		"":           false,
	} {
		if got := sessions.ScreenShowsInput(screen); got != want {
			t.Errorf("ScreenShowsInput(%q) = %v, want %v", screen, got, want)
		}
	}
}

func TestStartFalseCreatesNoSessionAndSessionStartRunsIt(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "later", "start": false}, &card)
	if _, err := h.reg.Deps().Store.Sessions.Latest(w.ID, card.ID); err == nil {
		t.Fatal("start: false must not create a session")
	}
	if st := h.status("later"); st.Running || st.Session != nil || st.Liveness != "none" {
		t.Fatalf("status before start = %+v", st)
	}
	h.fails("session_send", m{"card": "later", "text": "hi"}, "no session yet", "session_start")

	var st SessionStatus
	h.do("session_start", m{"card": "later"}, &st)
	if !st.Running || st.Session == nil || st.Session.CardID != card.ID {
		t.Fatalf("session_start = %+v", st)
	}
	waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })
	h.fails("session_start", m{"card": "later"}, "already has a running session", "session_restart")
	h.fails("session_start", m{"card": "nobody"}, `no card "nobody"`)
}

func TestSendKeysScreenInterruptStopAndRestartResume(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "talker", "key": "T-1"}, &card)
	waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })

	// addressed by nickname, key and id alike.
	h.do("session_send", m{"card": "talker", "text": "from the planner"}, nil)
	waitUntil(t, "the line", func() bool { return strings.Contains(h.file(".in"), "from the planner\n") })
	h.do("session_send", m{"card": "T-1", "text": "no enter", "enter": false}, nil)
	h.do("session_keys", m{"card": card.ID, "keys": []string{"Enter"}}, nil)
	waitUntil(t, "the line finished by a key", func() bool { return strings.Contains(h.file(".in"), "no enter\n") })
	h.fails("session_keys", m{"card": "talker", "keys": []string{"Hyper-Z"}}, "unknown key")

	var screen ScreenOut
	h.do("session_read_screen", m{"card": "talker"}, &screen)
	if !screen.Running || !strings.Contains(screen.Screen, "│ >") || !strings.Contains(screen.Screen, "from the planner") {
		t.Fatalf("screen = %+v", screen)
	}
	h.do("session_read_screen", m{"card": "talker", "lines": 1}, &screen)
	if strings.Count(screen.Screen, "\n") != 0 {
		t.Fatalf("lines: 1 returned %q", screen.Screen)
	}
	h.fails("session_read_screen", m{"card": "talker", "lines": 99999}, "between 1 and 5000")

	first := h.latest(w.ID, card.ID)
	// Interrupt: Ctrl-C kills the script (no trap), which ends the session.
	h.do("session_interrupt", m{"card": "talker"}, nil)
	waitUntil(t, "the session to end", func() bool { return h.card(card.ID).Liveness == domain.LivenessEnded })
	row, _ := h.reg.Deps().Store.Sessions.Get(first.ID)
	if row.Status != "exited" || row.EndedAt == nil {
		t.Fatalf("session row after exit = %+v", row)
	}
	h.fails("session_send", m{"card": "talker", "text": "x"}, "has ended", "session_restart")
	if st := h.status("talker"); st.Running || st.Liveness != "ended" {
		t.Fatalf("status after exit = %+v", st)
	}
	h.do("session_read_screen", m{"card": "talker"}, &screen) // the last screen stays readable
	if screen.Running || !strings.Contains(screen.Screen, "[process exited") {
		t.Fatalf("screen after exit = %+v", screen)
	}

	// Every launch stores its conversation id (G1), so a restart resumes it.
	if first.ClaudeSessionID == "" || !strings.Contains(h.file(".argv"), "--session-id\x00"+first.ClaudeSessionID) {
		t.Fatalf("the first launch must pass and store --session-id: %+v\n%q", first, h.file(".argv"))
	}
	// Claude writes the transcript after the first message; once it exists a restart resumes it.
	writeConversation(t, cfg, w.Root, first.ClaudeSessionID, "hello", 3000, time.Minute)
	h.do("session_restart", m{"card": "talker"}, nil)
	waitUntil(t, "the second run", func() bool { return h.latest(w.ID, card.ID).ID != first.ID })
	waitUntil(t, "claude to start again", func() bool { return h.card(card.ID).Liveness != domain.LivenessEnded })
	waitUntil(t, "the resumed argv", func() bool { return strings.Contains(h.file(".argv"), "--resume\x00"+first.ClaudeSessionID) })
	second := h.latest(w.ID, card.ID)
	if second.ClaudeSessionID != first.ClaudeSessionID || !strings.Contains(h.file(".argv"), "--resume\x00"+first.ClaudeSessionID) || strings.Contains(h.file(".argv"), "--session-id") {
		t.Fatalf("a restart must resume the stored conversation, not start a new one: %q", h.file(".argv"))
	}
	second.ClaudeSessionID = "abc-123-conversation"
	writeConversation(t, cfg, w.Root, second.ClaudeSessionID, "hello", 3000, time.Minute)
	if err := h.reg.Deps().Store.Sessions.Update(&second); err != nil {
		t.Fatal(err)
	}
	h.do("session_restart", m{"card": "talker"}, nil) // the second run is still alive: restart stops it
	waitUntil(t, "the resumed run", func() bool {
		a := strings.Split(strings.TrimSuffix(h.file(".argv"), "\x00"), "\x00")
		return len(a) == 6 && a[4] == "--resume" && a[5] == "abc-123-conversation"
	})
	third := h.latest(w.ID, card.ID)
	if third.ID == second.ID || third.ClaudeSessionID != "abc-123-conversation" {
		t.Fatalf("third = %+v", third)
	}
	// The old process's exit must not mark the card ended while the new one runs.
	time.Sleep(300 * time.Millisecond)
	if lv := h.card(card.ID).Liveness; lv == domain.LivenessEnded {
		t.Fatalf("the card shows ended although its restarted session is running")
	}
	// fresh: true ignores the known id.
	h.do("session_restart", m{"card": "talker", "fresh": true}, nil)
	waitUntil(t, "the fresh run", func() bool {
		argv := h.file(".argv")
		return strings.Contains(argv, "--session-id\x00") && !strings.Contains(argv, "--resume") && h.latest(w.ID, card.ID).ID != third.ID
	})
	if fresh := h.latest(w.ID, card.ID); fresh.ClaudeSessionID == "" || fresh.ClaudeSessionID == "abc-123-conversation" || fresh.ClaudeSessionID == first.ClaudeSessionID {
		t.Fatalf("a fresh restart gets a new conversation id: %+v", fresh)
	}

	h.do("session_stop", m{"card": "talker"}, nil)
	waitUntil(t, "stop to end the card", func() bool { return h.card(card.ID).Liveness == domain.LivenessEnded })
	h.fails("session_stop", m{"card": "talker"}, "no running session")
}

func TestSessionCommandSettingOverridesTheBinaryForAllProfiles(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	h.do("settings_update", m{"values": m{"session.command": "/bin/cat"}}, nil)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "catty", "launch_profile": "brief"}, &card)
	waitUntil(t, "cat to run", func() bool { return h.status("catty").Running })
	if h.file(".started") != "" {
		t.Fatal("the fake claude ran although session.command points at /bin/cat")
	}
	if got := h.latest(w.ID, card.ID).Command; got != "/bin/cat" {
		t.Fatalf("session command = %q", got)
	}
	// The brief profile's first prompt reaches cat, which echoes it. No claude flags were added.
	waitUntil(t, "the echoed first prompt", func() bool {
		var s ScreenOut
		h.do("session_read_screen", m{"card": "catty"}, &s)
		return strings.Contains(s.Screen, "/brief catty")
	})
	var s ScreenOut
	h.do("session_read_screen", m{"card": "catty"}, &s)
	if strings.Contains(s.Screen, "append-system-prompt") {
		t.Fatalf("claude flags leaked into a non-claude command:\n%s", s.Screen)
	}
}

func TestLivenessUpdatesTheCardAndEmitsChanged(t *testing.T) {
	h := newSessionHarness(t, true)
	w := h.workspace("W")
	h.do("settings_update", m{"values": m{"session.command": "/bin/cat"}}, nil)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "live"}, &card)
	if h.card(card.ID).Liveness != domain.LivenessIdle {
		t.Fatalf("a new session's card is %s, want idle", h.card(card.ID).Liveness)
	}
	before := h.rec.count()
	// cat never draws Claude's prompt: send once the start-up wait is over, as to any running program.
	time.Sleep(harnessReadyTimeout + 100*time.Millisecond)
	h.do("session_send", m{"card": "live", "text": "ping"}, nil)
	waitUntil(t, "working", func() bool { return h.card(card.ID).Liveness == domain.LivenessWorking })
	waitUntil(t, "idle again", func() bool { return h.card(card.ID).Liveness == domain.LivenessIdle })
	var sawCards bool
	h.rec.mu.Lock()
	for _, e := range h.rec.got[before:] {
		if ev, ok := e.payload.(ChangedEvent); ok && ev.WorkspaceID == w.ID {
			for _, ent := range ev.Entities {
				sawCards = sawCards || ent == "cards"
			}
		}
	}
	h.rec.mu.Unlock()
	if !sawCards {
		t.Fatal("a liveness change must emit changed with the cards entity")
	}
	h.do("session_stop", m{"card": "live"}, nil)
	waitUntil(t, "ended", func() bool { return h.card(card.ID).Liveness == domain.LivenessEnded })

	// The waiting hook the chat layer will call.
	h.do("session_restart", m{"card": "live"}, nil)
	waitUntil(t, "restart", func() bool { return h.status("live").Running })
	h.svc.SetWaiting(w.ID, card.ID, true)
	waitUntil(t, "waiting", func() bool { return h.card(card.ID).Liveness == domain.LivenessWaiting })
	h.svc.SetWaiting(w.ID, card.ID, false)
	waitUntil(t, "idle after the question", func() bool { return h.card(card.ID).Liveness == domain.LivenessIdle })
}

func TestPlannerDoesNotAutostartInATestHomeButStartsOnRequest(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W") // the first workspace becomes active
	h.do("workspace_switch", m{"workspace_id": w.ID}, nil)
	var st SessionStatus
	h.do("planner_get", m{}, &st)
	if st.Running || st.Session != nil {
		t.Fatalf("a test home must not autostart a planner: %+v", st)
	}
	if h.file(".started") != "" {
		t.Fatal("the fake claude ran in a test home without being asked")
	}
	h.fails("planner_send", m{"text": "hi"}, "no session yet", "planner_start")

	h.do("planner_start", m{}, &st)
	if !st.Running || st.Session == nil || st.Session.Kind != "planner" || st.Session.CardID != "" {
		t.Fatalf("planner_start = %+v", st)
	}
	waitUntil(t, "the planner to start", func() bool { return h.file(".started") != "" })
	argv := strings.Split(strings.TrimSuffix(h.file(".argv"), "\x00"), "\x00")
	if len(argv) != 6 || argv[4] != "--session-id" || !strings.Contains(argv[1], `You are the planner for the "W" workspace in Hadron.`) ||
		!strings.Contains(argv[1], "Needs you (needs-you), Working (active)") || !strings.Contains(argv[1], "Root folder: "+w.Root) {
		t.Fatalf("planner argv = %q", argv)
	}
	if !strings.Contains(h.file(".env"), "HADRON_CARD_ID=\n") || !strings.Contains(h.file(".env"), "HADRON_SESSION_ID="+st.Session.ID+"\n") {
		t.Fatalf("planner env = %s", h.file(".env"))
	}
	if h.file(".in") != "" {
		t.Fatalf("a planner gets no first prompt, but %q was typed", h.file(".in"))
	}
	h.fails("planner_start", m{}, "already has a running session")

	h.do("planner_send", m{"text": "[planner] hello"}, nil)
	waitUntil(t, "the planner line", func() bool { return strings.Contains(h.file(".in"), "[planner] hello") })
	var screen ScreenOut
	h.do("planner_read_screen", m{}, &screen)
	if !screen.Running || !strings.Contains(screen.Screen, "[planner] hello") {
		t.Fatalf("planner screen = %+v", screen)
	}
	h.do("planner_restart", m{}, nil)
	waitUntil(t, "a new planner session", func() bool { return h.latest(w.ID, "").ID != st.Session.ID })
}

func TestPlannerAutostartSettingStartsItWithTheWorkspace(t *testing.T) {
	h := newSessionHarness(t, false)
	h.do("settings_update", m{"values": m{"planner.autostart": true}}, nil)
	w := h.workspace("W") // first workspace: becomes active, so its planner starts
	waitUntil(t, "the planner to autostart", func() bool { return h.file(".started") != "" })
	if st := (func() SessionStatus { var s SessionStatus; h.do("planner_get", m{"workspace_id": w.ID}, &s); return s })(); !st.Running {
		t.Fatalf("planner_get = %+v", st)
	}
	// Switching to a workspace whose planner already runs starts nothing new.
	first := h.latest(w.ID, "")
	h.do("workspace_switch", m{"workspace_id": w.ID}, nil)
	if h.latest(w.ID, "").ID != first.ID {
		t.Fatal("switching restarted a running planner")
	}
	// Archiving a workspace hangs up its sessions; deleting does too.
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "c1"}, &card)
	h.workspace("Spare") // the last workspace in the tab bar cannot be archived
	h.do("workspace_archive", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "the sessions to be hung up", func() bool { return !h.runningIn(w.ID) })
}

func TestCommandNotFoundFailsStartButNotCardCreate(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	h.do("settings_update", m{"values": m{"session.command": "no-such-claude"}}, nil)
	h.fails("planner_start", m{}, "no-such-claude was not found on PATH", "session.command")

	// card_create keeps the card even though nothing can start.
	h.do("settings_update", m{"values": m{"session.command": "/nonexistent/claude"}}, nil)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "doomed"}, &card)
	waitUntil(t, "the failed start to be recorded", func() bool { return h.card(card.ID).Liveness == domain.LivenessEnded })
	st := h.status("doomed")
	if st.Running || st.Error == "" || st.Session == nil {
		t.Fatalf("status = %+v", st)
	}
	row := h.latest(w.ID, card.ID)
	if row.Status != "exited" || row.ExitCode != -1 {
		t.Fatalf("session row = %+v", row)
	}
	h.fails("session_send", m{"card": "doomed", "text": "x"}, "never started")
}

func TestUnknownLaunchProfileRollsBackCardCreate(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	h.fails("card_create", m{"workspace_id": w.ID, "nick": "nope", "launch_profile": "ghost"}, `launch profile "ghost" does not exist`, "brief, orchestrate, plain")
	if got := h.cards(m{"workspace_id": w.ID}); len(got) != 0 {
		t.Fatalf("the failed create left cards behind: %s", nicks(got))
	}
}

func TestDeletingACardHangsUpItsSession(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "short-lived"}, &card)
	waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })
	ps, _ := h.svc.Live(h.latest(w.ID, card.ID).ID)
	h.do("card_delete", m{"card_id": card.ID}, nil)
	select {
	case <-ps.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("deleting the card left its process running")
	}
}

func TestLaunchProfilesAreSeededAndEditable(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	var list []domain.LaunchProfile
	h.do("launch_profile_list", m{"workspace_id": w.ID}, &list)
	got := map[string]domain.LaunchProfile{}
	for _, p := range list {
		got[p.Name] = p
	}
	if len(list) != 3 || got["brief"].FirstPrompt != "/brief {{nick}}" || got["orchestrate"].FirstPrompt != "/orchestrate {{key}}" ||
		got["plain"].FirstPrompt != "" || got["plain"].Command != "claude" {
		t.Fatalf("seeded profiles = %+v", list)
	}

	var p domain.LaunchProfile
	h.do("launch_profile_create", m{"workspace_id": w.ID, "name": "Haiku", "args": []string{"--model", "haiku"}, "env": m{"A": "1"}, "first_prompt": "/brief {{nick}}"}, &p)
	if p.Command != "claude" || p.Args[1] != "haiku" || p.Env["A"] != "1" {
		t.Fatalf("created = %+v", p)
	}
	h.fails("launch_profile_create", m{"workspace_id": w.ID, "name": "haiku"}, "already exists")
	h.fails("launch_profile_update", m{"profile_id": p.ID, "name": "brief"}, "already exists")
	h.do("launch_profile_update", m{"profile_id": p.ID, "first_prompt": "", "args": []string{}}, &p)
	if p.FirstPrompt != "" || len(p.Args) != 0 || p.Name != "Haiku" {
		t.Fatalf("updated = %+v", p)
	}

	// A card using the custom profile gets its args and env.
	h.do("card_create", m{"workspace_id": w.ID, "nick": "custom", "launch_profile": "haiku"}, nil)
	waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })
	if !strings.Contains(h.file(".env"), "A=1\n") {
		t.Fatalf("the profile env did not reach the session:\n%s", h.file(".env"))
	}

	h.do("launch_profile_delete", m{"profile_id": p.ID}, nil)
	h.fails("launch_profile_delete", m{"profile_id": p.ID}, "does not exist")

	// create, update and delete wrote the config history with before and after.
	changes, err := h.reg.Deps().Store.Changes.List(w.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var seen = map[string]domain.Change{}
	for _, c := range changes {
		if strings.HasPrefix(c.Action, "launch_profile_") {
			seen[c.Action] = c
		}
	}
	if len(seen) != 3 || string(seen["launch_profile_create"].Before) != "null" || string(seen["launch_profile_delete"].After) != "null" ||
		!strings.Contains(string(seen["launch_profile_update"].Before), `"first_prompt":"/brief {{nick}}"`) {
		t.Fatalf("config history = %+v", seen)
	}
}

func TestBackfillGivesOldWorkspacesTheirProfilesOnce(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Old")
	st := h.reg.Deps().Store
	for _, p := range mustProfiles(t, st, w.ID) {
		if err := st.LaunchProfiles.Delete(p.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := board.BackfillLaunchProfiles(st); err != nil {
		t.Fatal(err)
	}
	if n := len(mustProfiles(t, st, w.ID)); n != 3 {
		t.Fatalf("after backfill: %d profiles, want 3", n)
	}
	for _, p := range mustProfiles(t, st, w.ID) {
		_ = st.LaunchProfiles.Delete(p.ID)
	}
	if err := board.BackfillLaunchProfiles(st); err != nil {
		t.Fatal(err)
	}
	if n := len(mustProfiles(t, st, w.ID)); n != 0 {
		t.Fatalf("a second backfill resurrected %d deleted profiles", n)
	}
}

func mustProfiles(t *testing.T, st *store.Store, ws string) []domain.LaunchProfile {
	t.Helper()
	l, err := st.LaunchProfiles.List(ws)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// runningIn reports whether any session of the workspace still runs.
func (h *sessionHarness) runningIn(workspaceID string) bool {
	rows, _ := h.reg.Deps().Store.Sessions.ListRunning()
	for _, r := range rows {
		if r.WorkspaceID == workspaceID {
			return true
		}
	}
	return false
}
