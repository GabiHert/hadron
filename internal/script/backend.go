package script

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HTTPRequest is a request after the sandbox resolved secrets into headers.
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

// HTTPResponse is what http.request returns.
type HTTPResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// ExecResult is what exec returns.
type ExecResult struct {
	Stdout, Stderr string
	Code           int
}

// Backend does the real work behind http.request, exec and mcp.call. Tests and recorded tests
// swap it for fixtures.
type Backend interface {
	HTTP(ctx context.Context, req HTTPRequest, allowed func(url string) bool) (HTTPResponse, error)
	Exec(ctx context.Context, binary string, args []string) (ExecResult, error)
	MCP(ctx context.Context, server, tool string, args any) (any, error)
}

const (
	maxBody     = 5 << 20
	httpTimeout = 30 * time.Second
	// ExecTimeout is how long an exec may run.
	ExecTimeout = 20 * time.Second
)

// Live is the real Backend.
type Live struct {
	Client *http.Client
	// Env returns the environment exec'd programs get (the login shell's, so gh finds its config).
	// It may be nil.
	Env func() []string
	// ClaudeConfig is the file that lists the user's MCP servers; default ~/.claude.json.
	ClaudeConfig string
}

// HTTP implements Backend. A redirect is followed only to a URL the permissions allow and on the
// same host, so a credential header never travels to another site.
func (l Live) HTTP(ctx context.Context, req HTTPRequest, allowed func(string) bool) (HTTPResponse, error) {
	client := l.Client
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	c.Timeout = httpTimeout
	c.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if r.URL.Host != via[0].URL.Host || !allowed(r.URL.String()) {
			return &PermissionError{Permission: "network: " + r.URL.String(), Message: "the server redirected to " + r.URL.String() + ", which the network permissions do not allow"}
		}
		return nil
	}
	hr, err := http.NewRequestWithContext(ctx, req.Method, req.URL, bytes.NewReader(req.Body))
	if err != nil {
		return HTTPResponse{}, err
	}
	for k, v := range req.Headers {
		hr.Header.Set(k, v)
	}
	resp, err := c.Do(hr)
	if err != nil {
		var pe *PermissionError
		if errors.As(err, &pe) {
			return HTTPResponse{}, pe
		}
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return HTTPResponse{}, err
	}
	if len(body) > maxBody {
		return HTTPResponse{}, fmt.Errorf("the response is larger than %d MB", maxBody>>20)
	}
	out := HTTPResponse{Status: resp.StatusCode, Headers: map[string]string{}, Body: body}
	for k := range resp.Header {
		out.Headers[strings.ToLower(k)] = resp.Header.Get(k)
	}
	return out, nil
}

// Exec implements Backend. There is no shell: the binary runs with the arguments as given.
func (l Live) Exec(ctx context.Context, binary string, args []string) (ExecResult, error) {
	ctx, cancel := context.WithTimeout(ctx, ExecTimeout)
	defer cancel()
	// Resolve the binary on the login shell's PATH before building the command: a GUI app's own
	// PATH lacks Homebrew and the like, and exec.Command records a failed lookup in cmd.Err, which
	// Run returns even if cmd.Path is set afterwards.
	env := os.Environ()
	if l.Env != nil {
		if e := l.Env(); len(e) > 0 {
			env = e
		}
	}
	name := binary
	if p := envValue(env, "PATH"); p != "" {
		if full := lookIn(binary, p); full != "" {
			name = full
		}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limited{b: &stdout}, &limited{b: &stderr}
	err := cmd.Run()
	res := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.Code = ee.ExitCode()
	case ctx.Err() != nil:
		return res, fmt.Errorf("%s ran longer than %s and was stopped", binary, ExecTimeout)
	default:
		return res, err
	}
	return res, nil
}

type limited struct{ b *bytes.Buffer }

func (w *limited) Write(p []byte) (int, error) {
	if w.b.Len()+len(p) > maxBody {
		return 0, errors.New("output is larger than 5 MB")
	}
	return w.b.Write(p)
}

func envValue(env []string, key string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v
		}
	}
	return ""
}

func lookIn(binary, path string) string {
	if strings.Contains(binary, "/") {
		return ""
	}
	for _, dir := range filepath.SplitList(path) {
		p := filepath.Join(dir, binary)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

type claudeServers struct {
	MCPServers map[string]struct {
		Type    string            `json:"type"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Command string            `json:"command"`
		Args    []string          `json:"args"`
		Env     map[string]string `json:"env"`
	} `json:"mcpServers"`
}

// MCP implements Backend: it connects to a server from the user's Claude config (stdio, or HTTP
// with static headers), calls one tool and disconnects. Servers that need an OAuth sign-in are
// not supported yet, because their tokens live in Claude's own storage.
func (l Live) MCP(ctx context.Context, server, tool string, args any) (any, error) {
	path := l.ClaudeConfig
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".claude.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mcp.call could not read your Claude config at %s: %w", path, err)
	}
	var cfg claudeServers
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	s, ok := cfg.MCPServers[server]
	if !ok {
		return nil, fmt.Errorf("your Claude config has no MCP server %q. Add it with claude mcp add", server)
	}
	var transport mcp.Transport
	switch {
	case s.Command != "":
		cmd := exec.CommandContext(ctx, s.Command, s.Args...)
		cmd.Env = os.Environ()
		for k, v := range s.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		transport = &mcp.CommandTransport{Command: cmd}
	case s.URL != "":
		client := &http.Client{Timeout: httpTimeout, Transport: headerTransport{h: s.Headers, next: http.DefaultTransport}}
		if s.Type == "sse" {
			transport = &mcp.SSEClientTransport{Endpoint: s.URL, HTTPClient: client}
		} else {
			transport = &mcp.StreamableClientTransport{Endpoint: s.URL, HTTPClient: client, DisableStandaloneSSE: true}
		}
	default:
		return nil, fmt.Errorf("MCP server %q in your Claude config has neither a command nor a url", server)
	}
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	cli := mcp.NewClient(&mcp.Implementation{Name: "hadron", Version: "1"}, nil)
	sess, err := cli.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("could not connect to MCP server %q (servers that need an OAuth sign-in are not supported yet): %w", server, err)
	}
	defer sess.Close()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return nil, err
	}
	return MCPResult(res), nil
}

type headerTransport struct {
	h    map[string]string
	next http.RoundTripper
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range t.h {
		r.Header.Set(k, v)
	}
	return t.next.RoundTrip(r)
}

// MCPResult flattens a tool result into {text, json?, is_error}.
func MCPResult(res *mcp.CallToolResult) map[string]any {
	out := map[string]any{"is_error": res.IsError}
	var texts []string
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			texts = append(texts, t.Text)
		}
	}
	text := strings.Join(texts, "\n")
	out["text"] = text
	if res.StructuredContent != nil {
		out["json"] = res.StructuredContent
	} else {
		var v any
		if json.Unmarshal([]byte(text), &v) == nil {
			out["json"] = v
		}
	}
	return out
}

// ---- fixtures ----

// Fixtures is a Backend that answers from recorded fixtures. Each fixture answers one call, in
// order of first match; a call with no fixture left is an error that names it.
type Fixtures struct {
	HTTPs []HTTPFixture
	Execs []ExecFixture
	MCPs  []MCPFixture
	used  map[any]bool
}

// FixturesOf builds a Backend from a test case.
func FixturesOf(t TestCase) *Fixtures {
	return &Fixtures{HTTPs: t.HTTPFixtures, Execs: t.ExecFixtures, MCPs: t.MCPFixtures, used: map[any]bool{}}
}

// HTTP implements Backend.
func (f *Fixtures) HTTP(_ context.Context, req HTTPRequest, _ func(string) bool) (HTTPResponse, error) {
	for i := range f.HTTPs {
		fx := &f.HTTPs[i]
		if f.used[fx] || (fx.Method != "" && !strings.EqualFold(fx.Method, req.Method)) {
			continue
		}
		if pat, err := ParsePattern(fx.URL); err != nil || !pat.MatchURL(req.URL) {
			continue
		}
		f.used[fx] = true
		resp := HTTPResponse{Status: fx.Status, Headers: map[string]string{}}
		if resp.Status == 0 {
			resp.Status = 200
		}
		for k, v := range fx.Headers {
			resp.Headers[strings.ToLower(k)] = fmt.Sprint(v)
		}
		switch {
		case fx.JSON != nil:
			resp.Body, _ = json.Marshal(fx.JSON)
		default:
			resp.Body = []byte(fx.Text)
		}
		return resp, nil
	}
	return HTTPResponse{}, fmt.Errorf("the test has no http_fixture left for %s %s", req.Method, req.URL)
}

// Exec implements Backend.
func (f *Fixtures) Exec(_ context.Context, binary string, args []string) (ExecResult, error) {
	for i := range f.Execs {
		fx := &f.Execs[i]
		if f.used[fx] || fx.Binary != binary || (fx.Args != nil && !reflect.DeepEqual(fx.Args, args)) {
			continue
		}
		f.used[fx] = true
		return ExecResult{Stdout: fx.Stdout, Stderr: fx.Stderr, Code: fx.Code}, nil
	}
	return ExecResult{}, fmt.Errorf("the test has no exec_fixture left for %s %s", binary, strings.Join(args, " "))
}

// MCP implements Backend.
func (f *Fixtures) MCP(_ context.Context, server, tool string, _ any) (any, error) {
	for i := range f.MCPs {
		fx := &f.MCPs[i]
		if f.used[fx] || fx.Server != server || fx.Tool != tool {
			continue
		}
		f.used[fx] = true
		return fx.Result, nil
	}
	return nil, fmt.Errorf("the test has no mcp_fixture left for %s.%s", server, tool)
}
