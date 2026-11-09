package actions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

// fakePlanner stands in for the planner's PTY: it records the lines typed into it.
type fakePlanner struct {
	mu      sync.Mutex
	running bool
	failure error
	lines   []string
}

func (f *fakePlanner) Running(*store.Store, string) bool { return f.running }
func (f *fakePlanner) Type(_ *store.Store, _ string, line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failure != nil {
		return f.failure
	}
	f.lines = append(f.lines, line)
	return nil
}
func (f *fakePlanner) typed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.lines...)
}

type chatHarness struct {
	*harness
	planner *fakePlanner
	chat    *chat.Service
	ws      domain.Workspace
	card    domain.Card // ACME-508 billing-fix
	bare    domain.Card // no key: plain
}

func newChatHarness(t *testing.T) *chatHarness {
	t.Helper()
	deps, rec := testDeps(t)
	fp := &fakePlanner{running: true}
	deps.Chat = chat.New(fp, deps.Store)
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &chatHarness{harness: &harness{t: t, reg: r, rec: rec}, planner: fp, chat: deps.Chat}
	h.ws = h.workspace("Acme")
	h.do("card_create", m{"workspace_id": h.ws.ID, "nick": "billing-fix", "key": "ACME-508", "start": false}, &h.card)
	h.do("card_create", m{"workspace_id": h.ws.ID, "nick": "plain", "start": false}, &h.bare)
	return h
}

// as invokes an action as another actor.
func (h *harness) as(actor Actor, name string, args any, out any) error {
	h.t.Helper()
	b, _ := json.Marshal(args)
	res, err := h.reg.Invoke(context.Background(), actor, name, string(b))
	if err != nil {
		return err
	}
	if out != nil {
		return json.Unmarshal([]byte(res), out)
	}
	return nil
}

func (h *chatHarness) session(card domain.Card) Actor {
	return Actor{Kind: KindSession, Name: card.Nick, WorkspaceID: card.WorkspaceID, CardID: card.ID}
}
func (h *chatHarness) plannerActor() Actor { return Actor{Kind: KindPlanner, WorkspaceID: h.ws.ID} }

func TestChatSendOnACardChannelTypesTheTaggedLineIntoThePlanner(t *testing.T) {
	h := newChatHarness(t)
	var msg domain.Message
	h.do("chat_send", m{"channel": "card:" + h.card.ID, "text": "please check the invoice rounding"}, &msg)
	h.chat.Wait()
	if got := h.planner.typed(); len(got) != 1 || got[0] != "[ACME-508 billing-fix] please check the invoice rounding" {
		t.Fatalf("typed = %q", got)
	}
	if msg.Author != "you" || msg.Kind != "text" || !msg.Delivered || msg.Reason != "" || msg.Channel != "card:"+h.card.ID {
		t.Errorf("message = %+v", msg)
	}
	// By nickname and by key too, and a card without a key is tagged by nickname alone.
	h.do("chat_send", m{"channel": "card:plain", "text": "hello"}, nil)
	h.do("chat_send", m{"channel": "card:ACME-508", "text": "again"}, nil)
	h.chat.Wait()
	got := h.planner.typed()
	if len(got) != 3 || got[1] != "[plain] hello" || got[2] != "[ACME-508 billing-fix] again" {
		t.Fatalf("typed = %q", got)
	}
}

func TestChatSendOnThePlannerChannelAndOrdering(t *testing.T) {
	h := newChatHarness(t)
	for _, text := range []string{"one", "two", "three", "four"} {
		h.do("chat_send", m{"channel": "planner", "text": text}, nil)
	}
	h.chat.Wait()
	want := []string{"[planner] one", "[planner] two", "[planner] three", "[planner] four"}
	if got := h.planner.typed(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("typed = %q, want %q", got, want)
	}
	var msgs []domain.Message
	h.do("chat_read", m{"channel": "planner"}, &msgs)
	if len(msgs) != 4 || msgs[0].Text != "one" || msgs[3].Text != "four" {
		t.Errorf("read = %+v", msgs)
	}
	h.do("chat_read", m{"channel": "planner", "limit": 2}, &msgs)
	if len(msgs) != 2 || msgs[0].Text != "three" {
		t.Errorf("limit 2 read = %+v", msgs)
	}
}

func TestAMessageForAPlannerThatIsNotRunningRecordsItsReason(t *testing.T) {
	h := newChatHarness(t)
	h.planner.running = false
	var msg domain.Message
	h.do("chat_send", m{"channel": "planner", "text": "anyone there?"}, &msg)
	h.chat.Wait()
	if msg.Delivered || msg.Reason != chat.ReasonPlannerNotRunning {
		t.Fatalf("message = %+v", msg)
	}
	if got := h.planner.typed(); len(got) != 0 {
		t.Errorf("typed = %q", got)
	}
	var msgs []domain.Message
	h.do("chat_read", m{"channel": "planner"}, &msgs)
	if len(msgs) != 1 || msgs[0].Delivered || msgs[0].Reason != "planner_not_running" {
		t.Errorf("stored = %+v", msgs)
	}
}

func TestATypingFailureAfterTheCallMarksTheMessageUndelivered(t *testing.T) {
	h := newChatHarness(t)
	h.planner.failure = errors.New("broken pipe")
	var msg domain.Message
	h.do("chat_send", m{"channel": "planner", "text": "hi"}, &msg)
	h.chat.Wait()
	var msgs []domain.Message
	h.do("chat_read", m{"channel": "planner"}, &msgs)
	if len(msgs) != 1 || msgs[0].Delivered || msgs[0].Reason != "type_failed: broken pipe" {
		t.Fatalf("stored = %+v", msgs)
	}
}

func TestCardReportStoresTheReportAndNotifiesThePlanner(t *testing.T) {
	h := newChatHarness(t)
	long := strings.Repeat("abcdefghij", 30) // 300 characters
	var msg domain.Message
	if err := h.as(h.session(h.card), "card_report", m{"kind": "progress", "text": long}, &msg); err != nil {
		t.Fatal(err)
	}
	h.chat.Wait()
	want := "[ACME-508 billing-fix · report] progress: " + long[:200]
	if got := h.planner.typed(); len(got) != 1 || got[0] != want {
		t.Fatalf("typed = %q\nwant   %q", got, want)
	}
	if msg.Author != "session" || msg.Kind != "report" || msg.ReportKind != "progress" || msg.Text != long || !msg.Delivered {
		t.Errorf("message = %+v", msg)
	}
	if msg.Channel != "card:"+h.card.ID {
		t.Errorf("channel = %q", msg.Channel)
	}
	// A multi-line report becomes a one-line notice, and a blocked kind is accepted.
	if err := h.as(h.session(h.bare), "card_report", m{"kind": "blocked", "text": "waiting on\nthe VPN"}, nil); err != nil {
		t.Fatal(err)
	}
	h.chat.Wait()
	if got := h.planner.typed(); got[1] != "[plain · report] blocked: waiting on the VPN" {
		t.Errorf("typed = %q", got)
	}
}

func TestCardReportIsForTheCardsOwnSessionOrYou(t *testing.T) {
	h := newChatHarness(t)
	args := m{"kind": "done", "text": "finished"}
	if err := h.as(h.session(h.bare), "card_report", m{"card": h.card.ID, "kind": "done", "text": "x"}, nil); err == nil || !strings.Contains(err.Error(), "own card") {
		t.Errorf("another card's session: %v", err)
	}
	if err := h.as(h.plannerActor(), "card_report", args, nil); err == nil || !strings.Contains(err.Error(), "not one") {
		t.Errorf("planner: %v", err)
	}
	if err := h.as(Actor{Kind: KindExternal}, "card_report", args, nil); err == nil {
		t.Error("external should be refused")
	}
	if err := h.try("card_report", args, nil); err == nil || !strings.Contains(err.Error(), "pass card") {
		t.Errorf("you without a card: %v", err)
	}
	h.do("card_report", m{"card": "billing-fix", "kind": "done", "text": "ok"}, nil)
	if err := h.as(h.session(h.card), "card_report", m{"kind": "weird", "text": "x"}, nil); err == nil || !strings.Contains(err.Error(), "progress, question, blocked, done") {
		t.Errorf("bad kind: %v", err)
	}
	// A session may name its own card.
	if err := h.as(h.session(h.card), "card_report", m{"card": "billing-fix", "kind": "done", "text": "ok"}, nil); err != nil {
		t.Errorf("own card by nickname: %v", err)
	}
}

func TestChatPostIsThePlannersAndChatSendIsTheUsers(t *testing.T) {
	h := newChatHarness(t)
	var msg domain.Message
	if err := h.as(h.plannerActor(), "chat_post", m{"channel": "card:billing-fix", "text": "On it."}, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Author != "planner" || msg.Kind != "text" || msg.Channel != "card:"+h.card.ID {
		t.Errorf("message = %+v", msg)
	}
	h.chat.Wait()
	if got := h.planner.typed(); len(got) != 0 {
		t.Errorf("a planner post is stored, not typed: %q", got)
	}
	h.do("chat_post", m{"channel": "planner", "text": "from you in tests"}, nil)

	if err := h.as(h.session(h.card), "chat_post", m{"channel": "planner", "text": "x"}, nil); err == nil || !strings.Contains(err.Error(), "not the planner") {
		t.Errorf("session chat_post: %v", err)
	}
	if err := h.as(Actor{Kind: KindExternal}, "chat_post", m{"channel": "planner", "text": "x"}, nil); err == nil {
		t.Error("external chat_post should be refused")
	}
	if err := h.as(h.plannerActor(), "chat_send", m{"channel": "planner", "text": "x"}, nil); err == nil || !strings.Contains(err.Error(), "speaks as the user") {
		t.Errorf("planner chat_send: %v", err)
	}
	other := h.workspace("Other")
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: other.ID}, "chat_post", m{"channel": "card:" + h.card.ID, "text": "x"}, nil); err == nil || !strings.Contains(err.Error(), "another workspace") {
		t.Errorf("cross-workspace chat_post: %v", err)
	}
}

func TestSanitizingStripsControlsAndCapsTheLength(t *testing.T) {
	h := newChatHarness(t)
	var msg domain.Message
	h.do("chat_send", m{"channel": "planner", "text": "  /rm -rf\x1b[31m red\x07\x00 text\r\nline two\ttab  "}, &msg)
	h.chat.Wait()
	if msg.Text != "/rm -rf[31m red text\nline twotab" {
		t.Errorf("stored text = %q", msg.Text)
	}
	// The slash cannot become a command: the line starts with the tag.
	if got := h.planner.typed()[0]; !strings.HasPrefix(got, "[planner] /rm") {
		t.Errorf("typed = %q", got)
	}
	h.do("chat_send", m{"channel": "planner", "text": strings.Repeat("é", 9000)}, &msg)
	if n := len([]rune(msg.Text)); n != chat.MaxText {
		t.Errorf("length = %d, want %d", n, chat.MaxText)
	}
	if err := h.try("chat_send", m{"channel": "planner", "text": "\x1b\x07  "}, nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("empty after sanitizing: %v", err)
	}
	if err := h.try("chat_send", m{"channel": "elsewhere", "text": "x"}, nil); err == nil || !strings.Contains(err.Error(), "not valid") {
		t.Errorf("bad channel: %v", err)
	}
	if err := h.try("chat_send", m{"channel": "card:nope", "text": "x"}, nil); err == nil || !strings.Contains(err.Error(), "no card") {
		t.Errorf("unknown card: %v", err)
	}
}

func TestChatListSummarizesChannels(t *testing.T) {
	h := newChatHarness(t)
	h.do("chat_send", m{"channel": "planner", "text": "hello"}, nil)
	time.Sleep(2 * time.Millisecond)
	h.do("card_report", m{"card": "billing-fix", "kind": "progress", "text": "halfway"}, nil)
	h.chat.Wait()
	var list []store.ChannelSummary
	h.do("chat_list", nil, &list)
	if len(list) != 2 || list[0].Channel != "card:"+h.card.ID || list[0].LastText != "halfway" || list[0].LastKind != "report" || list[1].Channel != "planner" || list[1].Count != 1 {
		b, _ := json.Marshal(list)
		t.Fatalf("list = %s", b)
	}
}

func TestWidgetPostStoresTheWidgetForTheRightAuthors(t *testing.T) {
	h := newChatHarness(t)
	var msg domain.Message
	if err := h.as(h.plannerActor(), "widget_post", m{"channel": "planner", "title": "Status", "html": "<b>ok</b>"}, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Kind != "widget" || msg.Author != "planner" || msg.WidgetTitle != "Status" || msg.WidgetHTML != "<b>ok</b>" {
		t.Errorf("widget = %+v", msg)
	}
	if err := h.as(h.session(h.card), "widget_post", m{"channel": "card:billing-fix", "title": "T", "html": "<i>x</i>"}, nil); err != nil {
		t.Errorf("own card: %v", err)
	}
	if err := h.as(h.session(h.bare), "widget_post", m{"channel": "card:billing-fix", "title": "T", "html": "<i>x</i>"}, nil); err == nil {
		t.Error("another card's session should be refused")
	}
	if err := h.as(Actor{Kind: KindExternal}, "widget_post", m{"channel": "planner", "title": "T", "html": "x"}, nil); err == nil {
		t.Error("external should be refused")
	}
	if err := h.try("widget_post", m{"channel": "planner", "title": "", "html": "x"}, nil); err == nil {
		t.Error("a widget needs a title")
	}
}

func TestLinesFormat(t *testing.T) {
	keyed := domain.Card{Key: "ACME-1", Nick: "a"}
	bare := domain.Card{Nick: "b"}
	if got := chat.AnswerNotice(keyed, 3, []string{"Option A", "Option C"}, "go\nahead"); got != "[ACME-1 a · answer] Q-03: Option A, Option C; notes: go ahead" {
		t.Errorf("answer notice = %q", got)
	}
	if got := chat.AnswerNotice(bare, 12, []string{"Approve"}, ""); got != "[b · answer] Q-12: Approve" {
		t.Errorf("answer notice = %q", got)
	}
}

// ---- the exact bytes, through a real PTY ----

// rawPlanner is a fake claude that turns bracketed paste on, sets the terminal raw and records every
// byte typed into it.
const rawPlanner = `#!/bin/sh
printf '\033[?2004h'
stty raw -echo
exec cat > "$HADRON_TEST_OUT.raw"
`

func TestTheExactBytesTypedIntoARealPlannerPTY(t *testing.T) {
	deps, rec := testDeps(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(rawPlanner), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "rec")
	env := []string{"PATH=" + bin + ":/usr/bin:/bin", "HADRON_TEST_OUT=" + out, "HOME=" + t.TempDir()}
	svc := sessions.New(sessions.Config{Store: deps.Store, Home: deps.Home, Env: func() []string { return env }, Window: 150 * time.Millisecond})
	deps.Sessions = svc
	deps.Chat = chat.New(chat.SessionsPlanner{Sessions: svc}, deps.Store)
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &harness{t: t, reg: r, rec: rec}
	t.Cleanup(svc.StopAll)

	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "key": "ACME-508", "start": false}, &card)

	// No planner yet: the message records why.
	var msg domain.Message
	h.do("chat_send", m{"workspace_id": w.ID, "channel": "planner", "text": "too early"}, &msg)
	if msg.Delivered || msg.Reason != "planner_not_running" {
		t.Fatalf("before the planner starts: %+v", msg)
	}

	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	read := func() string { b, _ := os.ReadFile(out + ".raw"); return string(b) }
	waitUntil(t, "the planner to switch bracketed paste on and go raw", func() bool {
		t2, err := svc.Find(deps.Store, w.ID, "")
		if err != nil || t2.Live == nil {
			return false
		}
		return strings.Contains(string(mustScrollback(t2)), "\x1b[?2004h") && fileExists(out+".raw")
	})
	time.Sleep(150 * time.Millisecond) // stty raw lands before the first byte

	h.do("chat_send", m{"workspace_id": w.ID, "channel": "card:billing-fix", "text": "check rounding"}, nil)
	h.do("chat_send", m{"workspace_id": w.ID, "channel": "planner", "text": "status?"}, nil)
	if err := h.as(Actor{Kind: KindSession, Name: "billing-fix", WorkspaceID: w.ID, CardID: card.ID}, "card_report", m{"kind": "done", "text": "all green"}, nil); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[200~[ACME-508 billing-fix] check rounding\x1b[201~\r" +
		"\x1b[200~[planner] status?\x1b[201~\r" +
		"\x1b[200~[ACME-508 billing-fix · report] done: all green\x1b[201~\r"
	waitUntil(t, "all three lines to arrive", func() bool { return len(read()) >= len(want) })
	if got := read(); got != want {
		t.Fatalf("bytes typed into the planner:\n got %q\nwant %q", got, want)
	}
}

func mustScrollback(t sessions.Target) []byte { b, _ := t.Live.Scrollback(); return b }

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func testCtx() context.Context { return context.Background() }
