package actions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
)

type qHarness struct {
	*chatHarness
	needs, working, done domain.Column
}

// newQHarness is a chat harness whose needs-you column is renamed and is not the first column.
func newQHarness(t *testing.T) *qHarness {
	h := &qHarness{chatHarness: newChatHarness(t)}
	for _, c := range h.columns(h.ws.ID) {
		switch c.Role {
		case domain.RoleNeedsYou:
			h.needs = c
		case domain.RoleActive:
			h.working = c
		case domain.RoleDone:
			h.done = c
		}
	}
	h.do("column_update", m{"column_id": h.needs.ID, "name": "Waiting for Gabriel"}, &h.needs)
	var ids []string
	for _, c := range h.columns(h.ws.ID) {
		if c.ID != h.needs.ID {
			ids = append(ids, c.ID)
		}
	}
	h.do("column_reorder", m{"workspace_id": h.ws.ID, "column_ids": append(ids, h.needs.ID)}, nil)
	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.working.ID}, &h.card)
	return h
}

func (h *qHarness) cardNow() domain.Card {
	var c domain.Card
	h.do("card_get", m{"card_id": h.card.ID}, &c)
	return c
}

func (h *qHarness) ask(args m) domain.Question {
	var q domain.Question
	if err := h.as(h.session(h.card), "question_ask", args, &q); err != nil {
		h.t.Fatal(err)
	}
	return q
}

var decision = m{"kind": "decision", "text": "Round invoices up or to nearest?", "context": "Finance said nothing.",
	"options": []m{{"label": "Nearest", "description": "recommended"}, {"label": "Up"}}}

func (h *qHarness) messages() []domain.Message {
	var msgs []domain.Message
	h.do("chat_read", m{"channel": "card:" + h.card.ID}, &msgs)
	return msgs
}

func TestQuestionRoundTripMovesTheCardToTheCustomNamedNeedsYouColumnAndBack(t *testing.T) {
	h := newQHarness(t)
	q := h.ask(decision)
	if q.Seq != 1 || q.Status != "open" || q.Kind != "decision" || len(q.Options) != 2 || q.CardID != h.card.ID {
		t.Fatalf("question = %+v", q)
	}
	card := h.cardNow()
	if card.ColumnID != h.needs.ID || card.ReturnColumnID != h.working.ID || card.Position != 0 {
		t.Fatalf("after the question the card is in %s (return %s), want %s (return %s)", card.ColumnID, card.ReturnColumnID, h.needs.ID, h.working.ID)
	}
	var qs []domain.Question
	h.do("question_list", m{"status": "open"}, &qs)
	if len(qs) != 1 || qs[0].ID != q.ID {
		t.Fatalf("open questions = %+v", qs)
	}

	// The conversation holds the question as a report, and a rule event; the planner heard of it.
	var sawReport, sawEvent bool
	for _, msg := range h.messages() {
		sawReport = sawReport || (msg.Kind == "report" && msg.ReportKind == "question" && msg.QuestionID == q.ID && msg.Author == "session")
		sawEvent = sawEvent || (msg.Kind == "event" && msg.Author == "rule" && strings.Contains(msg.Text, "Waiting for Gabriel"))
	}
	h.chat.Wait()
	if !sawReport || !sawEvent {
		t.Errorf("report=%v event=%v in %+v", sawReport, sawEvent, h.messages())
	}
	if got := h.planner.typed(); len(got) != 1 || got[0] != "[ACME-508 billing-fix · report] question: Round invoices up or to nearest?" {
		t.Errorf("planner heard %q", got)
	}

	var ans domain.Question
	h.do("question_answer", m{"question_id": q.ID, "selections": []string{"nearest"}, "notes": "keep cents\nexact"}, &ans)
	if ans.Status != "answered" || ans.AnsweredAt == nil {
		t.Fatalf("answered = %+v", ans)
	}
	var a struct {
		Selections []string `json:"selections"`
		Notes      string   `json:"notes"`
		Via        string   `json:"via"`
	}
	_ = json.Unmarshal(ans.Answer, &a)
	if len(a.Selections) != 1 || a.Selections[0] != "Nearest" || a.Notes != "keep cents\nexact" || a.Via != "you" {
		t.Errorf("answer = %s", ans.Answer)
	}
	card = h.cardNow()
	if card.ColumnID != h.working.ID || card.ReturnColumnID != "" {
		t.Fatalf("after the answer the card is in %s (return %q), want %s", card.ColumnID, card.ReturnColumnID, h.working.ID)
	}
	h.chat.Wait()
	want := "[ACME-508 billing-fix · answer] Q-01: Nearest; notes: keep cents exact"
	if got := h.planner.typed(); len(got) != 2 || got[1] != want {
		t.Errorf("planner heard %q, want last %q", got, want)
	}
	var answerMsg *domain.Message
	for _, msg := range h.messages() {
		if msg.Kind == "answer" {
			m := msg
			answerMsg = &m
		}
	}
	if answerMsg == nil || answerMsg.QuestionID != q.ID || !answerMsg.Delivered || !strings.Contains(answerMsg.Text, "Q-01: Nearest") {
		t.Errorf("answer message = %+v", answerMsg)
	}
	if err := h.try("question_answer", m{"question_id": q.ID, "selections": []string{"Up"}}, nil); err == nil || !strings.Contains(err.Error(), "already answered") {
		t.Errorf("answering twice: %v", err)
	}
}

func TestTheCardReturnsOnlyWhenTheLastQuestionAndGateAreDone(t *testing.T) {
	h := newQHarness(t)
	q1 := h.ask(decision)
	q2 := h.ask(m{"kind": "unblock", "text": "VPN is down"})
	if q2.Seq != 2 {
		t.Fatalf("seq = %d", q2.Seq)
	}
	var g domain.Gate
	if err := h.as(h.session(h.card), "gate_set", m{"kind": "merge", "summary": "PR #14 is green"}, &g); err != nil {
		t.Fatal(err)
	}
	// Setting the gate again updates it, never duplicates it.
	if err := h.as(h.plannerActor(), "gate_set", m{"card": "billing-fix", "kind": "merge", "summary": "PR #14 is green and reviewed"}, &g); err != nil {
		t.Fatal(err)
	}
	var gates []domain.Gate
	h.do("gate_list", nil, &gates)
	if len(gates) != 1 || gates[0].Summary != "PR #14 is green and reviewed" {
		t.Fatalf("gates = %+v", gates)
	}

	h.do("question_answer", m{"question_id": q1.ID, "selections": []string{"Up"}}, nil)
	if c := h.cardNow(); c.ColumnID != h.needs.ID {
		t.Fatal("one question is still open, so the card stays in Needs you")
	}
	h.do("question_withdraw", m{"question_id": q2.ID, "reason": "VPN is back"}, nil)
	if c := h.cardNow(); c.ColumnID != h.needs.ID {
		t.Fatal("the gate is still set, so the card stays in Needs you")
	}
	h.do("gate_clear", m{"card": "billing-fix", "kind": "merge"}, nil)
	if c := h.cardNow(); c.ColumnID != h.working.ID || c.ReturnColumnID != "" {
		t.Fatalf("everything is done, the card should be back in Working: %+v", c)
	}
	h.do("gate_list", nil, &gates)
	if len(gates) != 0 {
		t.Errorf("gates left: %+v", gates)
	}
}

func TestACardTheUserMovedByHandIsNotDraggedBack(t *testing.T) {
	h := newQHarness(t)
	q := h.ask(decision)
	h.do("card_move", m{"card_id": h.card.ID, "column_id": h.done.ID}, nil)
	h.do("question_answer", m{"question_id": q.ID, "selections": []string{"Up"}}, nil)
	if c := h.cardNow(); c.ColumnID != h.done.ID || c.ReturnColumnID != "" {
		t.Fatalf("the card should stay where the user put it: %+v", c)
	}
}

func TestAnAskFromACardAlreadyInNeedsYouKeepsItsOwnReturnColumn(t *testing.T) {
	h := newQHarness(t)
	h.ask(decision)
	h.ask(m{"kind": "unblock", "text": "second"})
	if c := h.cardNow(); c.ReturnColumnID != h.working.ID {
		t.Fatalf("the second question must not overwrite where the card came from: %+v", c)
	}
}

func TestSwitchedOffBuiltinRulesDoNothingAndWorkspacesWithoutNeedsYouKeepTheCard(t *testing.T) {
	h := newQHarness(t)
	rules, _ := h.reg.Deps().Store.Rules.List(h.ws.ID)
	for _, r := range rules {
		r.Enabled = false
		if err := h.reg.Deps().Store.Rules.Update(&r); err != nil {
			t.Fatal(err)
		}
	}
	h.ask(decision)
	if c := h.cardNow(); c.ColumnID != h.working.ID {
		t.Fatalf("with the rules off the card stays: %+v", c)
	}

	h2 := newQHarness(t)
	h2.do("column_update", m{"column_id": h2.needs.ID, "role": "backlog"}, nil)
	h2.ask(decision)
	if c := h2.cardNow(); c.ColumnID != h2.working.ID || c.ReturnColumnID != "" {
		t.Fatalf("no needs-you column: the card stays: %+v", c)
	}
}

func TestASessionCannotAnswerItsOwnQuestion(t *testing.T) {
	h := newQHarness(t)
	q := h.ask(decision)
	err := h.as(h.session(h.card), "question_answer", m{"question_id": q.ID, "selections": []string{"Up"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot answer its own question") {
		t.Fatalf("own question: %v", err)
	}
	err = h.as(h.session(h.bare), "question_answer", m{"question_id": q.ID, "selections": []string{"Up"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "cannot answer questions") {
		t.Fatalf("another session: %v", err)
	}
	var got domain.Question
	h.do("question_get", m{"question_id": q.ID}, &got)
	if got.Status != "open" {
		t.Errorf("the question must stay open: %+v", got)
	}
	if c := h.cardNow(); c.ColumnID != h.needs.ID {
		t.Error("a refused answer must not move the card")
	}
}

func approval(target string) m {
	return m{"kind": "approval", "text": "Run the migration?", "approval": m{"target": target, "runs_as": "deploy-role", "reversible": false, "command": "make migrate"}}
}

func TestPlannerApprovalIsRefusedUnderDecisionsOnly(t *testing.T) {
	h := newQHarness(t)
	h.do("planner_policy_set", m{"policy": "decisions_only"}, nil)
	ap := h.ask(approval("DEV"))
	if len(ap.Options) != 2 || ap.Options[0].Label != "Approve" || ap.Options[1].Label != "Reject" {
		t.Errorf("an approval without options gets Approve and Reject: %+v", ap.Options)
	}
	dec := h.ask(decision)
	err := h.as(h.plannerActor(), "question_answer", m{"question_id": ap.ID, "selections": []string{"Approve"}}, nil)
	if err == nil || !strings.Contains(err.Error(), "decisions_only") || !strings.Contains(err.Error(), "approvals") {
		t.Fatalf("planner approval under decisions_only: %v", err)
	}
	var got domain.Question
	h.do("question_get", m{"question_id": ap.ID}, &got)
	if got.Status != "open" {
		t.Error("the refused approval must stay open")
	}
	// A decision is the planner's to answer, and it is recorded as the planner's.
	if err := h.as(h.plannerActor(), "question_answer", m{"question_id": dec.ID, "selections": []string{"Nearest"}}, &got); err != nil {
		t.Fatalf("planner decision: %v", err)
	}
	var a struct{ Via string }
	_ = json.Unmarshal(got.Answer, &a)
	if a.Via != "planner" {
		t.Errorf("via = %q", a.Via)
	}
	for _, msg := range h.messages() {
		if msg.Kind == "answer" && msg.Author != "planner" {
			t.Errorf("answer message author = %q", msg.Author)
		}
	}
	h.chat.Wait()
	for _, line := range h.planner.typed() {
		if strings.Contains(line, "· answer] Q-") {
			t.Errorf("the planner is not told about its own answer: %q", line)
		}
	}
	// The user can still approve it.
	h.do("question_answer", m{"question_id": ap.ID, "selections": []string{"Approve"}}, nil)
}

func TestAskForPRDLetsThePlannerApproveDEVButNotPRD(t *testing.T) {
	h := newQHarness(t)
	h.do("planner_policy_set", m{"policy": "ask_for_prd"}, nil)
	dev := h.ask(approval("DEV database"))
	prd := h.ask(approval("PRD database"))
	if err := h.as(h.plannerActor(), "question_answer", m{"question_id": dev.ID, "selections": []string{"Approve"}}, nil); err != nil {
		t.Errorf("DEV approval: %v", err)
	}
	if err := h.as(h.plannerActor(), "question_answer", m{"question_id": prd.ID, "selections": []string{"Approve"}}, nil); err == nil || !strings.Contains(err.Error(), "ask_for_prd") {
		t.Errorf("PRD approval: %v", err)
	}
}

func TestAllowAllIsTheDefaultAndOnlyTheUserSetsThePolicy(t *testing.T) {
	h := newQHarness(t)
	var p PlannerPolicyOut
	h.do("planner_policy_get", nil, &p)
	if p.Policy != "allow_all" {
		t.Fatalf("default policy = %q", p.Policy)
	}
	ap := h.ask(approval("PRD"))
	if err := h.as(h.plannerActor(), "question_answer", m{"question_id": ap.ID, "selections": []string{"Approve"}}, nil); err != nil {
		t.Errorf("allow_all: %v", err)
	}
	h.do("settings_update", m{"values": m{SettingUserActions: "off"}}, nil) // external is covered by the mcp.user_actions tests
	for _, who := range []Actor{h.plannerActor(), h.session(h.card), {Kind: KindExternal}} {
		if err := h.as(who, "planner_policy_set", m{"policy": "allow_all"}, nil); err == nil || !strings.Contains(err.Error(), "only the user") {
			t.Errorf("%s set the policy: %v", who, err)
		}
		if err := h.as(who, "settings_update", m{"values": m{"planner.policy": "allow_all"}}, nil); err == nil || !strings.Contains(err.Error(), "only the user") {
			t.Errorf("%s set the policy through settings: %v", who, err)
		}
	}
	if err := h.try("planner_policy_set", m{"policy": "yolo"}, nil); err == nil || !strings.Contains(err.Error(), "allow_all, decisions_only, ask_for_prd") {
		t.Errorf("bad policy: %v", err)
	}
	if err := h.try("settings_update", m{"values": m{"planner.policy": "yolo"}}, nil); err == nil {
		t.Error("bad policy through settings")
	}
	h.do("settings_update", m{"values": m{"planner.policy": "decisions_only"}}, nil)
	h.do("planner_policy_get", nil, &p)
	if p.Policy != "decisions_only" {
		t.Errorf("policy = %q", p.Policy)
	}
}

func TestGateClearIsTheUsersUnlessThePolicyIsAllowAll(t *testing.T) {
	h := newQHarness(t)
	if err := h.as(h.session(h.card), "gate_set", m{"kind": "close", "summary": "ready to close"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.as(h.session(h.card), "gate_clear", m{"card": "billing-fix", "kind": "close"}, nil); err == nil || !strings.Contains(err.Error(), "session cannot clear") {
		t.Errorf("session clear: %v", err)
	}
	h.do("planner_policy_set", m{"policy": "decisions_only"}, nil)
	if err := h.as(h.plannerActor(), "gate_clear", m{"card": "billing-fix", "kind": "close"}, nil); err == nil || !strings.Contains(err.Error(), "decisions_only") {
		t.Errorf("planner clear under decisions_only: %v", err)
	}
	h.do("planner_policy_set", m{"policy": "allow_all"}, nil)
	if err := h.as(h.plannerActor(), "gate_clear", m{"card": "billing-fix", "kind": "close"}, nil); err != nil {
		t.Errorf("planner clear under allow_all: %v", err)
	}
	if err := h.try("gate_set", m{"card": "billing-fix", "kind": "bogus", "summary": "x"}, nil); err == nil || !strings.Contains(err.Error(), "merge, promote") {
		t.Errorf("bad gate kind: %v", err)
	}
}

func TestQuestionValidationSaysWhatToFix(t *testing.T) {
	h := newQHarness(t)
	s := h.session(h.card)
	for name, tc := range map[string]struct {
		args m
		want string
	}{
		"kind":            {m{"kind": "chat", "text": "x"}, "decision, unblock, approval"},
		"empty":           {m{"kind": "decision", "text": " \x07 "}, "text is empty"},
		"approval info":   {m{"kind": "approval", "text": "x"}, "approval.target"},
		"approval on dec": {m{"kind": "decision", "text": "x", "approval": m{"target": "a", "command": "b"}}, "kind approval only"},
		"dup option":      {m{"kind": "decision", "text": "x", "options": []m{{"label": "A"}, {"label": "a"}}}, "twice"},
		"blank option":    {m{"kind": "decision", "text": "x", "options": []m{{"label": " "}}}, "no label"},
		"multi one":       {m{"kind": "decision", "text": "x", "multi": true, "options": []m{{"label": "A"}}}, "at least two"},
		"another card":    {m{"card": "plain", "kind": "unblock", "text": "x"}, "own card"},
	} {
		if err := h.as(s, "question_ask", tc.args, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	for _, who := range []Actor{h.plannerActor(), {Kind: KindExternal}} {
		if err := h.as(who, "question_ask", decision, nil); err == nil || !strings.Contains(err.Error(), "own session") {
			t.Errorf("%s asked: %v", who, err)
		}
	}
	if err := h.try("question_ask", decision, nil); err == nil || !strings.Contains(err.Error(), "pass card") {
		t.Errorf("you without a card: %v", err)
	}

	multi := h.ask(m{"kind": "decision", "text": "Which regions?", "multi": true, "options": []m{{"label": "EU"}, {"label": "US"}, {"label": "APAC"}}})
	for name, tc := range map[string]struct {
		args m
		want string
	}{
		"not an option": {m{"selections": []string{"Mars"}}, "not an option"},
		"empty":         {m{}, "cannot be empty"},
	} {
		tc.args["question_id"] = multi.ID
		if err := h.try("question_answer", tc.args, nil); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	single := h.ask(decision)
	if err := h.try("question_answer", m{"question_id": single.ID, "selections": []string{"Up", "Nearest"}}, nil); err == nil || !strings.Contains(err.Error(), "takes one option") {
		t.Errorf("two picks on a single question: %v", err)
	}
	// Several picks on a multi question, and a notes-only answer, both work.
	var got domain.Question
	h.do("question_answer", m{"question_id": multi.ID, "selections": []string{"eu", "APAC", "EU"}}, &got)
	if !strings.Contains(string(got.Answer), `["EU","APAC"]`) {
		t.Errorf("multi answer = %s", got.Answer)
	}
	h.do("question_answer", m{"question_id": single.ID, "notes": "neither; round half to even"}, &got)
	if got.Status != "answered" || !strings.Contains(string(got.Answer), `"selections":[]`) {
		t.Errorf("notes-only answer = %s", got.Answer)
	}
}

func TestSessionLivenessIsWaitingWhileAQuestionIsOpen(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "key": "ACME-508"}, &card)
	waitUntil(t, "the session to run", func() bool { return h.status("billing-fix").Running })
	sess := Actor{Kind: KindSession, Name: card.Nick, WorkspaceID: w.ID, CardID: card.ID}

	var q domain.Question
	b, _ := json.Marshal(decision)
	out, err := h.reg.Invoke(testCtx(), sess, "question_ask", string(b))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(out), &q)
	waitUntil(t, "liveness waiting", func() bool {
		return h.status("billing-fix").Liveness == "waiting" && h.card(card.ID).Liveness == "waiting"
	})

	h.do("question_answer", m{"question_id": q.ID, "selections": []string{"Up"}}, nil)
	waitUntil(t, "liveness to leave waiting", func() bool { l := h.card(card.ID).Liveness; return l != "waiting" && l != "" })
}
