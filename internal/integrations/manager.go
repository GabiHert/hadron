// Package integrations connects the script sandbox to Hadron: it turns enabled integrations into
// field providers, runs their ops against the board, keeps the linked-item snapshots fresh and
// runs script actions for rules.
package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/dlog"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/script"
	"github.com/GabiHert/hadron/internal/store"
)

// Status values of a script.
const (
	StatusPending  = "pending"
	StatusEnabled  = "enabled"
	StatusDisabled = "disabled"
	StatusRejected = "rejected"
)

// Kinds of script.
const (
	KindIntegration = "integration"
	KindScript      = "script"
)

// Manager runs integrations and scripts. Create it with New.
type Manager struct {
	// Base is the store outside any transaction, for polling and Sync.
	Base    *store.Store
	Fields  *fields.Registry
	Secrets script.Secrets
	Backend script.Backend
	// Timeout and MemoryLimit override the sandbox defaults; tests shorten the timeout.
	Timeout     time.Duration
	MemoryLimit uint64
	// Notify shows a notification, for host.notify. May be nil.
	Notify func(workspaceID, title, body string)
	// FieldChanged runs the rules that watch a field, for changes a poll finds. May be nil.
	FieldChanged func(ctx context.Context, cardID, path string, old, now any) error
	// Log is Hadron's own log. May be nil.
	Log *dlog.Logger
	// Changed tells the frontend what to refetch. May be nil.
	Changed func(workspaceID string, entities ...string)

	// QueryTTL overrides DefaultQueryTTL; Now overrides the clock. Tests use both.
	QueryTTL time.Duration
	Now      func() time.Time

	qc       queryCache
	mu       sync.RWMutex
	enabled  map[string]script.Manifest // "<workspace id or ''>/<name>" -> manifest of an enabled integration
	lastPoll map[string]time.Time
	depth    atomic.Int32
}

// New returns a manager. Call Sync once the store is open.
func New(base *store.Store, reg *fields.Registry, secrets script.Secrets, backend script.Backend) *Manager {
	return &Manager{Base: base, Fields: reg, Secrets: secrets, Backend: backend, enabled: map[string]script.Manifest{}, lastPoll: map[string]time.Time{}}
}

func scopeKey(workspaceID, name string) string { return workspaceID + "/" + name }

// Sync registers a field provider for every enabled integration and drops the others. Call it
// after any change to a script's status or manifest.
func (m *Manager) Sync() error {
	all, err := m.Base.Scripts.ListAll()
	if err != nil {
		return err
	}
	enabled := map[string]script.Manifest{}
	names := map[string]bool{}
	for _, sc := range all {
		if sc.Kind != KindIntegration || sc.Status != StatusEnabled {
			continue
		}
		man, problems := script.ParseManifest(sc.Manifest)
		if len(problems) > 0 {
			log.Printf("integration %s: %v", sc.Name, problems)
			continue
		}
		enabled[scopeKey(sc.WorkspaceID, sc.Name)] = man
		names[sc.Name] = true
	}
	m.mu.Lock()
	previous := m.enabled
	m.enabled = enabled
	m.mu.Unlock()
	m.qc.mu.Lock() // a changed script may answer differently: forget what it answered before
	m.qc.entries = nil
	m.qc.mu.Unlock()
	for k := range previous {
		_, name, _ := strings.Cut(k, "/")
		if !names[name] {
			m.Fields.Unregister(name)
		}
	}
	for name := range names {
		if err := m.Fields.Register(&provider{m: m, ns: name}); err != nil {
			log.Printf("integration %s: %v", name, err)
		}
	}
	return nil
}

func (m *Manager) manifestFor(workspaceID, name string) (script.Manifest, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if man, ok := m.enabled[scopeKey(workspaceID, name)]; ok {
		return man, true
	}
	man, ok := m.enabled[scopeKey("", name)]
	return man, ok
}

// Find returns the script a workspace sees under a name: its own, else the global one.
func Find(st *store.Store, workspaceID, name string) (domain.Script, error) {
	list, err := st.Scripts.List(workspaceID)
	if err != nil {
		return domain.Script{}, err
	}
	var global *domain.Script
	for i, sc := range list {
		if sc.Name != name {
			continue
		}
		if sc.WorkspaceID == workspaceID {
			return sc, nil
		}
		global = &list[i]
	}
	if global != nil {
		return *global, nil
	}
	return domain.Script{}, fmt.Errorf("no integration or script named %q in this workspace. integration_list shows them: %w", name, store.ErrNotFound)
}

// ---- config ----

// ConfigKey is the settings key of an integration's config values.
func ConfigKey(name string) string { return "integration_config:" + name }

// Config returns an integration's config values.
func Config(st *store.Store, name string) map[string]any {
	out := map[string]any{}
	if raw, err := st.Settings.Get(ConfigKey(name)); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// ---- running ----

// Run is one op's outcome.
type Run struct {
	Result script.Result
	// Events are the field and column changes the code made, for rules to react to.
	Events []board.Event
}

// Op runs one op of a script inside st. It records a script_logs row, and keeps secrets out of it.
func (m *Manager) Op(ctx context.Context, st *store.Store, ws domain.Workspace, sc domain.Script, op string, input any) (Run, error) {
	man, problems := script.ParseManifest(sc.Manifest)
	if len(problems) > 0 {
		return Run{}, fmt.Errorf("%s has a manifest problem: %s", sc.Name, strings.Join(problems, "; "))
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return Run{}, err
	}
	if m.depth.Add(1) > 3 {
		m.depth.Add(-1)
		return Run{}, errors.New("integrations calling integrations are nested too deep")
	}
	defer m.depth.Add(-1)
	var events []board.Event
	runner := &script.Runner{Backend: m.Backend, Secrets: m.Secrets, Timeout: m.Timeout, MemoryLimit: m.MemoryLimit}
	res, runErr := runner.Run(ctx, script.Call{
		Name: sc.Name, Manifest: man, Source: sc.Source, Op: op, Input: raw,
		Config: Config(st, sc.Name), App: m.app(ctx, st, ws, sc, &events),
	})
	msg := ""
	if runErr != nil {
		msg = runErr.Error()
	}
	if len(res.Logs) > 0 {
		msg = strings.TrimSpace(msg + "\n" + strings.Join(res.Logs, "\n"))
	}
	m.Log.Debug("script run", "script", sc.Name, "op", op, "ms", res.Duration, "ok", runErr == nil, "error", errString(runErr))
	row := domain.ScriptLog{WorkspaceID: ws.ID, ScriptName: sc.Name, Op: op, OK: runErr == nil, DurationMS: int(res.Duration.Milliseconds()), Message: msg}
	if lerr := st.ScriptLogs.Create(&row); lerr != nil {
		log.Printf("script log: %v", lerr)
	}
	if runErr != nil {
		return Run{Result: res}, runErr
	}
	return Run{Result: res, Events: events}, nil
}

// RunAction implements board.ActionRunner: it runs the action of an enabled script named script,
// with the card and its links as input.
func (m *Manager) RunAction(ctx context.Context, tx *store.Store, ws domain.Workspace, card domain.Card, name, action string) (board.ActionResult, error) {
	sc, err := Find(tx, ws.ID, name)
	if err != nil {
		return board.ActionResult{}, err
	}
	if sc.Status != StatusEnabled {
		return board.ActionResult{}, fmt.Errorf("%s is %s, not enabled. Approve it in Settings, Integrations", name, sc.Status)
	}
	man, _ := script.ParseManifest(sc.Manifest)
	op := "actions." + action
	if !man.HasOp(op) {
		return board.ActionResult{}, fmt.Errorf("%s has no action %q. It has: %s", name, action, strings.Join(man.OpNames(), ", "))
	}
	links, _ := tx.Links.ListByCard(card.ID)
	view, err := m.cardView(tx, ws, card)
	if err != nil {
		return board.ActionResult{}, err
	}
	out, err := m.Op(ctx, tx, ws, sc, op, map[string]any{"card": view, "links": links})
	if err != nil {
		return board.ActionResult{}, err
	}
	text := ""
	var o struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(out.Result.Output, &o) == nil {
		text = o.Text
	}
	return board.ActionResult{Text: text, Events: out.Events}, nil
}

// ---- host app API ----

func (m *Manager) cardView(st *store.Store, ws domain.Workspace, card domain.Card) (map[string]any, error) {
	schema, err := m.Fields.Schema(st, ws)
	if err != nil {
		return nil, err
	}
	cols, err := fields.ColumnMap(st, ws.ID)
	if err != nil {
		return nil, err
	}
	vars, err := m.Fields.Values(st, ws, schema, cols, card)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": card.ID, "nick": card.Nick, "key": card.Key, "title": card.Title, "assignee": card.Assignee,
		"stage": card.Stage, "column": cols[card.ColumnID].Name, "fields": fields.Flat(vars),
	}, nil
}

func resolveCard(st *store.Store, ws domain.Workspace, ref string) (domain.Card, error) {
	if c, err := st.Cards.Get(ref); err == nil && c.WorkspaceID == ws.ID {
		return c, nil
	}
	c, err := st.Cards.ByNick(ws.ID, ref)
	if err != nil {
		return c, fmt.Errorf("no card %q in workspace %q", ref, ws.Name)
	}
	return c, nil
}

func (m *Manager) app(ctx context.Context, st *store.Store, ws domain.Workspace, sc domain.Script, events *[]board.Event) script.App {
	str := func(in map[string]any, k string) string { s, _ := in[k].(string); return s }
	return script.App{
		LinkUpsert: func(in map[string]any) (any, error) {
			card, err := resolveCard(st, ws, str(in, "card"))
			if err != nil {
				return nil, err
			}
			integ := str(in, "integration")
			if integ == "" {
				integ = sc.Name
			}
			if integ != sc.Name {
				return nil, fmt.Errorf("%s can only link items of its own integration, not %q", sc.Name, integ)
			}
			fl, _ := in["fields"].(map[string]any)
			l, err := m.upsertLink(st, card, integ, str(in, "item_id"), str(in, "title"), str(in, "url"), fl)
			return map[string]any{"link_id": l.ID}, err
		},
		LinkList: func(in map[string]any) (any, error) {
			var links []domain.Link
			if ref := str(in, "card"); ref != "" {
				card, err := resolveCard(st, ws, ref)
				if err != nil {
					return nil, err
				}
				links, err = st.Links.ListByCard(card.ID)
				if err != nil {
					return nil, err
				}
			} else {
				var err error
				if links, err = st.Links.ListByWorkspace(ws.ID); err != nil {
					return nil, err
				}
			}
			integ := str(in, "integration")
			if integ == "" {
				integ = sc.Name
			}
			out := []domain.Link{}
			for _, l := range links {
				if l.Integration == integ {
					out = append(out, l)
				}
			}
			return out, nil
		},
		FieldSet: func(cardRef, path string, value any) error {
			card, err := resolveCard(st, ws, cardRef)
			if err != nil {
				return err
			}
			if ns, _ := fields.SplitPath(path); ns == sc.Name {
				return fmt.Errorf("%s cannot set its own field %s. Return the fields from get or update instead, and Hadron keeps them in sync", sc.Name, path)
			}
			res, err := board.SetField(ctx, st, m.Fields, ws, card, path, value, false)
			if err != nil {
				return err
			}
			if res.Old != res.New {
				*events = append(*events, board.Event{Trigger: board.OnFieldChanged, CardID: card.ID, Field: path, Old: res.Old, New: res.New})
				if res.Moved {
					*events = append(*events, board.Event{Trigger: board.OnColumnEntered, CardID: card.ID, ColumnID: res.ToID})
				}
			}
			return nil
		},
		CardRead: func(ref string) (any, error) {
			card, err := resolveCard(st, ws, ref)
			if err != nil {
				return nil, err
			}
			return m.cardView(st, ws, card)
		},
		CardMove: func(ref, column string) error {
			card, err := resolveCard(st, ws, ref)
			if err != nil {
				return err
			}
			res, err := board.SetField(ctx, st, m.Fields, ws, card, "card.column", column, false)
			if err != nil {
				return err
			}
			if res.Moved {
				*events = append(*events,
					board.Event{Trigger: board.OnFieldChanged, CardID: card.ID, Field: "card.column", Old: res.Old, New: res.New},
					board.Event{Trigger: board.OnColumnEntered, CardID: card.ID, ColumnID: res.ToID})
			}
			return nil
		},
		CardList: func(in map[string]any) (any, error) {
			cards, err := st.Cards.List(ws.ID, store.CardFilter{})
			if err != nil {
				return nil, err
			}
			if expr := str(in, "expr"); expr != "" {
				ids, err := m.Fields.Filter(st, ws, expr, cards)
				if err != nil {
					return nil, err
				}
				keep := map[string]bool{}
				for _, id := range ids {
					keep[id] = true
				}
				var kept []domain.Card
				for _, c := range cards {
					if keep[c.ID] {
						kept = append(kept, c)
					}
				}
				cards = kept
			}
			out := []map[string]any{}
			for _, c := range cards {
				v, err := m.cardView(st, ws, c)
				if err != nil {
					return nil, err
				}
				if col := str(in, "column"); col != "" && !strings.EqualFold(fmt.Sprint(v["column"]), col) {
					continue
				}
				out = append(out, v)
			}
			return out, nil
		},
		SessionListRemote: func(in map[string]any) (any, error) {
			name := str(in, "integration")
			if name == "" {
				name = sc.Name
			}
			target, err := Find(st, ws.ID, name)
			if err != nil {
				return nil, err
			}
			run, err := m.Op(ctx, st, ws, target, "sessions.list", map[string]any{})
			if err != nil {
				return nil, err
			}
			var out any
			_ = json.Unmarshal(run.Result.Output, &out)
			return out, nil
		},
		Notify: func(title, body string) {
			if m.Notify != nil {
				m.Notify(ws.ID, title, body)
			}
		},
	}
}

// ---- links and snapshots ----

// upsertLink creates the link of (card, integration, item) or refreshes its snapshot.
func (m *Manager) upsertLink(st *store.Store, card domain.Card, integration, itemID, title, url string, fl map[string]any) (domain.Link, error) {
	links, err := st.Links.ListByCard(card.ID)
	if err != nil {
		return domain.Link{}, err
	}
	now := time.Now().UTC()
	for _, l := range links {
		if l.Integration == integration && l.ItemID == itemID {
			if title != "" {
				l.Title = title
			}
			if url != "" {
				l.URL = url
			}
			for k, v := range fl {
				l.Fields[k] = v
			}
			l.SyncedAt = &now
			return l, st.Links.Update(&l)
		}
	}
	l := domain.Link{WorkspaceID: card.WorkspaceID, CardID: card.ID, Integration: integration, ItemID: itemID, Title: title, URL: url, Fields: fl, SyncedAt: &now}
	if l.Title == "" {
		l.Title = itemID
	}
	return l, st.Links.Create(&l)
}

// FieldChange is one linked field whose value differs after a refresh.
type FieldChange struct {
	CardID   string
	Path     string
	Old, New any
}

// Apply merges a fresh item into a link's snapshot and returns the fields that changed.
func Apply(st *store.Store, l *domain.Link, title, url string, fl map[string]any) ([]FieldChange, error) {
	var changes []FieldChange
	for k, v := range fl {
		old := l.Fields[k]
		if !reflect.DeepEqual(old, v) {
			changes = append(changes, FieldChange{CardID: l.CardID, Path: l.Integration + "." + k, Old: old, New: v})
		}
		l.Fields[k] = v
	}
	if title != "" {
		l.Title = title
	}
	if url != "" {
		l.URL = url
	}
	now := time.Now().UTC()
	l.SyncedAt = &now
	return changes, st.Links.Update(l)
}

// Item is the shape of search, get and poll results.
type Item struct {
	ItemID string         `json:"item_id"`
	Title  string         `json:"title"`
	URL    string         `json:"url"`
	Fields map[string]any `json:"fields"`
}

// Get runs the get op for a linked item.
func (m *Manager) Get(ctx context.Context, st *store.Store, ws domain.Workspace, sc domain.Script, itemID string) (Item, error) {
	run, err := m.Op(ctx, st, ws, sc, "get", map[string]any{"item_id": itemID})
	if err != nil {
		return Item{}, err
	}
	var it Item
	if err := json.Unmarshal(run.Result.Output, &it); err != nil {
		return it, err
	}
	if it.Fields == nil {
		it.Fields = map[string]any{}
	}
	return it, nil
}

// ---- polling ----

// Poll refreshes the linked items of every enabled integration whose poll_seconds have passed.
// For each field that changed it runs the rules that watch the field.
func (m *Manager) Poll(ctx context.Context, now time.Time) error {
	all, err := m.Base.Scripts.ListAll()
	if err != nil {
		return err
	}
	var firstErr error
	for _, sc := range all {
		if sc.Kind != KindIntegration || sc.Status != StatusEnabled {
			continue
		}
		man, problems := script.ParseManifest(sc.Manifest)
		if len(problems) > 0 || man.PollSeconds <= 0 {
			continue
		}
		key := sc.ID
		m.mu.Lock()
		last := m.lastPoll[key]
		due := now.Sub(last) >= time.Duration(man.PollSeconds)*time.Second
		if due {
			m.lastPoll[key] = now
		}
		m.mu.Unlock()
		if !due {
			continue
		}
		if err := m.pollOne(ctx, sc, man); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("poll %s: %w", sc.Name, err)
		}
	}
	return firstErr
}

// pollOne refreshes one integration's links. The network work happens outside any transaction;
// the snapshots are written in a short one.
func (m *Manager) pollOne(ctx context.Context, sc domain.Script, man script.Manifest) error {
	links, err := m.Base.Links.ListByIntegration(sc.Name)
	if err != nil {
		return err
	}
	byWS := map[string][]domain.Link{}
	for _, l := range links {
		if sc.WorkspaceID != "" && l.WorkspaceID != sc.WorkspaceID {
			continue
		}
		byWS[l.WorkspaceID] = append(byWS[l.WorkspaceID], l)
	}
	for wsID, ls := range byWS {
		ws, err := m.Base.Workspaces.Get(wsID)
		if err != nil || !contains(ws.Integrations, sc.Name) {
			continue
		}
		fresh := map[string]Item{}
		if man.HasOp("poll") {
			items := make([]map[string]any, len(ls))
			for i, l := range ls {
				items[i] = map[string]any{"item_id": l.ItemID, "fields": l.Fields}
			}
			run, err := m.Op(ctx, m.Base, ws, sc, "poll", map[string]any{"items": items})
			if err != nil {
				return err
			}
			var out struct {
				Items []Item `json:"items"`
			}
			if err := json.Unmarshal(run.Result.Output, &out); err != nil {
				return err
			}
			for _, it := range out.Items {
				fresh[it.ItemID] = it
			}
		} else if man.HasOp("get") {
			for _, l := range ls {
				it, err := m.Get(ctx, m.Base, ws, sc, l.ItemID)
				if err != nil {
					return err
				}
				fresh[l.ItemID] = it
			}
		} else {
			continue
		}
		var changes []FieldChange
		err = m.Base.Tx(func(tx *store.Store) error {
			for _, l := range ls {
				it, ok := fresh[l.ItemID]
				if !ok {
					continue
				}
				cur, err := tx.Links.Get(l.ID)
				if err != nil {
					continue
				}
				c, err := Apply(tx, &cur, it.Title, it.URL, it.Fields)
				if err != nil {
					return err
				}
				changes = append(changes, c...)
			}
			return nil
		})
		if err != nil {
			return err
		}
		if len(changes) > 0 && m.Changed != nil {
			m.Changed(wsID, "links", "cards")
		}
		for _, c := range changes {
			if m.FieldChanged != nil {
				if err := m.FieldChanged(ctx, c.CardID, c.Path, c.Old, c.New); err != nil {
					log.Printf("poll %s: rules for %s: %v", sc.Name, c.Path, err)
				}
			}
		}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- field provider ----

type provider struct {
	m  *Manager
	ns string
}

func (p *provider) Namespace() string { return p.ns }

func (p *provider) Schema(ws domain.Workspace) []fields.Entry {
	man, ok := p.m.manifestFor(ws.ID, p.ns)
	if !ok {
		return nil
	}
	out := make([]fields.Entry, 0, len(man.Fields))
	for _, f := range man.Fields {
		label := f.Label
		if label == "" {
			label = f.Name
		}
		out = append(out, fields.Entry{Path: p.ns + "." + f.Name, Label: label, Type: f.Type, Options: f.Options, Filterable: f.Filterable, Editable: f.Editable, CardChip: f.CardChip, Source: p.ns})
	}
	return out
}

// Values returns the snapshot of the card's first link to the integration.
func (p *provider) Values(st *store.Store, card domain.Card) (map[string]any, error) {
	links, err := st.Links.ListByCard(card.ID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, l := range links {
		if l.Integration != p.ns {
			continue
		}
		for k, v := range l.Fields {
			if _, set := out[k]; !set {
				out[k] = v
			}
		}
	}
	return out, nil
}

// Set runs the integration's update op for the card's first link, then stores the new snapshot.
func (p *provider) Set(ctx context.Context, st *store.Store, card domain.Card, field string, value any) error {
	ws, err := st.Workspaces.Get(card.WorkspaceID)
	if err != nil {
		return err
	}
	sc, err := Find(st, ws.ID, p.ns)
	if err != nil {
		return err
	}
	if sc.Status != StatusEnabled {
		return fmt.Errorf("%s is %s. Approve it in Settings, Integrations first", p.ns, sc.Status)
	}
	man, _ := script.ParseManifest(sc.Manifest)
	if !man.HasOp("update") {
		return fmt.Errorf("%s cannot change %s.%s: it has no update op", p.ns, p.ns, field)
	}
	links, err := st.Links.ListByCard(card.ID)
	if err != nil {
		return err
	}
	var link *domain.Link
	for i := range links {
		if links[i].Integration == p.ns {
			link = &links[i]
			break
		}
	}
	if link == nil {
		return fmt.Errorf("card %q has no %s link, so %s.%s has nothing to change. Link an item first (the card's Links tab, or link_add)", card.Nick, p.ns, p.ns, field)
	}
	run, err := p.m.Op(ctx, st, ws, sc, "update", map[string]any{"item_id": link.ItemID, "field": field, "value": value, "fields": link.Fields})
	if err != nil {
		return fmt.Errorf("%s update failed: %w", p.ns, err)
	}
	var out struct {
		Fields map[string]any `json:"fields"`
		Title  string         `json:"title"`
		URL    string         `json:"url"`
	}
	_ = json.Unmarshal(run.Result.Output, &out)
	if out.Fields == nil {
		out.Fields = map[string]any{field: value}
	}
	_, err = Apply(st, link, out.Title, out.URL, out.Fields)
	return err
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
