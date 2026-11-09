package integrations

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/dash"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/script"
	"github.com/GabiHert/hadron/internal/store"
)

// A neutral example: count a repository's successful workflow runs per day.
const deploysScript = `
var ops = {
  queries: {
    deploys_per_day: function (args) {
      var r = host.http.request({ method: "GET", url: args.base + "/repos/" + args.repo + "/actions/runs", query: { status: "success" } });
      if (r.status >= 400) { throw new Error("the API answered " + r.status); }
      var perDay = {};
      r.json.workflow_runs.forEach(function (run) {
        var day = run.created_at.slice(0, 10);
        perDay[day] = (perDay[day] || 0) + 1;
      });
      var groups = Object.keys(perDay).sort().map(function (d) { return { key: d, label: d, count: perDay[d] }; });
      var out = { groups: groups };
      if (args.ttl) { out.ttl_s = args.ttl; }
      return out;
    }
  }
};
`

type fakeAPI struct {
	hits   atomic.Int32
	fail   atomic.Bool
	delay  atomic.Int64 // nanoseconds
	server *httptest.Server
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		time.Sleep(time.Duration(f.delay.Load()))
		if f.fail.Load() {
			http.Error(w, "boom", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []map[string]string{
			{"created_at": "2026-11-03T09:00:00Z"}, {"created_at": "2026-11-03T15:30:00Z"},
			{"created_at": "2026-11-03T18:00:00Z"}, {"created_at": "2026-11-04T08:00:00Z"},
		}})
	}))
	t.Cleanup(f.server.Close)
	return f
}

type queryFixture struct {
	m       *Manager
	ws      domain.Workspace
	api     *fakeAPI
	now     atomic.Int64 // unix nanoseconds of the fake clock
	changed atomic.Int32
	args    map[string]any
}

func newQueryFixture(t *testing.T) *queryFixture {
	t.Helper()
	t.Setenv("HADRON_SECRETS", "file")
	st, err := store.Open(filepath.Join(t.TempDir(), "hadron.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	api := newFakeAPI(t)
	f := &queryFixture{api: api, args: map[string]any{"base": api.server.URL, "repo": "acme/web"}}
	f.now.Store(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC).UnixNano())
	f.ws = domain.Workspace{Name: "Acme", Root: t.TempDir(), Integrations: []string{"ci"}}
	if err := st.Workspaces.Create(&f.ws); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(map[string]any{
		"name": "ci", "version": "1",
		"ops":         map[string]any{"queries": map[string]any{"deploys_per_day": map[string]any{"description": "Successful workflow runs per day"}}},
		"permissions": map[string]any{"network": []string{api.server.URL + "/*"}},
	})
	sc := domain.Script{Name: "ci", Kind: KindIntegration, Manifest: manifest, Source: deploysScript, Status: StatusEnabled}
	if err := st.Scripts.Create(&sc); err != nil {
		t.Fatal(err)
	}
	f.m = New(st, fields.NewRegistry(), script.NewSecrets(t.TempDir()), script.Live{})
	f.m.Now = func() time.Time { return time.Unix(0, f.now.Load()).UTC() }
	f.m.Changed = func(string, ...string) { f.changed.Add(1) }
	if err := f.m.Sync(); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *queryFixture) advance(d time.Duration) { f.now.Add(int64(d)) }

func (f *queryFixture) query(t *testing.T) dash.Result {
	t.Helper()
	res, err := f.m.Query(t.Context(), f.ws, "ci.deploys_per_day", f.args)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAScriptQueryIsRegisteredAndNamedAfterTheIntegration(t *testing.T) {
	f := newQueryFixture(t)
	specs := f.m.QuerySpecs(f.ws)
	if len(specs) != 1 || specs[0].Name != "ci.deploys_per_day" || specs[0].Shape != dash.ShapeGroups || !specs[0].AnyArgs || specs[0].Doc != "Successful workflow runs per day" {
		t.Fatalf("specs = %+v", specs)
	}
	other := f.ws
	other.Integrations = nil
	if len(f.m.QuerySpecs(other)) != 0 {
		t.Error("a workspace that does not use the integration does not see its queries")
	}
	if _, err := f.m.Query(t.Context(), f.ws, "ci.nope", nil); err == nil {
		t.Error("an undefined query is an error")
	}
	if _, err := f.m.Query(t.Context(), f.ws, "other.deploys_per_day", nil); err == nil {
		t.Error("an integration that is not enabled has no queries")
	}
}

func TestTheFirstCallIsPendingThenTheCacheServesUntilTheTTL(t *testing.T) {
	f := newQueryFixture(t)
	first := f.query(t)
	if !first.Pending || len(first.Groups) != 0 {
		t.Fatalf("nothing is fetched yet, and the call did not wait for it: %+v", first)
	}
	f.m.WaitQueries()
	if f.changed.Load() != 1 {
		t.Errorf("the frontend is told to refetch when the refresh ends, got %d", f.changed.Load())
	}
	res := f.query(t)
	if res.Pending || res.Stale || res.Error != "" || res.AgeS != 0 || len(res.Groups) != 2 {
		t.Fatalf("result = %+v", res)
	}
	if res.Groups[0] != (dash.Group{Key: "2026-11-03", Label: "2026-11-03", Count: 3}) || res.Groups[1].Count != 1 {
		t.Errorf("groups = %+v", res.Groups)
	}
	f.advance(9 * time.Minute)
	res = f.query(t)
	f.m.WaitQueries()
	if res.Stale || res.AgeS != 540 || f.api.hits.Load() != 1 {
		t.Fatalf("inside the 10 minute TTL nothing is fetched: stale=%v age=%d hits=%d", res.Stale, res.AgeS, f.api.hits.Load())
	}
	// Different arguments are a different cache entry.
	other := map[string]any{"base": f.api.server.URL, "repo": "acme/api"}
	if r, _ := f.m.Query(t.Context(), f.ws, "ci.deploys_per_day", other); !r.Pending {
		t.Error("another repo has its own cache entry")
	}
	f.m.WaitQueries()
}

func TestAnOldResultIsServedAtOnceAndRefreshedInTheBackground(t *testing.T) {
	f := newQueryFixture(t)
	f.query(t)
	f.m.WaitQueries()
	f.advance(11 * time.Minute)
	f.api.delay.Store(int64(300 * time.Millisecond))
	t0 := time.Now()
	res := f.query(t)
	if time.Since(t0) > 150*time.Millisecond {
		t.Fatalf("a render must not wait for the network, it took %v", time.Since(t0))
	}
	if !res.Stale || res.AgeS != 660 || len(res.Groups) != 2 {
		t.Fatalf("the old result is served with its age: %+v", res)
	}
	f.m.WaitQueries()
	if f.api.hits.Load() != 2 {
		t.Fatalf("hits = %d", f.api.hits.Load())
	}
	if res := f.query(t); res.Stale || res.AgeS != 0 {
		t.Errorf("after the refresh the result is fresh: %+v", res)
	}
	// Two renders in a row start one refresh, not two.
	f.advance(11 * time.Minute)
	f.query(t)
	f.query(t)
	f.m.WaitQueries()
	if f.api.hits.Load() != 3 {
		t.Errorf("one refresh at a time, hits = %d", f.api.hits.Load())
	}
}

func TestAFailedRefreshKeepsTheLastGoodDataAndShowsTheError(t *testing.T) {
	f := newQueryFixture(t)
	f.query(t)
	f.m.WaitQueries()
	f.advance(11 * time.Minute)
	f.api.fail.Store(true)
	f.query(t)
	f.m.WaitQueries()
	res := f.query(t)
	if res.Error == "" || len(res.Groups) != 2 || !res.Stale {
		t.Fatalf("the error shows next to the last good data: %+v", res)
	}
	if want := "the API answered 500"; !contains2(res.Error, want) {
		t.Errorf("error = %q, want it to say %q", res.Error, want)
	}
	// It recovers on the next refresh, and the error goes away.
	f.api.fail.Store(false)
	f.advance(11 * time.Minute)
	f.query(t)
	f.m.WaitQueries()
	if res := f.query(t); res.Error != "" || res.Stale || len(res.Groups) != 2 {
		t.Errorf("recovered: %+v", res)
	}
}

func TestAQueryThatNeverSucceededShowsItsErrorAndRetriesLater(t *testing.T) {
	f := newQueryFixture(t)
	f.api.fail.Store(true)
	f.query(t)
	f.m.WaitQueries()
	res := f.query(t)
	if res.Error == "" || res.Pending || len(res.Groups) != 0 {
		t.Fatalf("result = %+v", res)
	}
	hits := f.api.hits.Load()
	f.advance(10 * time.Second)
	f.query(t)
	f.m.WaitQueries()
	if f.api.hits.Load() != hits {
		t.Error("a failing query is not hammered: it waits 30 seconds between tries")
	}
	f.advance(25 * time.Second)
	f.api.fail.Store(false)
	f.query(t)
	f.m.WaitQueries()
	if res := f.query(t); res.Error != "" || len(res.Groups) != 2 {
		t.Errorf("after the retry: %+v", res)
	}
}

func TestTheScriptCanSetItsOwnTTL(t *testing.T) {
	f := newQueryFixture(t)
	f.args["ttl"] = 60
	f.query(t)
	f.m.WaitQueries()
	f.advance(30 * time.Second)
	if res := f.query(t); res.Stale {
		t.Error("30 s is inside a 60 s ttl")
	}
	f.advance(40 * time.Second)
	if res := f.query(t); !res.Stale {
		t.Error("70 s is past a 60 s ttl")
	}
	f.m.WaitQueries()
	if f.api.hits.Load() != 2 {
		t.Errorf("hits = %d", f.api.hits.Load())
	}
}

func TestAScriptQueryRunsUnderTheSandboxPermissions(t *testing.T) {
	f := newQueryFixture(t)
	f.args["base"] = "http://127.0.0.1:1" // not in permissions.network
	f.query(t)
	f.m.WaitQueries()
	res := f.query(t)
	if res.Error == "" || !contains2(res.Error, "network") {
		t.Errorf("a call outside the permissions is refused: %+v", res)
	}
}

func TestSyncForgetsCachedAnswers(t *testing.T) {
	f := newQueryFixture(t)
	f.query(t)
	f.m.WaitQueries()
	if err := f.m.Sync(); err != nil {
		t.Fatal(err)
	}
	if res := f.query(t); !res.Pending {
		t.Errorf("a changed script is asked again: %+v", res)
	}
	f.m.WaitQueries()
}

func contains2(s, sub string) bool { return strings.Contains(s, sub) }
