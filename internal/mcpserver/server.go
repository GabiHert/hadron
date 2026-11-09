// Package mcpserver exposes the action registry to Claude Code over MCP: one tool per action,
// served as Streamable HTTP on 127.0.0.1 behind a bearer token. The session header says who is
// calling, so a card's session is logged and authorised as that card.
package mcpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/GabiHert/hadron/internal/actions"
	"github.com/GabiHert/hadron/internal/run"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DefaultPort is where the server listens unless a setting or HADRON_MCP_PORT says otherwise.
const DefaultPort = 47820

// SettingPort is the settings key of the preferred port.
const SettingPort = "mcp.port"

// SessionHeader names the caller's session. The server maps it to the planner or to a card.
const SessionHeader = "X-Hadron-Session"

// Options configure a Server.
type Options struct {
	// Registry holds the actions served as tools.
	Registry *actions.Registry
	// Store resolves session ids to cards and reads the port setting.
	Store *store.Store
	// Home holds mcp-token and mcp.json. Empty means run.Home().
	Home string
	// Port overrides every other source of the preferred port. Zero means HADRON_MCP_PORT, then
	// the mcp.port setting, then DefaultPort. Tests pass a free port.
	Port int
	// Version is shown in the server info.
	Version string
}

// Server is the MCP server.
type Server struct {
	opts  Options
	token string

	mu   sync.Mutex
	http *http.Server
	port int
}

// New returns a stopped server.
func New(opts Options) *Server {
	if opts.Home == "" {
		opts.Home = run.Home()
	}
	return &Server{opts: opts}
}

// Info is what mcp.json holds and the Settings page shows.
type Info struct {
	Port int    `json:"port"`
	URL  string `json:"url"`
	PID  int    `json:"pid"`
}

// Token returns the bearer token, creating it on first use.
func (s *Server) Token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadToken()
}

func (s *Server) loadToken() (string, error) {
	if s.token != "" {
		return s.token, nil
	}
	tok, err := run.EnsureMCPToken(s.tokenFile())
	if err != nil {
		return "", err
	}
	s.token = tok
	return tok, nil
}

func (s *Server) tokenFile() string { return filepath.Join(s.opts.Home, "mcp-token") }

// preferredPort resolves the port to try first.
func (s *Server) preferredPort() int {
	if s.opts.Port > 0 {
		return s.opts.Port
	}
	if v := os.Getenv("HADRON_MCP_PORT"); v != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 && n < 65536 {
			return n
		}
	}
	if s.opts.Store != nil {
		if raw, err := s.opts.Store.Settings.Get(SettingPort); err == nil {
			var n int
			if json.Unmarshal(raw, &n) == nil && n > 0 && n < 65536 {
				return n
			}
		}
	}
	return DefaultPort
}

// Start listens on 127.0.0.1 and serves in the background. When the preferred port is busy it
// takes a free one. Either way it records the port in mcp.json.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.http != nil {
		return nil
	}
	if _, err := s.loadToken(); err != nil {
		return fmt.Errorf("could not create the MCP token: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(s.preferredPort()))
	if err != nil {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("could not listen on 127.0.0.1: %w", err)
		}
	}
	s.port = ln.Addr().(*net.TCPAddr).Port
	if err := s.writeInfo(); err != nil {
		_ = ln.Close()
		s.port = 0
		return err
	}
	s.http = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: s.handler()}
	go func(srv *http.Server) { _ = srv.Serve(ln) }(s.http)
	return nil
}

func (s *Server) writeInfo() error {
	b, _ := json.MarshalIndent(Info{Port: s.port, URL: s.urlLocked(), PID: os.Getpid()}, "", "  ")
	path := filepath.Join(s.opts.Home, "mcp.json")
	if err := os.MkdirAll(s.opts.Home, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("could not record the MCP port: %w", err)
	}
	return os.Chmod(path, 0o600)
}

// Close stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	srv := s.http
	s.http, s.port = nil, 0
	s.mu.Unlock()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if srv.Shutdown(ctx) != nil {
		_ = srv.Close()
	}
}

// Port is the port the server listens on, or 0 when it is stopped.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// URL is the address MCP clients connect to; empty when stopped.
func (s *Server) URL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.urlLocked()
}

func (s *Server) urlLocked() string {
	if s.port == 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d/mcp", s.port)
}

// ToolCount is the number of tools served.
func (s *Server) ToolCount() int { return s.opts.Registry.Len() }

// handler serves /mcp behind the Host, Origin and token checks.
func (s *Server) handler() http.Handler {
	srv := s.mcpServer()
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	mux := http.NewServeMux()
	mux.Handle("/mcp", h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A web page must not reach this, even through DNS rebinding: only our own host and port,
		// and no browser origin.
		if !s.allowedHost(r.Host) || r.Header.Get("Origin") != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !s.authorized(r.Header.Get("Authorization")) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(header string) bool {
	got, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || got == "" {
		return false
	}
	want, err := s.Token()
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) allowedHost(hostport string) bool {
	port := s.Port()
	host, p, err := net.SplitHostPort(hostport)
	if err != nil || p != strconv.Itoa(port) {
		return false
	}
	return host == "127.0.0.1" || strings.EqualFold(host, "localhost")
}

// actorFor maps the session header to who is calling: the planner's session is the planner, a
// card's session is session:<nick>, and anything else is external.
func (s *Server) actorFor(header http.Header) actions.Actor {
	id := strings.TrimSpace(header.Get(SessionHeader))
	if id == "" || s.opts.Store == nil {
		return actions.Actor{Kind: actions.KindExternal}
	}
	sess, err := s.opts.Store.Sessions.Get(id)
	if err != nil {
		return actions.Actor{Kind: actions.KindExternal}
	}
	if sess.CardID == "" {
		return actions.Actor{Kind: actions.KindPlanner, WorkspaceID: sess.WorkspaceID}
	}
	card, err := s.opts.Store.Cards.Get(sess.CardID)
	if err != nil {
		return actions.Actor{Kind: actions.KindExternal}
	}
	return actions.Actor{Kind: actions.KindSession, Name: card.Nick, WorkspaceID: sess.WorkspaceID, CardID: card.ID}
}

const instructions = "Hadron runs Claude Code sessions as cards on a board. Every tool is one Hadron action. " +
	"Call app_status first. Errors say what to fix."

func (s *Server) mcpServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: "hadron", Version: s.opts.Version}, &mcp.ServerOptions{Instructions: instructions})
	for _, info := range s.opts.Registry.Infos() {
		s.addTool(srv, info)
	}
	return srv
}

func (s *Server) addTool(srv *mcp.Server, info actions.Info) {
	name := info.Name
	tool := &mcp.Tool{Name: name, Description: info.Description, InputSchema: inputSchema(info)}
	tool.Annotations = &mcp.ToolAnnotations{ReadOnlyHint: !info.Mutates}
	srv.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var hdr http.Header
		if x := req.GetExtra(); x != nil {
			hdr = x.Header
		}
		args := "{}"
		if req.Params != nil && len(req.Params.Arguments) > 0 {
			args = string(req.Params.Arguments)
		}
		out, err := s.opts.Registry.Invoke(ctx, s.actorFor(hdr), name, args)
		if err != nil {
			var ie *actions.InputError
			msg := err.Error()
			if errors.As(err, &ie) {
				msg = ie.Error()
			}
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: out}}}, nil
	})
}
