// Package script runs integrations and user scripts in a goja sandbox. One runtime serves one call.
// The code reaches the outside world only through the host object, which checks every call
// against the manifest's permissions and every input and output against a JSON Schema.
package script

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

//go:embed manifest.schema.json
var manifestSchemaJSON []byte

// ManifestSchema returns the manifest's JSON Schema, for tools that help write one.
func ManifestSchema() json.RawMessage { return append(json.RawMessage(nil), manifestSchemaJSON...) }

// Manifest declares what an integration is and what it may do.
type Manifest struct {
	Name        string               `json:"name"`
	Version     string               `json:"version"`
	Description string               `json:"description,omitempty"`
	Fields      []FieldDef           `json:"fields,omitempty"`
	Ops         Ops                  `json:"ops,omitempty"`
	Config      map[string]ConfigDef `json:"config,omitempty"`
	Permissions Permissions          `json:"permissions,omitempty"`
	PollSeconds int                  `json:"poll_seconds,omitempty"`
	Tests       []TestCase           `json:"tests,omitempty"`
}

// FieldDef is a field the integration contributes.
type FieldDef struct {
	Name       string   `json:"name"`
	Label      string   `json:"label,omitempty"`
	Type       string   `json:"type"`
	Options    []string `json:"options,omitempty"`
	Filterable bool     `json:"filterable,omitempty"`
	Editable   bool     `json:"editable,omitempty"`
	CardChip   bool     `json:"card_chip,omitempty"`
}

// OpSpec describes one op: an optional description and JSON Schemas that replace the defaults.
type OpSpec struct {
	Description string          `json:"description,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	Output      json.RawMessage `json:"output,omitempty"`
	// TimeoutS is how long one run of a query may take, 1 to 180 seconds (queries only; default
	// DefaultTimeout). Queries run in the background and are cached, so they may take longer.
	TimeoutS int `json:"timeout_s,omitempty"`
}

// Query time budget limits.
const MaxQueryTimeoutS = 180

// Ops lists the ops an integration implements.
type Ops struct {
	Search   *OpSpec           `json:"search,omitempty"`
	Get      *OpSpec           `json:"get,omitempty"`
	Update   *OpSpec           `json:"update,omitempty"`
	Poll     *OpSpec           `json:"poll,omitempty"`
	Actions  map[string]OpSpec `json:"actions,omitempty"`
	Sessions *SessionOps       `json:"sessions,omitempty"`
	// Queries are named dashboard queries: each ops.queries.<name> returns {groups:[{key,label,count}]}
	// and registers the query <integration>.<name> for dashboard blocks.
	Queries map[string]OpSpec `json:"queries,omitempty"`
}

// SessionOps are the ops of an integration that runs remote (cloud agent) sessions.
type SessionOps struct {
	List   *OpSpec `json:"list,omitempty"`
	Output *OpSpec `json:"output,omitempty"`
	Send   *OpSpec `json:"send,omitempty"`
	Start  *OpSpec `json:"start,omitempty"`
	Stop   *OpSpec `json:"stop,omitempty"`
}

// ConfigDef is one setting the user fills in.
type ConfigDef struct {
	Type        string `json:"type"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     any    `json:"default,omitempty"`
}

// Permissions is everything the code may touch. The sandbox refuses the rest.
type Permissions struct {
	Network []string `json:"network,omitempty"`
	Secrets []string `json:"secrets,omitempty"`
	API     []string `json:"api,omitempty"`
	Exec    []string `json:"exec,omitempty"`
}

// TestCase is one recorded test of an op.
type TestCase struct {
	Name         string         `json:"name,omitempty"`
	Op           string         `json:"op"`
	Input        any            `json:"input,omitempty"`
	Config       map[string]any `json:"config,omitempty"`
	HTTPFixtures []HTTPFixture  `json:"http_fixtures,omitempty"`
	ExecFixtures []ExecFixture  `json:"exec_fixtures,omitempty"`
	MCPFixtures  []MCPFixture   `json:"mcp_fixtures,omitempty"`
	Expect       any            `json:"expect,omitempty"`
}

// HTTPFixture answers one http.request in a test.
type HTTPFixture struct {
	Method  string         `json:"method,omitempty"`
	URL     string         `json:"url"`
	Status  int            `json:"status,omitempty"`
	Headers map[string]any `json:"headers,omitempty"`
	JSON    any            `json:"json,omitempty"`
	Text    string         `json:"text,omitempty"`
}

// ExecFixture answers one exec in a test. Args, when given, must equal the call's arguments.
type ExecFixture struct {
	Binary string   `json:"binary"`
	Args   []string `json:"args,omitempty"`
	Stdout string   `json:"stdout,omitempty"`
	Stderr string   `json:"stderr,omitempty"`
	Code   int      `json:"code,omitempty"`
}

// MCPFixture answers one mcp.call in a test.
type MCPFixture struct {
	Server string `json:"server"`
	Tool   string `json:"tool"`
	Result any    `json:"result,omitempty"`
}

var schemaOnce struct {
	rs  *jsonschema.Resolved
	err error
}

func manifestResolved() (*jsonschema.Resolved, error) {
	if schemaOnce.rs == nil && schemaOnce.err == nil {
		var s jsonschema.Schema
		if err := json.Unmarshal(manifestSchemaJSON, &s); err != nil {
			schemaOnce.err = err
		} else {
			schemaOnce.rs, schemaOnce.err = s.Resolve(nil)
		}
	}
	return schemaOnce.rs, schemaOnce.err
}

// ParseManifest decodes and checks a manifest. The problems list says everything to fix, not just
// the first thing.
func ParseManifest(raw []byte) (Manifest, []string) {
	var m Manifest
	var generic any
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&generic); err != nil {
		return m, []string{"manifest.json is not valid JSON: " + err.Error()}
	}
	rs, err := manifestResolved()
	if err != nil {
		return m, []string{"internal error: the manifest schema does not load: " + err.Error()}
	}
	var problems []string
	if err := rs.Validate(generic); err != nil {
		for _, line := range strings.Split(err.Error(), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				problems = append(problems, "manifest: "+line)
			}
		}
		return m, problems
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, []string{"manifest: " + err.Error()}
	}
	problems = append(problems, m.check()...)
	return m, problems
}

func (m Manifest) check() []string {
	var p []string
	seen := map[string]bool{}
	for qn, q := range m.Ops.Queries {
		if q.TimeoutS != 0 && (q.TimeoutS < 1 || q.TimeoutS > MaxQueryTimeoutS) {
			p = append(p, fmt.Sprintf("manifest: ops.queries.%s.timeout_s must be between 1 and %d seconds, got %d", qn, MaxQueryTimeoutS, q.TimeoutS))
		}
		if qn == "links" || qn == "count" {
			p = append(p, fmt.Sprintf("manifest: ops.queries.%s is a reserved name: every integration already has <integration>.%s. Pick another name", qn, qn))
		}
	}
	for _, f := range m.Fields {
		if seen[f.Name] {
			p = append(p, fmt.Sprintf("manifest: field %q is listed twice", f.Name))
		}
		seen[f.Name] = true
		if f.Type == "enum" && len(f.Options) == 0 {
			p = append(p, fmt.Sprintf("manifest: enum field %q needs options", f.Name))
		}
		if f.Editable && m.Ops.Update == nil {
			p = append(p, fmt.Sprintf("manifest: field %q is editable, so the manifest needs an ops.update", f.Name))
		}
	}
	for _, pat := range m.Permissions.Network {
		if _, err := ParsePattern(pat); err != nil {
			p = append(p, "manifest: permissions.network: "+err.Error())
		}
	}
	for _, t := range m.Tests {
		if _, ok := m.OpSpec(t.Op); !ok {
			p = append(p, fmt.Sprintf("manifest: test %q runs op %q, which ops does not declare", t.Name, t.Op))
		}
	}
	return p
}

// OpNames lists every op the manifest declares, as dotted paths: search, actions.transition, sessions.list.
func (m Manifest) OpNames() []string {
	var out []string
	add := func(name string, s *OpSpec) {
		if s != nil {
			out = append(out, name)
		}
	}
	add("search", m.Ops.Search)
	add("get", m.Ops.Get)
	add("update", m.Ops.Update)
	add("poll", m.Ops.Poll)
	names := make([]string, 0, len(m.Ops.Actions))
	for n := range m.Ops.Actions {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, "actions."+n)
	}
	qnames := make([]string, 0, len(m.Ops.Queries))
	for n := range m.Ops.Queries {
		qnames = append(qnames, n)
	}
	sort.Strings(qnames)
	for _, n := range qnames {
		out = append(out, "queries."+n)
	}
	if s := m.Ops.Sessions; s != nil {
		add("sessions.list", s.List)
		add("sessions.output", s.Output)
		add("sessions.send", s.Send)
		add("sessions.start", s.Start)
		add("sessions.stop", s.Stop)
	}
	return out
}

// OpSpec finds the declaration of a dotted op path.
func (m Manifest) OpSpec(op string) (OpSpec, bool) {
	var s *OpSpec
	switch op {
	case "search":
		s = m.Ops.Search
	case "get":
		s = m.Ops.Get
	case "update":
		s = m.Ops.Update
	case "poll":
		s = m.Ops.Poll
	default:
		if name, ok := strings.CutPrefix(op, "actions."); ok {
			if a, found := m.Ops.Actions[name]; found {
				return a, true
			}
			return OpSpec{}, false
		}
		if name, ok := strings.CutPrefix(op, "queries."); ok {
			if q, found := m.Ops.Queries[name]; found {
				return q, true
			}
			return OpSpec{}, false
		}
		if name, ok := strings.CutPrefix(op, "sessions."); ok && m.Ops.Sessions != nil {
			sess := m.Ops.Sessions
			s = map[string]*OpSpec{"list": sess.List, "output": sess.Output, "send": sess.Send, "start": sess.Start, "stop": sess.Stop}[name]
		}
	}
	if s == nil {
		return OpSpec{}, false
	}
	return *s, true
}

// HasOp says whether the manifest declares op.
func (m Manifest) HasOp(op string) bool { _, ok := m.OpSpec(op); return ok }

// SamePermissions says whether two manifests ask for exactly the same permissions.
func SamePermissions(a, b Permissions) bool {
	norm := func(p Permissions) string {
		for _, l := range []*[]string{&p.Network, &p.Secrets, &p.API, &p.Exec} {
			c := append([]string(nil), *l...)
			sort.Strings(c)
			*l = c
		}
		out, _ := json.Marshal(p)
		return string(out)
	}
	return norm(a) == norm(b)
}

// Domains lists the hosts the network permissions reach, and "mcp:<server>" entries as they are.
func (p Permissions) Domains() []string {
	seen := map[string]bool{}
	var out []string
	for _, pat := range p.Network {
		d := pat
		if pp, err := ParsePattern(pat); err == nil && pp.mcp == "" {
			d = pp.scheme + "://" + pp.hostRaw
		}
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// ---- URL patterns ----

// Pattern is a parsed network permission.
type Pattern struct {
	raw     string
	mcp     string // set for mcp:<server>
	scheme  string
	hostRaw string
	host    *regexp.Regexp
	path    *regexp.Regexp
}

func globRE(glob, star string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^")
	parts := strings.Split(glob, "*")
	for i, part := range parts {
		if i > 0 {
			b.WriteString(star)
		}
		b.WriteString(regexp.QuoteMeta(part))
	}
	b.WriteString("$")
	return regexp.MustCompile(b.String())
}

// ParsePattern parses scheme://host/path with * wildcards. In the host, * never crosses a slash,
// an @ or a ?, so https://*.example.com/* cannot be satisfied by https://evil.com/.example.com/.
func ParsePattern(raw string) (Pattern, error) {
	if server, ok := strings.CutPrefix(raw, "mcp:"); ok {
		if server == "" {
			return Pattern{}, fmt.Errorf("%q needs a server name after mcp:", raw)
		}
		return Pattern{raw: raw, mcp: server}, nil
	}
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok || (scheme != "http" && scheme != "https") {
		return Pattern{}, fmt.Errorf("%q must look like https://host/path (use * as a wildcard) or mcp:<server>", raw)
	}
	host, path, _ := strings.Cut(rest, "/")
	if host == "" || strings.ContainsAny(host, "@?#") {
		return Pattern{}, fmt.Errorf("%q has no valid host", raw)
	}
	if host == "*" {
		return Pattern{}, fmt.Errorf("%q allows every host; name the host", raw)
	}
	return Pattern{raw: raw, scheme: scheme, hostRaw: strings.ToLower(host), host: globRE(strings.ToLower(host), "[^/@?#]*"), path: globRE("/"+path, ".*")}, nil
}

// String is the pattern as written.
func (p Pattern) String() string { return p.raw }

// MatchURL says whether the URL satisfies the pattern.
func (p Pattern) MatchURL(raw string) bool {
	if p.mcp != "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Scheme != p.scheme {
		return false
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	if u.RawQuery != "" {
		path += "?" + u.RawQuery
	}
	return p.host.MatchString(strings.ToLower(u.Host)) && p.path.MatchString(path)
}

// AllowsURL says whether any network permission lets the code call the URL.
func (p Permissions) AllowsURL(raw string) bool {
	for _, s := range p.Network {
		if pat, err := ParsePattern(s); err == nil && pat.MatchURL(raw) {
			return true
		}
	}
	return false
}

// AllowsMCP says whether the code may call the MCP server.
func (p Permissions) AllowsMCP(server string) bool {
	for _, s := range p.Network {
		if s == "mcp:"+server {
			return true
		}
	}
	return false
}

func (p Permissions) has(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
