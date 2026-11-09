package dash

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/store"
)

// QuerySpec describes a named query: what it returns and the arguments it takes.
type QuerySpec struct {
	Name  string `json:"name"`
	Shape string `json:"shape"`
	Doc   string `json:"doc"`
	// Args maps an argument name to its type: string, int or text.
	Args map[string]string `json:"args"`
}

// Builtin lists the built-in queries.
var Builtin = []QuerySpec{
	{Name: "cards.count", Shape: ShapeGroups, Doc: "Count cards grouped by column or by a field path (card.assignee, card.liveness, custom.priority, jira.status). expr narrows which cards count.", Args: map[string]string{"by": "string", "expr": "string"}},
	{Name: "cards.list", Shape: ShapeRows, Doc: "List cards, optionally those matching a CEL expression.", Args: map[string]string{"expr": "string", "limit": "int"}},
	{Name: "questions.open", Shape: ShapeRows, Doc: "The open questions waiting on the user, oldest first.", Args: map[string]string{"limit": "int"}},
	{Name: "mcp.calls", Shape: ShapeRows, Doc: "Recent tool calls. With by (actor, tool, hour or ok) it returns counts instead of rows. since is today or a number with m, h or d.", Args: map[string]string{"since": "string", "by": "string", "limit": "int"}},
	{Name: "rules.fired", Shape: ShapeRows, Doc: "What rules did lately: one row per firing.", Args: map[string]string{"since": "string", "limit": "int"}},
}

// IntegrationQueries are the two queries every enabled integration adds, as <integration>.links
// and <integration>.count.
var IntegrationQueries = []QuerySpec{
	{Name: "links", Shape: ShapeRows, Doc: "The cards linked to the integration's items, with each item's snapshot fields.", Args: map[string]string{"limit": "int"}},
	{Name: "count", Shape: ShapeGroups, Doc: "Count the integration's linked items by one of its fields, such as status.", Args: map[string]string{"by": "string"}},
}

// ShapeOf is the shape a call returns: mcp.calls with by gives groups instead of rows.
func ShapeOf(q QuerySpec, args map[string]any) string {
	if q.Name == "mcp.calls" {
		if by, _ := args["by"].(string); by != "" {
			return ShapeGroups
		}
	}
	return q.Shape
}

// Env is what a query reads.
type Env struct {
	Store     *store.Store
	Fields    *fields.Registry
	Workspace domain.Workspace
	Now       time.Time
}

// Lookup finds a query by name for a workspace: a built-in, or <integration>.links / .count of an
// integration the workspace has switched on.
func (e Env) Lookup(name string) (QuerySpec, bool) {
	for _, q := range Builtin {
		if q.Name == name {
			return q, true
		}
	}
	ns, rest, ok := strings.Cut(name, ".")
	if !ok || !contains(e.Workspace.Integrations, ns) {
		return QuerySpec{}, false
	}
	for _, q := range IntegrationQueries {
		if q.Name == rest {
			q.Name = name
			return q, true
		}
	}
	return QuerySpec{}, false
}

// QueryNames lists every query a lookup knows, for error messages. It probes the built-ins and
// the integration suffixes with the names the lookup reports.
func QueryNames(lookup func(string) (QuerySpec, bool)) []string {
	var out []string
	for _, q := range Builtin {
		out = append(out, q.Name)
	}
	out = append(out, "<integration>.links", "<integration>.count")
	return out
}

var (
	fieldPathRe = regexp.MustCompile(`^(column|[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*)$`)
	byRe        = regexp.MustCompile(`^[a-z]+$`)
)

// checkArgs lists the problems with the arguments of a query call.
func checkArgs(q QuerySpec, args map[string]any) []string {
	var errs []string
	names := make([]string, 0, len(args))
	for k := range args {
		names = append(names, k)
	}
	sort.Strings(names)
	valid := make([]string, 0, len(q.Args))
	for k := range q.Args {
		valid = append(valid, k)
	}
	sort.Strings(valid)
	for _, k := range names {
		typ, ok := q.Args[k]
		if !ok {
			errs = append(errs, fmt.Sprintf("unknown argument %q (valid: %s)", k, strings.Join(valid, ", ")))
			continue
		}
		v := args[k]
		switch typ {
		case "int":
			f, isNum := v.(float64)
			if i, isInt := v.(int); isInt {
				f, isNum = float64(i), true
			}
			if !isNum || f != float64(int(f)) || f < 1 || f > 500 {
				errs = append(errs, fmt.Sprintf("%s must be a whole number from 1 to 500", k))
			}
		case "string":
			s, isStr := v.(string)
			if !isStr {
				errs = append(errs, fmt.Sprintf("%s must be a string", k))
				break
			}
			if k == "since" {
				if _, err := SinceCutoff(s, time.Now()); err != nil {
					errs = append(errs, err.Error())
				}
			}
			if k == "by" && !fieldPathRe.MatchString(s) && !byRe.MatchString(s) {
				errs = append(errs, fmt.Sprintf("by %q must be column, a field path such as card.assignee, or a plain name", s))
			}
		}
	}
	return errs
}

// Group is one count of a groups result.
type Group struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// Result is what a query returns. Shape says which of Groups and Rows is filled.
type Result struct {
	Query  string           `json:"query"`
	Shape  string           `json:"shape"`
	Groups []Group          `json:"groups"`
	Rows   []map[string]any `json:"rows"`
}

func argString(args map[string]any, k string) string {
	s, _ := args[k].(string)
	return s
}

func argInt(args map[string]any, k string, def int) int {
	switch v := args[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}

// Run executes a named query.
func Run(ctx context.Context, e Env, name string, args map[string]any) (Result, error) {
	q, ok := e.Lookup(name)
	if !ok {
		return Result{}, fmt.Errorf("unknown query %q. Known: %s. An integration adds <integration>.links and <integration>.count once the workspace uses it", name, strings.Join(QueryNames(e.Lookup), ", "))
	}
	if problems := checkArgs(q, args); len(problems) > 0 {
		return Result{}, fmt.Errorf("%s: %s", name, strings.Join(problems, "; "))
	}
	res := Result{Query: name, Shape: q.Shape, Groups: []Group{}, Rows: []map[string]any{}}
	var err error
	switch name {
	case "cards.count":
		res.Groups, err = e.cardsCount(args)
	case "cards.list":
		res.Rows, err = e.cardsList(args)
	case "questions.open":
		res.Rows, err = e.questionsOpen(args)
	case "mcp.calls":
		if by := argString(args, "by"); by != "" {
			res.Shape = ShapeGroups
			res.Groups, err = e.mcpGroups(args, by)
		} else {
			res.Rows, err = e.mcpRows(args)
		}
	case "rules.fired":
		res.Rows, err = e.rulesFired(args)
	default:
		ns, rest, _ := strings.Cut(name, ".")
		if rest == "links" {
			res.Rows, err = e.integrationLinks(ns, args)
		} else {
			res.Groups, err = e.integrationCount(ns, args)
		}
	}
	return res, err
}

func (e Env) cards(expr string) ([]domain.Card, *fields.Evaluator, error) {
	cards, err := e.Store.Cards.List(e.Workspace.ID, store.CardFilter{})
	if err != nil {
		return nil, nil, err
	}
	ev, err := e.Fields.NewEvaluator(e.Store, e.Workspace, expr)
	if err != nil {
		return nil, nil, err
	}
	var out []domain.Card
	for _, c := range cards {
		if ok, _ := ev.Match(c); ok {
			out = append(out, c)
		}
	}
	return out, ev, nil
}

func (e Env) cardsCount(args map[string]any) ([]Group, error) {
	by := argString(args, "by")
	if by == "" {
		by = "column"
	}
	cards, ev, err := e.cards(argString(args, "expr"))
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	var order []string
	cols, err := fields.ColumnMap(e.Store, e.Workspace.ID)
	if err != nil {
		return nil, err
	}
	if by == "column" {
		list, err := e.Store.Columns.List(e.Workspace.ID)
		if err != nil {
			return nil, err
		}
		for _, c := range list {
			order = append(order, c.Name)
			counts[c.Name] = 0
		}
	} else if _, ok := fields.Find(ev.Schema(), by); !ok {
		paths := fields.Paths(ev.Schema())
		return nil, fmt.Errorf("no field %q in this workspace. Valid: column, %s", by, strings.Join(paths, ", "))
	}
	for _, c := range cards {
		key := "(none)"
		if by == "column" {
			key = cols[c.ColumnID].Name
		} else {
			flat, err := ev.Flat(c)
			if err != nil {
				return nil, err
			}
			if v := fmt.Sprint(flat[by]); v != "" && v != "<nil>" {
				key = v
			}
		}
		counts[key]++
	}
	out := []Group{}
	if by == "column" {
		for _, k := range order {
			out = append(out, Group{Key: k, Label: k, Count: counts[k]})
		}
		return out, nil
	}
	for k, n := range counts {
		out = append(out, Group{Key: k, Label: k, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func (e Env) cardsList(args map[string]any) ([]map[string]any, error) {
	cards, _, err := e.cards(argString(args, "expr"))
	if err != nil {
		return nil, err
	}
	cols, err := fields.ColumnMap(e.Store, e.Workspace.ID)
	if err != nil {
		return nil, err
	}
	limit := argInt(args, "limit", 100)
	out := []map[string]any{}
	for _, c := range cards {
		if len(out) >= limit {
			break
		}
		col := cols[c.ColumnID]
		out = append(out, map[string]any{"id": c.ID, "nick": c.Nick, "key": c.Key, "title": c.Title, "assignee": c.Assignee,
			"stage": c.Stage, "column": col.Name, "role": col.Role, "liveness": c.Liveness})
	}
	return out, nil
}

func (e Env) questionsOpen(args map[string]any) ([]map[string]any, error) {
	qs, err := e.Store.Questions.List(e.Workspace.ID, "", "open")
	if err != nil {
		return nil, err
	}
	limit := argInt(args, "limit", 100)
	out := []map[string]any{}
	for _, q := range qs {
		if len(out) >= limit {
			break
		}
		card, err := e.Store.Cards.Get(q.CardID)
		if err != nil {
			continue
		}
		out = append(out, map[string]any{"id": q.ID, "card_id": q.CardID, "card": card.Nick, "key": card.Key, "label": fmt.Sprintf("Q-%02d", q.Seq),
			"kind": q.Kind, "text": q.Text, "since": q.CreatedAt})
	}
	return out, nil
}

func (e Env) calls(args map[string]any) ([]domain.MCPCall, error) {
	cutoff, err := SinceCutoff(argString(args, "since"), e.Now)
	if err != nil {
		return nil, err
	}
	all, err := e.Store.MCPCalls.List(e.Workspace.ID, 1000)
	if err != nil {
		return nil, err
	}
	var out []domain.MCPCall
	for _, c := range all {
		if c.CreatedAt.Before(cutoff) {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (e Env) mcpRows(args map[string]any) ([]map[string]any, error) {
	calls, err := e.calls(args)
	if err != nil {
		return nil, err
	}
	limit := argInt(args, "limit", 50)
	out := []map[string]any{}
	for _, c := range calls {
		if len(out) >= limit {
			break
		}
		out = append(out, map[string]any{"at": c.CreatedAt, "actor": c.Actor, "tool": c.Tool, "args": string(c.Args), "ok": c.OK, "error": c.Error, "duration_ms": c.DurationMS})
	}
	return out, nil
}

func (e Env) mcpGroups(args map[string]any, by string) ([]Group, error) {
	calls, err := e.calls(args)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, c := range calls {
		var k string
		switch by {
		case "actor":
			k = c.Actor
		case "tool":
			k = c.Tool
		case "ok":
			k = map[bool]string{true: "ok", false: "failed"}[c.OK]
		case "hour":
			k = fmt.Sprintf("%02d", c.CreatedAt.Local().Hour())
		default:
			return nil, fmt.Errorf("mcp.calls by %q must be actor, tool, hour or ok", by)
		}
		counts[k]++
	}
	out := []Group{}
	if by == "hour" {
		for h := 0; h < 24; h++ {
			k := fmt.Sprintf("%02d", h)
			out = append(out, Group{Key: k, Label: k + ":00", Count: counts[k]})
		}
		return out, nil
	}
	for k, n := range counts {
		out = append(out, Group{Key: k, Label: k, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

func (e Env) rulesFired(args map[string]any) ([]map[string]any, error) {
	cutoff, err := SinceCutoff(argString(args, "since"), e.Now)
	if err != nil {
		return nil, err
	}
	limit := argInt(args, "limit", 50)
	rows, err := e.Store.Changes.ListByAction(e.Workspace.ID, []string{board.ChangeFire}, 500)
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for _, ch := range rows {
		if len(out) >= limit {
			break
		}
		if ch.CreatedAt.Before(cutoff) {
			continue
		}
		var a struct {
			Nick string `json:"nick"`
		}
		_ = jsonUnmarshal(ch.Args, &a)
		var after struct {
			Text string `json:"text"`
		}
		_ = jsonUnmarshal(ch.After, &after)
		out = append(out, map[string]any{"at": ch.CreatedAt, "rule": strings.TrimPrefix(ch.Actor, "rule:"), "card": a.Nick, "text": after.Text})
	}
	return out, nil
}

func (e Env) integrationLinks(ns string, args map[string]any) ([]map[string]any, error) {
	links, err := e.Store.Links.ListByWorkspace(e.Workspace.ID)
	if err != nil {
		return nil, err
	}
	limit := argInt(args, "limit", 100)
	out := []map[string]any{}
	for _, l := range links {
		if l.Integration != ns || len(out) >= limit {
			continue
		}
		card, err := e.Store.Cards.Get(l.CardID)
		if err != nil {
			continue
		}
		row := map[string]any{"card": card.Nick, "item_id": l.ItemID, "title": l.Title, "url": l.URL}
		for k, v := range l.Fields {
			row[k] = v
		}
		out = append(out, row)
	}
	return out, nil
}

func (e Env) integrationCount(ns string, args map[string]any) ([]Group, error) {
	by := argString(args, "by")
	if by == "" {
		return nil, fmt.Errorf("%s.count needs by: one of the integration's fields, such as status", ns)
	}
	by = strings.TrimPrefix(by, ns+".")
	links, err := e.Store.Links.ListByWorkspace(e.Workspace.ID)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, l := range links {
		if l.Integration != ns {
			continue
		}
		k := "(none)"
		if v, ok := l.Fields[by]; ok && fmt.Sprint(v) != "" {
			k = fmt.Sprint(v)
		}
		counts[k]++
	}
	out := []Group{}
	for k, n := range counts {
		out = append(out, Group{Key: k, Label: k, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}
