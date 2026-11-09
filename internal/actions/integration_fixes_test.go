package actions

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
)

func TestSessionControlPermissionsPerActor(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var a, b domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "launch_profile": "plain"}, &a)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "invoice-sync", "launch_profile": "plain"}, &b)
	waitUntil(t, "a running", func() bool { return h.status("billing-fix").Running })

	self := Actor{Kind: KindSession, Name: "billing-fix", WorkspaceID: w.ID, CardID: a.ID}
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	other := Actor{Kind: KindPlanner, WorkspaceID: "elsewhere"}

	// A card session reads only itself.
	if err := h.as(self, "session_status", m{"card": "billing-fix"}, nil); err != nil {
		t.Errorf("a session reads its own status: %v", err)
	}
	if err := h.as(self, "session_read_screen", m{"card": "billing-fix"}, nil); err != nil {
		t.Errorf("a session reads its own screen: %v", err)
	}
	for _, tc := range []struct {
		name string
		args m
		want string
	}{
		{"session_send", m{"card": "invoice-sync", "text": "x"}, "controls only itself"},
		{"session_read_screen", m{"card": "invoice-sync"}, "controls only itself"},
		{"session_send", m{"card": "billing-fix", "text": "x"}, "on its own session"},
		{"session_stop", m{"card": "billing-fix"}, "on its own session"},
		{"session_restart", m{"card": "billing-fix"}, "on its own session"},
		{"session_keys", m{"card": "billing-fix", "keys": []string{"Enter"}}, "on its own session"},
		{"planner_send", m{"workspace_id": w.ID, "text": "x"}, "on the planner"},
		{"planner_read_screen", m{"workspace_id": w.ID}, "on the planner"},
		{"planner_restart", m{"workspace_id": w.ID}, "on the planner"},
	} {
		err := h.as(self, tc.name, tc.args, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s as a session = %v; want an error containing %q", tc.name, err, tc.want)
		}
	}

	// The planner drives card sessions, but not itself, and not another workspace's.
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "the planner", func() bool { return h.file(".started") != "" })
	if err := h.as(planner, "session_send", m{"card": "invoice-sync", "text": "hello"}, nil); err != nil {
		t.Errorf("the planner types into a card session: %v", err)
	}
	if err := h.as(planner, "planner_read_screen", m{"workspace_id": w.ID}, nil); err != nil {
		t.Errorf("the planner reads its own screen: %v", err)
	}
	for name, args := range map[string]m{"planner_send": {"workspace_id": w.ID, "text": "x"}, "planner_restart": {"workspace_id": w.ID}, "planner_start": {"workspace_id": w.ID}} {
		if err := h.as(planner, name, args, nil); err == nil || !strings.Contains(err.Error(), "on itself") {
			t.Errorf("%s as the planner = %v; want a refusal", name, err)
		}
	}
	if err := h.as(other, "session_send", m{"card": a.ID, "text": "x"}, nil); err == nil || !strings.Contains(err.Error(), "another workspace") {
		t.Errorf("another workspace's planner = %v", err)
	}

	// The user may do all of it.
	h.do("session_send", m{"card": "billing-fix", "text": "hi"}, nil)
	h.do("planner_restart", m{"workspace_id": w.ID}, nil)
}

// ---- planner relay, user_only, gate_withdraw ----

func TestThePlannerRelaysTheUsersAnswerUnderEveryPolicy(t *testing.T) {
	h := newQHarness(t)
	h.do("planner_policy_set", m{"policy": "user_only"}, nil)
	ap := h.ask(approval("PRD"))

	// Under user_only the planner answers nothing itself.
	err := h.as(h.plannerActor(), "question_answer", m{"question_id": ap.ID, "selections": []string{"Approve"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "user_only") || !strings.Contains(err.Error(), "on_behalf_of") {
		t.Fatalf("planner answer under user_only: %v", err)
	}
	var userMsg domain.Message
	h.do("chat_send", m{"channel": "card:" + h.card.ID, "text": "yes, go ahead with the migration"}, &userMsg)

	// A relay needs the user's own message.
	for name, args := range map[string]m{
		"no source":     {"question_id": ap.ID, "selections": []string{"Approve"}, "on_behalf_of": "user"},
		"bad on_behalf": {"question_id": ap.ID, "selections": []string{"Approve"}, "on_behalf_of": "someone", "source_message_id": userMsg.ID},
		"unknown id":    {"question_id": ap.ID, "selections": []string{"Approve"}, "on_behalf_of": "user", "source_message_id": "nope"},
	} {
		if err := h.as(h.plannerActor(), "question_answer", args, nil); err == nil {
			t.Errorf("%s: a relay without a valid source is refused", name)
		}
	}
	var planner domain.Message
	if err := h.as(h.plannerActor(), "chat_post", m{"channel": "card:" + h.card.ID, "text": "I think yes"}, &planner); err != nil {
		t.Fatal(err)
	}
	err = h.as(h.plannerActor(), "question_answer", m{"question_id": ap.ID, "selections": []string{"Approve"}, "on_behalf_of": "user", "source_message_id": planner.ID}, nil)
	if err == nil || !strings.Contains(err.Error(), "not written by the user") {
		t.Fatalf("the planner's own message is no source: %v", err)
	}
	if err := h.as(h.session(h.card), "question_answer", m{"question_id": ap.ID, "selections": []string{"Approve"}, "on_behalf_of": "user", "source_message_id": userMsg.ID}, nil); err == nil || !strings.Contains(err.Error(), "only the planner relays") {
		t.Fatalf("a session cannot relay: %v", err)
	}
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: "elsewhere"}, "question_answer", m{"question_id": ap.ID, "selections": []string{"Approve"}, "on_behalf_of": "user", "source_message_id": userMsg.ID}, nil); err == nil {
		t.Fatal("another workspace's planner cannot relay")
	}

	var got domain.Question
	if err := h.as(h.plannerActor(), "question_answer", m{"question_id": ap.ID, "selections": []string{"Approve"}, "notes": "as I said", "on_behalf_of": "user", "source_message_id": userMsg.ID}, &got); err != nil {
		t.Fatalf("relay: %v", err)
	}
	var a struct {
		Via             string   `json:"via"`
		Selections      []string `json:"selections"`
		SourceMessageID string   `json:"source_message_id"`
	}
	_ = json.Unmarshal(got.Answer, &a)
	if got.Status != "answered" || a.Via != "planner-relay" || a.SourceMessageID != userMsg.ID || len(a.Selections) != 1 {
		t.Fatalf("answer = %s", got.Answer)
	}
	if c := h.cardNow(); c.ColumnID == h.needs.ID {
		t.Error("the relayed answer returns the card")
	}
}

func TestARelayedAnswerIsRefusedWhenTheMessageIsOlderThanTheQuestion(t *testing.T) {
	h := newQHarness(t)
	var old domain.Message
	h.do("chat_send", m{"channel": "card:" + h.card.ID, "text": "yes please"}, &old)
	time.Sleep(5 * time.Millisecond)
	q := h.ask(decision)
	err := h.as(h.plannerActor(), "question_answer", m{"question_id": q.ID, "selections": []string{"Up"}, "on_behalf_of": "user", "source_message_id": old.ID}, nil)
	if err == nil || !strings.Contains(err.Error(), "older than") {
		t.Fatalf("stale source: %v", err)
	}
}

func TestUserOnlyAlsoKeepsThePlannerFromClearingGates(t *testing.T) {
	h := newQHarness(t)
	h.do("planner_policy_set", m{"policy": "user_only"}, nil)
	var g domain.Gate
	if err := h.as(h.session(h.card), "gate_set", m{"kind": "merge", "summary": "PR #14 is green"}, &g); err != nil {
		t.Fatal(err)
	}
	if err := h.as(h.plannerActor(), "gate_clear", m{"gate_id": g.ID}, nil); err == nil || !strings.Contains(err.Error(), "user_only") {
		t.Fatalf("gate_clear under user_only: %v", err)
	}
	var p PlannerPolicyOut
	h.do("planner_policy_get", nil, &p)
	if p.Policy != "user_only" || len(p.Options) != 4 {
		t.Errorf("policy = %+v", p)
	}
}

func TestGateWithdrawTakesBackAGateAndTheCardReturns(t *testing.T) {
	h := newQHarness(t)
	var g domain.Gate
	if err := h.as(h.session(h.card), "gate_set", m{"kind": "merge", "summary": "PR #14 is green"}, &g); err != nil {
		t.Fatal(err)
	}
	if h.cardNow().ColumnID != h.needs.ID {
		t.Fatal("the gate moves the card to Needs you")
	}
	// Another session cannot, a session without a gate id can name its own by kind.
	if err := h.as(h.session(h.bare), "gate_withdraw", m{"gate_id": g.ID}, nil); err == nil || !strings.Contains(err.Error(), "only its own") {
		t.Fatalf("another session: %v", err)
	}
	var got domain.Gate
	if err := h.as(h.session(h.card), "gate_withdraw", m{"kind": "merge", "reason": "set on the wrong card"}, &got); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if got.ID != g.ID || h.cardNow().ColumnID == h.needs.ID {
		t.Errorf("the gate is gone and the card is back: %+v", h.cardNow())
	}
	var gates []domain.Gate
	h.do("gate_list", m{}, &gates)
	if len(gates) != 0 {
		t.Errorf("gates = %+v", gates)
	}
	found := false
	for _, msg := range h.messages() {
		found = found || strings.Contains(msg.Text, "Withdrew the merge gate: set on the wrong card")
	}
	if !found {
		t.Error("the withdrawal is an event on the card")
	}
	// The planner and the user may too.
	h.as(h.session(h.card), "gate_set", m{"kind": "close", "summary": "ready to close"}, nil)
	if err := h.as(h.plannerActor(), "gate_withdraw", m{"card": "billing-fix", "kind": "close"}, nil); err != nil {
		t.Errorf("the planner withdraws: %v", err)
	}
	h.as(h.session(h.card), "gate_set", m{"kind": "close", "summary": "ready to close"}, nil)
	h.do("gate_withdraw", m{"card": "billing-fix", "kind": "close"}, nil)
	if err := h.as(h.session(h.card), "gate_withdraw", m{"kind": "close"}, nil); err == nil {
		t.Error("nothing left to withdraw")
	}
}

// ---- question_ask: meanwhile, reversible, reversible_note, command_lang ----

func TestQuestionAskCarriesMeanwhileReversibleAndCommandLang(t *testing.T) {
	h := newQHarness(t)
	ask := func(args m) (domain.Question, error) {
		var q domain.Question
		err := h.as(h.session(h.card), "question_ask", args, &q)
		return q, err
	}
	ap := func(extra m) m {
		a := m{"target": "DEV", "runs_as": "deploy-role", "command": "ALTER TABLE invoices ADD COLUMN paid_at timestamptz;"}
		for k, v := range extra {
			a[k] = v
		}
		return m{"kind": "approval", "text": "Run the migration?", "meanwhile": "I keep going on the tests and stop before the deploy.", "approval": a}
	}

	q, err := ask(ap(m{"reversible": "partial", "reversible_note": "the column can be dropped, the backfill cannot", "command_lang": "SQL"}))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Reversible     string `json:"reversible"`
		ReversibleNote string `json:"reversible_note"`
		CommandLang    string `json:"command_lang"`
	}
	_ = json.Unmarshal(q.Approval, &got)
	if got.Reversible != "partial" || got.ReversibleNote != "the column can be dropped, the backfill cannot" || got.CommandLang != "sql" || !strings.HasPrefix(q.Meanwhile, "I keep going on the tests") {
		t.Fatalf("question = %+v approval=%s", q, q.Approval)
	}
	var stored domain.Question
	h.do("question_get", m{"question_id": q.ID}, &stored)
	if stored.Meanwhile != q.Meanwhile {
		t.Errorf("meanwhile is stored: %q", stored.Meanwhile)
	}

	for name, args := range map[string]m{
		"partial needs a note": ap(m{"reversible": "partial"}),
		"bad value":            ap(m{"reversible": "maybe"}),
		"missing":              ap(m{"reversible": ""}),
		"bad lang":             ap(m{"reversible": "yes", "command_lang": "not a language!"}),
	} {
		if _, err := ask(args); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	// Older sessions sent true or false; they still work and read back as yes and no.
	for in, want := range map[bool]string{true: "yes", false: "no"} {
		q, err := ask(ap(m{"reversible": in}))
		if err != nil {
			t.Fatalf("reversible %v: %v", in, err)
		}
		_ = json.Unmarshal(q.Approval, &got)
		if got.Reversible != want {
			t.Errorf("reversible %v reads back as %q", in, got.Reversible)
		}
	}
}

// ---- card_move keeps return_column_id true ----

func TestCardMoveUpdatesTheReturnColumn(t *testing.T) {
	h := newQHarness(t)
	q := h.ask(decision)
	if c := h.cardNow(); c.ColumnID != h.needs.ID || c.ReturnColumnID != h.working.ID {
		t.Fatalf("the question moves the card and remembers Working: %+v", c)
	}
	// The user drags the waiting card out to Done: the old pointer goes away at once.
	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.done.ID}, nil)
	if c := h.cardNow(); c.ReturnColumnID != "" {
		t.Fatalf("a card moved out of Needs you forgets its return column: %+v", c)
	}
	// Dragging it back into Needs you while the question is open remembers Done.
	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.needs.ID}, nil)
	if c := h.cardNow(); c.ReturnColumnID != h.done.ID {
		t.Fatalf("moved into Needs you with a question open, it returns to where it came from: %+v", c)
	}
	h.do("question_answer", m{"question_id": q.ID, "selections": []string{"Up"}}, nil)
	if c := h.cardNow(); c.ColumnID != h.done.ID || c.ReturnColumnID != "" {
		t.Fatalf("answering returns the card to Done: %+v", c)
	}
	// With nothing waiting, moving into Needs you remembers nothing.
	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.needs.ID}, nil)
	if c := h.cardNow(); c.ReturnColumnID != "" {
		t.Fatalf("nothing waits, so nothing to return to: %+v", c)
	}
}

// ---- a card session sets its own stage and custom fields ----

func TestACardSessionSetsItsOwnStageAndCustomFieldsOnly(t *testing.T) {
	h := newQHarness(t)
	h.do("custom_field_create", m{"name": "priority", "type": "enum", "options": []string{"low", "high"}}, nil)
	self := h.session(h.card)
	if err := h.as(self, "card_field_set", m{"path": "card.stage", "value": "reviewing"}, nil); err != nil {
		t.Fatalf("own stage, no card named: %v", err)
	}
	if err := h.as(self, "card_field_set", m{"card": "billing-fix", "path": "custom.priority", "value": "high"}, nil); err != nil {
		t.Fatalf("own custom field: %v", err)
	}
	if c := h.cardNow(); c.Stage != "reviewing" {
		t.Fatalf("stage = %q", c.Stage)
	}
	for _, tc := range []struct {
		args m
		want string
	}{
		{m{"path": "card.assignee", "value": "Ana"}, "card.stage, custom.*"},
		{m{"path": "card.column", "value": "Done"}, "card.stage, custom.*"},
		{m{"card": "plain", "path": "card.stage", "value": "x"}, "only on its own card"},
	} {
		err := h.as(self, "card_field_set", tc.args, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v = %v; want an error containing %q", tc.args, err, tc.want)
		}
	}
	// The planner and the user still set anything, and must name the card.
	if err := h.as(h.plannerActor(), "card_field_set", m{"card": "billing-fix", "path": "card.assignee", "value": "Ana"}, nil); err != nil {
		t.Errorf("the planner: %v", err)
	}
	h.fails("card_field_set", m{"path": "card.stage", "value": "x"}, "pass card")
}
