package actions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/sessions/menutest"
	"github.com/GabiHert/hadron/internal/testwait"
)

// The menus the fake claude (sessions/menutest/fakemenu) draws, as Claude Code does.
var (
	mcpMenu = m{"style": "plain", "title": "New MCP server found in this project: acme",
		"body":    []string{"MCP servers may execute code or access system resources."},
		"options": []m{{"label": "Use this MCP server"}, {"label": "Use this and all future MCP servers in this project"}, {"label": "Continue without using this MCP server"}},
		"cursor":  2}
	permMenu = m{"style": "numbered", "title": "Do you want to proceed?", "body": []string{"Bash command", "", "  mkdir demo_dir"},
		"options": []m{{"label": "Yes"}, {"label": "Yes, and don't ask again for mkdir commands"}, {"label": "No, and tell Claude what to do differently (esc)"}},
		"footer":  "Esc to cancel · Tab to amend"}
	askMenu = m{"style": "ask", "header": "Database", "title": "Which database should we use?",
		"options": []m{{"label": "Postgres", "description": "Relational and mature"}, {"label": "SQLite", "description": "Embedded, one file"}},
		"footer":  "Enter to select · ↑/↓ to navigate · Esc to cancel"}
	multiMenu = m{"style": "multi", "header": "Checks", "title": "Which checks should run?",
		"options": []m{{"label": "Lint", "description": "Static checks"}, {"label": "Tests", "description": "Unit tests", "checked": true}, {"label": "Build", "description": "Compile the app"}},
		"footer":  "Enter to select · ↑/↓ to navigate · Esc to cancel"}
)

type menuHarness struct {
	*sessionHarness
	w    domain.Workspace
	card domain.Card
}

// newMenuHarness starts a card whose claude shows the given menus one after another.
func newMenuHarness(t *testing.T, menus ...m) *menuHarness {
	t.Helper()
	spec := filepath.Join(t.TempDir(), "menus.json")
	b, _ := json.Marshal(menus)
	if err := os.WriteFile(spec, b, 0o644); err != nil {
		t.Fatal(err)
	}
	h := newSessionHarnessWith(t, false, testwait.Scaled(5*time.Second), func(c *sessions.Config) { c.MenuHold = 50 * time.Millisecond }, "HADRON_FAKE_MENUS="+spec)
	fake, err := os.ReadFile(menutest.Build(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.bin, "claude"), fake, 0o755); err != nil {
		t.Fatal(err)
	}
	mh := &menuHarness{sessionHarness: h, w: h.workspace("Acme")}
	h.do("card_create", m{"workspace_id": mh.w.ID, "nick": "billing-fix"}, &mh.card)
	return mh
}

// sync runs the watcher until cond holds.
func (h *menuHarness) sync(what string, cond func() bool) {
	h.t.Helper()
	waitUntil(h.t, what, func() bool {
		h.reg.SyncMenus(context.Background())
		return cond()
	})
}

func (h *menuHarness) questions(status string) []domain.Question {
	qs, err := h.reg.Deps().Store.Questions.List(h.w.ID, h.card.ID, status)
	if err != nil {
		h.t.Fatal(err)
	}
	return qs
}

// menuQ waits for the card's one open question about a menu, and returns it.
func (h *menuHarness) menuQ(title string) domain.Question {
	h.t.Helper()
	var q domain.Question
	h.sync("a question for "+title, func() bool {
		for _, x := range h.questions("open") {
			if x.Menu != nil && x.Text == title {
				q = x
				return true
			}
		}
		return false
	})
	return q
}

func (h *menuHarness) answers() string { return h.file(".answers") }

func (h *menuHarness) cardNow() domain.Card {
	var c domain.Card
	h.do("card_get", m{"card_id": h.card.ID}, &c)
	return c
}

func (h *menuHarness) inNeedsYou() bool {
	c := h.cardNow()
	for _, col := range h.columns(h.w.ID) {
		if col.Role == domain.RoleNeedsYou {
			return c.ColumnID == col.ID
		}
	}
	return false
}

func optionLabelsOf(q domain.Question) string {
	var l []string
	for _, o := range q.Options {
		s := o.Label
		if o.Default {
			s += "*"
		}
		l = append(l, s)
	}
	return strings.Join(l, " | ")
}

func TestAnMCPMenuBecomesOneQuestionAndItsAnswerPressesTheKeys(t *testing.T) {
	h := newMenuHarness(t, mcpMenu)
	q := h.menuQ("New MCP server found in this project: acme")

	if q.Kind != "approval" || q.Menu.Kind != "mcp" || q.Multi {
		t.Fatalf("question = %+v", q)
	}
	if got := optionLabelsOf(q); got != "Use this MCP server | Use this and all future MCP servers in this project | Continue without using this MCP server* | Cancel (Esc)" {
		t.Fatalf("options = %q (the highlighted one is starred)", got)
	}
	if !strings.Contains(q.Context, "MCP servers may execute code") {
		t.Errorf("context = %q", q.Context)
	}
	// The card waits on the user, like any question.
	if !h.inNeedsYou() {
		t.Error("the card did not move to Needs you")
	}
	// The same menu, still up, never makes a second question.
	for i := 0; i < 5; i++ {
		h.reg.SyncMenus(context.Background())
		time.Sleep(20 * time.Millisecond)
	}
	if n := len(h.questions("")); n != 1 {
		t.Fatalf("%d questions for one menu, want 1", n)
	}

	h.do("question_answer", m{"question_id": q.ID, "selections": []string{"Use this MCP server"}}, nil)
	waitUntil(t, "the menu answered", func() bool {
		return strings.Contains(h.answers(), "New MCP server found in this project: acme|Use this MCP server|")
	})
	// The menu is gone and the answer stands: no question is asked again or closed as a Terminal answer.
	h.sync("the prompt back", func() bool {
		t, err := h.svc.Find(h.reg.Deps().Store, h.w.ID, h.card.ID)
		return err == nil && t.Live != nil && sessions.ScreenShowsInput(t.Live.Screen(0, false))
	})
	for i := 0; i < 5; i++ {
		h.reg.SyncMenus(context.Background())
		time.Sleep(20 * time.Millisecond)
	}
	all := h.questions("")
	if len(all) != 1 || all[0].Status != "answered" || !strings.Contains(string(all[0].Answer), `"via":"you"`) {
		t.Fatalf("questions after the answer = %+v", all)
	}
}

func TestAPermissionPromptAnsweredNoSendsTheNotesToClaudeAfterIt(t *testing.T) {
	h := newMenuHarness(t, permMenu)
	q := h.menuQ("Do you want to proceed?")
	if q.Menu.Kind != "permission" || q.Kind != "approval" || !strings.Contains(q.Context, "mkdir demo_dir") {
		t.Fatalf("question = %+v", q)
	}
	// The planner answers under the default policy.
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: h.w.ID}, "question_answer", m{"question_id": q.ID,
		"selections": []string{"No, and tell Claude what to do differently (esc)"}, "notes": "make it under tmp instead"}, nil); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the No row chosen", func() bool {
		return strings.Contains(h.answers(), "Do you want to proceed?|No, and tell Claude what to do differently (esc)|")
	})
	waitUntil(t, "the notes typed at the prompt", func() bool { return strings.Contains(h.file(".in"), "make it under tmp instead") })
}

func TestAnAskUserQuestionTakesTypedTextAndAMultiSelectTicksAndSubmits(t *testing.T) {
	h := newMenuHarness(t, askMenu, multiMenu)
	q := h.menuQ("Which database should we use?")
	if q.Menu.Kind != "question" || !q.Menu.FreeText || q.Kind != "decision" {
		t.Fatalf("question = %+v", q)
	}
	if got := optionLabelsOf(q); got != "Postgres* | SQLite | Chat about this | Cancel (Esc)" {
		t.Fatalf("options = %q", got)
	}
	h.do("question_answer", m{"question_id": q.ID, "notes": "the one we already run"}, nil)
	waitUntil(t, "the text typed into the menu", func() bool {
		return strings.Contains(h.answers(), "Which database should we use?||the one we already run")
	})

	mq := h.menuQ("Which checks should run?")
	if !mq.Multi || optionLabelsOf(mq) != "Lint | Tests* | Build | Chat about this | Cancel (Esc)" {
		t.Fatalf("multi question = %+v options %q", mq, optionLabelsOf(mq))
	}
	h.do("question_answer", m{"question_id": mq.ID, "selections": []string{"Lint", "Build"}}, nil)
	waitUntil(t, "Lint and Build ticked, Tests unticked, and submitted past the review", func() bool {
		return strings.Contains(h.answers(), "Which checks should run?|Lint,Build|")
	})
}

func TestAMenuAnsweredInTheTerminalClosesItsQuestion(t *testing.T) {
	h := newMenuHarness(t, mcpMenu)
	q := h.menuQ("New MCP server found in this project: acme")
	h.do("session_keys", m{"card": h.card.ID, "keys": []string{"Up", "Enter"}}, nil)
	waitUntil(t, "the menu answered by hand", func() bool {
		return strings.Contains(h.answers(), "|Use this and all future MCP servers in this project|")
	})
	h.sync("the question closed", func() bool {
		got, err := h.reg.Deps().Store.Questions.Get(q.ID)
		return err == nil && got.Status == "answered"
	})
	got, _ := h.reg.Deps().Store.Questions.Get(q.ID)
	if !strings.Contains(string(got.Answer), `"via":"terminal"`) {
		t.Fatalf("answer = %s", got.Answer)
	}
	if h.inNeedsYou() {
		t.Error("the card stays in Needs you")
	}
	var events []domain.Message
	h.do("chat_read", m{"channel": "card:" + h.card.ID}, &events)
	found := false
	for _, e := range events {
		found = found || strings.Contains(e.Text, "was answered in the Terminal")
	}
	if !found {
		t.Error("the card's chat does not say the menu was answered in the Terminal")
	}
}

func TestSessionMenuReadsTheMenuAndSessionMenuAnswerCancelsIt(t *testing.T) {
	h := newMenuHarness(t, permMenu, mcpMenu)
	q := h.menuQ("Do you want to proceed?")
	var out SessionMenuOut
	h.do("session_menu", m{"card": h.card.Nick}, &out)
	if !out.Open || out.Menu.Kind != "permission" || out.QuestionID != q.ID || len(out.Menu.Options) != 3 {
		t.Fatalf("session_menu = %+v", out)
	}
	var res SessionMenuAnswerOut
	h.do("session_menu_answer", m{"card": h.card.Nick, "cancel": true}, &res)
	if res.QuestionID != q.ID || len(res.Keys) != 1 || res.Keys[0] != "Escape" || res.Next == nil || res.Next.Kind != "mcp" {
		t.Fatalf("session_menu_answer = %+v", res)
	}
	if !strings.Contains(h.answers(), "Do you want to proceed?|cancel") {
		t.Fatalf("answers = %q", h.answers())
	}
	got, _ := h.reg.Deps().Store.Questions.Get(q.ID)
	if got.Status != "answered" || !strings.Contains(string(got.Answer), "Cancel (Esc)") {
		t.Fatalf("question after the cancel = %+v", got)
	}
	// The next menu gets its own question; a session cannot answer it.
	next := h.menuQ("New MCP server found in this project: acme")
	if err := h.as(Actor{Kind: KindSession, Name: h.card.Nick, WorkspaceID: h.w.ID, CardID: h.card.ID}, "session_menu_answer", m{"card": h.card.ID, "choices": []string{"1"}}, nil); err == nil {
		t.Fatal("a session answered its own menu question")
	}
	h.do("session_menu_answer", m{"card": h.card.ID, "choices": []string{"1"}}, &res)
	if res.QuestionID != next.ID || !strings.Contains(h.answers(), "acme|Use this MCP server|") {
		t.Fatalf("answering by number: %+v, answers %q", res, h.answers())
	}
}

func TestAMenuThatIgnoresTheFirstEnterIsTriedAgain(t *testing.T) {
	stubborn := m{}
	for k, v := range mcpMenu {
		stubborn[k] = v
	}
	stubborn["stubborn"] = true
	h := newMenuHarness(t, stubborn)
	h.menuQ("New MCP server found in this project: acme")
	var res SessionMenuAnswerOut
	h.do("session_menu_answer", m{"card": h.card.ID, "choices": []string{"Use this MCP server"}}, &res)
	enters := 0
	for _, k := range res.Keys {
		if k == "Enter" {
			enters++
		}
	}
	if enters != 2 || !strings.Contains(h.answers(), "|Use this MCP server|") {
		t.Fatalf("keys %q, answers %q", res.Keys, h.answers())
	}
}

func TestAMenuQuestionIsWithdrawnWhenTheSessionStops(t *testing.T) {
	h := newMenuHarness(t, mcpMenu)
	q := h.menuQ("New MCP server found in this project: acme")
	h.do("session_stop", m{"card": h.card.ID}, nil)
	h.sync("the question withdrawn", func() bool {
		got, err := h.reg.Deps().Store.Questions.Get(q.ID)
		return err == nil && got.Status == "withdrawn"
	})
}
