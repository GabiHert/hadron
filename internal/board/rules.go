package board

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/store"
)

// Trigger names (spec section 7).
const (
	OnFieldChanged     = "field_changed"
	OnColumnEntered    = "column_entered"
	OnCardCreated      = "card_created"
	OnReport           = "report"
	OnQuestionOpened   = "question_opened"
	OnQuestionAnswered = "question_answered"
	OnIdleFor          = "idle_for"
	OnCron             = "cron"
)

// Triggers lists every trigger name.
var Triggers = []string{OnFieldChanged, OnColumnEntered, OnCardCreated, OnReport, OnQuestionOpened, OnQuestionAnswered, OnIdleFor, OnCron}

// Action types.
const (
	ActMove        = "move"
	ActSetField    = "set_field"
	ActNotify      = "notify"
	ActTellPlanner = "tell_planner"
	ActRunAction   = "run_action"
)

// ActionTypes lists every action type.
var ActionTypes = []string{ActMove, ActSetField, ActNotify, ActTellPlanner, ActRunAction}

// ReturnColumn is the value of a move action's To that sends a card back to where it was before a
// built-in rule moved it to Needs you.
const ReturnColumn = "return_column"

// MaxActionsPerCard is the loop guard: one event lets rules run at most this many actions on a card.
const MaxActionsPerCard = 10

// Trigger is a rule's trigger, stored as JSON.
type Trigger struct {
	On      string `json:"on" jsonschema:"field_changed, column_entered, card_created, report, question_opened, question_answered, idle_for or cron"`
	Field   string `json:"field,omitempty" jsonschema:"field_changed: the field path to watch, such as jira.status; empty means any field"`
	Column  string `json:"column,omitempty" jsonschema:"column_entered: the column id; empty means any column"`
	Kind    string `json:"kind,omitempty" jsonschema:"report: progress, question, blocked or done; empty means any report"`
	Minutes int    `json:"minutes,omitempty" jsonschema:"idle_for: how many minutes a card's session stays idle"`
	Spec    string `json:"spec,omitempty" jsonschema:"cron: a five-field cron spec, such as 0 9 * * 1-5"`
}

// Action is one thing a rule does.
type Action struct {
	Type           string `json:"type" jsonschema:"move, set_field, notify, tell_planner or run_action"`
	ColumnID       string `json:"column_id,omitempty" jsonschema:"move: the destination column id"`
	Role           string `json:"role,omitempty" jsonschema:"move: the first column with this role (needs-you, active, blocked, done, backlog)"`
	To             string `json:"to,omitempty" jsonschema:"move: return_column sends the card back to where a built-in rule took it from"`
	RememberReturn bool   `json:"remember_return,omitempty" jsonschema:"move: remember the column the card leaves, for a later return_column"`
	Path           string `json:"path,omitempty" jsonschema:"set_field: the field to write"`
	Value          any    `json:"value,omitempty" jsonschema:"set_field: the value to write"`
	Title          string `json:"title,omitempty" jsonschema:"notify: the notification title; default is the rule name"`
	Text           string `json:"text,omitempty" jsonschema:"notify and tell_planner: the message; {{card.nick}} and {{<field path>}} are filled in"`
	Script         string `json:"script,omitempty" jsonschema:"run_action: the integration or script name"`
	Action         string `json:"action,omitempty" jsonschema:"run_action: the action to run"`
}

// ParseTrigger decodes a rule's stored trigger.
func ParseTrigger(raw json.RawMessage) (Trigger, error) {
	var t Trigger
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, fmt.Errorf("the trigger is not valid JSON: %w", err)
	}
	return t, nil
}

// ParseActions decodes a rule's stored actions.
func ParseActions(raw json.RawMessage) ([]Action, error) {
	var a []Action
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("the actions are not valid JSON: %w", err)
	}
	return a, nil
}

// Event is something that happened to a card that rules may react to.
type Event struct {
	Trigger    string
	CardID     string
	Field      string // field_changed
	Old, New   any    // field_changed
	ColumnID   string // column_entered
	ReportKind string // report
	Minutes    int    // idle_for
	Spec       string // cron
}

// Effect is a side effect a rule asks for. The caller performs it once the transaction commits, so
// a rolled-back run (and a dry run) leaves nothing behind.
type Effect struct {
	Kind        string // notify or tell_planner
	WorkspaceID string
	CardID      string
	Rule        string
	Title       string
	Text        string
}

// Firing is one rule action that ran, or was skipped.
type Firing struct {
	RuleID  string `json:"rule_id"`
	Rule    string `json:"rule"`
	CardID  string `json:"card_id"`
	Card    string `json:"card"`
	Action  string `json:"action"`
	From    string `json:"from"`
	To      string `json:"to"`
	Text    string `json:"text"`
	Skipped bool   `json:"skipped"`
	Reason  string `json:"reason,omitempty"`
	FromID  string `json:"-"`
	ToID    string `json:"-"`
}

// Outcome is everything one event caused.
type Outcome struct {
	Firings []Firing
	Effects []Effect
	// MovedTo is the column the card's first move led to, if any.
	MovedTo *domain.Column
}

// ActionRunner runs a script action for a run_action rule. It runs inside the event's transaction
// and returns the events the script's own changes caused, so rules can react to them.
type ActionRunner interface {
	RunAction(ctx context.Context, tx *store.Store, ws domain.Workspace, card domain.Card, script, action string) (ActionResult, error)
}

// ActionResult is what a script action did.
type ActionResult struct {
	Text   string
	Events []Event
}

// Engine runs rules. Fields supplies the CEL environment and integration fields; nil means native
// and custom fields only. With DryRun set, a linked field is not written back to its integration
// and run_action runs nothing. Runner runs run_action; without one the action is skipped.
type Engine struct {
	Fields *fields.Registry
	DryRun bool
	Runner ActionRunner
}

// Change actions the engine writes to the history.
const (
	ChangeFire = "rule_fire"
	ChangeSkip = "rule_skip"
)

type runState struct {
	e       *Engine
	tx      *store.Store
	ws      domain.Workspace
	rules   []domain.Rule
	env     *fields.Env
	cache   map[string]*fields.Compiled
	budget  map[string]int
	skipped map[string]bool
	out     Outcome
	queue   []Event
}

// Handle runs every enabled rule that matches ev, in position order, then the events their actions
// cause, until nothing more happens or the loop guard stops a card. Run it inside the transaction
// that made the change. A rule with a condition runs only when the condition holds for the card.
func (e *Engine) Handle(ctx context.Context, tx *store.Store, ev Event) (Outcome, error) {
	card, err := tx.Cards.Get(ev.CardID)
	if err != nil {
		return Outcome{}, err
	}
	ws, err := tx.Workspaces.Get(card.WorkspaceID)
	if err != nil {
		return Outcome{}, err
	}
	rules, err := tx.Rules.List(ws.ID)
	if err != nil {
		return Outcome{}, err
	}
	if e.Fields == nil {
		e.Fields = fields.NewRegistry()
	}
	rs := &runState{e: e, tx: tx, ws: ws, rules: rules, cache: map[string]*fields.Compiled{}, budget: map[string]int{}, skipped: map[string]bool{}, queue: []Event{ev}}
	for len(rs.queue) > 0 {
		next := rs.queue[0]
		rs.queue = rs.queue[1:]
		for _, rule := range rs.rules {
			if !rule.Enabled {
				continue
			}
			t, err := ParseTrigger(rule.Trigger)
			if err != nil || !matches(t, next) {
				continue
			}
			if err := rs.fire(ctx, rule, next); err != nil {
				return rs.out, err
			}
		}
	}
	return rs.out, nil
}

func matches(t Trigger, ev Event) bool {
	if t.On != ev.Trigger {
		return false
	}
	switch t.On {
	case OnFieldChanged:
		return t.Field == "" || t.Field == ev.Field
	case OnColumnEntered:
		return t.Column == "" || t.Column == ev.ColumnID
	case OnReport:
		return t.Kind == "" || t.Kind == ev.ReportKind
	case OnIdleFor:
		return t.Minutes == ev.Minutes
	case OnCron:
		return t.Spec == ev.Spec
	}
	return true
}

func (rs *runState) fire(ctx context.Context, rule domain.Rule, ev Event) error {
	card, err := rs.tx.Cards.Get(ev.CardID)
	if err != nil {
		return err
	}
	ok, why := rs.holds(rule, card)
	if !ok {
		if why != "" {
			return rs.skip(rule, card, why)
		}
		return nil
	}
	acts, err := ParseActions(rule.Actions)
	if err != nil {
		return rs.skip(rule, card, err.Error())
	}
	var texts []string
	for _, a := range acts {
		if rs.budget[card.ID] >= MaxActionsPerCard {
			if err := rs.skip(rule, card, fmt.Sprintf("loop guard: %d rule actions already ran on %s for this event, so this rule was skipped. Check for rules that undo each other", MaxActionsPerCard, card.Nick)); err != nil {
				return err
			}
			break
		}
		card, err = rs.tx.Cards.Get(card.ID)
		if err != nil {
			return err
		}
		f, followups, changed, err := rs.act(ctx, rule, card, a)
		if err != nil {
			if rerr := rs.skip(rule, card, fmt.Sprintf("%s failed: %v", a.Type, err)); rerr != nil {
				return rerr
			}
			continue
		}
		if f.Skipped {
			if err := rs.skip(rule, card, f.Reason); err != nil {
				return err
			}
			continue
		}
		if !changed {
			continue
		}
		rs.budget[card.ID]++
		f.RuleID, f.Rule, f.CardID, f.Card, f.Action = rule.ID, rule.Name, card.ID, card.Nick, a.Type
		rs.out.Firings = append(rs.out.Firings, f)
		texts = append(texts, f.Text)
		if err := rs.record(rule, card, f); err != nil {
			return err
		}
		rs.queue = append(rs.queue, followups...)
	}
	if len(texts) == 0 {
		return nil
	}
	text := strings.Join(texts, "; ")
	if !rule.Builtin {
		text += " (rule: " + rule.Name + ")"
	}
	return rs.tx.Messages.Create(&domain.Message{WorkspaceID: card.WorkspaceID, Channel: "card:" + card.ID, Author: "rule", Kind: "event", Text: text, Delivered: true})
}

// holds evaluates the rule's condition for the card. A condition that cannot run is logged, not fatal.
func (rs *runState) holds(rule domain.Rule, card domain.Card) (bool, string) {
	expr := strings.TrimSpace(rule.Condition)
	if expr == "" {
		return true, ""
	}
	prog, ok := rs.cache[expr]
	if !ok {
		if rs.env == nil {
			env, err := rs.e.Fields.NewEnv(rs.ws)
			if err != nil {
				return false, "condition could not be built: " + err.Error()
			}
			rs.env = env
		}
		var err error
		if prog, err = rs.env.Compile(expr); err != nil {
			return false, "condition does not compile: " + err.Error()
		}
		rs.cache[expr] = prog
	}
	schema, err := rs.e.Fields.Schema(rs.tx, rs.ws)
	if err != nil {
		return false, err.Error()
	}
	cols, err := fields.ColumnMap(rs.tx, rs.ws.ID)
	if err != nil {
		return false, err.Error()
	}
	vars, err := rs.e.Fields.Values(rs.tx, rs.ws, schema, cols, card)
	if err != nil {
		return false, err.Error()
	}
	match, err := prog.Match(vars)
	if err != nil {
		return false, "condition failed: " + err.Error()
	}
	return match, ""
}

// act runs one action. changed is false when it was a no-op, such as moving a card to the column it
// is in. followups are the events the change causes.
func (rs *runState) act(ctx context.Context, rule domain.Rule, card domain.Card, a Action) (f Firing, followups []Event, changed bool, err error) {
	switch a.Type {
	case ActMove:
		return rs.move(rule, card, a)
	case ActSetField:
		res, err := SetField(ctx, rs.tx, rs.e.Fields, rs.ws, card, a.Path, a.Value, rs.e.DryRun)
		if err != nil {
			return f, nil, false, err
		}
		if res.Old == res.New {
			return f, nil, false, nil
		}
		f = Firing{From: fmt.Sprint(res.Old), To: fmt.Sprint(res.New), Text: fmt.Sprintf("Set %s to %v", a.Path, res.New)}
		followups = []Event{{Trigger: OnFieldChanged, CardID: card.ID, Field: a.Path, Old: res.Old, New: res.New}}
		if res.Moved {
			followups = append(followups, Event{Trigger: OnColumnEntered, CardID: card.ID, ColumnID: res.ToID})
			f.FromID, f.ToID = res.FromID, res.ToID
		}
		if err := rs.change(rule, card, map[string]any{a.Path: res.Old}, map[string]any{a.Path: res.New}); err != nil {
			return f, nil, false, err
		}
		return f, followups, true, nil
	case ActNotify, ActTellPlanner:
		text := rs.render(card, a.Text)
		if strings.TrimSpace(text) == "" {
			return f, nil, false, fmt.Errorf("%s needs text", a.Type)
		}
		title := a.Title
		if title == "" {
			title = rule.Name
		}
		rs.out.Effects = append(rs.out.Effects, Effect{Kind: a.Type, WorkspaceID: card.WorkspaceID, CardID: card.ID, Rule: rule.Name, Title: title, Text: text})
		verb := "Notified"
		if a.Type == ActTellPlanner {
			verb = "Told the planner"
		}
		f = Firing{Text: fmt.Sprintf("%s: %s", verb, text), To: text}
		return f, nil, true, nil
	case ActRunAction:
		label := a.Script + "." + a.Action
		if rs.e.DryRun {
			return Firing{Skipped: true, Reason: "dry run: " + label + " would run, and a dry run runs no script"}, nil, false, nil
		}
		if rs.e.Runner == nil {
			return Firing{Skipped: true, Reason: "no scripts are loaded, so " + label + " did not run"}, nil, false, nil
		}
		res, err := rs.e.Runner.RunAction(ctx, rs.tx, rs.ws, card, a.Script, a.Action)
		if err != nil {
			// A script that fails (a server is down, a permission is missing) must not undo the
			// move that fired next to it; the log shows why it did not run.
			return Firing{Skipped: true, Reason: label + " failed: " + err.Error()}, nil, false, nil
		}
		text := res.Text
		if text == "" {
			text = "Ran " + label
		}
		f = Firing{Text: text, To: label}
		return f, res.Events, true, nil
	}
	return f, nil, false, fmt.Errorf("unknown action type %q", a.Type)
}

func (rs *runState) move(rule domain.Rule, card domain.Card, a Action) (Firing, []Event, bool, error) {
	var f Firing
	cols, err := rs.tx.Columns.List(card.WorkspaceID)
	if err != nil {
		return f, nil, false, err
	}
	var target *domain.Column
	text := ""
	switch {
	case a.To == ReturnColumn:
		// Where the card sits now must be where the opening rule put it. A card the user dragged
		// somewhere else on purpose stays there.
		role := domain.RoleNeedsYou
		for _, r := range rs.rules {
			if r.Builtin && r.Enabled {
				if t, _ := ParseTrigger(r.Trigger); t.On == OnQuestionOpened {
					if list, _ := ParseActions(r.Actions); len(list) > 0 && list[0].Type == ActMove && list[0].Role != "" {
						role = list[0].Role
					}
				}
			}
		}
		if card.ReturnColumnID == "" {
			return f, nil, false, nil
		}
		here, err := rs.tx.Columns.Get(card.ColumnID)
		if err != nil || here.Role != role {
			return f, nil, false, rs.forgetReturn(card)
		}
		back, err := rs.tx.Columns.Get(card.ReturnColumnID)
		if err != nil || back.WorkspaceID != card.WorkspaceID {
			return f, nil, false, rs.forgetReturn(card) // the column is gone
		}
		if err := rs.forgetReturn(card); err != nil {
			return f, nil, false, err
		}
		target = &back
		text = fmt.Sprintf("Returned to %s: nothing is waiting on you", back.Name)
	case a.ColumnID != "":
		for i := range cols {
			if cols[i].ID == a.ColumnID {
				target = &cols[i]
			}
		}
		if target == nil {
			return f, nil, false, fmt.Errorf("column %q is gone. Edit the rule to pick another", a.ColumnID)
		}
		text = "Moved to " + target.Name
	default:
		role := a.Role
		if role == "" {
			role = domain.RoleNeedsYou
		}
		for i := range cols {
			if cols[i].ID == card.ColumnID && cols[i].Role == role {
				return f, nil, false, nil // already there
			}
			if target == nil && cols[i].Role == role {
				target = &cols[i]
			}
		}
		if target == nil {
			return f, nil, false, nil // the workspace has no such column; the card stays put
		}
		text = "Moved to " + target.Name
		if role == domain.RoleNeedsYou {
			text += ": it needs you"
		}
	}
	if target.ID == card.ColumnID {
		return f, nil, false, nil
	}
	from := card.ColumnID
	var fromName string
	for _, c := range cols {
		if c.ID == from {
			fromName = c.Name
		}
	}
	if a.RememberReturn {
		fresh, err := rs.tx.Cards.Get(card.ID)
		if err != nil {
			return f, nil, false, err
		}
		fresh.ReturnColumnID = from
		if err := rs.tx.Cards.Update(&fresh); err != nil {
			return f, nil, false, err
		}
	}
	if _, err := MoveCard(rs.tx, card.ID, target.ID, 0); err != nil {
		return f, nil, false, err
	}
	if rs.out.MovedTo == nil {
		rs.out.MovedTo = target
	}
	if err := rs.change(rule, card, map[string]any{"column_id": from}, map[string]any{"column_id": target.ID}); err != nil {
		return f, nil, false, err
	}
	f = Firing{From: fromName, To: target.Name, FromID: from, ToID: target.ID, Text: text}
	return f, ColumnEvents(card.ID, target.ID, fromName, target.Name), true, nil
}

// ColumnEvents are the events a card entering a column causes: column_entered, and field_changed
// for card.column.
func ColumnEvents(cardID, columnID, fromName, toName string) []Event {
	return []Event{
		{Trigger: OnColumnEntered, CardID: cardID, ColumnID: columnID},
		{Trigger: OnFieldChanged, CardID: cardID, Field: "card.column", Old: fromName, New: toName},
	}
}

func (rs *runState) forgetReturn(card domain.Card) error {
	fresh, err := rs.tx.Cards.Get(card.ID)
	if err != nil {
		return err
	}
	fresh.ReturnColumnID = ""
	return rs.tx.Cards.Update(&fresh)
}

// change writes the history row of one action that changed something.
func (rs *runState) change(rule domain.Rule, card domain.Card, before, after any) error {
	return rs.write(rule, card, ChangeFire, before, after, "")
}

func (rs *runState) record(rule domain.Rule, card domain.Card, f Firing) error {
	if f.Action == ActNotify || f.Action == ActTellPlanner {
		return rs.write(rule, card, ChangeFire, nil, map[string]any{"action": f.Action, "text": f.To}, "")
	}
	return nil
}

func (rs *runState) skip(rule domain.Rule, card domain.Card, reason string) error {
	key := rule.ID + "/" + card.ID + "/" + reason
	if rs.skipped[key] {
		return nil
	}
	rs.skipped[key] = true
	rs.out.Firings = append(rs.out.Firings, Firing{RuleID: rule.ID, Rule: rule.Name, CardID: card.ID, Card: card.Nick, Skipped: true, Reason: reason})
	return rs.write(rule, card, ChangeSkip, nil, nil, reason)
}

func (rs *runState) write(rule domain.Rule, card domain.Card, action string, before, after any, reason string) error {
	args := map[string]any{"card_id": card.ID, "nick": card.Nick, "rule_id": rule.ID}
	if reason != "" {
		args["reason"] = reason
	}
	ch := domain.Change{WorkspaceID: card.WorkspaceID, Actor: "rule:" + rule.Name, Action: action, Args: raw(args), Before: raw(before), After: raw(after)}
	return rs.tx.Changes.Create(&ch)
}

var tmplRe = regexp.MustCompile(`\{\{\s*([a-z_]+\.[a-z0-9_]+)\s*\}\}`)

// render fills {{card.nick}} and {{<field path>}} in a rule's message.
func (rs *runState) render(card domain.Card, text string) string {
	schema, err := rs.e.Fields.Schema(rs.tx, rs.ws)
	if err != nil {
		return text
	}
	cols, err := fields.ColumnMap(rs.tx, rs.ws.ID)
	if err != nil {
		return text
	}
	vars, err := rs.e.Fields.Values(rs.tx, rs.ws, schema, cols, card)
	if err != nil {
		return text
	}
	flat := fields.Flat(vars)
	return tmplRe.ReplaceAllStringFunc(text, func(m string) string {
		path := tmplRe.FindStringSubmatch(m)[1]
		if v, ok := flat[path]; ok {
			return fmt.Sprint(v)
		}
		return m
	})
}
