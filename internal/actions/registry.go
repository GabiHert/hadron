// Package actions is the action registry: the one source of every user-visible operation. The
// frontend calls actions through App.Invoke, and the MCP server registers each as a tool, so the UI
// can do nothing that MCP can't.
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

	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/integrations"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/GabiHert/hadron/internal/update"
)

// EventChanged is the Wails event the frontend refetches on.
const EventChanged = "changed"

// ChangedEvent is the payload of EventChanged.
type ChangedEvent struct {
	WorkspaceID string   `json:"workspace_id"`
	Entities    []string `json:"entities"`
}

// Actor says who is calling. The UI is "you", MCP maps its session header to a planner or card
// session, and rules run as "rule:<name>".
type Actor struct {
	Kind        string // you, planner, session, rule, external
	Name        string // card nick for a session, rule name for a rule
	WorkspaceID string // the workspace a planner or session belongs to; the default for actions
	CardID      string // the card a session belongs to
}

// Actor kinds.
const (
	KindYou      = "you"
	KindPlanner  = "planner"
	KindSession  = "session"
	KindRule     = "rule"
	KindExternal = "external"
)

// You is the UI's actor.
var You = Actor{Kind: KindYou}

// String is the form stored in changes and mcp_calls: you, planner, session:<nick>, rule:<name>, external.
func (a Actor) String() string {
	if a.Name != "" && (a.Kind == KindSession || a.Kind == KindRule) {
		return a.Kind + ":" + a.Name
	}
	if a.Kind == "" {
		return KindExternal
	}
	return a.Kind
}

// Deps is what actions need from the app. Tests fill in only what they use.
type Deps struct {
	Store *store.Store
	// Emit sends an event to the frontend. It may be nil.
	Emit func(event string, payload any)
	// Version is the running app version.
	Version string
	// Update is the self-updater. It may be nil, as in tests.
	Update *update.Service
	// Home is the data folder, shown by app_status.
	Home string
	// Sessions starts and drives Claude Code sessions. It may be nil, as in most tests; session
	// actions then fail with a clear error and cards do not start sessions.
	Sessions *sessions.Service
	// MCP reports the MCP server's address, token and tool count. It may be nil, as in tests.
	MCP func() MCPInfoOut
	// Chat types tagged lines into planners. It may be nil, as in tests: a planner is then never reachable.
	Chat *chat.Service
	// Fields is the field schema registry; integrations register their providers on it. It may be
	// nil, as in most tests: only the native and custom fields exist then.
	Fields *fields.Registry
	// Integrations runs integrations and scripts. It may be nil, as in most tests: integration
	// actions then fail with a clear error and run_action rules are skipped.
	Integrations *integrations.Manager
}

// Spec describes an action.
type Spec struct {
	Name        string // snake_case; also the MCP tool name
	Description string
	Mutates     bool
	// Entities are what a successful call changed, sent with the changed event so the frontend
	// refetches them. Only for mutating actions.
	Entities []string
	// Sensitive hides the arguments from mcp_calls and the config history, for actions that take a
	// secret such as a token.
	Sensitive bool
}

// Call is what a handler receives besides its input.
type Call struct {
	Actor Actor
	// Store runs inside a transaction for mutating actions, so a failed action changes nothing.
	// Handlers must use it, never Deps.Store: the store holds one connection.
	Store *store.Store
	Deps  Deps

	workspaceID string
	extra       []string
	changes     []pendingChange
	sensitive   bool
	afterCommit []func()
}

// AfterCommit runs fn once the call's transaction has committed, and never if it rolls back. Use it
// for side effects that must not outlive a failed call, such as starting a process.
func (c *Call) AfterCommit(fn func()) { c.afterCommit = append(c.afterCommit, fn) }

type pendingChange struct {
	workspaceID   string
	before, after any
}

// SetWorkspace records the workspace a call touched, for the changed event and the logs.
func (c *Call) SetWorkspace(id string) {
	if c.workspaceID == "" {
		c.workspaceID = id
	}
}

// Touch adds entities to the changed event beyond the Spec's.
func (c *Call) Touch(entities ...string) { c.extra = append(c.extra, entities...) }

// RecordChange writes a config-history row (before and after) when the call succeeds. Pass nil
// for before on a create and nil for after on a delete.
func (c *Call) RecordChange(workspaceID string, before, after any) {
	c.changes = append(c.changes, pendingChange{workspaceID: workspaceID, before: before, after: after})
	c.SetWorkspace(workspaceID)
}

// Workspace resolves the workspace an action targets: the explicit id, else the actor's own
// workspace, else the active one.
func (c *Call) Workspace(explicit string) (domain.Workspace, error) {
	id := explicit
	if id == "" {
		id = c.Actor.WorkspaceID
	}
	if id == "" {
		if v, err := c.Store.Settings.Get(SettingActiveWorkspace); err == nil {
			_ = json.Unmarshal(v, &id)
		}
	}
	if id == "" {
		return domain.Workspace{}, errors.New("workspace_id is required: no workspace is active yet. Pass workspace_id, or create a workspace first")
	}
	w, err := c.Store.Workspaces.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return w, fmt.Errorf("workspace %q does not exist. Call workspace_list for the ids", id)
	}
	if err == nil {
		c.SetWorkspace(w.ID)
	}
	return w, err
}

// SettingActiveWorkspace is the settings key that holds the active workspace id.
const SettingActiveWorkspace = "active_workspace"

// Handler is an action's implementation.
type Handler[In, Out any] func(ctx context.Context, c *Call, in In) (Out, error)

// Info describes a registered action to callers that build on the registry (MCP, tests).
type Info struct {
	Spec
	In     reflect.Type
	Out    reflect.Type
	Fields []Field
}

// Field is one input field of an action.
type Field struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type entry struct {
	info Info
	run  func(ctx context.Context, c *Call, raw json.RawMessage) (any, error)
}

// Registry holds every action.
type Registry struct {
	deps    Deps
	actions map[string]*entry
	clock   ruleClock
	dclock  dashClock
}

// NewRegistry returns an empty registry.
func NewRegistry(deps Deps) *Registry { return &Registry{deps: deps, actions: map[string]*entry{}} }

// Deps returns the registry's dependencies.
func (r *Registry) Deps() Deps { return r.deps }

// Register adds an action. It panics on a duplicate or malformed spec, which is a programming error.
func Register[In, Out any](r *Registry, spec Spec, fn Handler[In, Out]) {
	if spec.Name == "" || spec.Description == "" {
		panic("actions: an action needs a name and a description")
	}
	if _, dup := r.actions[spec.Name]; dup {
		panic("actions: duplicate action " + spec.Name)
	}
	inT, outT := reflect.TypeFor[In](), reflect.TypeFor[Out]()
	if inT.Kind() != reflect.Struct {
		panic("actions: " + spec.Name + ": In must be a struct")
	}
	fields := fieldsOf(inT)
	r.actions[spec.Name] = &entry{
		info: Info{Spec: spec, In: inT, Out: outT, Fields: fields},
		run: func(ctx context.Context, c *Call, raw json.RawMessage) (any, error) {
			in, err := decodeInput[In](spec.Name, fields, raw)
			if err != nil {
				return nil, err
			}
			return fn(ctx, c, in)
		},
	}
}

// Len is the number of registered actions.
func (r *Registry) Len() int { return len(r.actions) }

// Names lists every action name, sorted.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.actions))
	for n := range r.actions {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Infos describes every action, sorted by name.
func (r *Registry) Infos() []Info {
	out := make([]Info, 0, len(r.actions))
	for _, n := range r.Names() {
		out = append(out, r.actions[n].info)
	}
	return out
}

// Info describes one action.
func (r *Registry) Info(name string) (Info, bool) {
	e, ok := r.actions[name]
	if !ok {
		return Info{}, false
	}
	return e.info, true
}

// UnknownActionError is returned for a name that is not registered.
type UnknownActionError struct {
	Name        string
	Suggestions []string
}

func (e *UnknownActionError) Error() string {
	msg := fmt.Sprintf("unknown action %q.", e.Name)
	if len(e.Suggestions) > 0 {
		msg += " Did you mean: " + strings.Join(e.Suggestions, ", ") + "?"
	}
	return msg + " Call app_status to see how many actions exist, or check the action list in the README."
}

// InputError is returned when the arguments do not fit the action's input.
type InputError struct {
	Action   string
	Problems []string
}

func (e *InputError) Error() string {
	return fmt.Sprintf("invalid input for %s: %s", e.Action, strings.Join(e.Problems, "; "))
}

// Invoke runs an action for an actor. argsJSON is a JSON object (empty means {}). The result is
// the action's output as JSON.
func (r *Registry) Invoke(ctx context.Context, actor Actor, name, argsJSON string) (string, error) {
	e, ok := r.actions[name]
	if !ok {
		return "", &UnknownActionError{Name: name, Suggestions: r.suggest(name)}
	}
	raw := json.RawMessage(strings.TrimSpace(argsJSON))
	started := time.Now()
	c := &Call{Actor: actor, Deps: r.deps, Store: r.deps.Store, sensitive: e.info.Sensitive}

	var out any
	var runErr error
	if e.info.Mutates {
		runErr = r.deps.Store.Tx(func(tx *store.Store) error {
			c.Store = tx
			var err error
			if out, err = e.run(ctx, c, raw); err != nil {
				return err
			}
			return c.flushChanges(name, actor, raw)
		})
	} else {
		out, runErr = e.run(ctx, c, raw)
	}

	// Every mutating call is logged. A read is logged only when it did not come from the UI, which
	// refetches constantly: the log is about what Claude and other clients did.
	logged := e.info.Mutates || actor.Kind != KindYou
	// Only calls from others tell the log view to refetch; the UI's own calls already emit changed.
	announce := actor.Kind != KindYou
	if logged {
		r.logCall(c, name, raw, runErr, time.Since(started))
	}
	if runErr != nil {
		if announce {
			r.emit(c.workspaceID, EntityMCPCalls)
		}
		return "", runErr
	}
	for _, fn := range c.afterCommit {
		fn()
	}
	switch {
	case e.info.Mutates:
		entities := append(append([]string{}, e.info.Entities...), c.extra...)
		if announce {
			entities = append(entities, EntityMCPCalls)
		}
		r.emit(c.workspaceID, dedupe(entities)...)
	case announce:
		r.emit(c.workspaceID, EntityMCPCalls)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return "", fmt.Errorf("%s: could not encode the result: %w", name, err)
	}
	return string(b), nil
}

// EntityMCPCalls is the changed-event entity of the tool-call log.
const EntityMCPCalls = "mcp_calls"

func (r *Registry) emit(workspaceID string, entities ...string) {
	if r.deps.Emit != nil {
		r.deps.Emit(EventChanged, ChangedEvent{WorkspaceID: workspaceID, Entities: entities})
	}
}

// loggedArgs is what the logs keep of the arguments.
func (c *Call) loggedArgs(args json.RawMessage) json.RawMessage {
	if c.sensitive {
		return json.RawMessage(`"[redacted]"`)
	}
	return compactJSON(args)
}

func (c *Call) flushChanges(action string, actor Actor, args json.RawMessage) error {
	for _, ch := range c.changes {
		row := domain.Change{WorkspaceID: ch.workspaceID, Actor: actor.String(), Action: action, Args: c.loggedArgs(args)}
		row.Before, row.After = marshalOrNull(ch.before), marshalOrNull(ch.after)
		if err := c.Store.Changes.Create(&row); err != nil {
			return fmt.Errorf("could not write the config history: %w", err)
		}
	}
	return nil
}

// logCall writes mcp_calls outside the action's transaction so a failed call is logged too.
func (r *Registry) logCall(c *Call, name string, args json.RawMessage, err error, d time.Duration) {
	row := domain.MCPCall{WorkspaceID: c.workspaceID, Actor: c.Actor.String(), Tool: name, Args: c.loggedArgs(args), OK: err == nil, DurationMS: int(d.Milliseconds())}
	if err != nil {
		row.Error = err.Error()
	}
	_ = r.deps.Store.MCPCalls.Create(&row)
}

func (r *Registry) suggest(name string) []string {
	var out []string
	for _, n := range r.Names() {
		if strings.HasPrefix(n, name) || strings.HasPrefix(name, n) || strings.Contains(n, name) || editDistance(n, name) <= 3 {
			out = append(out, n)
		}
		if len(out) == 4 {
			break
		}
	}
	return out
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func compactJSON(raw json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage("{}")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return json.RawMessage("null")
	}
	return buf.Bytes()
}

func marshalOrNull(v any) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// ---- input decoding and validation ----

// Validator is implemented by inputs that check more than required fields.
type Validator interface{ Validate() error }

func fieldsOf(t reflect.Type) []Field {
	var out []Field
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		out = append(out, Field{
			Name:        name,
			Required:    !strings.Contains(opts, "omitempty"),
			Type:        jsonType(f.Type),
			Description: f.Tag.Get("jsonschema"),
		})
	}
	return out
}

func jsonType(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
		return "number"
	case reflect.Slice, reflect.Array:
		return "array"
	default:
		return "object"
	}
}

func decodeInput[In any](action string, fields []Field, raw json.RawMessage) (In, error) {
	var in In
	body := bytes.TrimSpace(raw)
	if len(body) == 0 || string(body) == "null" {
		body = []byte("{}")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return in, &InputError{Action: action, Problems: []string{"the arguments must be a JSON object, such as {\"name\": \"value\"}"}}
	}
	var problems []string
	known := map[string]Field{}
	names := make([]string, 0, len(fields))
	for _, f := range fields {
		known[f.Name] = f
		names = append(names, f.Name)
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, ok := known[k]; !ok {
			problems = append(problems, fmt.Sprintf("unknown field %q (valid fields: %s)", k, strings.Join(names, ", ")))
		}
	}
	for _, f := range fields {
		if !f.Required {
			continue
		}
		v, present := obj[f.Name]
		if !present || string(bytes.TrimSpace(v)) == "null" || (f.Type == "string" && string(bytes.TrimSpace(v)) == `""`) {
			problems = append(problems, fmt.Sprintf("%s is required", f.Name))
		}
	}
	if len(problems) > 0 {
		return in, &InputError{Action: action, Problems: problems}
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&in); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) {
			return in, &InputError{Action: action, Problems: []string{fmt.Sprintf("%s must be a %s, got a %s", te.Field, jsonTypeOfGo(te.Type), te.Value)}}
		}
		return in, &InputError{Action: action, Problems: []string{err.Error()}}
	}
	if v, ok := any(&in).(Validator); ok {
		if err := v.Validate(); err != nil {
			return in, &InputError{Action: action, Problems: []string{err.Error()}}
		}
	}
	return in, nil
}

func jsonTypeOfGo(t reflect.Type) string { return jsonType(t) }
