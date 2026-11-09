package actions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
)

// writeConversation writes a fake Claude Code conversation for a root and returns its id.
func writeConversation(t *testing.T, cfg, root, id string, first any, pad int, age time.Duration) {
	t.Helper()
	real, _ := filepath.EvalSymlinks(root)
	dir := filepath.Join(cfg, "projects", sessions.EncodeProjectDir(real))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	line := func(v any) {
		j, _ := json.Marshal(v)
		b.Write(j)
		b.WriteByte('\n')
	}
	line(map[string]any{"type": "permission-mode", "sessionId": id})
	line(map[string]any{"type": "user", "isMeta": true, "message": map[string]any{"role": "user", "content": "<local-command-caveat>ignore</local-command-caveat>"}})
	line(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "<command-name>/clear</command-name>"}})
	line(map[string]any{"type": "user", "isSidechain": true, "message": map[string]any{"role": "user", "content": "a sidechain prompt"}})
	line(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "content": "x"}}}})
	line(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": first}})
	for b.Len() < pad {
		line(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": strings.Repeat("filler ", 20)}})
	}
	path := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	_ = os.Chtimes(path, when, when)
}

func adoptHarness(t *testing.T) (*sessionHarness, domain.Workspace, string) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	return h, w, cfg
}

func TestTheProjectFolderNameReplacesEveryNonAlphanumeric(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/ana/Documents/app":             "-Users-ana-Documents-app",
		"/Users/ana.b/My Projects/x_y/.hidden": "-Users-ana-b-My-Projects-x-y--hidden",
		"/private/tmp/claude-501/-Users-ana":   "-private-tmp-claude-501--Users-ana",
	} {
		if got := sessions.EncodeProjectDir(in); got != want {
			t.Errorf("%s -> %s, want %s", in, got, want)
		}
	}
}

func TestAdoptableListReadsTheWorkspacesConversations(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	writeConversation(t, cfg, w.Root, "11111111-aaaa-bbbb-cccc-000000000001", "Fix the invoice rounding\nplease", 3000, 2*time.Hour)
	writeConversation(t, cfg, w.Root, "11111111-aaaa-bbbb-cccc-000000000002", []any{map[string]any{"type": "text", "text": strings.Repeat("long ", 80)}}, 3000, time.Hour)
	writeConversation(t, cfg, w.Root, "11111111-aaaa-bbbb-cccc-000000000003", "tiny", 0, time.Minute) // under 2 KB
	// A conversation of another folder is not listed.
	writeConversation(t, cfg, t.TempDir(), "11111111-aaaa-bbbb-cccc-000000000009", "elsewhere", 3000, time.Minute)

	var list []AdoptableOut
	h.do("session_adoptable_list", m{"workspace_id": w.ID}, &list)
	if len(list) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if !strings.HasSuffix(list[0].ClaudeSessionID, "002") || !strings.HasSuffix(list[1].ClaudeSessionID, "001") {
		t.Errorf("newest first: %s, %s", list[0].ClaudeSessionID, list[1].ClaudeSessionID)
	}
	if list[1].FirstUserMessage != "Fix the invoice rounding please" {
		t.Errorf("first message = %q (meta, command, sidechain and tool results are skipped; whitespace collapses)", list[1].FirstUserMessage)
	}
	if n := len([]rune(list[0].FirstUserMessage)); n != 200 || !strings.HasSuffix(list[0].FirstUserMessage, "…") {
		t.Errorf("long messages are cut to 200 characters: %d", n)
	}
	if list[0].Size < sessions.MinAdoptableSize || list[0].AlreadyInHadron {
		t.Errorf("%+v", list[0])
	}
}

func TestAdoptCreatesACardAndResumesTheConversation(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	id := "22222222-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, w.Root, id, "Plan the migration", 3000, time.Hour)

	var out AdoptOut
	h.do("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "migration", "key": "ACME-7"}, &out)
	if out.Card == nil || out.Card.Nick != "migration" || out.Card.Key != "ACME-7" || !strings.Contains(out.Warning, "Exit that conversation") {
		t.Fatalf("out = %+v", out)
	}
	waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })
	argv := strings.Split(strings.TrimSuffix(h.file(".argv"), "\x00"), "\x00")
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--resume "+id) || !strings.Contains(joined, "--append-system-prompt") || !strings.Contains(joined, "--mcp-config") {
		t.Fatalf("argv = %q", argv)
	}
	rows, _ := h.reg.Deps().Store.Sessions.ByClaudeID(id)
	if len(rows) < 1 || rows[0].CardID != out.Card.ID {
		t.Errorf("sessions holding the id: %+v", rows)
	}

	// It now shows as already in Hadron, and cannot be adopted twice.
	var list []AdoptableOut
	h.do("session_adoptable_list", m{"workspace_id": w.ID}, &list)
	if len(list) != 1 || !list[0].AlreadyInHadron || list[0].HeldBy != "card migration" {
		t.Errorf("list = %+v", list)
	}
	h.fails("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "again"}, "already in Hadron", "card migration")
	if len(h.cards(m{"workspace_id": w.ID})) != 1 {
		t.Error("a refused adopt must not leave a card")
	}
}

func TestAdoptWithoutStartingAndRefusals(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	id := "33333333-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, w.Root, id, "Hello", 3000, time.Hour)
	h.fails("session_adopt", m{"workspace_id": w.ID, "claude_session_id": "no-such-conversation-id", "nick": "x"}, "has no transcript")
	h.fails("session_adopt", m{"workspace_id": w.ID, "claude_session_id": "../../etc/passwd", "nick": "x"}, "has no transcript")
	h.fails("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "Bad Nick"}, "kebab-case")

	var out AdoptOut
	h.do("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "later", "start": false}, &out)
	time.Sleep(150 * time.Millisecond)
	if h.file(".started") != "" || out.Status.Running {
		t.Error("start=false must not start claude")
	}
	// session_start then resumes it.
	h.do("session_start", m{"card": "later"}, nil)
	waitUntil(t, "claude to start", func() bool { return h.file(".started") != "" })
	if !strings.Contains(h.file(".argv"), "--resume") {
		t.Errorf("argv = %q", h.file(".argv"))
	}
}

func TestPlannerAdoptResumesTheConversationAsThePlanner(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	id := "44444444-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, w.Root, id, "You are the planner", 3000, time.Hour)
	var out AdoptOut
	h.do("planner_adopt", m{"workspace_id": w.ID, "claude_session_id": id}, &out)
	if out.Card != nil || out.Status.Session == nil || out.Status.Session.Kind != "planner" {
		t.Fatalf("out = %+v", out)
	}
	waitUntil(t, "the planner", func() bool { return h.file(".started") != "" })
	argv := h.file(".argv")
	if !strings.Contains(argv, "--resume\x00"+id) || !strings.Contains(argv, `You are the planner for the "W" workspace`) {
		t.Errorf("argv = %q", argv)
	}
	h.fails("planner_adopt", m{"workspace_id": w.ID, "claude_session_id": id}, "planner is running")
	_ = fmt.Sprint()
}

func TestAdoptTypesTheHandoffOnceAndRecordsIt(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	id := "55555555-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, w.Root, id, "Plan the migration", 3000, time.Hour)
	h.do("prompt_set", m{"workspace_id": w.ID, "kind": "handoff", "template": "Welcome {{card.nick}} to {{workspace.name}}."}, nil)

	var out AdoptOut
	h.do("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "migration"}, &out)
	waitUntil(t, "the handoff", func() bool { return strings.Contains(h.file(".in"), "Welcome migration to W.") })
	if n := strings.Count(h.file(".in"), "Welcome"); n != 1 {
		t.Errorf("typed %d times", n)
	}
	var msgs []domain.Message
	h.do("chat_read", m{"workspace_id": w.ID, "channel": "card:" + out.Card.ID}, &msgs)
	found := false
	for _, msg := range msgs {
		found = found || (msg.Kind == "event" && strings.Contains(msg.Text, "Welcome migration to W."))
	}
	if !found {
		t.Errorf("the handoff is recorded as an event on the card: %+v", msgs)
	}
}

func TestAdoptWithoutHandoffTypesNothing(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	id := "66666666-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, w.Root, id, "Hello", 3000, time.Hour)
	var out AdoptOut
	h.do("session_adopt", m{"workspace_id": w.ID, "claude_session_id": id, "nick": "quiet", "handoff": false}, &out)
	waitUntil(t, "claude", func() bool { return h.file(".started") != "" })
	time.Sleep(500 * time.Millisecond)
	if h.file(".in") != "" {
		t.Errorf("typed %q", h.file(".in"))
	}
	var msgs []domain.Message
	h.do("chat_read", m{"workspace_id": w.ID, "channel": "card:" + out.Card.ID}, &msgs)
	for _, msg := range msgs {
		if strings.Contains(msg.Text, "Handoff") {
			t.Errorf("no handoff, no event: %+v", msg)
		}
	}
}

func TestPlannerAdoptTypesThePlannerHandoffAndDefaultsAreEditable(t *testing.T) {
	h, w, cfg := adoptHarness(t)
	id := "77777777-aaaa-bbbb-cccc-000000000001"
	writeConversation(t, cfg, w.Root, id, "Hello", 3000, time.Hour)
	var p PromptOut
	h.do("prompt_get", m{"workspace_id": w.ID, "kind": "handoff_planner"}, &p)
	if p.Custom || !strings.Contains(p.Template, "planner of workspace {{workspace.name}}") || !strings.Contains(p.Template, "[KEY nick] or [planner]") {
		t.Fatalf("default planner handoff = %+v", p)
	}
	h.do("prompt_get", m{"workspace_id": w.ID, "kind": "handoff"}, &p)
	if !strings.Contains(p.Template, "card {{card.nick}} in workspace {{workspace.name}}") || !strings.Contains(p.Template, "question_ask") || !strings.Contains(p.Template, "gate_set") {
		t.Fatalf("default handoff = %+v", p)
	}
	var r PromptRenderOut
	h.do("prompt_render", m{"workspace_id": w.ID, "kind": "handoff"}, &r)
	if !strings.Contains(r.Text, "card example-card in workspace W") {
		t.Errorf("preview uses a sample card: %q", r.Text)
	}
	h.do("planner_adopt", m{"workspace_id": w.ID, "claude_session_id": id}, nil)
	waitUntil(t, "the planner handoff", func() bool { return strings.Contains(h.file(".in"), "planner of workspace W") })
}
