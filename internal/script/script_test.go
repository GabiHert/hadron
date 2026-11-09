package script

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func manifestOf(t *testing.T, raw string) Manifest {
	t.Helper()
	m, problems := ParseManifest([]byte(raw))
	if len(problems) > 0 {
		t.Fatalf("manifest problems: %v", problems)
	}
	return m
}

const baseManifest = `{"name":"demo","version":"1","ops":{"get":{},"actions":{"echo":{}}},"permissions":%s}`

func demo(t *testing.T, perms string) Manifest {
	return manifestOf(t, strings.Replace(baseManifest, "%s", perms, 1))
}

func run(t *testing.T, r *Runner, m Manifest, src, op, input string) (Result, error) {
	t.Helper()
	return r.Run(context.Background(), Call{Name: "demo", Manifest: m, Source: src, Op: op, Input: json.RawMessage(input)})
}

func fileSecrets(t *testing.T, kv map[string]string) *FileSecrets {
	t.Helper()
	f := &FileSecrets{Path: filepath.Join(t.TempDir(), "secrets.json")}
	for k, v := range kv {
		if err := f.Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func TestAnOutsideCallThrowsPermissionError(t *testing.T) {
	m := demo(t, `{"network":["https://api.example.com/*"],"exec":["gh"],"api":["notify"]}`)
	r := &Runner{Backend: &Fixtures{}}
	cases := map[string]struct{ src, perm string }{
		"http":   {`host.http.request({url: "https://evil.example.org/x"})`, "network: https://evil.example.org/x"},
		"exec":   {`host.exec("rm", ["-rf", "/"])`, "exec: rm"},
		"api":    {`host.card.move({card: "a", column: "b"})`, "api: card.move"},
		"secret": {`host.secret("token")`, "secrets: token"},
		"mcp":    {`host.mcp.call("linear", "list_issues", {})`, "network: mcp:linear"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := run(t, r, m, `var ops = {get: function (i) { `+c.src+`; return {}; }, actions: {echo: function (i) { return {}; }}};`, "get", `{"item_id":"1"}`)
			var pe *PermissionError
			if !errors.As(err, &pe) {
				t.Fatalf("want PermissionError, got %v", err)
			}
			if pe.Permission != c.perm {
				t.Errorf("permission = %q, want %q", pe.Permission, c.perm)
			}
		})
	}
	t.Run("the code can catch it and see its name", func(t *testing.T) {
		src := `var ops = {get: function (i) { try { host.exec("rm", []); } catch (e) { return {item_id: e.name + "|" + e.permission, title: ""}; } }, actions: {echo: function(){return {}}}};`
		res, err := run(t, r, m, src, "get", `{"item_id":"1"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(res.Output), "PermissionError|exec: rm") {
			t.Errorf("output = %s", res.Output)
		}
	})
}

func TestNetworkPatternsAreStrict(t *testing.T) {
	p := Permissions{Network: []string{"https://*.atlassian.net/*", "https://api.example.com/v1/*"}}
	allow := []string{"https://acme.atlassian.net/rest/api/3/issue/X-1?fields=a", "https://api.example.com/v1/items"}
	deny := []string{
		"https://evil.com/x.atlassian.net/",
		"https://acme.atlassian.net.evil.com/",
		"https://user@acme.atlassian.net/",
		"http://acme.atlassian.net/",
		"https://api.example.com/v2/items",
		"https://evil.com/?https://acme.atlassian.net/",
	}
	for _, u := range allow {
		if !p.AllowsURL(u) {
			t.Errorf("should allow %s", u)
		}
	}
	for _, u := range deny {
		if p.AllowsURL(u) {
			t.Errorf("should deny %s", u)
		}
	}
	if _, err := ParsePattern("https://*/*"); err == nil {
		t.Error("a pattern for every host must be refused")
	}
}

func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// A hostile or careless server that echoes the credentials back.
		_ = json.NewEncoder(w).Encode(map[string]any{"authorization": r.Header.Get("Authorization"), "key": r.Header.Get("X-Api-Key"), "path": r.URL.Path})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestASecretValueNeverReachesTheCode(t *testing.T) {
	srv := echoServer(t)
	const secret = "s3cr3t-token-VALUE-123"
	m := demo(t, `{"network":["http://127.0.0.1:*/*"],"secrets":["token"]}`)
	r := &Runner{Backend: Live{}, Secrets: fileSecrets(t, map[string]string{"demo/token": secret})}
	src := `
var ops = {
  get: function (input) {
    var h = host.secret("token");
    var shown = [String(h), JSON.stringify(h), typeof h, Object.keys(h).length];
    host.log("handle:", h, shown.join(","));
    var bearer = host.http.request({url: "` + srv.URL + `/a", auth: {type: "bearer", secret: h}});
    var hdr = host.http.request({url: "` + srv.URL + `/b", headers: {"X-Api-Key": h}});
    // The server echoed the credential; the sandbox must have removed it before the code sees it.
    host.log("echo:", bearer.text, hdr.text);
    return {item_id: "1", title: JSON.stringify([bearer.json, hdr.json, shown])};
  },
  actions: {echo: function () { return {}; }}
};`
	res, err := run(t, r, m, src, "get", `{"item_id":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	all := string(res.Output) + strings.Join(res.Logs, "\n")
	if strings.Contains(all, secret) {
		t.Errorf("the secret leaked:\n%s", all)
	}
	if !strings.Contains(all, "[secret]") {
		t.Errorf("expected the scrubbed marker in:\n%s", all)
	}

	// Plain strings are refused as auth, so a token cannot be typed into code by accident.
	_, err = run(t, r, m, `var ops = {get: function(){ host.http.request({url: "`+srv.URL+`/", auth: {type:"bearer", secret: "abc"}}); return {}; }, actions:{echo:function(){return {}}}};`, "get", `{"item_id":"1"}`)
	if err == nil || !strings.Contains(err.Error(), "secret('name')") {
		t.Errorf("a string secret should be refused, got %v", err)
	}
	// A secret is not usable as a body, an argument or an app-data value.
	_, err = run(t, r, m, `var ops = {get: function(){ host.http.request({url: "`+srv.URL+`/", json: {t: host.secret("token")}}); return {}; }, actions:{echo:function(){return {}}}};`, "get", `{"item_id":"1"}`)
	if err == nil || !strings.Contains(err.Error(), "auth or as a header") {
		t.Errorf("a secret in a body should be refused, got %v", err)
	}
}

func TestAnErrorNeverCarriesTheSecret(t *testing.T) {
	const secret = "another-SECRET-value-99"
	m := demo(t, `{"network":["http://127.0.0.1:*/*"],"secrets":["token"]}`)
	srv := echoServer(t)
	r := &Runner{Backend: Live{}, Secrets: fileSecrets(t, map[string]string{"demo/token": secret})}
	src := `var ops = {get: function(){ var r = host.http.request({url: "` + srv.URL + `/", headers: {"X-Api-Key": host.secret("token")}}); throw new Error("server said " + JSON.stringify(r)); }, actions:{echo:function(){return {}}}};`
	_, err := run(t, r, m, src, "get", `{"item_id":"1"}`)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error = %v", err)
	}
}

func TestTheTimeoutIsEnforced(t *testing.T) {
	m := demo(t, `{}`)
	r := &Runner{Backend: &Fixtures{}, Timeout: 150 * time.Millisecond}
	start := time.Now()
	_, err := run(t, r, m, `var ops = {get: function(){ while (true) {} }, actions:{echo:function(){return {}}}};`, "get", `{"item_id":"1"}`)
	var te *TimeoutError
	if !errors.As(err, &te) {
		t.Fatalf("want TimeoutError, got %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("took %s to stop", time.Since(start))
	}
}

func TestTimeWaitingOnAHostCallDoesNotCount(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer slow.Close()
	m := demo(t, `{"network":["http://127.0.0.1:*/*"]}`)
	r := &Runner{Backend: Live{}, Timeout: 150 * time.Millisecond}
	res, err := run(t, r, m, `var ops = {get: function(){ var r = host.http.request({url: "`+slow.URL+`/"}); return {item_id: "1", title: String(r.json.ok)}; }, actions:{echo:function(){return {}}}};`, "get", `{"item_id":"1"}`)
	if err != nil {
		t.Fatalf("a slow server is not a slow script: %v", err)
	}
	if !strings.Contains(string(res.Output), "true") {
		t.Errorf("output = %s", res.Output)
	}
}

func TestTheMemoryGuardStopsRunaways(t *testing.T) {
	m := demo(t, `{}`)
	r := &Runner{Backend: &Fixtures{}, MemoryLimit: 24 << 20, Timeout: 20 * time.Second}
	_, err := run(t, r, m, `var ops = {get: function(){ var a = []; while (true) { a.push(new Array(200000).fill(7)); } }, actions:{echo:function(){return {}}}};`, "get", `{"item_id":"1"}`)
	var me *MemoryError
	if !errors.As(err, &me) {
		t.Fatalf("want MemoryError, got %v", err)
	}
}

func TestNoRequireAndNoTimers(t *testing.T) {
	m := demo(t, `{}`)
	r := &Runner{Backend: &Fixtures{}}
	src := `var ops = {get: function(){ return {item_id: [typeof require, typeof setTimeout, typeof setInterval, typeof process, typeof fetch].join(","), title: ""}; }, actions:{echo:function(){return {}}}};`
	res, err := run(t, r, m, src, "get", `{"item_id":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Output), "undefined,undefined,undefined,undefined,undefined") {
		t.Errorf("globals leaked: %s", res.Output)
	}
}

func TestInputAndOutputAreValidated(t *testing.T) {
	m := demo(t, `{}`)
	r := &Runner{Backend: &Fixtures{}}
	_, err := run(t, r, m, `var ops = {get: function(){ return {}; }, actions:{echo:function(){return {}}}};`, "get", `{}`)
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Where != "input" {
		t.Errorf("a missing item_id should fail the input schema, got %v", err)
	}
	_, err = run(t, r, m, `var ops = {get: function(){ return {item_id: "1"}; }, actions:{echo:function(){return {}}}};`, "get", `{"item_id":"1"}`)
	if !errors.As(err, &ve) || ve.Where != "output" {
		t.Errorf("a missing title should fail the output schema, got %v", err)
	}
	_, err = run(t, r, m, `var ops = {};`, "get", `{"item_id":"1"}`)
	if err == nil || !strings.Contains(err.Error(), "does not define it") {
		t.Errorf("an undefined op should say so, got %v", err)
	}
	_, err = run(t, r, m, `var ops = {};`, "search", `{"q":"x"}`)
	if err == nil || !strings.Contains(err.Error(), "does not declare the op") {
		t.Errorf("an undeclared op should say so, got %v", err)
	}
}

func TestManifestProblemsAreAllListed(t *testing.T) {
	_, problems := ParseManifest([]byte(`{"name":"Bad Name","version":"1"}`))
	if len(problems) == 0 || !strings.Contains(problems[0], "name") {
		t.Errorf("want a problem naming the field, got %v", problems)
	}
	_, problems = ParseManifest([]byte(`{"name":"ok","version":"1","fields":[{"name":"s","type":"text","editable":true}],"permissions":{"network":["ftp://x/y"]}}`))
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "ops.update") || !strings.Contains(joined, "permissions.network") {
		t.Errorf("problems = %v", problems)
	}
}

func TestEveryStarterTemplatePassesItsRecordedTests(t *testing.T) {
	for _, name := range TemplateNames() {
		t.Run(name, func(t *testing.T) {
			man, src, err := Template(name)
			if err != nil {
				t.Fatal(err)
			}
			m, problems := ParseManifest([]byte(man))
			if len(problems) > 0 {
				t.Fatalf("manifest: %v", problems)
			}
			if err := CheckSyntax(src); err != nil {
				t.Fatal(err)
			}
			res := RunTests(context.Background(), m.Name, m, src, nil)
			for _, c := range res.Cases {
				if !c.OK {
					t.Errorf("%s: %s", c.Name, c.Error)
				}
			}
			if !res.OK || res.Passed == 0 {
				t.Errorf("result = %+v", res)
			}
		})
	}
	if got := strings.Join(TemplateNames(), ","); got != "empty,github,jira,linear" {
		t.Errorf("templates = %s", got)
	}
}

func TestTheScriptStarterPassesToo(t *testing.T) {
	man, src, err := Template(ScriptTemplate)
	if err != nil {
		t.Fatal(err)
	}
	m, problems := ParseManifest([]byte(man))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	if res := RunTests(context.Background(), m.Name, m, src, nil); !res.OK {
		t.Errorf("%+v", res)
	}
	if p := CheckOps(m, src); len(p) > 0 {
		t.Errorf("ops: %v", p)
	}
	if p := CheckOps(m, `var ops = {};`); len(p) != 1 || !strings.Contains(p[0], "actions.notify_done") {
		t.Errorf("a missing op must be named: %v", p)
	}
}

func TestATestThatDoesNotMatchFails(t *testing.T) {
	man, src, _ := Template("jira")
	m, problems := ParseManifest([]byte(man))
	if len(problems) > 0 {
		t.Fatal(problems)
	}
	m.Tests[2].Expect = map[string]any{"fields": map[string]any{"status": "Nope"}}
	res := RunTests(context.Background(), m.Name, m, src, nil)
	if res.OK || res.Failed != 1 {
		t.Errorf("a wrong expectation must fail exactly that test: %+v", res)
	}
}

func TestFileSecretsAreOwnerOnly(t *testing.T) {
	f := fileSecrets(t, map[string]string{"jira/token": "abc"})
	st, err := os.Stat(f.Path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode().Perm())
	}
	if v, ok, _ := f.Get("jira/token"); !ok || v != "abc" {
		t.Errorf("get = %q %v", v, ok)
	}
	if err := f.Set("bad key", "x"); err == nil {
		t.Error("a malformed key must be refused")
	}
}

func TestExecRunsOnlyAllowListedBinaries(t *testing.T) {
	m := demo(t, `{"exec":["echo"]}`)
	r := &Runner{Backend: Live{}}
	res, err := run(t, r, m, `var ops = {get: function(){ var r = host.exec("echo", ["hello", 7]); return {item_id: r.stdout.trim(), title: String(r.code)}; }, actions:{echo:function(){return {}}}};`, "get", `{"item_id":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Output), `"item_id":"hello 7"`) {
		t.Errorf("output = %s", res.Output)
	}
	_, err = run(t, r, m, `var ops = {get: function(){ host.exec("/bin/echo", []); return {}; }, actions:{echo:function(){return {}}}};`, "get", `{"item_id":"1"}`)
	var pe *PermissionError
	if !errors.As(err, &pe) {
		t.Errorf("a path must not satisfy a bare-name permission: %v", err)
	}
}

func TestMCPCallReachesAnHTTPServerFromTheClaudeConfig(t *testing.T) {
	srv := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "1"}, nil)
	type in struct {
		Q string `json:"q"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "list_issues", Description: "x"}, func(ctx context.Context, req *mcp.CallToolRequest, a in) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `{"issues":[{"identifier":"ENG-1","q":"` + a.Q + `"}]}`}}}, nil, nil
	})
	ts := httptest.NewServer(mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil))
	defer ts.Close()
	cfg := filepath.Join(t.TempDir(), "claude.json")
	_ = os.WriteFile(cfg, []byte(`{"mcpServers":{"linear":{"type":"http","url":"`+ts.URL+`"}}}`), 0o600)

	m := manifestOf(t, `{"name":"demo","version":"1","ops":{"get":{},"actions":{"echo":{}}},"permissions":{"network":["mcp:linear"]}}`)
	r := &Runner{Backend: Live{ClaudeConfig: cfg}}
	res, err := run(t, r, m, `var ops = {get: function(){ var r = host.mcp.call("linear", "list_issues", {q: "bill"}); return {item_id: r.json.issues[0].identifier, title: r.json.issues[0].q}; }, actions:{echo:function(){return {}}}};`, "get", `{"item_id":"1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Output), `"item_id":"ENG-1"`) || !strings.Contains(string(res.Output), `"title":"bill"`) {
		t.Errorf("output = %s", res.Output)
	}
}

func TestSamePermissionsIgnoresOrder(t *testing.T) {
	a := Permissions{Network: []string{"a", "b"}}
	b := Permissions{Network: []string{"b", "a"}}
	if !SamePermissions(a, b) {
		t.Error("order must not matter")
	}
	if SamePermissions(a, Permissions{Network: []string{"a"}}) {
		t.Error("a removed permission is a change")
	}
}

// A GUI app's own PATH lacks the user's tools (Homebrew), so Exec must find a binary on the login
// shell's PATH even when the process PATH cannot.
func TestLiveExecFindsBinaryOnLoginShellPath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "hadron-fake-tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho found \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	live := Live{Env: func() []string { return []string{"PATH=" + dir + ":/usr/bin:/bin", "HOME=" + dir} }}
	res, err := live.Exec(context.Background(), "hadron-fake-tool", []string{"ok"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.Code != 0 || strings.TrimSpace(res.Stdout) != "found ok" {
		t.Fatalf("got code %d stdout %q stderr %q", res.Code, res.Stdout, res.Stderr)
	}
}
