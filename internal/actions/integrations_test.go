package actions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/dlog"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/integrations"
	"github.com/GabiHert/hadron/internal/script"
)

const jiraToken = "jira-API-token-VALUE-7788"

// fakeJira stands in for Jira Cloud behind the sandbox's Backend. It echoes the credential it
// received into its answers, like a careless server, so the tests can prove nothing leaks.
type fakeJira struct {
	mu      sync.Mutex
	status  string
	calls   []string
	updates int
}

func (f *fakeJira) HTTP(_ context.Context, req script.HTTPRequest, _ func(string) bool) (script.HTTPResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, _ := url.Parse(req.URL)
	f.calls = append(f.calls, req.Method+" "+u.Path)
	ok := func(v any) (script.HTTPResponse, error) {
		b, _ := json.Marshal(v)
		return script.HTTPResponse{Status: 200, Headers: map[string]string{}, Body: b}, nil
	}
	issue := map[string]any{"key": "ACME-12", "echo": req.Headers["Authorization"], "fields": map[string]any{
		"summary": "Fix billing", "status": map[string]any{"name": f.status}, "priority": map[string]any{"name": "High"}}}
	switch {
	case req.Method == "GET" && strings.HasSuffix(u.Path, "/transitions"):
		return ok(map[string]any{"transitions": []any{
			map[string]any{"id": "31", "name": "Finish", "to": map[string]any{"name": "Finished"}},
			map[string]any{"id": "21", "name": "Start", "to": map[string]any{"name": "In progress"}}}})
	case req.Method == "POST" && strings.HasSuffix(u.Path, "/transitions"):
		var body struct {
			Transition struct{ ID string } `json:"transition"`
		}
		_ = json.Unmarshal(req.Body, &body)
		f.status = map[string]string{"31": "Finished", "21": "In progress"}[body.Transition.ID]
		f.updates++
		return script.HTTPResponse{Status: 204, Headers: map[string]string{}}, nil
	case req.Method == "GET" && strings.HasSuffix(u.Path, "/search/jql"):
		return ok(map[string]any{"issues": []any{issue}})
	case req.Method == "GET" && strings.Contains(u.Path, "/issue/ACME-12"):
		return ok(issue)
	}
	return script.HTTPResponse{Status: 404, Headers: map[string]string{}, Body: []byte("no such route " + u.Path)}, nil
}
func (f *fakeJira) Exec(context.Context, string, []string) (script.ExecResult, error) {
	return script.ExecResult{}, fmt.Errorf("no exec in this test")
}
func (f *fakeJira) MCP(context.Context, string, string, any) (any, error) {
	return nil, fmt.Errorf("no mcp in this test")
}

type integHarness struct {
	*rulesHarness
	mgr  *integrations.Manager
	jira *fakeJira
	home string
	lg   *dlog.Logger
}

func newIntegHarness(t *testing.T) *integHarness {
	t.Helper()
	t.Setenv("HADRON_SECRETS", "file")
	deps, rec := testDeps(t)
	deps.Fields = fields.NewRegistry()
	fj := &fakeJira{status: "In progress"}
	mgr := integrations.New(deps.Store, deps.Fields, script.NewSecrets(deps.Home), fj)
	deps.Integrations = mgr
	lg := dlog.New(filepath.Join(deps.Home, "logs"))
	lg.SetDebug(true)
	deps.Log, mgr.Log = lg, lg
	r := NewRegistry(deps)
	RegisterAll(r)
	mgr.FieldChanged = r.FieldChanged
	h := &rulesHarness{harness: &harness{t: t, reg: r, rec: rec}}
	h.ws = h.workspace("W")
	cols := h.columns(h.ws.ID)
	h.working, h.done, h.needs = colByRole(cols, domain.RoleActive), colByRole(cols, domain.RoleDone), colByRole(cols, domain.RoleNeedsYou)
	h.do("card_create", m{"nick": "alpha", "key": "ACME-1", "column_id": h.working.ID, "start": false}, &h.a)
	return &integHarness{rulesHarness: h, mgr: mgr, jira: fj, home: deps.Home, lg: lg}
}

// jiraEnabled scaffolds the jira template, configures and approves it.
func (h *integHarness) jiraEnabled() ScriptView {
	var v ScriptView
	h.do("integration_scaffold", m{"name": "jira", "template": "jira"}, &v)
	h.do("integration_config_set", m{"name": "jira", "config": m{"site": "acme.atlassian.net", "email": "me@acme.com"}}, nil)
	h.do("integration_secret_set", m{"name": "jira", "secret": "token", "value": jiraToken}, nil)
	h.do("integration_approve", m{"name": "jira"}, &v)
	return v
}

func TestScaffoldTestApproveEnablesAnIntegrationAndItsFields(t *testing.T) {
	h := newIntegHarness(t)
	var v ScriptView
	h.do("integration_scaffold", m{"name": "jira", "template": "jira"}, &v)
	if v.Status != "pending" || v.UsedHere {
		t.Fatalf("a scaffold is pending and off: %+v", v)
	}
	var schema FieldSchemaOut
	h.do("field_schema_get", nil, &schema)
	if _, ok := fields.Find(schema.Fields, "jira.status"); ok {
		t.Error("a pending integration must not contribute fields")
	}
	h.fails("integration_run_op", m{"name": "jira", "op": "search", "input": m{"q": "x"}}, "pending", "integration_test")
	var res script.TestResult
	h.do("integration_test", m{"name": "jira"}, &res)
	if !res.OK || res.Passed != 4 {
		t.Fatalf("the jira template must pass its recorded tests: %+v", res)
	}
	h.do("integration_approve", m{"name": "jira"}, &v)
	if v.Status != "enabled" || !v.UsedHere || v.ApprovedBy != "you" {
		t.Fatalf("approved: %+v", v)
	}
	h.do("field_schema_get", nil, &schema)
	e, ok := fields.Find(schema.Fields, "jira.status")
	if !ok || !e.Editable || !e.CardChip || e.Source != "jira" {
		t.Errorf("jira.status = %+v %v", e, ok)
	}
	var list []ScriptView
	h.do("integration_list", nil, &list)
	if len(list) != 1 || list[0].Secrets[0].Set || len(list[0].Config) != 2 {
		t.Errorf("list = %+v", list)
	}
	var scripts []ScriptView
	h.do("script_list", nil, &scripts)
	if len(scripts) != 0 {
		t.Errorf("script_list must not show integrations: %+v", scripts)
	}
	h.fails("script_approve", m{"name": "jira"}, "is a integration", "integration_*")
}

func TestCardFieldSetOnALinkedFieldCallsUpdateThenRulesRun(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()
	h.finishedRule()

	var link domain.Link
	h.do("link_add", m{"card": "alpha", "integration": "jira", "item_id": "ACME-12"}, &link)
	if link.Fields["status"] != "In progress" || link.Title != "ACME-12 Fix billing" {
		t.Fatalf("link = %+v", link)
	}
	if got := h.card(h.a); got.ColumnID != h.working.ID {
		t.Fatal("linking an In progress item must not move the card")
	}
	var f CardFieldsOut
	h.do("card_fields_get", m{"card": "alpha"}, &f)
	if f.Fields["jira.status"] != "In progress" {
		t.Fatalf("fields = %v", f.Fields)
	}

	var set CardFieldSetOut
	h.do("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "Finished"}, &set)
	if h.jira.updates != 1 {
		t.Errorf("the integration's update op should have run once; calls: %v", h.jira.calls)
	}
	if set.Old != "In progress" || set.Value != "Finished" {
		t.Errorf("set = %+v", set)
	}
	if got := h.card(h.a); got.ColumnID != h.done.ID {
		t.Errorf("the rule should have moved the card to Done, it is in %s", got.ColumnID)
	}
	var links []domain.Link
	h.do("link_list", m{"card": "alpha"}, &links)
	if links[0].Fields["status"] != "Finished" {
		t.Errorf("the snapshot should follow: %+v", links[0].Fields)
	}

	h.fails("card_field_set", m{"card": "alpha", "path": "jira.summary", "value": "x"}, "cannot be set")
	h.jira.status = "In progress"
	h.fails("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "Nonsense"}, "no transition", "Available")
}

func TestAPollThatFindsAChangedStatusRunsTheRules(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()
	h.finishedRule()
	h.do("link_add", m{"card": "alpha", "integration": "jira", "item_id": "ACME-12"}, nil)

	h.jira.status = "Finished" // someone finished it in Jira
	if err := h.mgr.Poll(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := h.card(h.a); got.ColumnID != h.done.ID {
		t.Errorf("the poll should have moved the card to Done")
	}
	// Not due again for poll_seconds.
	h.jira.calls = nil
	_ = h.mgr.Poll(context.Background(), time.Now())
	if len(h.jira.calls) != 0 {
		t.Errorf("polled again too soon: %v", h.jira.calls)
	}
}

// dumpDB returns every text cell of every table of the database file.
func dumpDB(t *testing.T, home string) string {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(home, "hadron.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var all strings.Builder
	tables, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for tables.Next() {
		var n string
		_ = tables.Scan(&n)
		names = append(names, n)
	}
	tables.Close()
	for _, n := range names {
		rows, err := db.Query(`SELECT * FROM "` + n + `"`)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := rows.Columns()
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			_ = rows.Scan(ptrs...)
			for _, v := range vals {
				fmt.Fprintf(&all, "%s: %v\n", n, v)
			}
		}
		rows.Close()
	}
	return all.String()
}

func TestASecretNeverReachesOutputLogsCallsOrChanges(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()
	h.finishedRule()
	h.do("link_add", m{"card": "alpha", "integration": "jira", "item_id": "ACME-12"}, nil)

	var op RunOpOut
	h.do("integration_run_op", m{"name": "jira", "op": "search", "input": m{"q": "ACME-12"}}, &op)
	b, _ := json.Marshal(op)
	if strings.Contains(string(b), jiraToken) {
		t.Errorf("the op's output leaked the secret: %s", b)
	}
	h.do("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "Finished"}, nil)
	var calls []domain.MCPCall
	h.do("mcp_calls", m{"limit": 500}, &calls)
	var logs []domain.ScriptLog
	h.do("integration_logs", nil, &logs)
	if len(logs) == 0 {
		t.Fatal("the ops should have been logged")
	}

	// Hadron's own debug log has seen every one of those calls, and holds no secret.
	lines := h.lg.Tail(2000, "", "")
	if len(lines) == 0 {
		t.Fatal("debug logging was on, so the actions should be in the log")
	}
	seenSecretSet, seenScript := false, false
	for _, ln := range lines {
		if strings.Contains(ln.Raw, jiraToken) || strings.Contains(ln.Raw, "base64") {
			t.Errorf("the secret reached the log: %s", ln.Raw)
		}
		seenSecretSet = seenSecretSet || (ln.Msg == "action" && ln.Fields["action"] == "integration_secret_set")
		seenScript = seenScript || ln.Msg == "script run"
	}
	if !seenSecretSet || !seenScript {
		t.Errorf("the secret_set action and the script runs are logged by name: %v %v", seenSecretSet, seenScript)
	}
	files, _ := filepath.Glob(filepath.Join(h.home, "logs", "*"))
	for _, f := range files {
		raw, _ := os.ReadFile(f)
		if strings.Contains(string(raw), jiraToken) {
			t.Errorf("%s holds the secret", f)
		}
	}

	dump := dumpDB(t, h.home)
	if strings.Contains(dump, jiraToken) {
		i := strings.Index(dump, jiraToken)
		t.Fatalf("the secret is in the database: ...%s...", dump[max(0, i-120):min(len(dump), i+120)])
	}
	for _, tbl := range []string{"changes:", "mcp_calls:", "script_logs:"} {
		if !strings.Contains(dump, tbl) {
			t.Errorf("expected rows in %s", tbl)
		}
	}
	// integration_secret_set's call is logged, with its arguments redacted.
	if !strings.Contains(dump, "[redacted]") {
		t.Error("the secret_set call should be logged with redacted arguments")
	}
	// The value sits in the owner-only secrets file, where it belongs.
	file := h.mgr.Secrets.(*script.FileSecrets)
	if v, ok, _ := file.Get("jira/token"); !ok || v != jiraToken {
		t.Errorf("secret not stored: %q %v", v, ok)
	}
}

func TestOnlyTheUserStoresSecrets(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()
	planner := Actor{Kind: KindPlanner, WorkspaceID: h.ws.ID}
	if err := h.as(planner, "integration_secret_set", m{"name": "jira", "secret": "token", "value": "abc"}, nil); err == nil || !strings.Contains(err.Error(), "only the user") {
		t.Errorf("err = %v", err)
	}
	h.fails("integration_secret_set", m{"name": "jira", "secret": "nope", "value": "abc"}, "does not declare the secret")
}

func TestPlannerApprovalFollowsThePolicy(t *testing.T) {
	h := newIntegHarness(t)
	planner := Actor{Kind: KindPlanner, WorkspaceID: h.ws.ID}
	session := Actor{Kind: KindSession, Name: "alpha", WorkspaceID: h.ws.ID, CardID: h.a.ID}

	h.do("integration_scaffold", m{"name": "jira", "template": "jira"}, nil)
	h.do("integration_scaffold", m{"name": "plain", "template": "empty"}, nil)

	if err := h.as(session, "integration_approve", m{"name": "plain"}, nil); err == nil || !strings.Contains(err.Error(), "card session cannot approve") {
		t.Errorf("a session must not approve: %v", err)
	}

	h.do("planner_policy_set", m{"policy": "decisions_only"}, nil)
	err := h.as(planner, "integration_approve", m{"name": "jira"}, nil)
	if err == nil || !strings.Contains(err.Error(), "decisions_only") || !strings.Contains(err.Error(), "atlassian.net") {
		t.Errorf("under decisions_only the planner must not approve new network domains: %v", err)
	}
	h.do("planner_policy_set", m{"policy": "ask_for_prd"}, nil)
	if err := h.as(planner, "integration_approve", m{"name": "jira"}, nil); err == nil {
		t.Error("ask_for_prd refuses new network domains too")
	}
	var v ScriptView
	if err := h.as(planner, "integration_approve", m{"name": "plain"}, &v); err != nil || v.Status != "enabled" || v.ApprovedBy != "planner" {
		t.Errorf("an integration with no network access is fine under a restrictive policy: %v %+v", err, v)
	}
	h.do("planner_policy_set", m{"policy": "allow_all"}, nil)
	if err := h.as(planner, "integration_approve", m{"name": "jira"}, &v); err != nil || v.Status != "enabled" {
		t.Errorf("allow_all lets the planner approve: %v", err)
	}
}

func TestAnEditThatKeepsThePermissionsRetestsAndReenables(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()

	var src FileOut
	h.do("integration_read_file", m{"name": "jira", "path": "index.js"}, &src)
	var v ScriptView
	h.do("integration_write_file", m{"name": "jira", "path": "index.js", "content": "// edited\n" + src.Content}, &v)
	if v.Status != "enabled" || v.TestResult == nil || !v.TestResult.OK {
		t.Fatalf("an edit with the same permissions that passes its tests stays enabled: %+v", v)
	}

	broken := strings.Replace(src.Content, `status: f.status ? f.status.name : ""`, `status: "always"`, 1)
	h.do("integration_write_file", m{"name": "jira", "path": "index.js", "content": broken}, &v)
	if v.Status != "pending" || v.TestResult == nil || v.TestResult.OK {
		t.Fatalf("an edit that fails its tests goes back to pending: %+v", v)
	}
	var schema FieldSchemaOut
	h.do("field_schema_get", nil, &schema)
	if _, ok := fields.Find(schema.Fields, "jira.status"); ok {
		t.Error("a pending integration's fields disappear")
	}

	h.do("integration_write_file", m{"name": "jira", "path": "index.js", "content": src.Content}, &v)
	if v.Status != "enabled" {
		t.Fatalf("fixing it re-enables it without a new approval: %+v", v)
	}

	var man FileOut
	h.do("integration_read_file", m{"name": "jira", "path": "manifest.json"}, &man)
	wider := strings.Replace(man.Content, `"https://*.atlassian.net/*"`, `"https://*.atlassian.net/*", "https://api.evil.example/*"`, 1)
	h.do("integration_write_file", m{"name": "jira", "path": "manifest.json", "content": wider}, &v)
	if v.Status != "pending" || len(v.NewDomains) == 0 {
		t.Fatalf("asking for more access needs a new approval: %+v", v)
	}

	h.fails("integration_write_file", m{"name": "jira", "path": "index.js", "content": "var ops = {"}, "index.js")
	h.fails("integration_write_file", m{"name": "jira", "path": "manifest.json", "content": `{"name":"jira"}`}, "manifest")
	h.fails("integration_write_file", m{"name": "jira", "path": "other.txt", "content": "x"}, "manifest.json or index.js")
}

func TestDisableAndRemoveCleanUp(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()
	h.do("link_add", m{"card": "alpha", "integration": "jira", "item_id": "ACME-12"}, nil)

	var v ScriptView
	h.do("integration_disable", m{"name": "jira"}, &v)
	if v.Status != "disabled" || v.UsedHere {
		t.Errorf("%+v", v)
	}
	h.fails("link_search", m{"integration": "jira", "q": "x"}, "disabled")
	var rm RemoveOut
	h.do("integration_remove", m{"name": "jira"}, &rm)
	if rm.LinksRemoved != 1 {
		t.Errorf("%+v", rm)
	}
	var list []ScriptView
	h.do("integration_list", nil, &list)
	if len(list) != 0 {
		t.Errorf("%+v", list)
	}
	if v, ok, _ := h.mgr.Secrets.Get("jira/token"); ok {
		t.Errorf("removing an integration deletes its secrets, found %q", v)
	}
}

func TestARunActionRuleRunsAUserScript(t *testing.T) {
	h := newIntegHarness(t)
	var v ScriptView
	h.do("script_scaffold", m{"name": "mover"}, &v)
	manifest := `{"name":"mover","version":"1","ops":{"actions":{"go":{}}},"permissions":{"api":["card.move","field.set"]},
	  "tests":[{"name":"go","op":"actions.go","input":{"card":{"nick":"x"}},"expect":{"text":"moved"}}]}`
	h.do("script_write_file", m{"name": "mover", "path": "manifest.json", "content": manifest}, nil)
	h.do("script_write_file", m{"name": "mover", "path": "index.js", "content": `var ops = {actions: {go: function (i) { host.card.move({card: i.card.nick, column: "Done"}); return {text: "moved " + i.card.nick}; }}};`}, nil)
	// The test expects "moved", the code says "moved alpha": the recorded test catches it.
	h.fails("script_approve", m{"name": "mover"}, "tests fail")
	h.do("script_write_file", m{"name": "mover", "path": "index.js", "content": `var ops = {actions: {go: function (i) { host.card.move({card: i.card.nick, column: "Done"}); return {text: "moved"}; }}};`}, nil)
	h.do("script_approve", m{"name": "mover"}, &v)
	if v.Status != "enabled" {
		t.Fatalf("%+v", v)
	}
	var rule domain.Rule
	h.do("rule_create", m{"name": "Stage review runs mover", "trigger": m{"on": "field_changed", "field": "card.stage"},
		"actions": []m{{"type": "run_action", "script": "mover", "action": "go"}}}, &rule)

	var dry []map[string]any
	h.do("rule_dry_run", m{"rule_id": rule.ID}, &dry)
	if got := h.card(h.a); got.ColumnID != h.working.ID {
		t.Fatal("a dry run runs no script")
	}
	h.do("card_field_set", m{"card": "alpha", "path": "card.stage", "value": "review"}, nil)
	if got := h.card(h.a); got.ColumnID != h.done.ID {
		t.Errorf("run_action should have run the script, which moved the card to Done")
	}
	var logs []domain.ScriptLog
	h.do("script_logs", m{"name": "mover"}, &logs)
	if len(logs) != 1 || !logs[0].OK || logs[0].Op != "actions.go" {
		t.Errorf("logs = %+v", logs)
	}
}

func TestAScriptFailureIsLoggedNotFatalToTheRule(t *testing.T) {
	h := newIntegHarness(t)
	h.do("script_scaffold", m{"name": "needy"}, nil)
	man := `{"name":"needy","version":"1","ops":{"actions":{"go":{}}},"permissions":{},"tests":[{"op":"actions.go","input":{"card":{}},"expect":{"text":"ok"}}]}`
	h.do("script_write_file", m{"name": "needy", "path": "manifest.json", "content": man}, nil)
	// Passes its tests (the stub catches nothing) but calls something it has no permission for.
	h.do("script_write_file", m{"name": "needy", "path": "index.js", "content": `var ops = {actions: {go: function (i) { if (i.card.nick) { host.card.move({card: i.card.nick, column: "Done"}); } return {text: "ok"}; }}};`}, nil)
	h.do("script_approve", m{"name": "needy"}, nil)
	h.do("rule_create", m{"name": "r", "trigger": m{"on": "field_changed", "field": "card.stage"},
		"actions": []m{{"type": "run_action", "script": "needy", "action": "go"}, {"type": "set_field", "path": "card.assignee", "value": "Ana"}}}, nil)
	h.do("card_field_set", m{"card": "alpha", "path": "card.stage", "value": "x"}, nil)
	if got := h.card(h.a); got.Assignee != "Ana" || got.ColumnID != h.working.ID {
		t.Errorf("the next action still runs and the denied move did not happen: %+v", got)
	}
	found := false
	for _, e := range h.log() {
		if e.Kind == "skip" && strings.Contains(e.Reason, "PermissionError") {
			found = true
		}
	}
	if !found {
		t.Errorf("the rule log should say the script was refused: %+v", h.log())
	}
	var logs []domain.ScriptLog
	h.do("script_logs", m{"name": "needy"}, &logs)
	if len(logs) != 1 || logs[0].OK || !strings.Contains(logs[0].Message, "PermissionError") {
		t.Errorf("logs = %+v", logs)
	}
}

func TestRemoteSessionsComeFromTheIntegration(t *testing.T) {
	h := newIntegHarness(t)
	var v ScriptView
	h.do("integration_scaffold", m{"name": "cloud", "template": "empty"}, &v)
	man := `{"name":"cloud","version":"1","ops":{"get":{},"sessions":{"list":{},"output":{},"send":{},"start":{},"stop":{}}},"permissions":{},
	  "tests":[{"op":"sessions.list","expect":{"sessions":[{"id":"s1"},{"id":"s2"}]}}]}`
	h.do("integration_write_file", m{"name": "cloud", "path": "manifest.json", "content": man}, nil)
	src := `
var stopped = [];
var ops = {
  get: function (i) { return {item_id: i.item_id, title: "T " + i.item_id, fields: {}}; },
  sessions: {
    list: function () { return {sessions: [{id: "s1", title: "Fix it", status: "running", item_id: "9"}, {id: "s2", title: "Other", status: "done", item_id: "10"}]}; },
    output: function (i) { return {text: "output of " + i.id}; },
    send: function (i) { host.log("send", i.text); return {}; },
    start: function (i) { return {id: "new-" + (i.item_id || "x"), status: "running", title: i.prompt || ""}; },
    stop: function (i) { return {}; }
  }
};`
	h.do("integration_write_file", m{"name": "cloud", "path": "index.js", "content": src}, nil)
	h.do("integration_approve", m{"name": "cloud"}, &v)
	h.do("link_add", m{"card": "alpha", "integration": "cloud", "item_id": "9"}, nil)

	var all, mine []RemoteSession
	h.do("remote_session_list", nil, &all)
	h.do("remote_session_list", m{"card": "alpha"}, &mine)
	if len(all) != 2 || len(mine) != 1 || mine[0].ID != "s1" || mine[0].Integration != "cloud" {
		t.Errorf("all=%+v mine=%+v", all, mine)
	}
	var out RemoteSessionOutputOut
	h.do("remote_session_output", m{"integration": "cloud", "id": "s1"}, &out)
	if out.Text != "output of s1" {
		t.Errorf("%+v", out)
	}
	var started RemoteSession
	h.do("remote_session_start", m{"integration": "cloud", "card": "alpha", "prompt": "go"}, &started)
	if started.ID != "new-9" || started.ItemID != "9" {
		t.Errorf("%+v", started)
	}
	h.do("remote_session_send", m{"integration": "cloud", "id": "s1", "text": "hello"}, nil)
	h.do("remote_session_stop", m{"integration": "cloud", "id": "s1"}, nil)
	h.fails("remote_session_list", m{"integration": "jira"}, "no integration")
}

func TestIntegrationActionsAreGatedByActor(t *testing.T) {
	h := newIntegHarness(t)
	session := Actor{Kind: KindSession, Name: "alpha", WorkspaceID: h.ws.ID, CardID: h.a.ID}
	if err := h.as(session, "integration_scaffold", m{"name": "x"}, nil); err == nil || !strings.Contains(err.Error(), "card session cannot") {
		t.Errorf("err = %v", err)
	}
	h.fails("integration_scaffold", m{"name": "Bad Name"}, "lowercase")
	h.fails("integration_scaffold", m{"name": "card"}, "reserved")
	h.fails("integration_scaffold", m{"name": "x", "template": "nope"}, "no template")
	h.do("integration_scaffold", m{"name": "dup"}, nil)
	h.fails("integration_scaffold", m{"name": "dup"}, "already exists")
}

func TestIntegrationUseTogglesAWorkspace(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()
	var v ScriptView
	h.do("integration_use", m{"name": "jira", "enabled": false}, &v)
	if v.UsedHere {
		t.Error("switched off for the workspace")
	}
	var schema FieldSchemaOut
	h.do("field_schema_get", nil, &schema)
	if _, ok := fields.Find(schema.Fields, "jira.status"); ok {
		t.Error("an integration that is off here contributes no fields")
	}
	h.do("integration_use", m{"name": "jira", "enabled": true}, &v)
	if !v.UsedHere {
		t.Error("back on")
	}
}

func TestCardChipsShowLinkedFieldsOnTheCardFace(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()
	var chips map[string][]ChipOut
	h.do("card_chips", nil, &chips)
	if len(chips) != 0 {
		t.Fatalf("no link, no chips: %+v", chips)
	}
	h.do("link_add", m{"card": "alpha", "integration": "jira", "item_id": "ACME-12"}, nil)
	h.do("card_chips", nil, &chips)
	got := chips[h.a.ID]
	if len(got) != 1 || got[0].Path != "jira.status" || got[0].Value != "In progress" || got[0].Label != "Jira status" {
		t.Errorf("chips = %+v", chips)
	}
}
