package actions

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/fields/fieldstest"
)

// rulesHarness: a workspace with the planner columns, the jira fake enabled, a fake planner, and
// cards alpha and beta in Working.
type rulesHarness struct {
	*harness
	jira    *fieldstest.Fake
	planner *fakePlanner
	chat    *chat.Service
	ws      domain.Workspace
	working domain.Column
	done    domain.Column
	needs   domain.Column
	a, b    domain.Card
}

func newRulesHarness(t *testing.T) *rulesHarness {
	t.Helper()
	deps, rec := testDeps(t)
	fp := &fakePlanner{running: true}
	deps.Chat = chat.New(fp, deps.Store)
	deps.Fields = fields.NewRegistry()
	jira := fieldstest.NewJira()
	if err := deps.Fields.Register(jira); err != nil {
		t.Fatal(err)
	}
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &rulesHarness{harness: &harness{t: t, reg: r, rec: rec}, jira: jira, planner: fp, chat: deps.Chat}
	h.ws = h.workspace("W")
	h.do("workspace_update", m{"workspace_id": h.ws.ID, "integrations": []string{"jira"}}, nil)
	cols := h.columns(h.ws.ID)
	h.working, h.done, h.needs = colByRole(cols, domain.RoleActive), colByRole(cols, domain.RoleDone), colByRole(cols, domain.RoleNeedsYou)
	h.do("card_create", m{"nick": "alpha", "key": "ACME-1", "column_id": h.working.ID, "start": false}, &h.a)
	h.do("card_create", m{"nick": "beta", "column_id": h.working.ID, "start": false}, &h.b)
	return h
}

func (h *rulesHarness) card(c domain.Card) domain.Card {
	h.t.Helper()
	var out domain.Card
	h.do("card_get", m{"card_id": c.ID}, &out)
	return out
}

func (h *rulesHarness) rules() []domain.Rule {
	var out []domain.Rule
	h.do("rule_list", nil, &out)
	return out
}

func (h *rulesHarness) events(c domain.Card) []domain.Message {
	h.t.Helper()
	var msgs []domain.Message
	h.do("chat_read", m{"channel": "card:" + c.ID}, &msgs)
	var out []domain.Message
	for _, msg := range msgs {
		if msg.Kind == "event" {
			out = append(out, msg)
		}
	}
	return out
}

func (h *rulesHarness) log() []RuleLogEntry {
	var out []RuleLogEntry
	h.do("rule_log", m{"limit": 200}, &out)
	return out
}

// finishedRule is the acceptance rule: a linked jira.status becoming Finished moves the card to Done.
func (h *rulesHarness) finishedRule() domain.Rule {
	var r domain.Rule
	h.do("rule_create", m{
		"name":      "Jira finished moves to Done",
		"trigger":   m{"on": "field_changed", "field": "jira.status"},
		"condition": "jira.status == 'Finished'",
		"actions":   []m{{"type": "move", "column_id": h.done.Name}},
	}, &r)
	return r
}

func TestJiraStatusFinishedMovesTheCardToDone(t *testing.T) {
	h := newRulesHarness(t)
	rule := h.finishedRule()

	var trig board.Trigger
	_ = json.Unmarshal(rule.Trigger, &trig)
	acts, _ := board.ParseActions(rule.Actions)
	if acts[0].ColumnID != h.done.ID {
		t.Fatalf("a column named in a rule is stored by id: %+v", acts)
	}

	h.do("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "In progress"}, nil)
	if got := h.card(h.a).ColumnID; got != h.working.ID {
		t.Fatalf("the condition is false, so the card stays in Working; it is in %s", got)
	}
	var out CardFieldSetOut
	h.do("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "Finished"}, &out)
	if got := h.card(h.a).ColumnID; got != h.done.ID {
		t.Fatalf("jira.status Finished must move the card to Done, it is in %s", got)
	}
	if got := h.card(h.b).ColumnID; got != h.working.ID {
		t.Fatal("another card moved")
	}

	// It wrote a rule-authored event on the card and a history row as rule:<name>.
	ev := h.events(h.a)
	if len(ev) != 1 || ev[0].Author != "rule" || !strings.Contains(ev[0].Text, "Moved to Done") || !strings.Contains(ev[0].Text, "Jira finished moves to Done") {
		t.Fatalf("event messages: %+v", ev)
	}
	changes, _ := h.reg.deps.Store.Changes.List(h.ws.ID, 100)
	found := false
	for _, ch := range changes {
		if ch.Actor == "rule:Jira finished moves to Done" && ch.Action == "rule_fire" {
			found = true
			if !strings.Contains(string(ch.Before), h.working.ID) || !strings.Contains(string(ch.After), h.done.ID) {
				t.Fatalf("before/after: %s -> %s", ch.Before, ch.After)
			}
		}
	}
	if !found {
		t.Fatal("no rule_fire change as rule:<name>")
	}
	if log := h.log(); len(log) != 1 || log[0].Kind != "fire" || log[0].Card != "alpha" || log[0].Rule != "Jira finished moves to Done" {
		t.Fatalf("rule_log: %+v", log)
	}
}

func TestRemoteFieldChangesRunTheRulesThroughFieldChanged(t *testing.T) {
	// The M6 path: the integration reports a change, then the app runs the field_changed rules.
	h := newRulesHarness(t)
	h.finishedRule()
	h.jira.Put(h.b.ID, "status", "Finished")
	// Nothing ran yet: the board still shows Working.
	if h.card(h.b).ColumnID != h.working.ID {
		t.Fatal("a remote change alone must not move anything")
	}
	// The integration reports it, and the rules run.
	if err := h.reg.FieldChanged(t.Context(), h.b.ID, "jira.status", "In progress", "Finished"); err != nil {
		t.Fatal(err)
	}
	if h.card(h.b).ColumnID != h.done.ID {
		t.Fatal("FieldChanged must run the field_changed rules")
	}
	var ids FilterEvalOut
	h.do("filter_eval", m{"expr": "jira.status == 'Finished'"}, &ids)
	if len(ids.CardIDs) != 1 || ids.CardIDs[0] != h.b.ID {
		t.Fatalf("filter sees the remote value: %+v", ids)
	}
}

func TestLoopGuardStopsTwoRulesUndoingEachOther(t *testing.T) {
	h := newRulesHarness(t)
	ping := m{"name": "to done", "trigger": m{"on": "column_entered", "column": h.working.Name}, "actions": []m{{"type": "move", "column_id": h.done.Name}}}
	pong := m{"name": "to working", "trigger": m{"on": "column_entered", "column": h.done.Name}, "actions": []m{{"type": "move", "column_id": h.working.Name}}}
	h.do("rule_create", ping, nil)
	h.do("rule_create", pong, nil)

	// Move alpha into Needs you first so entering Working is a real move.
	h.do("card_move", m{"card_id": h.a.ID, "column_id": h.needs.ID}, nil)
	h.do("card_move", m{"card_id": h.a.ID, "column_id": h.working.ID}, nil) // starts the ping-pong

	fires, skips := 0, 0
	for _, e := range h.log() {
		switch e.Kind {
		case "fire":
			fires++
		case "skip":
			skips++
			if !strings.Contains(e.Reason, "loop guard") || e.Card != "alpha" {
				t.Fatalf("skip entry: %+v", e)
			}
		}
	}
	if fires != board.MaxActionsPerCard {
		t.Fatalf("exactly %d rule actions may run per card per event, got %d", board.MaxActionsPerCard, fires)
	}
	if skips < 1 {
		t.Fatal("the guard must log a skip")
	}
	// It ended: 10 moves from Working lands on Working again (even count).
	if got := h.card(h.a).ColumnID; got != h.working.ID {
		t.Fatalf("after 10 moves the card is back where it started, in %s", got)
	}
	// The other card is unaffected and the call returned, so there is no runaway.
	if h.card(h.b).ColumnID != h.working.ID {
		t.Fatal("beta moved")
	}
}

func TestDryRunReportsAndAppliesNothing(t *testing.T) {
	h := newRulesHarness(t)
	rule := h.finishedRule()
	h.jira.Put(h.a.ID, "status", "Finished") // changed remotely: no event ran
	h.jira.Put(h.b.ID, "status", "In progress")

	changesBefore, _ := h.reg.deps.Store.Changes.List(h.ws.ID, 500)
	msgsBefore := len(h.events(h.a))

	var items []board.Firing
	h.do("rule_dry_run", nil, &items)
	if len(items) != 1 {
		t.Fatalf("one card would move: %+v", items)
	}
	it := items[0]
	if it.Card != "alpha" || it.Rule != rule.Name || it.Action != "move" || it.From != "Working" || it.To != "Done" || it.Skipped {
		t.Fatalf("dry run item: %+v", it)
	}

	// Nothing was applied: not the card, not a message, not a history row, not a log entry.
	if h.card(h.a).ColumnID != h.working.ID {
		t.Fatal("the dry run moved a card")
	}
	if len(h.events(h.a)) != msgsBefore {
		t.Fatal("the dry run wrote a message")
	}
	changesAfter, _ := h.reg.deps.Store.Changes.List(h.ws.ID, 500)
	if len(changesAfter) != len(changesBefore) {
		t.Fatalf("the dry run wrote history: %d -> %d", len(changesBefore), len(changesAfter))
	}
	if len(h.log()) != 0 {
		t.Fatal("the dry run left a rule log entry")
	}
	if len(h.jira.Sets) != 0 {
		t.Fatalf("the dry run called the integration: %v", h.jira.Sets)
	}

	// Filtering by rule id.
	h.do("rule_dry_run", m{"rule_id": "no-such"}, &items)
	if len(items) != 0 {
		t.Fatalf("%+v", items)
	}
	// The same rules still apply for real afterwards.
	h.do("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "In progress"}, nil)
	h.do("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "Finished"}, nil)
	if h.card(h.a).ColumnID != h.done.ID {
		t.Fatal("the rule works after a dry run")
	}
}

func TestDryRunNeverCallsTheIntegrationForSetField(t *testing.T) {
	h := newRulesHarness(t)
	h.do("rule_create", m{
		"name":    "Starting a card sets jira In progress",
		"trigger": m{"on": "column_entered", "column": h.working.Name},
		"actions": []m{{"type": "set_field", "path": "jira.status", "value": "In progress"}},
	}, nil)
	var items []board.Firing
	h.do("rule_dry_run", nil, &items)
	if len(items) != 2 || items[0].Action != "set_field" || items[0].To != "In progress" {
		t.Fatalf("%+v", items)
	}
	if len(h.jira.Sets) != 0 {
		t.Fatalf("dry run wrote to the integration: %v", h.jira.Sets)
	}
	// Really moving a card in does call the integration's update op.
	h.do("card_move", m{"card_id": h.a.ID, "column_id": h.needs.ID}, nil)
	h.do("card_move", m{"card_id": h.a.ID, "column_id": h.working.ID}, nil)
	if len(h.jira.Sets) != 1 || h.jira.Sets[0] != "alpha:status=In progress" {
		t.Fatalf("Sets: %v", h.jira.Sets)
	}
}

func TestBuiltinRulesCannotBeDeletedOrEditedOnlyDisabled(t *testing.T) {
	h := newRulesHarness(t)
	rules := h.rules()
	if len(rules) != 2 || !rules[0].Builtin || !rules[1].Builtin {
		t.Fatalf("a new workspace has the two built-in rules: %+v", rules)
	}
	h.fails("rule_delete", m{"rule_id": rules[0].ID}, "built-in", "rule_update", "enabled")
	h.fails("rule_update", m{"rule_id": rules[0].ID, "name": "mine"}, "built-in")
	h.fails("rule_update", m{"rule_id": rules[0].ID, "condition": "true"}, "built-in")

	var r domain.Rule
	h.do("rule_update", m{"rule_id": rules[0].ID, "enabled": false}, &r)
	if r.Enabled {
		t.Fatal("not disabled")
	}
	// With the opening rule off, a question no longer moves the card.
	var q domain.Question
	h.as(Actor{Kind: KindSession, Name: "alpha", WorkspaceID: h.ws.ID, CardID: h.a.ID}, "question_ask", m{"kind": "decision", "text": "Which?", "options": []m{{"label": "A"}, {"label": "B"}}}, &q)
	if h.card(h.a).ColumnID != h.working.ID {
		t.Fatal("a disabled built-in rule moved the card")
	}
	h.do("rule_update", m{"rule_id": rules[0].ID, "enabled": true}, nil)
}

func TestBuiltinRulesRunThroughTheEngineUnchanged(t *testing.T) {
	h := newRulesHarness(t)
	session := Actor{Kind: KindSession, Name: "alpha", WorkspaceID: h.ws.ID, CardID: h.a.ID}
	var q domain.Question
	if err := h.as(session, "question_ask", m{"kind": "decision", "text": "Which?", "options": []m{{"label": "A"}, {"label": "B"}}}, &q); err != nil {
		t.Fatal(err)
	}
	if got := h.card(h.a); got.ColumnID != h.needs.ID || got.ReturnColumnID != h.working.ID {
		t.Fatalf("question must move the card to Needs you and remember Working: %+v", got)
	}
	ev := h.events(h.a)
	if len(ev) != 1 || ev[0].Text != "Moved to Needs you: it needs you" {
		t.Fatalf("%+v", ev)
	}
	h.do("question_answer", m{"question_id": q.ID, "selections": []string{"A"}}, nil)
	if got := h.card(h.a); got.ColumnID != h.working.ID || got.ReturnColumnID != "" {
		t.Fatalf("the answer must return the card: %+v", got)
	}
	ev = h.events(h.a)
	if len(ev) != 2 || ev[1].Text != "Returned to Working: nothing is waiting on you" {
		t.Fatalf("%+v", ev)
	}
}

func TestRuleValidationListsEveryProblem(t *testing.T) {
	h := newRulesHarness(t)
	err := h.try("rule_create", m{
		"name":      "bad",
		"trigger":   m{"on": "field_changed", "field": "jira.nope"},
		"condition": "card.nick ==",
		"actions":   []m{{"type": "move"}, {"type": "set_field", "path": "card.liveness", "value": "idle"}, {"type": "notify"}},
	}, nil)
	if err == nil {
		t.Fatal("must fail")
	}
	for _, want := range []string{"jira.nope", "condition:", "Syntax error", "exactly one of column_id, role or to", "card.liveness cannot be set", "text is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	h.fails("rule_create", m{"name": "x", "trigger": m{"on": "teleport"}, "actions": []m{{"type": "notify", "text": "hi"}}}, "trigger.on", "field_changed")
	h.fails("rule_create", m{"name": "x", "trigger": m{"on": "cron", "spec": "nope"}, "actions": []m{{"type": "notify", "text": "hi"}}}, "cron spec")
	h.fails("rule_create", m{"name": "x", "trigger": m{"on": "idle_for"}, "actions": []m{{"type": "notify", "text": "hi"}}}, "minutes")
	h.fails("rule_create", m{"name": "x", "trigger": m{"on": "column_entered", "column": "Nowhere"}, "actions": []m{{"type": "notify", "text": "hi"}}}, "Columns:")
	h.fails("rule_create", m{"name": "x", "trigger": m{"on": "card_created"}, "actions": []m{}}, "at least one action")
	h.fails("rule_create", m{"name": "x", "trigger": m{"on": "card_created"}, "actions": []m{{"type": "set_field", "path": "jira.status", "value": "Cancelled"}}}, "To do, In progress, Finished")
}

func TestRuleUpdateReorderDeleteAndHistory(t *testing.T) {
	h := newRulesHarness(t)
	r := h.finishedRule()
	var u domain.Rule
	h.do("rule_update", m{"rule_id": r.ID, "name": "Renamed", "condition": "jira.status == 'Finished' && card.assignee == 'You'"}, &u)
	if u.Name != "Renamed" || !strings.Contains(u.Condition, "assignee") {
		t.Fatalf("%+v", u)
	}
	h.fails("rule_update", m{"rule_id": r.ID, "condition": "nope =="}, "condition:")

	ids := []string{}
	for _, x := range h.rules() {
		ids = append(ids, x.ID)
	}
	rev := []string{ids[2], ids[1], ids[0]}
	var ordered []domain.Rule
	h.do("rule_reorder", m{"rule_ids": rev}, &ordered)
	if ordered[0].ID != ids[2] || h.rules()[0].ID != ids[2] {
		t.Fatalf("order not applied: %+v", ordered)
	}
	h.fails("rule_reorder", m{"rule_ids": rev[:2]}, "all 3 rule ids")
	h.fails("rule_reorder", m{"rule_ids": []string{ids[0], ids[0], ids[1]}}, "twice")

	h.do("rule_delete", m{"rule_id": r.ID}, nil)
	if len(h.rules()) != 2 {
		t.Fatal("not deleted")
	}
	h.fails("rule_update", m{"rule_id": r.ID, "enabled": false}, "does not exist")

	changes, _ := h.reg.deps.Store.Changes.List(h.ws.ID, 100)
	var actions []string
	for _, c := range changes {
		actions = append(actions, c.Action)
	}
	joined := strings.Join(actions, " ")
	for _, want := range []string{"rule_create", "rule_update", "rule_reorder", "rule_delete"} {
		if !strings.Contains(joined, want) {
			t.Errorf("config history lacks %s: %s", want, joined)
		}
	}
}

func TestOtherTriggersAndActions(t *testing.T) {
	h := newRulesHarness(t)
	h.do("custom_field_create", m{"name": "priority", "type": "enum", "options": []string{"low", "high"}}, nil)
	h.do("rule_create", m{"name": "New cards are low", "trigger": m{"on": "card_created"}, "actions": []m{{"type": "set_field", "path": "custom.priority", "value": "low"}}}, nil)
	h.do("rule_create", m{"name": "Done report tells the planner", "trigger": m{"on": "report", "kind": "done"},
		"actions": []m{{"type": "tell_planner", "text": "{{card.nick}} says done, jira is {{jira.status}}"}, {"type": "notify", "title": "Finished", "text": "{{card.nick}} reported done"}}}, nil)
	h.do("rule_create", m{"name": "Priority raised", "trigger": m{"on": "field_changed", "field": "custom.priority"}, "condition": "custom.priority == 'high'",
		"actions": []m{{"type": "move", "role": "needs-you"}}}, nil)
	h.do("rule_create", m{"name": "Run a script", "trigger": m{"on": "report"}, "actions": []m{{"type": "run_action", "script": "jira", "action": "transition"}}}, nil)

	var c domain.Card
	h.do("card_create", m{"nick": "gamma", "start": false}, &c)
	var f CardFieldsOut
	h.do("card_fields_get", m{"card": "gamma"}, &f)
	if f.Fields["custom.priority"] != "low" {
		t.Fatalf("card_created rule: %v", f.Fields["custom.priority"])
	}

	h.do("card_field_set", m{"card": "gamma", "path": "custom.priority", "value": "high"}, nil)
	if got := h.card(c); got.ColumnID != h.needs.ID {
		t.Fatalf("field_changed chain: set_field -> field_changed -> move; card is in %s", got.ColumnID)
	}

	h.jira.Put(c.ID, "status", "In progress")
	h.rec.mu.Lock()
	h.rec.got = nil
	h.rec.mu.Unlock()
	h.do("card_report", m{"card": "gamma", "kind": "progress", "text": "halfway"}, nil)
	h.do("card_report", m{"card": "gamma", "kind": "done", "text": "all done"}, nil)
	h.chat.Wait()

	typed := strings.Join(h.planner.typed(), "\n")
	if !strings.Contains(typed, `[rule · Done report tells the planner] [gamma] gamma says done, jira is In progress`) {
		t.Fatalf("tell_planner line missing:\n%s", typed)
	}
	if strings.Count(typed, "says done") != 1 {
		t.Fatalf("the progress report must not match kind done:\n%s", typed)
	}
	notified := false
	h.rec.mu.Lock()
	for _, e := range h.rec.got {
		if e.event == EventNotify {
			n := e.payload.(NotifyEvent)
			notified = n.Title == "Finished" && n.Body == "gamma reported done"
		}
	}
	h.rec.mu.Unlock()
	if !notified {
		t.Fatal("the notify action must emit a notify event")
	}
	var found bool
	for _, e := range h.log() {
		if e.Kind == "skip" && strings.Contains(e.Reason, "no scripts are loaded") {
			found = true
		}
	}
	if !found {
		t.Fatalf("run_action must be logged as skipped: %+v", h.log())
	}
}

func TestMovingToTheColumnItIsInIsANoOp(t *testing.T) {
	h := newRulesHarness(t)
	h.do("rule_create", m{"name": "stay", "trigger": m{"on": "report"}, "actions": []m{{"type": "move", "column_id": h.working.Name}}}, nil)
	h.do("card_report", m{"card": "alpha", "kind": "progress", "text": "x"}, nil)
	if len(h.log()) != 0 || len(h.events(h.a)) != 0 {
		t.Fatalf("a no-op move must write nothing: %+v", h.log())
	}
}

func TestIdleAndCronTimers(t *testing.T) {
	h := newRulesHarness(t)
	h.do("rule_create", m{"name": "Idle nudge", "trigger": m{"on": "idle_for", "minutes": 30}, "actions": []m{{"type": "set_field", "path": "card.stage", "value": "stale"}}}, nil)
	h.do("rule_create", m{"name": "Morning check", "trigger": m{"on": "cron", "spec": "0 9 * * *"}, "condition": "card.nick == 'beta'",
		"actions": []m{{"type": "set_field", "path": "card.assignee", "value": "Ana"}}}, nil)

	ctx := t.Context()
	base := time.Now().UTC()
	// The first tick only starts the clock.
	if err := h.reg.TickRules(ctx, base); err != nil {
		t.Fatal(err)
	}
	if h.card(h.a).Stage != "" {
		t.Fatal("not idle long enough")
	}
	// 31 minutes later both cards have been idle for 30 minutes: the rule fires once for each.
	if err := h.reg.TickRules(ctx, base.Add(31*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if h.card(h.a).Stage != "stale" || h.card(h.b).Stage != "stale" {
		t.Fatalf("idle_for did not fire: %q %q", h.card(h.a).Stage, h.card(h.b).Stage)
	}
	fires := len(h.log())
	if err := h.reg.TickRules(ctx, base.Add(32*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(h.log()) != fires {
		t.Fatal("idle_for fired again for the same idle spell")
	}

	// Cron: a tick that crosses 09:00 fires it, and the condition picks beta alone.
	next9 := time.Date(2030, 1, 2, 8, 59, 0, 0, time.UTC)
	h.reg.clock.mu.Lock()
	h.reg.clock.last = next9
	h.reg.clock.mu.Unlock()
	if err := h.reg.TickRules(ctx, next9.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if h.card(h.b).Assignee != "Ana" || h.card(h.a).Assignee != "You" {
		t.Fatalf("cron + condition: %q %q", h.card(h.a).Assignee, h.card(h.b).Assignee)
	}
}

func TestNoReportForFiresOncePerQuietSpellWithVariables(t *testing.T) {
	h := newRulesHarness(t)
	h.do("rule_create", m{"name": "Quiet session", "trigger": m{"on": "no_report_for", "minutes": 20},
		"actions": []m{{"type": "tell_planner", "text": "{{card.nick}} has been quiet for {{quiet.minutes}} min; last report ({{report.last_kind}}, {{report.last_ago}}): {{report.last}}"}}}, nil)
	h.fails("rule_create", m{"name": "x", "trigger": m{"on": "no_report_for"}, "actions": []m{{"type": "notify", "text": "hi"}}}, "minutes")

	ctx := t.Context()
	base := time.Now().UTC()
	// alpha's session started now and never reported; beta has no running session.
	if err := h.reg.deps.Store.Sessions.Create(&domain.Session{WorkspaceID: h.ws.ID, CardID: h.a.ID, Kind: "card", Command: "claude", Cwd: "/", Status: "running", StartedAt: base}); err != nil {
		t.Fatal(err)
	}
	if err := h.reg.TickRules(ctx, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	h.chat.Wait()
	if len(h.planner.typed()) != 0 {
		t.Fatalf("not quiet long enough: %q", h.planner.typed())
	}
	if err := h.reg.TickRules(ctx, base.Add(21*time.Minute)); err != nil {
		t.Fatal(err)
	}
	h.chat.Wait()
	got := h.planner.typed()
	want := "[rule · Quiet session] [ACME-1 alpha] alpha has been quiet for 21 min; last report (none, never): no report yet"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("typed = %q\nwant %q", got, want)
	}
	// Once per quiet spell.
	if err := h.reg.TickRules(ctx, base.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	h.chat.Wait()
	if len(h.planner.typed()) != 1 {
		t.Fatalf("fired again in the same spell: %q", h.planner.typed())
	}
	// A report starts a new spell, and the next quiet one tells what the last report said.
	h.as(Actor{Kind: KindSession, Name: "alpha", WorkspaceID: h.ws.ID, CardID: h.a.ID}, "card_report", m{"kind": "progress", "text": "migrating the invoices"}, nil)
	if err := h.reg.TickRules(ctx, time.Now().UTC().Add(25*time.Minute)); err != nil {
		t.Fatal(err)
	}
	h.chat.Wait()
	got = h.planner.typed()
	if len(got) < 2 || !strings.Contains(got[len(got)-1], "last report (progress, 25 minutes ago): migrating the invoices") {
		t.Fatalf("typed = %q", got)
	}
}
