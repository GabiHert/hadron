package fields

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
	"github.com/google/cel-go/cel"
)

// Env is the CEL environment of one workspace: one map variable per namespace.
type Env struct {
	Namespaces []string
	env        *cel.Env
}

// NewEnv builds the environment for a workspace.
func (r *Registry) NewEnv(ws domain.Workspace) (*Env, error) {
	ns := []string{NSCard, NSCustom}
	for _, p := range r.enabled(ws) {
		ns = append(ns, p.Namespace())
	}
	opts := make([]cel.EnvOption, len(ns))
	for i, n := range ns {
		opts[i] = cel.Variable(n, cel.MapType(cel.StringType, cel.DynType))
	}
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, err
	}
	return &Env{Namespaces: ns, env: env}, nil
}

// Compiled is an expression ready to evaluate.
type Compiled struct{ prg cel.Program }

// Compile checks an expression. A syntax or type error comes back verbatim from CEL, so the user
// sees the line, column and a caret. The expression must evaluate to true or false.
func (e *Env) Compile(expr string) (*Compiled, error) {
	ast, iss := e.env.Compile(expr)
	if iss != nil && iss.Err() != nil {
		return nil, errors.New(iss.Err().Error())
	}
	if t := ast.OutputType(); t != cel.BoolType && t != cel.DynType {
		return nil, fmt.Errorf("the expression must be true or false for each card, but it returns %s. Add a comparison, such as card.stage == \"review\"", t)
	}
	prg, err := e.env.Program(ast)
	if err != nil {
		return nil, errors.New(err.Error())
	}
	return &Compiled{prg: prg}, nil
}

// Match evaluates the expression for one card's values.
func (c *Compiled) Match(vars map[string]any) (bool, error) {
	out, _, err := c.prg.Eval(vars)
	if err != nil {
		return false, errors.New(err.Error())
	}
	b, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("the expression returned %v, not true or false", out.Value())
	}
	return b, nil
}

// Values builds the CEL variables of a card: one map per namespace. Declared fields that have no
// value yet read as an empty string, 0 or false, so `custom.priority == ""` works.
func (r *Registry) Values(st *store.Store, ws domain.Workspace, schema []Entry, cols map[string]domain.Column, card domain.Card) (map[string]any, error) {
	col := cols[card.ColumnID]
	vars := map[string]any{
		NSCard: map[string]any{
			"column": col.Name, "role": col.Role, "assignee": card.Assignee, "liveness": card.Liveness,
			"key": card.Key, "nick": card.Nick, "stage": card.Stage, "title": card.Title,
		},
	}
	custom := map[string]any{}
	for _, e := range schema {
		ns, name := SplitPath(e.Path)
		if ns == NSCustom {
			custom[name] = zero(e.Type)
		}
	}
	for k, v := range card.Fields {
		if _, declared := custom[k]; declared && v != nil {
			custom[k] = v
		}
	}
	vars[NSCustom] = custom
	for _, p := range r.enabled(ws) {
		ns := p.Namespace()
		m := map[string]any{}
		for _, e := range schema {
			if n, name := SplitPath(e.Path); n == ns {
				m[name] = zero(e.Type)
			}
		}
		got, err := p.Values(st, card)
		if err != nil {
			return nil, fmt.Errorf("%s fields of card %q: %w", ns, card.Nick, err)
		}
		for k, v := range got {
			if v != nil {
				m[k] = v
			}
		}
		vars[ns] = m
	}
	return vars, nil
}

// Flat turns the namespace maps into path -> value, for card_fields_get.
func Flat(vars map[string]any) map[string]any {
	out := map[string]any{}
	for ns, v := range vars {
		if m, ok := v.(map[string]any); ok {
			for k, x := range m {
				out[ns+"."+k] = x
			}
		}
	}
	return out
}

// ColumnMap indexes a workspace's columns by id.
func ColumnMap(st *store.Store, workspaceID string) (map[string]domain.Column, error) {
	cols, err := st.Columns.List(workspaceID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]domain.Column, len(cols))
	for _, c := range cols {
		m[c.ID] = c
	}
	return m, nil
}

// Evaluator evaluates one expression over many cards of a workspace.
type Evaluator struct {
	reg    *Registry
	st     *store.Store
	ws     domain.Workspace
	schema []Entry
	cols   map[string]domain.Column
	prog   *Compiled
}

// NewEvaluator compiles expr (an empty expression matches every card).
func (r *Registry) NewEvaluator(st *store.Store, ws domain.Workspace, expr string) (*Evaluator, error) {
	ev := &Evaluator{reg: r, st: st, ws: ws}
	var err error
	if ev.schema, err = r.Schema(st, ws); err != nil {
		return nil, err
	}
	if ev.cols, err = ColumnMap(st, ws.ID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(expr) == "" {
		return ev, nil
	}
	env, err := r.NewEnv(ws)
	if err != nil {
		return nil, err
	}
	if ev.prog, err = env.Compile(expr); err != nil {
		return nil, err
	}
	return ev, nil
}

// Flat returns the card's values keyed by path (card.column, custom.priority, jira.status...).
func (ev *Evaluator) Flat(card domain.Card) (map[string]any, error) {
	vars, err := ev.reg.Values(ev.st, ev.ws, ev.schema, ev.cols, card)
	if err != nil {
		return nil, err
	}
	return Flat(vars), nil
}

// Schema is the schema the evaluator checks against.
func (ev *Evaluator) Schema() []Entry { return ev.schema }

// Match reports whether the card satisfies the expression. A runtime error, such as a missing key,
// means the card does not match; the error is returned for callers that want to show it.
func (ev *Evaluator) Match(card domain.Card) (bool, error) {
	if ev.prog == nil {
		return true, nil
	}
	vars, err := ev.reg.Values(ev.st, ev.ws, ev.schema, ev.cols, card)
	if err != nil {
		return false, err
	}
	return ev.prog.Match(vars)
}

// Filter returns the ids of the cards that match, in input order. When the expression fails at
// runtime for every card, the first error is returned so the user sees why nothing matched.
func (r *Registry) Filter(st *store.Store, ws domain.Workspace, expr string, cards []domain.Card) ([]string, error) {
	ev, err := r.NewEvaluator(st, ws, expr)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	var first error
	failed := 0
	for _, c := range cards {
		ok, err := ev.Match(c)
		if err != nil {
			failed++
			if first == nil {
				first = err
			}
			continue
		}
		if ok {
			ids = append(ids, c.ID)
		}
	}
	if first != nil && failed == len(cards) {
		return nil, first
	}
	return ids, nil
}

// Paths lists schema paths, sorted, for messages.
func Paths(schema []Entry) []string {
	out := make([]string, len(schema))
	for i, e := range schema {
		out[i] = e.Path
	}
	sort.Strings(out)
	return out
}

var _ = context.Background
