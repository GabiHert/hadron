package mcpserver

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/actions"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fixture struct {
	t     *testing.T
	home  string
	st    *store.Store
	reg   *actions.Registry
	srv   *Server
	ws    domain.Workspace
	card  domain.Card
	cardS domain.Session
	planS domain.Session
	token string
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HADRON_HOME", home)
	t.Setenv("HADRON_MCP_PORT", "")
	st, err := store.Open(filepath.Join(home, "hadron.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	reg := actions.NewRegistry(actions.Deps{Store: st, Version: "test", Home: home})
	actions.RegisterAll(reg)

	out, err := reg.Invoke(context.Background(), actions.You, "workspace_create", `{"name":"Acme","root":"`+home+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var ws struct {
		Workspace domain.Workspace `json:"workspace"`
	}
	_ = json.Unmarshal([]byte(out), &ws)
	if ws.Workspace.ID == "" {
		var w domain.Workspace
		_ = json.Unmarshal([]byte(out), &w)
		ws.Workspace = w
	}
	f := &fixture{t: t, home: home, st: st, reg: reg, ws: ws.Workspace}
	out, err = reg.Invoke(context.Background(), actions.You, "card_create", `{"nick":"billing-fix","start":false}`)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(out), &f.card)
	f.cardS = domain.Session{WorkspaceID: f.ws.ID, CardID: f.card.ID, Kind: "card", Status: "running"}
	f.planS = domain.Session{WorkspaceID: f.ws.ID, Kind: "planner", Status: "running"}
	for _, s := range []*domain.Session{&f.cardS, &f.planS} {
		if err := st.Sessions.Create(s); err != nil {
			t.Fatal(err)
		}
	}
	f.srv = New(Options{Registry: reg, Store: st, Home: home, Port: freePort(t), Version: "test"})
	if err := f.srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.srv.Close)
	f.token, _ = f.srv.Token()
	return f
}

// headerTransport adds fixed headers to every request.
type headerTransport map[string]string

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h {
		r.Header.Set(k, v)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (f *fixture) connect(headers map[string]string) *mcp.ClientSession {
	f.t.Helper()
	h := headerTransport{"Authorization": "Bearer " + f.token}
	for k, v := range headers {
		h[k] = v
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: f.srv.URL(), HTTPClient: &http.Client{Transport: h}, DisableStandaloneSSE: true}, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { cs.Close() })
	return cs
}

func (f *fixture) call(cs *mcp.ClientSession, tool string, args any) (string, bool) {
	f.t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		f.t.Fatal(err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func TestToolCountEqualsTheRegistryCount(t *testing.T) {
	f := newFixture(t)
	cs := f.connect(nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tools) != f.reg.Len() || len(res.Tools) == 0 {
		t.Fatalf("%d tools served, %d actions registered", len(res.Tools), f.reg.Len())
	}
	names := map[string]bool{}
	for _, tool := range res.Tools {
		names[tool.Name] = true
	}
	for _, n := range f.reg.Names() {
		if !names[n] {
			t.Errorf("action %s has no tool", n)
		}
	}
	// The schema comes from the In struct: card_move needs a card and a column.
	for _, tool := range res.Tools {
		if tool.Name != "card_move" {
			continue
		}
		b, _ := json.Marshal(tool.InputSchema)
		if !strings.Contains(string(b), `"column_id"`) || !strings.Contains(string(b), `"required"`) {
			t.Errorf("card_move schema = %s", b)
		}
	}
}

func TestACallWithoutTheTokenGets401(t *testing.T) {
	f := newFixture(t)
	for _, auth := range []string{"", "Bearer wrong", "Bearer "} {
		req, _ := http.NewRequest("POST", f.srv.URL(), strings.NewReader(`{}`))
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("auth %q: status %d, want 401", auth, resp.StatusCode)
		}
	}
}

func TestAHostOrOriginThatIsNotOursIsRefused(t *testing.T) {
	f := newFixture(t)
	for name, mod := range map[string]func(*http.Request){
		"foreign host":   func(r *http.Request) { r.Host = "evil.example:" + itoa(f.srv.Port()) },
		"wrong port":     func(r *http.Request) { r.Host = "127.0.0.1:1" },
		"browser origin": func(r *http.Request) { r.Header.Set("Origin", "http://localhost:3000") },
	} {
		req, _ := http.NewRequest("POST", f.srv.URL(), strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+f.token)
		mod(req)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("%s: status %d, want 403", name, resp.StatusCode)
		}
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestActorComesFromTheSessionHeader(t *testing.T) {
	f := newFixture(t)
	cases := []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"card session", map[string]string{SessionHeader: f.cardS.ID}, "session:billing-fix"},
		{"planner session", map[string]string{SessionHeader: f.planS.ID}, "planner"},
		{"no header", nil, "external"},
		{"unknown session id", map[string]string{SessionHeader: "no-such-session"}, "external"},
	}
	for _, c := range cases {
		cs := f.connect(c.headers)
		if out, isErr := f.call(cs, "card_update", map[string]any{"card_id": f.card.ID, "stage": c.name}); isErr {
			t.Fatalf("%s: %s", c.name, out)
		}
		calls, err := f.st.MCPCalls.List(f.ws.ID, 1)
		if err != nil || len(calls) == 0 {
			t.Fatalf("%s: no mcp_calls row (%v)", c.name, err)
		}
		if calls[0].Actor != c.want || calls[0].Tool != "card_update" || !calls[0].OK {
			t.Errorf("%s: logged actor=%q tool=%q ok=%v, want actor %q", c.name, calls[0].Actor, calls[0].Tool, calls[0].OK, c.want)
		}
	}
}

func TestAReadFromMCPIsLoggedAndAFailureComesBackAsAToolError(t *testing.T) {
	f := newFixture(t)
	cs := f.connect(map[string]string{SessionHeader: f.cardS.ID})
	if out, isErr := f.call(cs, "card_get", map[string]any{"card_id": f.card.ID}); isErr || !strings.Contains(out, "billing-fix") {
		t.Fatalf("card_get: %v %s", isErr, out)
	}
	out, isErr := f.call(cs, "card_get", map[string]any{"card_id": "nope"})
	if !isErr || !strings.Contains(out, "does not exist") {
		t.Fatalf("want a tool error that says what to fix, got %v %q", isErr, out)
	}
	calls, _ := f.st.MCPCalls.List(f.ws.ID, 5)
	var ok, failed bool
	for _, c := range calls {
		ok = ok || (c.Tool == "card_get" && c.OK && c.Actor == "session:billing-fix")
	}
	all, _ := f.st.MCPCalls.List("", 10)
	for _, c := range all {
		failed = failed || (c.Tool == "card_get" && !c.OK && c.Error != "")
	}
	if !ok || !failed {
		t.Errorf("reads should be logged with their outcome: ok=%v failed=%v", ok, failed)
	}
}

func TestTheUIReadsAreNotLogged(t *testing.T) {
	f := newFixture(t)
	before, _ := f.st.MCPCalls.List("", 500)
	if _, err := f.reg.Invoke(context.Background(), actions.You, "card_list", `{}`); err != nil {
		t.Fatal(err)
	}
	after, _ := f.st.MCPCalls.List("", 500)
	if len(after) != len(before) {
		t.Errorf("a UI read wrote %d rows", len(after)-len(before))
	}
}

func TestPortFallbackAndTheInfoFile(t *testing.T) {
	f := newFixture(t)
	// Take the port the first server holds; a second server must fall back to a free one.
	second := New(Options{Registry: f.reg, Store: f.st, Home: t.TempDir(), Port: f.srv.Port()})
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.Port() == f.srv.Port() || second.Port() == 0 {
		t.Fatalf("port = %d, first = %d", second.Port(), f.srv.Port())
	}
	var info Info
	b, err := os.ReadFile(filepath.Join(second.opts.Home, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(b, &info)
	if info.Port != second.Port() || info.URL != second.URL() {
		t.Errorf("mcp.json = %+v, server on %d", info, second.Port())
	}
	if st, _ := os.Stat(filepath.Join(second.opts.Home, "mcp.json")); st.Mode().Perm() != 0o600 {
		t.Errorf("mcp.json mode = %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(filepath.Join(second.opts.Home, "mcp-token")); st.Mode().Perm() != 0o600 {
		t.Errorf("mcp-token mode = %v", st.Mode().Perm())
	}
}

func TestPreferredPortOrderOptionEnvSettingDefault(t *testing.T) {
	f := newFixture(t)
	s := New(Options{Registry: f.reg, Store: f.st, Home: f.home})
	if got := s.preferredPort(); got != DefaultPort {
		t.Errorf("default = %d", got)
	}
	if err := f.st.Settings.Set(SettingPort, json.RawMessage("51111")); err != nil {
		t.Fatal(err)
	}
	if got := s.preferredPort(); got != 51111 {
		t.Errorf("setting = %d", got)
	}
	t.Setenv("HADRON_MCP_PORT", "52222")
	if got := s.preferredPort(); got != 52222 {
		t.Errorf("env = %d", got)
	}
	s.opts.Port = 53333
	if got := s.preferredPort(); got != 53333 {
		t.Errorf("option = %d", got)
	}
}
