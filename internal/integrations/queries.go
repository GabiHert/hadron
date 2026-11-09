package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/GabiHert/hadron/internal/dash"
	"github.com/GabiHert/hadron/internal/domain"
)

// DefaultQueryTTL is how long a script-defined dashboard query's result is served before a refresh
// starts. The script can set its own with ttl_s.
const DefaultQueryTTL = 10 * time.Minute

// queryRetry is how soon a query that has never succeeded is tried again.
const queryRetry = 30 * time.Second

// queryBudget caps one background refresh, on top of the sandbox's own time limit.
const queryBudget = 2 * time.Minute

type queryEntry struct {
	res        []dash.Group
	has        bool          // res holds a good result
	at         time.Time     // when res was fetched
	tried      time.Time     // when the last attempt ended, good or bad
	ttl        time.Duration // how long res is fresh
	err        string        // why the last refresh failed; "" after a success
	refreshing bool
}

type queryCache struct {
	mu      sync.Mutex
	entries map[string]*queryEntry
	wg      sync.WaitGroup
}

// WaitQueries waits for the background query refreshes that are running. Tests use it.
func (m *Manager) WaitQueries() { m.qc.wg.Wait() }

func (m *Manager) queryNow() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// QuerySpecs implements dash.CustomQueries: the ops.queries of the enabled integrations the
// workspace uses, named <integration>.<query>.
func (m *Manager) QuerySpecs(ws domain.Workspace) []dash.QuerySpec {
	var out []dash.QuerySpec
	for _, ns := range ws.Integrations {
		man, ok := m.manifestFor(ws.ID, ns)
		if !ok {
			continue
		}
		names := make([]string, 0, len(man.Ops.Queries))
		for n := range man.Ops.Queries {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			doc := man.Ops.Queries[n].Description
			if doc == "" {
				doc = "A query the " + ns + " integration defines."
			}
			out = append(out, dash.QuerySpec{Name: ns + "." + n, Shape: dash.ShapeGroups, Doc: doc, Args: map[string]string{}, AnyArgs: true})
		}
	}
	return out
}

// Query implements dash.CustomQueries. A dashboard render must never wait for the network, so it
// answers from the cache: the last result with its age, marked stale when it is older than its
// time to live. The first call has nothing to show and says Pending. A refresh runs in the
// background; when it ends the frontend is told to refetch. When the last refresh failed, the
// result carries the error next to the last good data.
func (m *Manager) Query(ctx context.Context, ws domain.Workspace, name string, args map[string]any) (dash.Result, error) {
	ns, qn, _ := strings.Cut(name, ".")
	man, ok := m.manifestFor(ws.ID, ns)
	op := "queries." + qn
	if !ok || !man.HasOp(op) {
		return dash.Result{}, fmt.Errorf("unknown query %q: integration %s defines no ops.queries.%s", name, ns, qn)
	}
	argJSON, _ := json.Marshal(args)
	key := ws.ID + "|" + name + "|" + string(argJSON)
	now := m.queryNow()

	c := &m.qc
	c.mu.Lock()
	if c.entries == nil {
		c.entries = map[string]*queryEntry{}
	}
	e := c.entries[key]
	start := false
	switch {
	case e == nil:
		e = &queryEntry{ttl: m.queryTTL(), refreshing: true}
		c.entries[key] = e
		start = true
	case !e.refreshing && e.has && now.Sub(e.at) >= e.ttl && (e.err == "" || now.Sub(e.tried) >= queryRetry):
		e.refreshing, start = true, true
	case !e.refreshing && !e.has && now.Sub(e.tried) >= queryRetry:
		e.refreshing, start = true, true
	}
	res := dash.Result{Query: name, Shape: dash.ShapeGroups, Groups: []dash.Group{}, Rows: []map[string]any{}}
	switch {
	case e.has:
		res.Groups = append(res.Groups, e.res...)
		res.AgeS = int(now.Sub(e.at) / time.Second)
		res.Stale = now.Sub(e.at) >= e.ttl
		res.Error = e.err
	case e.err != "":
		res.Error = e.err
	default:
		res.Pending = true
	}
	if start {
		c.wg.Add(1)
		go m.refreshQuery(key, ws, ns, op, args)
	}
	c.mu.Unlock()
	return res, nil
}

func (m *Manager) queryTTL() time.Duration {
	if m.QueryTTL > 0 {
		return m.QueryTTL
	}
	return DefaultQueryTTL
}

// refreshQuery runs the script's query once and stores the outcome.
func (m *Manager) refreshQuery(key string, ws domain.Workspace, ns, op string, args map[string]any) {
	defer m.qc.wg.Done()
	ctx, cancel := context.WithTimeout(context.Background(), queryBudget)
	defer cancel()
	groups, ttl, err := m.runQuery(ctx, ws, ns, op, args)

	c := &m.qc
	c.mu.Lock()
	if e := c.entries[key]; e != nil { // Sync drops the cache when an integration changes
		e.refreshing = false
		e.tried = m.queryNow()
		if err != nil {
			e.err = err.Error() // the last good data, if any, stays, and keeps its age
		} else {
			e.res, e.has, e.err = groups, true, ""
			e.at, e.ttl = e.tried, ttl
		}
	}
	c.mu.Unlock()
	m.Log.Debug("dashboard query refreshed", "query", ns+"."+strings.TrimPrefix(op, "queries."), "ok", err == nil, "groups", len(groups))
	if m.Changed != nil {
		m.Changed(ws.ID, "dash")
	}
}

func (m *Manager) runQuery(ctx context.Context, ws domain.Workspace, ns, op string, args map[string]any) ([]dash.Group, time.Duration, error) {
	sc, err := Find(m.Base, ws.ID, ns)
	if err != nil {
		return nil, 0, err
	}
	if args == nil {
		args = map[string]any{}
	}
	run, err := m.Op(ctx, m.Base, ws, sc, op, args)
	if err != nil {
		return nil, 0, err
	}
	var out struct {
		Groups []struct {
			Key   string  `json:"key"`
			Label string  `json:"label"`
			Count float64 `json:"count"`
		} `json:"groups"`
		TTL float64 `json:"ttl_s"`
	}
	if err := json.Unmarshal(run.Result.Output, &out); err != nil {
		return nil, 0, fmt.Errorf("the query did not return {groups: [...]}: %w", err)
	}
	groups := make([]dash.Group, 0, len(out.Groups))
	for _, g := range out.Groups {
		label := g.Label
		if label == "" {
			label = g.Key
		}
		groups = append(groups, dash.Group{Key: g.Key, Label: label, Count: int(g.Count + 0.5)})
	}
	ttl := m.queryTTL()
	if out.TTL > 0 {
		ttl = time.Duration(out.TTL * float64(time.Second))
	}
	return groups, ttl, nil
}
