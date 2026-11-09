package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/dlog"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/store"
)

// EventNotify is the Wails event a rule's notify action emits. The frontend shows it as a toast.
const EventNotify = "notify"

// NotifyEvent is the payload of EventNotify.
type NotifyEvent struct {
	WorkspaceID string `json:"workspace_id"`
	CardID      string `json:"card_id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
}

// RuleListIn is the input of rule_list and rule_log.
type RuleListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// RuleCreateIn is the input of rule_create.
type RuleCreateIn struct {
	WorkspaceID string         `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Name        string         `json:"name" jsonschema:"what the rule does, in a few words"`
	Trigger     board.Trigger  `json:"trigger" jsonschema:"when the rule runs"`
	Condition   string         `json:"condition,omitempty" jsonschema:"a CEL expression that must hold for the card, such as card.assignee == 'You'"`
	Actions     []board.Action `json:"actions" jsonschema:"what the rule does, in order"`
	Enabled     *bool          `json:"enabled,omitempty" jsonschema:"default true"`
}

// RuleUpdateIn is the input of rule_update. Only the fields you pass change. A built-in rule can
// only be switched on or off.
type RuleUpdateIn struct {
	RuleID    string          `json:"rule_id" jsonschema:"the rule id"`
	Name      *string         `json:"name,omitempty" jsonschema:"new name"`
	Trigger   *board.Trigger  `json:"trigger,omitempty" jsonschema:"new trigger"`
	Condition *string         `json:"condition,omitempty" jsonschema:"new CEL condition, or an empty string for none"`
	Actions   *[]board.Action `json:"actions,omitempty" jsonschema:"new actions"`
	Enabled   *bool           `json:"enabled,omitempty" jsonschema:"switch the rule on or off"`
}

// RuleReorderIn is the input of rule_reorder.
type RuleReorderIn struct {
	WorkspaceID string   `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	RuleIDs     []string `json:"rule_ids" jsonschema:"every rule id of the workspace, in the new order; rules run in this order"`
}

// RuleIDIn is the input of rule_delete.
type RuleIDIn struct {
	RuleID string `json:"rule_id" jsonschema:"the rule id"`
}

// RuleDryRunIn is the input of rule_dry_run.
type RuleDryRunIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	RuleID      string `json:"rule_id,omitempty" jsonschema:"only report what this rule would do"`
}

// RuleLogIn is the input of rule_log.
type RuleLogIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Limit       int    `json:"limit,omitempty" jsonschema:"how many entries, newest first (default 50)"`
}

// RuleLogEntry is one firing or skip in the rule log.
type RuleLogEntry struct {
	ID     string          `json:"id"`
	At     time.Time       `json:"at"`
	Rule   string          `json:"rule"`
	Kind   string          `json:"kind"` // fire or skip
	CardID string          `json:"card_id"`
	Card   string          `json:"card"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
	Reason string          `json:"reason,omitempty"`
}

// NotifyIn is the input of notify.
type NotifyIn struct {
	Title string `json:"title" jsonschema:"the notification title"`
	Body  string `json:"body,omitempty" jsonschema:"the notification text"`
}

func registerRules(r *Registry) {
	Register(r, Spec{Name: "rule_list", Description: "List a workspace's rules in the order they run. Each has a trigger, an optional CEL condition and actions. The two built-in rules move a card to Needs you when a question or gate opens, and back when it closes."},
		func(ctx context.Context, c *Call, in RuleListIn) ([]domain.Rule, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			return c.Store.Rules.List(w.ID)
		})

	Register(r, Spec{Name: "rule_create", Description: "Create a rule: when a trigger happens and the optional CEL condition holds, run the actions. Triggers: field_changed{field}, column_entered{column}, card_created, report{kind}, question_opened, question_answered, idle_for{minutes}, cron{spec}. Actions: move{column_id|role|to}, set_field{path,value}, notify{text}, tell_planner{text}, run_action{script,action}. One event lets rules run at most 10 actions per card.",
		Mutates: true, Entities: []string{"rules"}},
		func(ctx context.Context, c *Call, in RuleCreateIn) (domain.Rule, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return domain.Rule{}, err
			}
			name, err := checkName("rule", in.Name)
			if err != nil {
				return domain.Rule{}, err
			}
			t, acts, err := board.CheckRule(c.Store, fieldRegistry(c), w, in.Trigger, in.Condition, in.Actions)
			if err != nil {
				return domain.Rule{}, err
			}
			rule := domain.Rule{WorkspaceID: w.ID, Name: name, Enabled: in.Enabled == nil || *in.Enabled, Trigger: toRaw(t), Condition: strings.TrimSpace(in.Condition), Actions: toRaw(acts)}
			if err := c.Store.Rules.Create(&rule); err != nil {
				return rule, err
			}
			c.RecordChange(w.ID, nil, rule)
			return rule, nil
		})

	Register(r, Spec{Name: "rule_update", Description: "Change a rule. A built-in rule can only be switched on or off with enabled.",
		Mutates: true, Entities: []string{"rules"}},
		func(ctx context.Context, c *Call, in RuleUpdateIn) (domain.Rule, error) {
			rule, w, err := getRule(c, in.RuleID)
			if err != nil {
				return rule, err
			}
			before := rule
			if rule.Builtin && (in.Name != nil || in.Trigger != nil || in.Condition != nil || in.Actions != nil) {
				return rule, fmt.Errorf("%q is a built-in rule: it can be switched off with enabled: false, but not edited", rule.Name)
			}
			if !rule.Builtin {
				t, err := board.ParseTrigger(rule.Trigger)
				if err != nil {
					return rule, err
				}
				acts, err := board.ParseActions(rule.Actions)
				if err != nil {
					return rule, err
				}
				cond := rule.Condition
				if in.Name != nil {
					if rule.Name, err = checkName("rule", *in.Name); err != nil {
						return rule, err
					}
				}
				if in.Trigger != nil {
					t = *in.Trigger
				}
				if in.Condition != nil {
					cond = strings.TrimSpace(*in.Condition)
				}
				if in.Actions != nil {
					acts = *in.Actions
				}
				if t, acts, err = board.CheckRule(c.Store, fieldRegistry(c), w, t, cond, acts); err != nil {
					return rule, err
				}
				rule.Trigger, rule.Condition, rule.Actions = toRaw(t), cond, toRaw(acts)
			}
			if in.Enabled != nil {
				rule.Enabled = *in.Enabled
			}
			if err := c.Store.Rules.Update(&rule); err != nil {
				return rule, err
			}
			c.RecordChange(w.ID, before, rule)
			return rule, nil
		})

	Register(r, Spec{Name: "rule_reorder", Description: "Set the order rules run in. Pass every rule id of the workspace.",
		Mutates: true, Entities: []string{"rules"}},
		func(ctx context.Context, c *Call, in RuleReorderIn) ([]domain.Rule, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			rules, err := c.Store.Rules.List(w.ID)
			if err != nil {
				return nil, err
			}
			byID := map[string]domain.Rule{}
			for _, r := range rules {
				byID[r.ID] = r
			}
			seen := map[string]bool{}
			for _, id := range in.RuleIDs {
				if _, ok := byID[id]; !ok {
					return nil, fmt.Errorf("rule %q is not in workspace %q. rule_list shows the ids", id, w.Name)
				}
				if seen[id] {
					return nil, fmt.Errorf("rule %q appears twice", id)
				}
				seen[id] = true
			}
			if len(seen) != len(rules) {
				return nil, fmt.Errorf("pass all %d rule ids of the workspace, got %d", len(rules), len(seen))
			}
			c.RecordChange(w.ID, rules, nil)
			out := make([]domain.Rule, len(in.RuleIDs))
			for i, id := range in.RuleIDs {
				r := byID[id]
				r.Position = i
				if err := c.Store.Rules.Update(&r); err != nil {
					return nil, err
				}
				out[i] = r
			}
			c.changes[len(c.changes)-1].after = out
			return out, nil
		})

	Register(r, Spec{Name: "rule_delete", Description: "Delete a rule. The two built-in rules cannot be deleted; switch them off with rule_update.",
		Mutates: true, Entities: []string{"rules"}},
		func(ctx context.Context, c *Call, in RuleIDIn) (domain.Rule, error) {
			rule, w, err := getRule(c, in.RuleID)
			if err != nil {
				return rule, err
			}
			if rule.Builtin {
				return rule, fmt.Errorf("%q is a built-in rule and cannot be deleted. Switch it off with rule_update (enabled: false)", rule.Name)
			}
			if err := c.Store.Rules.Delete(rule.ID); err != nil {
				return rule, err
			}
			c.RecordChange(w.ID, rule, nil)
			return rule, nil
		})

	Register(r, Spec{Name: "rule_dry_run", Description: "Report what the rules would do to the board right now, without applying anything: a list of {card, rule, action, from, to}. It tries every rule that watches a field or a column against the cards as they are; rules that wait for an event, a report, a question, idle time or the clock are not simulated."},
		func(ctx context.Context, c *Call, in RuleDryRunIn) ([]board.Firing, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			return dryRun(ctx, c, w, in.RuleID)
		})

	Register(r, Spec{Name: "rule_log", Description: "List what rules did lately, newest first: each firing with the card, the before and after, and each skip with its reason (a loop guard stop, a failing condition, run_action)."},
		func(ctx context.Context, c *Call, in RuleLogIn) ([]RuleLogEntry, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			limit := in.Limit
			if limit <= 0 {
				limit = 50
			}
			rows, err := c.Store.Changes.ListByAction(w.ID, []string{board.ChangeFire, board.ChangeSkip}, limit)
			if err != nil {
				return nil, err
			}
			out := make([]RuleLogEntry, len(rows))
			for i, ch := range rows {
				var args struct {
					CardID string `json:"card_id"`
					Nick   string `json:"nick"`
					Reason string `json:"reason"`
				}
				_ = json.Unmarshal(ch.Args, &args)
				kind := "fire"
				if ch.Action == board.ChangeSkip {
					kind = "skip"
				}
				out[i] = RuleLogEntry{ID: ch.ID, At: ch.CreatedAt, Rule: strings.TrimPrefix(ch.Actor, "rule:"), Kind: kind, CardID: args.CardID, Card: args.Nick, Before: ch.Before, After: ch.After, Reason: args.Reason}
			}
			return out, nil
		})

	Register(r, Spec{Name: "notify", Description: "Show a notification in the app: a toast for the user."},
		func(ctx context.Context, c *Call, in NotifyIn) (NotifyIn, error) {
			if strings.TrimSpace(in.Title) == "" {
				return in, errors.New("title is required")
			}
			if c.Deps.Emit != nil {
				c.Deps.Emit(EventNotify, NotifyEvent{WorkspaceID: c.Actor.WorkspaceID, Title: in.Title, Body: in.Body})
			}
			return in, nil
		})
}

func toRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func getRule(c *Call, id string) (domain.Rule, domain.Workspace, error) {
	rule, err := c.Store.Rules.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return rule, domain.Workspace{}, fmt.Errorf("rule %q does not exist. rule_list shows the ids", id)
	}
	if err != nil {
		return rule, domain.Workspace{}, err
	}
	w, err := c.Workspace(rule.WorkspaceID)
	return rule, w, err
}

// errRollback ends a dry run's transaction without committing it.
var errRollback = errors.New("dry run: roll back")

// dryRun simulates the field and column rules against the cards as they are, in a transaction that
// is rolled back, so no card, message or history row survives. Effects are dropped.
func dryRun(ctx context.Context, c *Call, w domain.Workspace, ruleID string) ([]board.Firing, error) {
	var firings []board.Firing
	err := c.Deps.Store.Tx(func(tx *store.Store) error {
		rules, err := tx.Rules.List(w.ID)
		if err != nil {
			return err
		}
		cards, err := tx.Cards.List(w.ID, store.CardFilter{})
		if err != nil {
			return err
		}
		reg := fieldRegistry(c)
		schema, err := reg.Schema(tx, w)
		if err != nil {
			return err
		}
		cols, err := fields.ColumnMap(tx, w.ID)
		if err != nil {
			return err
		}
		var events []board.Event
		seen := map[string]bool{}
		add := func(key string, ev board.Event) {
			if !seen[key] {
				seen[key] = true
				events = append(events, ev)
			}
		}
		for _, rule := range rules {
			if !rule.Enabled || rule.Builtin {
				continue
			}
			t, err := board.ParseTrigger(rule.Trigger)
			if err != nil {
				continue
			}
			for _, card := range cards {
				switch t.On {
				case board.OnFieldChanged:
					if t.Field == "" {
						continue
					}
					vars, err := reg.Values(tx, w, schema, cols, card)
					if err != nil {
						return err
					}
					if v := fields.Flat(vars)[t.Field]; !isZero(v) {
						add("f/"+t.Field+"/"+card.ID, board.Event{Trigger: board.OnFieldChanged, CardID: card.ID, Field: t.Field, New: v})
					}
				case board.OnColumnEntered:
					if t.Column == "" || card.ColumnID == t.Column {
						add("c/"+card.ColumnID+"/"+card.ID, board.Event{Trigger: board.OnColumnEntered, CardID: card.ID, ColumnID: card.ColumnID})
					}
				}
			}
		}
		eng := board.Engine{Fields: reg, DryRun: true}
		for _, ev := range events {
			out, err := eng.Handle(ctx, tx, ev)
			if err != nil {
				return err
			}
			for _, f := range out.Firings {
				if ruleID == "" || f.RuleID == ruleID {
					firings = append(firings, f)
				}
			}
		}
		return errRollback
	})
	if err != nil && !errors.Is(err, errRollback) {
		return nil, err
	}
	if firings == nil {
		firings = []board.Firing{}
	}
	return firings, nil
}

func isZero(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == ""
	case float64:
		return x == 0
	case bool:
		return !x
	}
	return false
}

// runEvent runs the rules that match an event, inside the call's transaction, and performs their
// notify and tell_planner effects once the call commits.
func runEvent(c *Call, ev board.Event) error {
	eng := board.Engine{Fields: fieldRegistry(c), Runner: runnerOf(c.Deps)}
	out, err := eng.Handle(context.Background(), c.Store, ev)
	if err != nil {
		return err
	}
	if len(out.Firings) > 0 {
		c.Touch("cards", "messages")
		logFirings(c.Deps.Log, out.Firings)
	}
	dispatchEffects(c.Deps, c.Store, c.AfterCommit, out.Effects)
	return nil
}

func logFirings(l *dlog.Logger, firings []board.Firing) {
	if l == nil || !l.Debugging() {
		return
	}
	for _, f := range firings {
		l.Debug("rule", "rule", f.Rule, "card", f.Card, "action", f.Action, "skipped", f.Skipped, "reason", f.Reason)
	}
}

// dispatchEffects schedules a run's effects through after, which defers them past the commit.
func dispatchEffects(deps Deps, st *store.Store, after func(func()), effects []board.Effect) {
	for _, e := range effects {
		e := e
		switch e.Kind {
		case board.ActNotify:
			after(func() {
				if deps.Emit != nil {
					deps.Emit(EventNotify, NotifyEvent{WorkspaceID: e.WorkspaceID, CardID: e.CardID, Title: e.Title, Body: e.Text})
				}
			})
		case board.ActTellPlanner:
			card, err := st.Cards.Get(e.CardID)
			if err != nil || deps.Chat == nil {
				continue
			}
			line := chat.Tag(card) + " rule \"" + e.Rule + "\": " + e.Text
			after(func() {
				deps.Chat.Enqueue(e.WorkspaceID, line, func(why string) {
					msg := domain.Message{WorkspaceID: e.WorkspaceID, Channel: chat.CardChannel(e.CardID), Author: "rule", Kind: "event", Text: "Rule \"" + e.Rule + "\" could not tell the planner (" + why + "): " + e.Text, Delivered: true}
					if deps.Store.Messages.Create(&msg) == nil && deps.Emit != nil {
						deps.Emit(EventChanged, ChangedEvent{WorkspaceID: e.WorkspaceID, Entities: []string{EntityMessages}})
					}
				})
			})
		}
	}
}

// ---- idle_for and cron ----

// ruleClock remembers what the timer already did.
type ruleClock struct {
	mu       sync.Mutex
	last     time.Time
	idleDone map[string]time.Time // rule/card -> the card's updated_at when the rule last fired
}

// TickRules fires the idle_for and cron rules that are due at now. The app calls it every
// 30 seconds. A cron rule fires for each card (so its condition picks which), at most once per
// scheduled time; the first tick only starts the clock, so a restart never replays missed times.
func (r *Registry) TickRules(ctx context.Context, now time.Time) error {
	r.clock.mu.Lock()
	defer r.clock.mu.Unlock()
	last := r.clock.last
	r.clock.last = now
	if r.clock.idleDone == nil {
		r.clock.idleDone = map[string]time.Time{}
	}
	wss, err := r.deps.Store.Workspaces.List(false)
	if err != nil {
		return err
	}
	var firstErr error
	for _, w := range wss {
		rules, err := r.deps.Store.Rules.List(w.ID)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			if !rule.Enabled {
				continue
			}
			t, err := board.ParseTrigger(rule.Trigger)
			if err != nil {
				continue
			}
			var events []board.Event
			switch t.On {
			case board.OnCron:
				if last.IsZero() {
					continue
				}
				sched, err := board.ParseCron(t.Spec)
				if err != nil || sched.Next(last).After(now) {
					continue
				}
				cards, err := r.deps.Store.Cards.List(w.ID, store.CardFilter{})
				if err != nil {
					return err
				}
				for _, card := range cards {
					events = append(events, board.Event{Trigger: board.OnCron, CardID: card.ID, Spec: t.Spec})
				}
			case board.OnIdleFor:
				cards, err := r.deps.Store.Cards.List(w.ID, store.CardFilter{Liveness: domain.LivenessIdle})
				if err != nil {
					return err
				}
				for _, card := range cards {
					key := rule.ID + "/" + card.ID
					if now.Sub(card.UpdatedAt) >= time.Duration(t.Minutes)*time.Minute && !r.clock.idleDone[key].Equal(card.UpdatedAt) {
						events = append(events, board.Event{Trigger: board.OnIdleFor, CardID: card.ID, Minutes: t.Minutes})
						r.clock.idleDone[key] = card.UpdatedAt
					}
				}
			}
			for _, ev := range events {
				if err := r.runTimed(ctx, w.ID, rule.ID, ev); err != nil && firstErr == nil {
					firstErr = err
				}
			}
		}
	}
	return firstErr
}

// runTimed runs one timer event in its own transaction, as the rules do for any event.
func (r *Registry) runTimed(ctx context.Context, workspaceID, ruleID string, ev board.Event) error {
	var out board.Outcome
	err := r.deps.Store.Tx(func(tx *store.Store) error {
		var err error
		out, err = (&board.Engine{Fields: r.fieldsOrEmpty(), Runner: runnerOf(r.deps)}).Handle(ctx, tx, ev)
		return err
	})
	if err != nil {
		return err
	}
	logFirings(r.deps.Log, out.Firings)
	if ev.Trigger == board.OnIdleFor {
		// The rule's own change bumps the card's updated_at; remember the new one so it does not fire again.
		if card, err := r.deps.Store.Cards.Get(ev.CardID); err == nil {
			r.clock.idleDone[ruleID+"/"+card.ID] = card.UpdatedAt
		}
	}
	dispatchEffects(r.deps, r.deps.Store, func(fn func()) { fn() }, out.Effects)
	if len(out.Firings) > 0 {
		r.emit(workspaceID, "cards", "messages")
	}
	return nil
}

func (r *Registry) fieldsOrEmpty() *fields.Registry {
	if r.deps.Fields != nil {
		return r.deps.Fields
	}
	return noProviders
}

// FieldChanged runs the field_changed rules for a change that did not come through an action, such
// as an integration's poll finding that a linked item changed on the remote side. The caller says
// what the old and new values were. It runs in its own transaction and performs the rules' effects.
func (r *Registry) FieldChanged(ctx context.Context, cardID, path string, old, now any) error {
	card, err := r.deps.Store.Cards.Get(cardID)
	if err != nil {
		return err
	}
	return r.runTimed(ctx, card.WorkspaceID, "", board.Event{Trigger: board.OnFieldChanged, CardID: cardID, Field: path, Old: old, New: now})
}

// runnerOf is the run_action runner, or nil when the app has no integrations. A nil *Manager must
// not become a non-nil interface.
func runnerOf(d Deps) board.ActionRunner {
	if d.Integrations == nil {
		return nil
	}
	return d.Integrations
}
