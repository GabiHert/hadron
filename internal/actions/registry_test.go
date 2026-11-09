package actions

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

type emitted struct {
	event   string
	payload any
}

type recorder struct {
	mu  sync.Mutex
	got []emitted
}

func (r *recorder) emit(event string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, emitted{event, payload})
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

// testDeps returns deps on a temp store. Tests never touch ~/.hadron.
func testDeps(t *testing.T) (Deps, *recorder) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HADRON_HOME", home)
	st, err := store.Open(filepath.Join(home, "hadron.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	rec := &recorder{}
	return Deps{Store: st, Emit: rec.emit, Version: "test", Home: home}, rec
}

type renameIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"the workspace"`
	Name        string `json:"name" jsonschema:"the new name"`
	Notes       string `json:"notes,omitempty"`
	Count       int    `json:"count,omitempty"`
}

func (in renameIn) Validate() error {
	if strings.Contains(in.Name, "/") {
		return errors.New("name must not contain a slash")
	}
	return nil
}

type renameOut struct {
	Workspace domain.Workspace `json:"workspace"`
}

func testRegistry(t *testing.T) (*Registry, *recorder) {
	deps, rec := testDeps(t)
	r := NewRegistry(deps)
	Register(r, Spec{Name: "ws_make", Description: "Make a workspace.", Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in struct {
			Name string `json:"name"`
		}) (domain.Workspace, error) {
			w := domain.Workspace{Name: in.Name}
			if err := c.Store.Workspaces.Create(&w); err != nil {
				return w, err
			}
			c.RecordChange(w.ID, nil, w)
			return w, nil
		})
	Register(r, Spec{Name: "ws_rename", Description: "Rename a workspace.", Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in renameIn) (renameOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return renameOut{}, err
			}
			before := w
			w.Name = in.Name
			if err := c.Store.Workspaces.Update(&w); err != nil {
				return renameOut{}, err
			}
			c.RecordChange(w.ID, before, w)
			return renameOut{w}, nil
		})
	Register(r, Spec{Name: "ws_boom", Description: "Writes, then fails.", Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in struct{}) (struct{}, error) {
			if err := c.Store.Workspaces.Create(&domain.Workspace{Name: "ghost"}); err != nil {
				return struct{}{}, err
			}
			return struct{}{}, errors.New("boom: nothing was saved")
		})
	Register(r, Spec{Name: "ws_list", Description: "List workspaces."},
		func(ctx context.Context, c *Call, in struct{}) ([]domain.Workspace, error) {
			return c.Store.Workspaces.List(true)
		})
	return r, rec
}

func mustInvoke(t *testing.T, r *Registry, actor Actor, name, args string) string {
	t.Helper()
	out, err := r.Invoke(context.Background(), actor, name, args)
	if err != nil {
		t.Fatalf("%s(%s): %v", name, args, err)
	}
	return out
}

func TestUnknownActionReturnsAReadableErrorWithSuggestions(t *testing.T) {
	r, _ := testRegistry(t)
	_, err := r.Invoke(context.Background(), You, "ws_renam", "{}")
	var ue *UnknownActionError
	if !errors.As(err, &ue) {
		t.Fatalf("error = %T %v, want *UnknownActionError", err, err)
	}
	for _, want := range []string{`unknown action "ws_renam"`, "Did you mean", "ws_rename"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err.Error(), want)
		}
	}
	if _, err := r.Invoke(context.Background(), You, "zzzz_nothing_like_it", ""); err == nil || strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("a name with no near match should not suggest anything: %v", err)
	}
}

func TestInvalidInputIsReadable(t *testing.T) {
	r, rec := testRegistry(t)
	w := mustInvoke(t, r, You, "ws_make", `{"name":"a"}`)
	var made domain.Workspace
	_ = json.Unmarshal([]byte(w), &made)
	before := rec.count()

	cases := []struct {
		name, args string
		wants      []string
	}{
		{"missing required", `{"workspace_id":"` + made.ID + `"}`, []string{"invalid input for ws_rename", "name is required"}},
		{"empty required string", `{"workspace_id":"x","name":""}`, []string{"name is required"}},
		{"all problems at once", `{}`, []string{"workspace_id is required", "name is required"}},
		{"unknown field", `{"workspace_id":"x","name":"n","nmae":"typo"}`, []string{`unknown field "nmae"`, "valid fields: workspace_id, name, notes, count"}},
		{"wrong type", `{"workspace_id":"x","name":"n","count":"three"}`, []string{"count must be a number", "got a string"}},
		{"not an object", `[1,2]`, []string{"must be a JSON object"}},
		{"not json", `{oops`, []string{"must be a JSON object"}},
		{"validate method", `{"workspace_id":"x","name":"a/b"}`, []string{"name must not contain a slash"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.Invoke(context.Background(), You, "ws_rename", tc.args)
			var ie *InputError
			if !errors.As(err, &ie) {
				t.Fatalf("error = %T %v, want *InputError", err, err)
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err.Error(), want)
				}
			}
		})
	}
	if rec.count() != before {
		t.Error("a rejected input must not emit a changed event")
	}
}

func TestConfigActionsWriteBeforeAndAfter(t *testing.T) {
	r, _ := testRegistry(t)
	var w domain.Workspace
	_ = json.Unmarshal([]byte(mustInvoke(t, r, You, "ws_make", `{"name":"old"}`)), &w)
	mustInvoke(t, r, Actor{Kind: KindPlanner}, "ws_rename", `{"workspace_id":"`+w.ID+`","name":"new"}`)

	changes, err := r.Deps().Store.Changes.List(w.ID, 10)
	if err != nil || len(changes) != 2 {
		t.Fatalf("changes = %d, %v; want 2", len(changes), err)
	}
	rename, create := changes[0], changes[1]
	if rename.Action != "ws_rename" || rename.Actor != "planner" {
		t.Fatalf("rename change = %s by %s", rename.Action, rename.Actor)
	}
	var before, after domain.Workspace
	_ = json.Unmarshal(rename.Before, &before)
	_ = json.Unmarshal(rename.After, &after)
	if before.Name != "old" || after.Name != "new" || before.ID != w.ID {
		t.Fatalf("before=%q after=%q, want old -> new", before.Name, after.Name)
	}
	if !strings.Contains(string(rename.Args), `"name":"new"`) {
		t.Errorf("args = %s", rename.Args)
	}
	if string(create.Before) != "null" || !strings.Contains(string(create.After), `"old"`) || create.Actor != "you" {
		t.Fatalf("create change before=%s after=%s actor=%s; want null -> old by you", create.Before, create.After, create.Actor)
	}
}

func TestFailedMutationRollsBackAndIsLogged(t *testing.T) {
	r, rec := testRegistry(t)
	_, err := r.Invoke(context.Background(), You, "ws_boom", "{}")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v", err)
	}
	if ws, _ := r.Deps().Store.Workspaces.List(true); len(ws) != 0 {
		t.Fatalf("a failed action left %d workspaces behind", len(ws))
	}
	calls, _ := r.Deps().Store.MCPCalls.List("", 10)
	if len(calls) != 1 || calls[0].OK || calls[0].Tool != "ws_boom" || !strings.Contains(calls[0].Error, "boom") {
		t.Fatalf("mcp_calls = %+v", calls)
	}
	if rec.count() != 0 {
		t.Error("a failed action must not emit a changed event")
	}
}

func TestMutatingCallsAreLoggedAndEmitChanged(t *testing.T) {
	r, rec := testRegistry(t)
	var w domain.Workspace
	_ = json.Unmarshal([]byte(mustInvoke(t, r, Actor{Kind: KindSession, Name: "billing-fix"}, "ws_make", `{"name":"a"}`)), &w)
	mustInvoke(t, r, You, "ws_list", "")

	calls, _ := r.Deps().Store.MCPCalls.List("", 10)
	if len(calls) != 1 {
		t.Fatalf("mcp_calls = %d rows, want 1 (reads are not logged)", len(calls))
	}
	if c := calls[0]; c.Actor != "session:billing-fix" || c.Tool != "ws_make" || !c.OK || c.WorkspaceID != w.ID {
		t.Fatalf("call = %+v", c)
	}
	if rec.count() != 1 {
		t.Fatalf("events = %d, want 1 (reads emit nothing)", rec.count())
	}
	ev := rec.got[0]
	ce, ok := ev.payload.(ChangedEvent)
	if ev.event != EventChanged || !ok || ce.WorkspaceID != w.ID || len(ce.Entities) != 2 || ce.Entities[0] != "workspaces" || ce.Entities[1] != EntityMCPCalls {
		t.Fatalf("event = %+v", ev)
	}
}

func TestWorkspaceResolution(t *testing.T) {
	r, _ := testRegistry(t)
	var a, b domain.Workspace
	_ = json.Unmarshal([]byte(mustInvoke(t, r, You, "ws_make", `{"name":"a"}`)), &a)
	_ = json.Unmarshal([]byte(mustInvoke(t, r, You, "ws_make", `{"name":"b"}`)), &b)

	if _, err := r.Invoke(context.Background(), You, "ws_rename", `{"workspace_id":"","name":"x"}`); err == nil {
		t.Fatal("empty workspace_id is required, should have failed")
	}
	// The planner's own workspace is the default when the input omits it... but workspace_id is
	// required by this test action's schema, so use the helper directly.
	deps := r.Deps()
	c := &Call{Actor: Actor{Kind: KindPlanner, WorkspaceID: b.ID}, Store: deps.Store, Deps: deps}
	if w, err := c.Workspace(""); err != nil || w.ID != b.ID {
		t.Fatalf("actor workspace = %v, %v", w.ID, err)
	}
	c = &Call{Actor: You, Store: deps.Store, Deps: deps}
	if _, err := c.Workspace(""); err == nil || !strings.Contains(err.Error(), "workspace_id is required") {
		t.Fatalf("no default = %v", err)
	}
	_ = deps.Store.Settings.Set(SettingActiveWorkspace, json.RawMessage(`"`+a.ID+`"`))
	if w, err := c.Workspace(""); err != nil || w.ID != a.ID {
		t.Fatalf("active workspace = %v, %v", w.ID, err)
	}
	if _, err := c.Workspace("nope"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("missing workspace = %v", err)
	}
}

func TestActorString(t *testing.T) {
	for actor, want := range map[Actor]string{
		You:                                 "you",
		{Kind: KindPlanner}:                 "planner",
		{Kind: KindSession, Name: "x"}:      "session:x",
		{Kind: KindRule, Name: "Done rule"}: "rule:Done rule",
		{Kind: KindExternal}:                "external",
		{}:                                  "external",
	} {
		if got := actor.String(); got != want {
			t.Errorf("%+v.String() = %q, want %q", actor, got, want)
		}
	}
}

func TestInfosListFieldsAndRequired(t *testing.T) {
	r, _ := testRegistry(t)
	info, ok := r.Info("ws_rename")
	if !ok || !info.Mutates || len(info.Fields) != 4 {
		t.Fatalf("info = %+v", info)
	}
	byName := map[string]Field{}
	for _, f := range info.Fields {
		byName[f.Name] = f
	}
	if !byName["name"].Required || byName["notes"].Required || byName["count"].Type != "number" || byName["name"].Description != "the new name" {
		t.Fatalf("fields = %+v", byName)
	}
	if r.Len() != 4 || r.Names()[0] != "ws_boom" {
		t.Fatalf("Len=%d Names=%v", r.Len(), r.Names())
	}
}

func TestRegisterPanicsOnDuplicate(t *testing.T) {
	r, _ := testRegistry(t)
	defer func() {
		if recover() == nil {
			t.Fatal("registering a duplicate should panic")
		}
	}()
	Register(r, Spec{Name: "ws_list", Description: "again"}, func(ctx context.Context, c *Call, in struct{}) (struct{}, error) { return struct{}{}, nil })
}
