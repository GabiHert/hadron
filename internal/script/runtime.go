package script

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// Defaults of the sandbox.
const (
	DefaultTimeout     = 10 * time.Second
	DefaultMemoryLimit = 64 << 20
	maxLogLines        = 200
	maxCallStack       = 2000
)

// PermissionError is thrown into the code, and returned from Run when the code does not catch it,
// when it asks for something its manifest does not permit.
type PermissionError struct {
	// Permission names what was missing, such as "network: https://example.com/x" or "api: card.move".
	Permission string
	Message    string
}

func (e *PermissionError) Error() string {
	if e.Message != "" {
		return "PermissionError: " + e.Message
	}
	return "PermissionError: the manifest does not grant " + e.Permission
}

// TimeoutError says the code ran past the limit.
type TimeoutError struct{ After time.Duration }

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("the script ran longer than %s and was stopped. Make the loop smaller or call fewer APIs", e.After)
}

// MemoryError says the code used too much memory.
type MemoryError struct{ Limit uint64 }

func (e *MemoryError) Error() string {
	return fmt.Sprintf("the script used more than %d MB of memory and was stopped", e.Limit>>20)
}

// ScriptError is an exception the code threw.
type ScriptError struct {
	Name, Message, Stack string
}

func (e *ScriptError) Error() string {
	if e.Name == "" || e.Name == "Error" {
		return e.Message
	}
	return e.Name + ": " + e.Message
}

// SecretHandle is what secret(name) returns. It has no fields the code can read: the value is
// resolved by the host when the handle is used as auth or as a header value.
type SecretHandle struct{ name string }

// App is how the code reaches Hadron's data. Each entry is nil when the caller offers nothing, and
// the code then gets an error that says so. Inputs and outputs are plain JSON values.
type App struct {
	LinkUpsert        func(in map[string]any) (any, error)
	LinkList          func(in map[string]any) (any, error)
	FieldSet          func(card, path string, value any) error
	CardRead          func(card string) (any, error)
	CardMove          func(card, column string) error
	CardList          func(in map[string]any) (any, error)
	SessionListRemote func(in map[string]any) (any, error)
	Notify            func(title, body string)
}

// Runner runs ops. Zero limits mean the defaults.
type Runner struct {
	Backend     Backend
	Secrets     Secrets
	Timeout     time.Duration
	MemoryLimit uint64
}

// Call is one op run.
type Call struct {
	Name     string // the integration or script name
	Manifest Manifest
	Source   string
	Op       string // dotted path: search, actions.transition, sessions.list
	Input    json.RawMessage
	Config   map[string]any
	App      App
}

// Timeout is the time one run may take. For a query it is the manifest's timeout_s when set.
func (c Call) timeoutOr(def time.Duration) time.Duration {
	if spec, ok := c.Manifest.OpSpec(c.Op); ok && strings.HasPrefix(c.Op, "queries.") && spec.TimeoutS > 0 {
		return time.Duration(spec.TimeoutS) * time.Second
	}
	return def
}

// Result is what a run produced.
type Result struct {
	Output   json.RawMessage
	Logs     []string
	Duration time.Duration
}

type sandbox struct {
	rt     *goja.Runtime
	r      *Runner
	c      Call
	ctx    context.Context
	parse  goja.Callable
	mu     sync.Mutex
	inHost bool
	until  time.Time
	seen   []string
	logs   []string
	reason string
}

// CheckSyntax compiles the source without running it.
func CheckSyntax(source string) error {
	_, err := goja.Compile("index.js", source, false)
	if err != nil {
		return fmt.Errorf("index.js: %s", strings.TrimPrefix(err.Error(), "SyntaxError: "))
	}
	return nil
}

// Run executes one op in a fresh runtime. It validates the input and the output, enforces the
// permissions, the time and the memory limits, and keeps every secret value out of the output,
// the logs and the error.
func (r *Runner) Run(ctx context.Context, c Call) (res Result, err error) {
	started := time.Now()
	s := &sandbox{r: r, c: c, ctx: ctx}
	defer func() {
		res.Duration = time.Since(started)
		res.Logs = s.logs
		if err != nil {
			err = s.scrubError(err)
		}
	}()
	if !c.Manifest.HasOp(c.Op) {
		return res, fmt.Errorf("%s does not declare the op %q. It declares: %s", c.Name, c.Op, strings.Join(c.Manifest.OpNames(), ", "))
	}
	inSchema, outSchema := SchemaFor(c.Manifest, c.Op)
	input := c.Input
	if len(bytes.TrimSpace(input)) == 0 {
		input = json.RawMessage("{}")
	}
	if verr := Validate(inSchema, input); verr != nil {
		return res, &ValidationError{Op: c.Op, Where: "input", Detail: verr.Error()}
	}
	timeout, limit := r.Timeout, r.MemoryLimit
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	timeout = c.timeoutOr(timeout)
	if limit == 0 {
		limit = DefaultMemoryLimit
	}
	s.rt = goja.New()
	s.rt.SetMaxCallStackSize(maxCallStack)
	s.until = time.Now().Add(timeout)
	if err := s.install(); err != nil {
		return res, err
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.watch(stop, timeout, limit)
	}()
	defer func() { close(stop); wg.Wait() }()

	out, err := s.invoke(input)
	if err != nil {
		var ie *goja.InterruptedError
		if errors.As(err, &ie) {
			switch s.reason {
			case "memory":
				return res, &MemoryError{Limit: limit}
			case "cancelled":
				return res, context.Canceled
			}
			return res, &TimeoutError{After: timeout}
		}
		return res, err
	}
	out = s.scrubBytes(out)
	if verr := Validate(outSchema, out); verr != nil {
		return res, &ValidationError{Op: c.Op, Where: "output", Detail: verr.Error()}
	}
	res.Output = out
	return res, nil
}

func (s *sandbox) interrupt(reason string) {
	s.mu.Lock()
	if s.reason == "" {
		s.reason = reason
	}
	s.mu.Unlock()
	s.rt.Interrupt(reason)
}

// watch interrupts the code when it runs too long (time spent inside host calls does not count,
// since those have their own limits), when the caller gives up, or when the heap grows past the
// limit since the call began.
func (s *sandbox) watch(stop <-chan struct{}, timeout time.Duration, limit uint64) {
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	n := 0
	hard := time.Now().Add(10 * timeout)
	for {
		select {
		case <-stop:
			return
		case <-s.ctx.Done():
			s.interrupt("cancelled")
			return
		case <-tick.C:
		}
		s.mu.Lock()
		over := !s.inHost && time.Now().After(s.until)
		s.mu.Unlock()
		if over || time.Now().After(hard) {
			s.interrupt("timeout")
			return
		}
		if n++; n%4 == 0 {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > base.HeapAlloc && m.HeapAlloc-base.HeapAlloc > limit {
				s.interrupt("memory")
				return
			}
		}
	}
}

const prelude = `
class PermissionError extends Error {
  constructor(message, permission) { super(message); this.name = 'PermissionError'; this.permission = permission; }
}
function __stringify(v) { var s = JSON.stringify(v); return s === undefined ? 'null' : s; }
`

func (s *sandbox) install() error {
	rt := s.rt
	if _, err := rt.RunString(prelude); err != nil {
		return err
	}
	parse, ok := goja.AssertFunction(rt.Get("JSON").ToObject(rt).Get("parse"))
	if !ok {
		return errors.New("JSON.parse is missing")
	}
	s.parse = parse

	host := rt.NewObject()
	set := func(o *goja.Object, name string, f func(call goja.FunctionCall) (any, error)) {
		_ = o.Set(name, func(call goja.FunctionCall) goja.Value {
			v, err := f(call)
			if err != nil {
				s.throw(err)
			}
			if gv, ok := v.(goja.Value); ok {
				return gv
			}
			return s.toJS(v)
		})
	}
	httpObj, mcpObj, linkObj, fieldObj, cardObj, sessObj := rt.NewObject(), rt.NewObject(), rt.NewObject(), rt.NewObject(), rt.NewObject(), rt.NewObject()
	set(httpObj, "request", func(call goja.FunctionCall) (any, error) { return s.httpRequest(call) })
	set(mcpObj, "call", func(call goja.FunctionCall) (any, error) { return s.mcpCall(call) })
	set(host, "exec", func(call goja.FunctionCall) (any, error) { return s.execBin(call) })
	set(host, "secret", func(call goja.FunctionCall) (any, error) { return s.secret(call) })
	set(linkObj, "upsert", func(call goja.FunctionCall) (any, error) {
		return s.api("link.upsert", call, func(in map[string]any) (any, error) {
			if s.c.App.LinkUpsert == nil {
				return nil, errNoApp("link.upsert")
			}
			return s.c.App.LinkUpsert(in)
		})
	})
	set(linkObj, "list", func(call goja.FunctionCall) (any, error) {
		return s.api("link.list", call, func(in map[string]any) (any, error) {
			if s.c.App.LinkList == nil {
				return nil, errNoApp("link.list")
			}
			return s.c.App.LinkList(in)
		})
	})
	set(fieldObj, "set", func(call goja.FunctionCall) (any, error) {
		return s.api("field.set", call, func(in map[string]any) (any, error) {
			if s.c.App.FieldSet == nil {
				return nil, errNoApp("field.set")
			}
			return nil, s.c.App.FieldSet(fmt.Sprint(in["card"]), fmt.Sprint(in["path"]), in["value"])
		})
	})
	set(cardObj, "read", func(call goja.FunctionCall) (any, error) {
		return s.api("card.read", call, func(in map[string]any) (any, error) {
			if s.c.App.CardRead == nil {
				return nil, errNoApp("card.read")
			}
			return s.c.App.CardRead(fmt.Sprint(in["card"]))
		})
	})
	set(cardObj, "move", func(call goja.FunctionCall) (any, error) {
		return s.api("card.move", call, func(in map[string]any) (any, error) {
			if s.c.App.CardMove == nil {
				return nil, errNoApp("card.move")
			}
			return nil, s.c.App.CardMove(fmt.Sprint(in["card"]), fmt.Sprint(in["column"]))
		})
	})
	set(cardObj, "list", func(call goja.FunctionCall) (any, error) {
		return s.api("card.list", call, func(in map[string]any) (any, error) {
			if s.c.App.CardList == nil {
				return nil, errNoApp("card.list")
			}
			return s.c.App.CardList(in)
		})
	})
	set(sessObj, "list_remote", func(call goja.FunctionCall) (any, error) {
		return s.api("session.list_remote", call, func(in map[string]any) (any, error) {
			if s.c.App.SessionListRemote == nil {
				return nil, errNoApp("session.list_remote")
			}
			return s.c.App.SessionListRemote(in)
		})
	})
	set(host, "notify", func(call goja.FunctionCall) (any, error) {
		return s.api("notify", call, func(in map[string]any) (any, error) {
			if s.c.App.Notify == nil {
				return nil, errNoApp("notify")
			}
			title, _ := in["title"].(string)
			s.c.App.Notify(s.scrub(title), s.scrub(fmt.Sprint(in["body"])))
			return nil, nil
		})
	})
	set(host, "log", func(call goja.FunctionCall) (any, error) {
		parts := make([]string, len(call.Arguments))
		for i, a := range call.Arguments {
			if str, isStr := a.Export().(string); isStr {
				parts[i] = str
			} else if b, err := json.Marshal(hideHandles(a.Export())); err == nil {
				parts[i] = string(b)
			}
		}
		line := s.scrub(strings.Join(parts, " "))
		if len(line) > 2048 {
			line = line[:2048] + "..."
		}
		if len(s.logs) < maxLogLines {
			s.logs = append(s.logs, line)
		}
		return nil, nil
	})
	_ = host.Set("http", httpObj)
	_ = host.Set("mcp", mcpObj)
	_ = host.Set("link", linkObj)
	_ = host.Set("field", fieldObj)
	_ = host.Set("card", cardObj)
	_ = host.Set("session", sessObj)

	cfg := map[string]any{}
	for k, d := range s.c.Manifest.Config {
		if d.Default != nil {
			cfg[k] = d.Default
		}
	}
	for k, v := range s.c.Config {
		cfg[k] = v
	}
	_ = host.Set("config", s.toJS(cfg))
	_ = rt.Set("host", host)

	if _, err := rt.RunScript("index.js", s.c.Source); err != nil {
		return s.mapException(err, "while loading index.js")
	}
	return nil
}

func errNoApp(name string) error {
	return fmt.Errorf("host.%s is not available here (this run has no board attached)", name)
}

// toJS turns a Go value into a plain JS value by way of JSON, so the code never holds a Go object.
func (s *sandbox) toJS(v any) goja.Value {
	if v == nil {
		return goja.Undefined()
	}
	b, err := json.Marshal(v)
	if err != nil {
		return goja.Undefined()
	}
	out, err := s.parse(goja.Undefined(), s.rt.ToValue(string(b)))
	if err != nil {
		return goja.Undefined()
	}
	return out
}

func (s *sandbox) throw(err error) {
	var pe *PermissionError
	if errors.As(err, &pe) {
		ctor, ok := goja.AssertConstructor(s.rt.Get("PermissionError"))
		if ok {
			if obj, cerr := ctor(nil, s.rt.ToValue(pe.Error()), s.rt.ToValue(pe.Permission)); cerr == nil {
				panic(obj)
			}
		}
	}
	panic(s.rt.NewGoError(errors.New(s.scrub(err.Error()))))
}

// enter marks the code as waiting on a host call, so the wait does not count against its time.
func (s *sandbox) enter() func() {
	s.mu.Lock()
	s.inHost = true
	t := time.Now()
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		s.inHost = false
		s.until = s.until.Add(time.Since(t))
		s.mu.Unlock()
	}
}

func (s *sandbox) invoke(input json.RawMessage) (json.RawMessage, error) {
	rt := s.rt
	opsV, err := rt.RunString(`(typeof ops === 'undefined') ? undefined : ops`)
	if err != nil {
		return nil, s.mapException(err, "")
	}
	cur := opsV
	for _, part := range strings.Split(s.c.Op, ".") {
		if cur == nil || goja.IsUndefined(cur) || goja.IsNull(cur) {
			break
		}
		cur = cur.ToObject(rt).Get(part)
	}
	fn, ok := goja.AssertFunction(cur)
	if !ok {
		return nil, fmt.Errorf("index.js declares ops.%s in the manifest but does not define it. Write ops.%s = function (input) { ... }", s.c.Op, s.c.Op)
	}
	arg, err := s.parse(goja.Undefined(), rt.ToValue(string(input)))
	if err != nil {
		return nil, err
	}
	ret, err := fn(goja.Undefined(), arg)
	if err != nil {
		return nil, s.mapException(err, "")
	}
	if p, isObj := ret.(*goja.Object); isObj {
		if _, isPromise := p.Export().(*goja.Promise); isPromise {
			return nil, errors.New("ops must return their result, not a Promise: the sandbox calls are synchronous")
		}
	}
	str, _ := goja.AssertFunction(rt.Get("__stringify"))
	sv, err := str(goja.Undefined(), ret)
	if err != nil {
		return nil, s.mapException(err, "while encoding the result")
	}
	return json.RawMessage(sv.String()), nil
}

func (s *sandbox) mapException(err error, ctx string) error {
	var exc *goja.Exception
	if !errors.As(err, &exc) {
		return err
	}
	v := exc.Value()
	if o, ok := v.(*goja.Object); ok {
		name := fmt.Sprint(o.Get("name"))
		msg := fmt.Sprint(o.Get("message"))
		if name == "PermissionError" {
			perm := ""
			if p := o.Get("permission"); p != nil {
				perm = fmt.Sprint(p)
			}
			return &PermissionError{Permission: perm, Message: strings.TrimPrefix(msg, "PermissionError: ")}
		}
		return &ScriptError{Name: name, Message: strings.TrimSpace(ctx + " " + msg), Stack: exc.String()}
	}
	return &ScriptError{Message: strings.TrimSpace(ctx + " " + v.String()), Stack: exc.String()}
}

// ---- secrets and scrubbing ----

func (s *sandbox) secret(call goja.FunctionCall) (any, error) {
	name, _ := call.Argument(0).Export().(string)
	if name == "" {
		return nil, errors.New("secret(name) needs the secret's name")
	}
	if !s.c.Manifest.Permissions.has(s.c.Manifest.Permissions.Secrets, name) {
		return nil, &PermissionError{Permission: "secrets: " + name, Message: fmt.Sprintf("the manifest does not grant secrets: %q. Add it to permissions.secrets", name)}
	}
	return s.rt.ToValue(&SecretHandle{name: name}), nil
}

func (s *sandbox) secretValue(h *SecretHandle) (string, error) {
	if s.r.Secrets == nil {
		return "", errors.New("no secret store is attached to this run")
	}
	v, ok, err := s.r.Secrets.Get(SecretKey(s.c.Name, h.name))
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("the secret %q is not set. Open Settings, Integrations, %s and enter it", h.name, s.c.Name)
	}
	if v != "" {
		s.seen = append(s.seen, v, base64.StdEncoding.EncodeToString([]byte(v)))
		if i := strings.IndexByte(v, ':'); i >= 0 {
			s.seen = append(s.seen, base64.StdEncoding.EncodeToString([]byte(v[i:])))
		}
	}
	return v, nil
}

func (s *sandbox) scrub(str string) string {
	for _, v := range s.seen {
		if len(v) >= 4 {
			str = strings.ReplaceAll(str, v, "[secret]")
			if j, err := json.Marshal(v); err == nil && len(j) > 2 {
				str = strings.ReplaceAll(str, string(j[1:len(j)-1]), "[secret]")
			}
		}
	}
	return str
}

func (s *sandbox) scrubBytes(b json.RawMessage) json.RawMessage {
	if len(s.seen) == 0 {
		return b
	}
	return json.RawMessage(s.scrub(string(b)))
}

func (s *sandbox) scrubError(err error) error {
	if len(s.seen) == 0 {
		return err
	}
	var pe *PermissionError
	var se *ScriptError
	switch {
	case errors.As(err, &pe):
		return &PermissionError{Permission: s.scrub(pe.Permission), Message: s.scrub(pe.Message)}
	case errors.As(err, &se):
		return &ScriptError{Name: se.Name, Message: s.scrub(se.Message), Stack: s.scrub(se.Stack)}
	}
	if scrubbed := s.scrub(err.Error()); scrubbed != err.Error() {
		return errors.New(scrubbed)
	}
	return err
}

// hideHandles replaces secret handles in a value with a marker, so the value can be encoded.
func hideHandles(v any) any {
	switch x := v.(type) {
	case *SecretHandle:
		return "<secret " + x.name + ">"
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = hideHandles(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = hideHandles(e)
		}
		return out
	}
	return v
}

func hasHandle(v any) bool {
	switch x := v.(type) {
	case *SecretHandle:
		return true
	case map[string]any:
		for _, e := range x {
			if hasHandle(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if hasHandle(e) {
				return true
			}
		}
	}
	return false
}

func (s *sandbox) check(name string, v any) error {
	b, err := json.Marshal(hideHandles(v))
	if err != nil {
		return fmt.Errorf("host.%s: the argument cannot be encoded as JSON: %w", name, err)
	}
	if err := Validate(hostSchemas[name], b); err != nil {
		return fmt.Errorf("host.%s: %w", name, err)
	}
	return nil
}

// ---- host calls ----

func (s *sandbox) api(name string, call goja.FunctionCall, f func(map[string]any) (any, error)) (any, error) {
	if !s.c.Manifest.Permissions.has(s.c.Manifest.Permissions.API, name) {
		return nil, &PermissionError{Permission: "api: " + name, Message: fmt.Sprintf("the manifest does not grant api: %q. Add it to permissions.api", name)}
	}
	in := map[string]any{}
	if a := call.Argument(0).Export(); a != nil {
		m, ok := a.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("host.%s takes an object", name)
		}
		in = m
	}
	if hasHandle(in) {
		return nil, fmt.Errorf("host.%s cannot take a secret: secrets are only for http.request auth and headers", name)
	}
	if err := s.check(name, in); err != nil {
		return nil, err
	}
	defer s.enter()()
	return f(in)
}

func (s *sandbox) httpRequest(call goja.FunctionCall) (any, error) {
	args, _ := call.Argument(0).Export().(map[string]any)
	if args == nil {
		return nil, errors.New("host.http.request takes an object: {method, url, headers, json, body, auth}")
	}
	for k, v := range args {
		if v == nil {
			delete(args, k)
		}
	}
	if err := s.check("http.request", args); err != nil {
		return nil, err
	}
	method := strings.ToUpper(fmt.Sprint(orDefault(args["method"], "GET")))
	target := fmt.Sprint(args["url"])
	if q, ok := args["query"].(map[string]any); ok && len(q) > 0 {
		u, err := url.Parse(target)
		if err != nil {
			return nil, fmt.Errorf("host.http.request: bad url: %w", err)
		}
		vals := u.Query()
		for k, v := range q {
			vals.Set(k, fmt.Sprint(v))
		}
		u.RawQuery = vals.Encode()
		target = u.String()
	}
	allowed := s.c.Manifest.Permissions.AllowsURL
	if !allowed(target) {
		return nil, &PermissionError{Permission: "network: " + target, Message: fmt.Sprintf("the manifest does not grant network access to %s. Add a matching pattern to permissions.network", target)}
	}
	req := HTTPRequest{Method: method, URL: target, Headers: map[string]string{}}
	if h, ok := args["headers"].(map[string]any); ok {
		for k, v := range h {
			val, err := s.headerValue(v)
			if err != nil {
				return nil, fmt.Errorf("header %s: %w", k, err)
			}
			req.Headers[k] = val
		}
	}
	if a, ok := args["auth"].(map[string]any); ok {
		if err := s.applyAuth(req.Headers, a); err != nil {
			return nil, err
		}
	}
	switch {
	case args["json"] != nil:
		b, err := json.Marshal(hideHandles(args["json"]))
		if err != nil {
			return nil, err
		}
		if hasHandle(args["json"]) {
			return nil, errors.New("a secret can only be used as auth or as a header value, not in the body")
		}
		req.Body = b
		if !hasHeader(req.Headers, "Content-Type") {
			req.Headers["Content-Type"] = "application/json"
		}
	case args["body"] != nil:
		req.Body = []byte(fmt.Sprint(args["body"]))
	}
	defer s.enter()()
	resp, err := s.r.Backend.HTTP(s.ctx, req, allowed)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"status": resp.Status, "headers": resp.Headers, "text": s.scrub(string(resp.Body))}
	var parsed any
	if len(resp.Body) > 0 && json.Unmarshal([]byte(s.scrub(string(resp.Body))), &parsed) == nil {
		out["json"] = parsed
	}
	return out, nil
}

func orDefault(v any, d any) any {
	if v == nil {
		return d
	}
	return v
}

func hasHeader(h map[string]string, name string) bool {
	for k := range h {
		if strings.EqualFold(k, name) {
			return true
		}
	}
	return false
}

func (s *sandbox) headerValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case *SecretHandle:
		return s.secretValue(x)
	case map[string]any:
		h, ok := x["secret"].(*SecretHandle)
		if !ok {
			return "", errors.New("an object header value is {prefix, secret: secret('name')}")
		}
		val, err := s.secretValue(h)
		if err != nil {
			return "", err
		}
		prefix, _ := x["prefix"].(string)
		return prefix + val, nil
	}
	return fmt.Sprint(v), nil
}

func (s *sandbox) applyAuth(h map[string]string, a map[string]any) error {
	handle, _ := a["secret"].(*SecretHandle)
	if handle == nil {
		return errors.New("auth needs secret: secret('name'). A plain string is not accepted, so a token is never written into code")
	}
	val, err := s.secretValue(handle)
	if err != nil {
		return err
	}
	switch a["type"] {
	case "bearer":
		h["Authorization"] = "Bearer " + val
	case "basic":
		user, _ := a["user"].(string)
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+val))
		s.seen = append(s.seen, base64.StdEncoding.EncodeToString([]byte(user+":"+val)))
	case "header":
		name, _ := a["name"].(string)
		if name == "" {
			return errors.New("auth type header needs name")
		}
		prefix, _ := a["prefix"].(string)
		h[name] = prefix + val
	}
	return nil
}

func (s *sandbox) mcpCall(call goja.FunctionCall) (any, error) {
	args := make([]any, 0, 3)
	for _, a := range call.Arguments {
		args = append(args, a.Export())
	}
	if err := s.check("mcp.call", args); err != nil {
		return nil, err
	}
	server, tool := fmt.Sprint(args[0]), fmt.Sprint(args[1])
	if !s.c.Manifest.Permissions.AllowsMCP(server) {
		return nil, &PermissionError{Permission: "network: mcp:" + server, Message: fmt.Sprintf("the manifest does not grant mcp:%s. Add \"mcp:%s\" to permissions.network", server, server)}
	}
	var in any
	if len(args) == 3 {
		in = args[2]
	}
	if hasHandle(in) {
		return nil, errors.New("a secret can only be used as auth or as a header value")
	}
	defer s.enter()()
	return s.r.Backend.MCP(s.ctx, server, tool, in)
}

func (s *sandbox) execBin(call goja.FunctionCall) (any, error) {
	args := make([]any, 0, 2)
	for _, a := range call.Arguments {
		args = append(args, a.Export())
	}
	if hasHandle(args) {
		return nil, errors.New("a secret can only be used as auth or as a header value, not as an argument")
	}
	if err := s.check("exec", args); err != nil {
		return nil, err
	}
	bin := fmt.Sprint(args[0])
	if !s.c.Manifest.Permissions.has(s.c.Manifest.Permissions.Exec, bin) {
		return nil, &PermissionError{Permission: "exec: " + bin, Message: fmt.Sprintf("the manifest does not grant exec: %q. Add the binary's name to permissions.exec", bin)}
	}
	if strings.Contains(bin, "/") {
		return nil, errors.New("exec takes a binary name, not a path")
	}
	var argv []string
	if len(args) == 2 {
		for _, a := range args[1].([]any) {
			argv = append(argv, fmt.Sprint(a))
		}
	}
	defer s.enter()()
	res, err := s.r.Backend.Exec(s.ctx, bin, argv)
	if err != nil {
		return nil, err
	}
	return map[string]any{"stdout": res.Stdout, "stderr": res.Stderr, "code": res.Code}, nil
}

// ---- recorded tests ----

// TestResult is the outcome of the manifest's recorded tests.
type TestResult struct {
	OK     bool         `json:"ok"`
	Passed int          `json:"passed"`
	Failed int          `json:"failed"`
	Cases  []CaseResult `json:"cases"`
	RanAt  time.Time    `json:"ran_at"`
}

// CaseResult is one recorded test's outcome.
type CaseResult struct {
	Name   string `json:"name"`
	Op     string `json:"op"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Output any    `json:"output,omitempty"`
}

// RunTests runs every recorded test with the network, exec and MCP answered by the test's fixtures.
// An integration without tests does not pass: there is nothing to approve on.
func RunTests(ctx context.Context, name string, m Manifest, source string, secrets Secrets) TestResult {
	res := TestResult{RanAt: time.Now().UTC(), Cases: []CaseResult{}}
	if len(m.Tests) == 0 {
		res.Cases = append(res.Cases, CaseResult{Name: "(none)", Error: "the manifest has no tests. Record at least one test per op in manifest.tests"})
		res.Failed = 1
		return res
	}
	for i, t := range m.Tests {
		cr := CaseResult{Name: t.Name, Op: t.Op}
		if cr.Name == "" {
			cr.Name = fmt.Sprintf("test %d", i+1)
		}
		r := &Runner{Backend: FixturesOf(t), Secrets: tempSecrets(name, m), Timeout: 5 * time.Second}
		input, _ := json.Marshal(t.Input)
		if t.Input == nil {
			input = []byte("{}")
		}
		out, err := r.Run(ctx, Call{Name: name, Manifest: m, Source: source, Op: t.Op, Input: input, Config: t.Config, App: testApp()})
		if err != nil {
			cr.Error = err.Error()
		} else {
			var got any
			_ = json.Unmarshal(out.Output, &got)
			cr.Output = got
			if t.Expect == nil || subset(t.Expect, got) {
				cr.OK = true
			} else {
				exp, _ := json.Marshal(t.Expect)
				cr.Error = fmt.Sprintf("the output did not match expect.\nexpected (subset): %s\ngot: %s", exp, out.Output)
			}
		}
		if cr.OK {
			res.Passed++
		} else {
			res.Failed++
		}
		res.Cases = append(res.Cases, cr)
	}
	res.OK = res.Failed == 0
	return res
}

// tempSecrets gives a test every secret the manifest names, with a made-up value, so tests run
// without the user's real secrets.
func tempSecrets(name string, m Manifest) Secrets {
	mem := &memSecrets{m: map[string]string{}}
	for _, n := range m.Permissions.Secrets {
		mem.m[SecretKey(name, n)] = "test-secret-" + n + "-value"
	}
	return mem
}

type memSecrets struct{ m map[string]string }

func (s *memSecrets) Get(k string) (string, bool, error) { v, ok := s.m[k]; return v, ok, nil }
func (s *memSecrets) Set(k, v string) error              { s.m[k] = v; return nil }
func (s *memSecrets) Delete(k string) error              { delete(s.m, k); return nil }

// testApp is the board a test sees: empty, and it accepts writes.
func testApp() App {
	return App{
		LinkUpsert:        func(map[string]any) (any, error) { return map[string]any{"ok": true}, nil },
		LinkList:          func(map[string]any) (any, error) { return []any{}, nil },
		FieldSet:          func(string, string, any) error { return nil },
		CardRead:          func(c string) (any, error) { return map[string]any{"id": c}, nil },
		CardMove:          func(string, string) error { return nil },
		CardList:          func(map[string]any) (any, error) { return []any{}, nil },
		SessionListRemote: func(map[string]any) (any, error) { return []any{}, nil },
		Notify:            func(string, string) {},
	}
}

// subset says whether every key of want is in got with an equal value (arrays must match in
// length and order).
func subset(want, got any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range w {
			if !subset(v, g[k]) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !subset(w[i], g[i]) {
				return false
			}
		}
		return true
	}
	wb, _ := json.Marshal(want)
	gb, _ := json.Marshal(got)
	return bytes.Equal(wb, gb)
}

// SortedKeys is a helper for stable output.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CheckOps loads the source and reports each declared op that the code does not define as a
// function. The top level of the code runs, with no host access beyond what a stub allows, so
// code that calls the network at load time fails here.
func CheckOps(m Manifest, source string) []string {
	s := &sandbox{r: &Runner{Backend: &Fixtures{}}, c: Call{Name: m.Name, Manifest: m, Source: source}, ctx: context.Background()}
	s.rt = goja.New()
	s.rt.SetMaxCallStackSize(maxCallStack)
	s.until = time.Now().Add(DefaultTimeout)
	timer := time.AfterFunc(2*time.Second, func() { s.rt.Interrupt("timeout") })
	defer timer.Stop()
	if err := s.install(); err != nil {
		return []string{"index.js: " + err.Error()}
	}
	opsV, err := s.rt.RunString(`(typeof ops === 'undefined') ? undefined : ops`)
	var problems []string
	if err != nil || opsV == nil || goja.IsUndefined(opsV) {
		return []string{"index.js must define a variable named ops: var ops = { get: function (input) { ... } };"}
	}
	for _, op := range m.OpNames() {
		cur := opsV
		for _, part := range strings.Split(op, ".") {
			if cur == nil || goja.IsUndefined(cur) || goja.IsNull(cur) {
				break
			}
			cur = cur.ToObject(s.rt).Get(part)
		}
		if _, ok := goja.AssertFunction(cur); !ok {
			problems = append(problems, fmt.Sprintf("index.js does not define ops.%s, which the manifest declares", op))
		}
	}
	return problems
}
