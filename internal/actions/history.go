package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// Kinds of config entity a change can be about.
const (
	kindWorkspace   = "workspace"
	kindColumn      = "column"
	kindColumns     = "column order"
	kindRule        = "rule"
	kindRules       = "rule order"
	kindView        = "view"
	kindBlock       = "dashboard block"
	kindBlocks      = "dashboard"
	kindProfile     = "launch profile"
	kindScript      = "script"
	kindPrompt      = "prompt"
	kindCustomField = "custom field"
	kindOther       = ""
)

// HistoryIn is the input of config_history.
type HistoryIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Limit       int    `json:"limit,omitempty" jsonschema:"how many of the newest changes (default 50, at most 500)"`
	Actor       string `json:"actor,omitempty" jsonschema:"only changes by this actor: you, planner, external, session, rule, or an exact one such as session:billing-fix or rule:Done moves"`
}

// HistoryEntry is one config change as the history lists it.
type HistoryEntry struct {
	ID         string    `json:"id"`
	At         time.Time `json:"at"`
	Actor      string    `json:"actor"`
	ActorKind  string    `json:"actor_kind"`
	Action     string    `json:"action"`
	Kind       string    `json:"kind"`
	Summary    string    `json:"summary"`
	Reverted   bool      `json:"reverted"`
	Revertible bool      `json:"revertible"`
	Reason     string    `json:"reason,omitempty" jsonschema:"why it cannot be reverted, when revertible is false"`
}

// ChangeIDIn names one change.
type ChangeIDIn struct {
	ChangeID string `json:"change_id" jsonschema:"the id from config_history"`
}

// DiffEntry is one field (or item) that differs between a change's before and after.
type DiffEntry struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

// DiffOut is the result of config_diff.
type DiffOut struct {
	Change HistoryEntry `json:"change"`
	Diff   []DiffEntry  `json:"diff"`
}

// RevertOut is the result of config_revert.
type RevertOut struct {
	Reverted HistoryEntry `json:"reverted"`
	Summary  string       `json:"summary"`
}

func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || string(t) == "null"
}

func decodeAny(raw json.RawMessage) any {
	var v any
	if isNull(raw) || json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return v
}

// kindOf says which entity a change is about, from the shape of its before and after.
func kindOf(ch domain.Change) string {
	if ch.Action == board.ChangeFire || ch.Action == board.ChangeSkip {
		return kindOther
	}
	for _, raw := range []json.RawMessage{ch.Before, ch.After} {
		switch v := decodeAny(raw).(type) {
		case map[string]any:
			if k := kindOfMap(v); k != kindOther {
				return k
			}
		case []any:
			if len(v) == 0 {
				continue
			}
			switch e := v[0].(type) {
			case string:
				return kindColumns
			case map[string]any:
				switch kindOfMap(e) {
				case kindBlock:
					return kindBlocks
				case kindRule:
					return kindRules
				}
			}
		}
	}
	// An empty before or after list: the dashboard is the only one that can be emptied.
	if strings.HasPrefix(ch.Action, "dash_") {
		return kindBlocks
	}
	return kindOther
}

func has(m map[string]any, keys ...string) bool {
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}

func kindOfMap(m map[string]any) string {
	switch {
	case has(m, "manifest", "source"):
		return kindScript
	case has(m, "first_prompt"):
		return kindProfile
	case has(m, "trigger", "actions"):
		return kindRule
	case has(m, "root", "filter_bar"):
		return kindWorkspace
	case has(m, "role", "wip"):
		return kindColumn
	case has(m, "span", "type", "title"):
		return kindBlock
	case has(m, "filters", "name"):
		return kindView
	case has(m, "kind", "custom", "template"):
		return kindPrompt
	case has(m, "path", "type", "label"):
		return kindCustomField
	}
	return kindOther
}

func actorKind(actor string) string {
	k, _, _ := strings.Cut(actor, ":")
	return k
}

func nameOf(m map[string]any) string {
	for _, k := range []string{"name", "title", "path", "kind"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

var noisy = map[string]bool{"updated_at": true, "created_at": true}

// diffMaps lists the top-level keys that differ between two objects.
func diffMaps(before, after map[string]any) []DiffEntry {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		if !noisy[k] {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	var out []DiffEntry
	for _, k := range names {
		if !reflect.DeepEqual(before[k], after[k]) {
			out = append(out, DiffEntry{Field: k, Before: before[k], After: after[k]})
		}
	}
	return out
}

func diffOf(ch domain.Change) []DiffEntry {
	b, a := decodeAny(ch.Before), decodeAny(ch.After)
	bm, bok := b.(map[string]any)
	am, aok := a.(map[string]any)
	switch {
	case bok && aok:
		return diffMaps(bm, am)
	case bok && b != nil && a == nil:
		return []DiffEntry{{Field: "(removed)", Before: nameOf(bm)}}
	case aok && b == nil:
		return []DiffEntry{{Field: "(created)", After: nameOf(am)}}
	}
	bl, _ := b.([]any)
	al, _ := a.([]any)
	if bl != nil || al != nil {
		return []DiffEntry{{Field: "items", Before: describeList(bl), After: describeList(al)}}
	}
	return nil
}

func describeList(l []any) any {
	out := make([]any, len(l))
	for i, e := range l {
		if m, ok := e.(map[string]any); ok {
			out[i] = nameOf(m)
		} else {
			out[i] = e
		}
	}
	return out
}

func summaryOf(ch domain.Change, kind string) string {
	b, a := decodeAny(ch.Before), decodeAny(ch.After)
	bm, bok := b.(map[string]any)
	am, aok := a.(map[string]any)
	short := func(v any) string {
		s := fmt.Sprint(v)
		if bs, err := json.Marshal(v); err == nil && len(bs) > 0 && bs[0] != '"' {
			s = string(bs)
		}
		if len(s) > 40 {
			s = s[:40] + "…"
		}
		return s
	}
	label := kind
	if label == kindOther {
		label = ch.Action
	}
	switch {
	case bok && aok:
		d := diffMaps(bm, am)
		if len(d) == 0 {
			return fmt.Sprintf("%s %s: no visible change", label, nameOf(am))
		}
		parts := make([]string, 0, 3)
		for i, e := range d {
			if i == 3 {
				parts = append(parts, fmt.Sprintf("+%d more", len(d)-3))
				break
			}
			parts = append(parts, fmt.Sprintf("%s %s → %s", e.Field, short(e.Before), short(e.After)))
		}
		return fmt.Sprintf("%s %s: %s", label, nameOf(am), strings.Join(parts, "; "))
	case bok && b != nil && (a == nil || (aok && am["deleted"] == true)):
		return fmt.Sprintf("deleted %s %s", label, nameOf(bm))
	case aok && b == nil:
		return fmt.Sprintf("created %s %s", label, nameOf(am))
	}
	bl, _ := b.([]any)
	al, _ := a.([]any)
	if bl != nil || al != nil {
		return fmt.Sprintf("%s: %d → %d items", label, len(bl), len(al))
	}
	return label
}

func entryOf(ch domain.Change) HistoryEntry {
	kind := kindOf(ch)
	e := HistoryEntry{ID: ch.ID, At: ch.CreatedAt, Actor: ch.Actor, ActorKind: actorKind(ch.Actor), Action: ch.Action, Kind: kind, Summary: summaryOf(ch, kind), Reverted: ch.Reverted}
	switch {
	case kind == kindOther:
		e.Reason = "this is not a config change, so there is nothing to revert"
	case ch.Reverted:
		e.Reason = "it was already reverted"
	default:
		e.Revertible = true
	}
	return e
}

func registerHistory(r *Registry) {
	Register(r, Spec{Name: "config_history", Description: "List the newest config changes of a workspace, newest first: who made each (you, planner, external, session:<nick>, rule:<name>), what, a one-line summary, and whether it can still be reverted. Covers workspaces, columns, rules, views, the dashboard, prompts, launch profiles and scripts. Pass actor to filter."},
		func(ctx context.Context, c *Call, in HistoryIn) ([]HistoryEntry, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			limit := in.Limit
			if limit <= 0 {
				limit = 50
			}
			if limit > 500 {
				return nil, fmt.Errorf("limit must be between 1 and 500, got %d", limit)
			}
			rows, err := c.Store.Changes.List(w.ID, 2000)
			if err != nil {
				return nil, err
			}
			out := []HistoryEntry{}
			for _, ch := range rows {
				if ch.Action == board.ChangeFire || ch.Action == board.ChangeSkip {
					continue // rule_log shows those
				}
				if in.Actor != "" && ch.Actor != in.Actor && actorKind(ch.Actor) != in.Actor {
					continue
				}
				out = append(out, entryOf(ch))
				if len(out) == limit {
					break
				}
			}
			return out, nil
		})

	Register(r, Spec{Name: "config_diff", Description: "Show what one config change changed: each field with its value before and after."},
		func(ctx context.Context, c *Call, in ChangeIDIn) (DiffOut, error) {
			ch, err := c.Store.Changes.Get(in.ChangeID)
			if err != nil {
				return DiffOut{}, fmt.Errorf("no change %q. config_history lists the ids", in.ChangeID)
			}
			d := diffOf(ch)
			if d == nil {
				d = []DiffEntry{}
			}
			return DiffOut{Change: entryOf(ch), Diff: d}, nil
		})

	Register(r, Spec{Name: "config_revert", Description: "Undo one config change by putting its before-state back. The revert is itself a change in the history. Refused, with the reason, when the thing has changed since (so a later edit is never silently overwritten), when it is not a config change, or when it was already reverted. Revert the newest change first to unwind several.",
		Mutates: true, Entities: []string{"workspaces", "columns", "rules", "views", "dash", "prompts", "launch_profiles", EntityScripts, "fields", "cards"}},
		func(ctx context.Context, c *Call, in ChangeIDIn) (RevertOut, error) {
			if c.Actor.Kind == KindSession {
				return RevertOut{}, errors.New("a card session cannot revert config changes")
			}
			ch, err := c.Store.Changes.Get(in.ChangeID)
			if err != nil {
				return RevertOut{}, fmt.Errorf("no change %q. config_history lists the ids", in.ChangeID)
			}
			c.SetWorkspace(ch.WorkspaceID)
			if c.Actor.WorkspaceID != "" && ch.WorkspaceID != "" && c.Actor.WorkspaceID != ch.WorkspaceID {
				return RevertOut{}, errors.New("that change belongs to another workspace")
			}
			entry := entryOf(ch)
			if !entry.Revertible {
				return RevertOut{}, errors.New(entry.Reason)
			}
			summary, err := revertChange(c, ch, entry.Kind)
			if err != nil {
				return RevertOut{}, fmt.Errorf("cannot revert %q: %w", entry.Summary, err)
			}
			if err := c.Store.Changes.MarkReverted(ch.ID); err != nil {
				return RevertOut{}, err
			}
			entry.Reverted, entry.Revertible, entry.Reason = true, false, "it was already reverted"
			return RevertOut{Reverted: entry, Summary: summary}, nil
		})
}

// ---- reverting ----

// same says whether the current state of a thing equals the state a change recorded, leaving out
// timestamps and the fields other actions move without recording (positions, panel state).
func same(current any, recorded json.RawMessage, ignore ...string) bool {
	cb, err := json.Marshal(current)
	if err != nil {
		return false
	}
	var a, b any
	if json.Unmarshal(cb, &a) != nil || json.Unmarshal(recorded, &b) != nil {
		return false
	}
	return reflect.DeepEqual(strip(a, ignore), strip(b, ignore))
}

func strip(v any, ignore []string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if noisy[k] || contains(ignore, k) {
				continue
			}
			out[k] = strip(e, ignore)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = strip(e, ignore)
		}
		return out
	}
	return v
}

// crud reverts changes to one kind of entity stored as a row.
type crud[T any] struct {
	kind   string
	ignore []string
	get    func(*Call, string) (T, error)
	create func(*Call, *T) error // nil: the kind cannot be recreated
	update func(*Call, *T) error
	del    func(*Call, T) error // nil: the kind cannot be deleted by a revert
	id     func(T) string
	keep   func(dst *T, cur T) // carry over what other actions move without recording
}

func (k crud[T]) revert(c *Call, ch domain.Change) (string, error) {
	var before, after *T
	if !isNull(ch.Before) {
		before = new(T)
		if err := json.Unmarshal(ch.Before, before); err != nil {
			return "", fmt.Errorf("the recorded before-state is unreadable: %w", err)
		}
	}
	afterDeleted := false
	if !isNull(ch.After) {
		var probe map[string]any
		if json.Unmarshal(ch.After, &probe) == nil && probe["deleted"] == true {
			afterDeleted = true
		} else {
			after = new(T)
			if err := json.Unmarshal(ch.After, after); err != nil {
				return "", fmt.Errorf("the recorded after-state is unreadable: %w", err)
			}
		}
	}
	switch {
	case before != nil && after != nil:
		cur, err := k.get(c, k.id(*after))
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("the %s was deleted since", k.kind)
		}
		if err != nil {
			return "", err
		}
		if !same(cur, ch.After, k.ignore...) {
			return "", fmt.Errorf("the %s has changed since this change, so reverting would overwrite a later edit. Revert the later changes first", k.kind)
		}
		restored := *before
		if k.keep != nil {
			k.keep(&restored, cur)
		}
		if err := k.update(c, &restored); err != nil {
			return "", err
		}
		c.RecordChange(c.workspaceID, cur, restored)
		return fmt.Sprintf("restored the %s to its earlier state", k.kind), nil
	case before == nil && after != nil:
		cur, err := k.get(c, k.id(*after))
		if errors.Is(err, store.ErrNotFound) {
			return "", fmt.Errorf("the %s is already gone", k.kind)
		}
		if err != nil {
			return "", err
		}
		if k.del == nil {
			return "", fmt.Errorf("undoing the creation of a %s would delete it, which Hadron does not do for you. Delete it yourself", k.kind)
		}
		if !same(cur, ch.After, k.ignore...) {
			return "", fmt.Errorf("the %s has changed since it was created, so deleting it would lose a later edit", k.kind)
		}
		if err := k.del(c, cur); err != nil {
			return "", err
		}
		c.RecordChange(c.workspaceID, cur, nil)
		return fmt.Sprintf("removed the %s that change created", k.kind), nil
	case before != nil && (after == nil || afterDeleted):
		if k.create == nil || afterDeleted {
			return "", fmt.Errorf("a deleted %s cannot be brought back by a revert here (its cards or links moved with it)", k.kind)
		}
		if _, err := k.get(c, k.id(*before)); err == nil {
			return "", fmt.Errorf("a %s with that id exists again", k.kind)
		}
		restored := *before
		if err := k.create(c, &restored); err != nil {
			return "", err
		}
		c.RecordChange(c.workspaceID, nil, restored)
		return fmt.Sprintf("brought the %s back", k.kind), nil
	}
	return "", errors.New("the change recorded nothing to revert")
}

func revertChange(c *Call, ch domain.Change, kind string) (string, error) {
	if ch.WorkspaceID != "" {
		c.SetWorkspace(ch.WorkspaceID)
	}
	switch kind {
	case kindWorkspace:
		return crud[domain.Workspace]{kind: kind, ignore: []string{"ui", "position"},
			get:    func(c *Call, id string) (domain.Workspace, error) { return c.Store.Workspaces.Get(id) },
			update: func(c *Call, w *domain.Workspace) error { return c.Store.Workspaces.Update(w) },
			id:     func(w domain.Workspace) string { return w.ID },
			keep:   func(dst *domain.Workspace, cur domain.Workspace) { dst.UI, dst.Position = cur.UI, cur.Position },
		}.revert(c, ch)
	case kindColumn:
		return crud[domain.Column]{kind: kind, ignore: []string{"position"},
			get:    func(c *Call, id string) (domain.Column, error) { return c.Store.Columns.Get(id) },
			update: func(c *Call, x *domain.Column) error { return c.Store.Columns.Update(x) },
			id:     func(x domain.Column) string { return x.ID },
			keep:   func(dst *domain.Column, cur domain.Column) { dst.Position = cur.Position },
		}.revert(c, ch)
	case kindRule:
		return crud[domain.Rule]{kind: kind, ignore: []string{"position"},
			get:    func(c *Call, id string) (domain.Rule, error) { return c.Store.Rules.Get(id) },
			create: func(c *Call, x *domain.Rule) error { return c.Store.Rules.Create(x) },
			update: func(c *Call, x *domain.Rule) error { return c.Store.Rules.Update(x) },
			del: func(c *Call, x domain.Rule) error {
				if x.Builtin {
					return errors.New("a built-in rule cannot be deleted")
				}
				return c.Store.Rules.Delete(x.ID)
			},
			id:   func(x domain.Rule) string { return x.ID },
			keep: func(dst *domain.Rule, cur domain.Rule) { dst.Position = cur.Position },
		}.revert(c, ch)
	case kindView:
		return crud[domain.View]{kind: kind, ignore: []string{"position"},
			get:    func(c *Call, id string) (domain.View, error) { return c.Store.Views.Get(id) },
			create: func(c *Call, x *domain.View) error { return c.Store.Views.Create(x) },
			update: func(c *Call, x *domain.View) error { return c.Store.Views.Update(x) },
			del:    func(c *Call, x domain.View) error { return c.Store.Views.Delete(x.ID) },
			id:     func(x domain.View) string { return x.ID },
			keep:   func(dst *domain.View, cur domain.View) { dst.Position = cur.Position },
		}.revert(c, ch)
	case kindProfile:
		return crud[domain.LaunchProfile]{kind: kind,
			get:    func(c *Call, id string) (domain.LaunchProfile, error) { return c.Store.LaunchProfiles.Get(id) },
			create: func(c *Call, x *domain.LaunchProfile) error { return c.Store.LaunchProfiles.Create(x) },
			update: func(c *Call, x *domain.LaunchProfile) error { return c.Store.LaunchProfiles.Update(x) },
			del:    func(c *Call, x domain.LaunchProfile) error { return c.Store.LaunchProfiles.Delete(x.ID) },
			id:     func(x domain.LaunchProfile) string { return x.ID },
		}.revert(c, ch)
	case kindBlock:
		return crud[domain.Block]{kind: kind, ignore: []string{"position"},
			get:    func(c *Call, id string) (domain.Block, error) { return c.Store.Blocks.Get(id) },
			create: func(c *Call, x *domain.Block) error { return c.Store.Blocks.Create(x) },
			update: func(c *Call, x *domain.Block) error { return c.Store.Blocks.Update(x) },
			del:    func(c *Call, x domain.Block) error { return c.Store.Blocks.Delete(x.ID) },
			id:     func(x domain.Block) string { return x.ID },
			keep:   func(dst *domain.Block, cur domain.Block) { dst.Position = cur.Position },
		}.revert(c, ch)
	case kindScript:
		s, err := crud[domain.Script]{kind: kind,
			get:    func(c *Call, id string) (domain.Script, error) { return c.Store.Scripts.Get(id) },
			create: func(c *Call, x *domain.Script) error { return c.Store.Scripts.Create(x) },
			update: func(c *Call, x *domain.Script) error { return c.Store.Scripts.Update(x) },
			del: func(c *Call, x domain.Script) error {
				_ = c.Store.ScriptLogs.DeleteByName(x.Name)
				return c.Store.Scripts.Delete(x.ID)
			},
			id: func(x domain.Script) string { return x.ID },
		}.revert(c, ch)
		if err == nil {
			syncAfter(c)
		}
		return s, err
	case kindPrompt:
		return revertPrompt(c, ch)
	case kindColumns:
		return revertColumnOrder(c, ch)
	case kindBlocks:
		return revertBlocks(c, ch)
	case kindRules:
		return "", errors.New("the order of rules was recorded without its result, so it cannot be checked against later edits. Drag the rules back in Settings, Rules")
	case kindCustomField:
		return "", errors.New("custom field changes cannot be reverted yet. Edit the field in Settings")
	}
	return "", errors.New("this kind of change cannot be reverted")
}

func revertPrompt(c *Call, ch domain.Change) (string, error) {
	type rec struct {
		Kind     string `json:"kind"`
		Custom   bool   `json:"custom"`
		Template string `json:"template"`
	}
	var before, after rec
	if json.Unmarshal(ch.Before, &before) != nil || json.Unmarshal(ch.After, &after) != nil {
		return "", errors.New("the recorded prompt state is unreadable")
	}
	cur, err := promptOf(c, ch.WorkspaceID, after.Kind)
	if err != nil {
		return "", err
	}
	if cur.Custom != after.Custom || (cur.Custom && cur.Template != after.Template) {
		return "", fmt.Errorf("the %s prompt has changed since, so reverting would overwrite a later edit", after.Kind)
	}
	var tpl *string
	if before.Custom {
		tpl = &before.Template
	}
	if err := c.Store.Prompts.Set(ch.WorkspaceID, after.Kind, tpl); err != nil {
		return "", err
	}
	c.RecordChange(ch.WorkspaceID, rec{after.Kind, cur.Custom, cur.Template}, before)
	return fmt.Sprintf("restored the %s prompt", after.Kind), nil
}

func revertColumnOrder(c *Call, ch domain.Change) (string, error) {
	var before, after []string
	if json.Unmarshal(ch.Before, &before) != nil || json.Unmarshal(ch.After, &after) != nil {
		return "", errors.New("the recorded column order is unreadable")
	}
	cols, err := c.Store.Columns.List(ch.WorkspaceID)
	if err != nil {
		return "", err
	}
	now := columnOrder(cols)
	if !reflect.DeepEqual(now, after) {
		return "", errors.New("the columns have been reordered, added or removed since, so reverting would undo a later edit")
	}
	if err := c.Store.Columns.Reorder(ch.WorkspaceID, before); err != nil {
		return "", err
	}
	c.RecordChange(ch.WorkspaceID, now, before)
	return "restored the column order", nil
}

func revertBlocks(c *Call, ch domain.Change) (string, error) {
	var before, after []domain.Block
	if (!isNull(ch.Before) && json.Unmarshal(ch.Before, &before) != nil) || (!isNull(ch.After) && json.Unmarshal(ch.After, &after) != nil) {
		return "", errors.New("the recorded dashboard is unreadable")
	}
	cur, err := c.Store.Blocks.List(ch.WorkspaceID)
	if err != nil {
		return "", err
	}
	if after == nil {
		after = []domain.Block{}
	}
	if !same(cur, mustJSON(after)) {
		return "", errors.New("the dashboard has changed since, so reverting would overwrite a later edit")
	}
	for _, b := range cur {
		if err := c.Store.Blocks.Delete(b.ID); err != nil {
			return "", err
		}
	}
	restored := make([]domain.Block, 0, len(before))
	for _, b := range before {
		b := b
		if err := c.Store.Blocks.Create(&b); err != nil {
			return "", err
		}
		restored = append(restored, b)
	}
	c.RecordChange(ch.WorkspaceID, cur, restored)
	return fmt.Sprintf("restored the dashboard (%d blocks)", len(restored)), nil
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
