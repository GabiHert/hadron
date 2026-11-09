package dash

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/fields/fieldstest"
	"github.com/GabiHert/hadron/internal/store"
)

type fixture struct {
	st       *store.Store
	env      Env
	ws       domain.Workspace
	cols     map[string]domain.Column
	a, b, c  domain.Card
	jira     *fieldstest.Fake
	registry *fields.Registry
	dbPath   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "hadron.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	reg := fields.NewRegistry()
	jira := fieldstest.NewJira()
	if err := reg.Register(jira); err != nil {
		t.Fatal(err)
	}
	ws := domain.Workspace{Name: "W", Integrations: []string{"jira"}}
	if err := st.Workspaces.Create(&ws); err != nil {
		t.Fatal(err)
	}
	if err := st.Tx(func(tx *store.Store) error { _, err := board.SeedWorkspace(tx, ws, nil); return err }); err != nil {
		t.Fatal(err)
	}
	cols, _ := st.Columns.List(ws.ID)
	byName := map[string]domain.Column{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	mk := func(nick, assignee, col string) domain.Card {
		card := domain.Card{WorkspaceID: ws.ID, ColumnID: byName[col].ID, Nick: nick, Assignee: assignee, Liveness: domain.LivenessIdle}
		if err := st.Cards.Create(&card); err != nil {
			t.Fatal(err)
		}
		return card
	}
	f := &fixture{st: st, ws: ws, cols: byName, jira: jira, registry: reg, dbPath: dbPath}
	f.a, f.b, f.c = mk("alpha", "Ana", "Working"), mk("beta", "Ana", "Working"), mk("gamma", "Bruno", "Done")
	jira.Put(f.a.ID, "status", "In progress")
	jira.Put(f.b.ID, "status", "Finished")
	ws, _ = st.Workspaces.Get(ws.ID)
	f.ws = ws
	f.env = Env{Store: st, Fields: reg, Workspace: ws, Now: time.Now()}
	return f
}

func (f *fixture) run(t *testing.T, q string, args map[string]any) Result {
	t.Helper()
	res, err := Run(context.Background(), f.env, q, args)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return res
}

func groupsOf(r Result) map[string]int {
	out := map[string]int{}
	for _, g := range r.Groups {
		out[g.Key] = g.Count
	}
	return out
}

func TestCardsCountByColumnKeepsEmptyColumnsInOrder(t *testing.T) {
	f := newFixture(t)
	res := f.run(t, "cards.count", map[string]any{"by": "column"})
	if res.Shape != ShapeGroups {
		t.Fatalf("shape = %s", res.Shape)
	}
	var keys []string
	for _, g := range res.Groups {
		keys = append(keys, g.Key)
	}
	if got := strings.Join(keys, ","); got != "Needs you,Working,Blocked,Done,Backlog" {
		t.Errorf("columns in order, empty ones included: %s", got)
	}
	if g := groupsOf(res); g["Working"] != 2 || g["Done"] != 1 || g["Needs you"] != 0 {
		t.Errorf("groups = %v", g)
	}
	// by defaults to column.
	if res2 := f.run(t, "cards.count", nil); len(res2.Groups) != 5 {
		t.Errorf("default by = %+v", res2.Groups)
	}
}

func TestCardsCountByFieldPathAndExpr(t *testing.T) {
	f := newFixture(t)
	g := groupsOf(f.run(t, "cards.count", map[string]any{"by": "card.assignee"}))
	if g["Ana"] != 2 || g["Bruno"] != 1 {
		t.Errorf("by assignee = %v", g)
	}
	g = groupsOf(f.run(t, "cards.count", map[string]any{"by": "jira.status"}))
	if g["In progress"] != 1 || g["Finished"] != 1 || g["(none)"] != 1 {
		t.Errorf("by an integration field = %v", g)
	}
	g = groupsOf(f.run(t, "cards.count", map[string]any{"by": "card.assignee", "expr": "card.column == 'Working'"}))
	if g["Ana"] != 2 || g["Bruno"] != 0 {
		t.Errorf("expr narrows the cards counted = %v", g)
	}
	if _, err := Run(context.Background(), f.env, "cards.count", map[string]any{"by": "card.nope"}); err == nil || !strings.Contains(err.Error(), "no field") {
		t.Errorf("an unknown field says so: %v", err)
	}
	if _, err := Run(context.Background(), f.env, "cards.count", map[string]any{"expr": "card.assignee =="}); err == nil {
		t.Error("a CEL compile error comes back")
	}
}

func TestCardsList(t *testing.T) {
	f := newFixture(t)
	res := f.run(t, "cards.list", nil)
	if res.Shape != ShapeRows || len(res.Rows) != 3 {
		t.Fatalf("%+v", res)
	}
	res = f.run(t, "cards.list", map[string]any{"expr": "jira.status == 'Finished'"})
	if len(res.Rows) != 1 || res.Rows[0]["nick"] != "beta" || res.Rows[0]["column"] != "Working" || res.Rows[0]["role"] != "active" {
		t.Errorf("rows = %+v", res.Rows)
	}
	if res = f.run(t, "cards.list", map[string]any{"limit": float64(2)}); len(res.Rows) != 2 {
		t.Errorf("limit: %d", len(res.Rows))
	}
}

func TestQuestionsOpen(t *testing.T) {
	f := newFixture(t)
	if res := f.run(t, "questions.open", nil); len(res.Rows) != 0 {
		t.Fatalf("none yet: %+v", res.Rows)
	}
	q := domain.Question{WorkspaceID: f.ws.ID, CardID: f.a.ID, Seq: 1, Kind: "approval", Text: "Run the migration?", Status: "open"}
	if err := f.st.Questions.Create(&q); err != nil {
		t.Fatal(err)
	}
	answered := domain.Question{WorkspaceID: f.ws.ID, CardID: f.b.ID, Seq: 1, Kind: "decision", Text: "Done?", Status: "answered"}
	if err := f.st.Questions.Create(&answered); err != nil {
		t.Fatal(err)
	}
	res := f.run(t, "questions.open", nil)
	if len(res.Rows) != 1 || res.Rows[0]["label"] != "Q-01" || res.Rows[0]["card"] != "alpha" || res.Rows[0]["kind"] != "approval" {
		t.Errorf("rows = %+v", res.Rows)
	}
}

func seedCalls(t *testing.T, f *fixture) {
	t.Helper()
	at := func(actor, tool string, ok bool, age time.Duration) {
		c := domain.MCPCall{WorkspaceID: f.ws.ID, Actor: actor, Tool: tool, OK: ok}
		if err := f.st.MCPCalls.Create(&c); err != nil {
			t.Fatal(err)
		}
		// Age the row: the repo stamps now.
		raw, err := sql.Open("sqlite3", "file:"+f.dbPath+"?_busy_timeout=5000")
		if err != nil {
			t.Fatal(err)
		}
		defer raw.Close()
		if _, err := raw.Exec(`UPDATE mcp_calls SET created_at = ? WHERE id = ?`, time.Now().Add(-age).UTC().Format(time.RFC3339Nano), c.ID); err != nil {
			t.Fatal(err)
		}
	}
	at("planner", "card_list", true, time.Minute)
	at("planner", "card_move", true, 2*time.Minute)
	at("session:alpha", "card_report", false, 3*time.Hour)
	at("you", "rule_list", true, 10*24*time.Hour)
}

func TestMCPCallsRowsGroupsAndSince(t *testing.T) {
	f := newFixture(t)
	seedCalls(t, f)
	res := f.run(t, "mcp.calls", nil)
	if res.Shape != ShapeRows || len(res.Rows) != 4 || res.Rows[0]["tool"] != "card_list" {
		t.Fatalf("newest first: %+v", res.Rows)
	}
	if res = f.run(t, "mcp.calls", map[string]any{"since": "1h"}); len(res.Rows) != 2 {
		t.Errorf("since 1h: %d rows", len(res.Rows))
	}
	if res = f.run(t, "mcp.calls", map[string]any{"since": "7d"}); len(res.Rows) != 3 {
		t.Errorf("since 7d: %d rows", len(res.Rows))
	}
	res = f.run(t, "mcp.calls", map[string]any{"by": "actor"})
	if res.Shape != ShapeGroups || groupsOf(res)["planner"] != 2 || groupsOf(res)["you"] != 1 {
		t.Errorf("by actor: %+v", res.Groups)
	}
	if g := groupsOf(f.run(t, "mcp.calls", map[string]any{"by": "ok"})); g["ok"] != 3 || g["failed"] != 1 {
		t.Errorf("by ok: %v", g)
	}
	hours := f.run(t, "mcp.calls", map[string]any{"by": "hour"})
	total := 0
	for _, g := range hours.Groups {
		total += g.Count
	}
	if len(hours.Groups) != 24 || total != 4 {
		t.Errorf("by hour: %d groups, %d calls", len(hours.Groups), total)
	}
	if _, err := Run(context.Background(), f.env, "mcp.calls", map[string]any{"by": "nonsense"}); err == nil {
		t.Error("an unknown by is refused")
	}
	if res = f.run(t, "mcp.calls", map[string]any{"limit": float64(1)}); len(res.Rows) != 1 {
		t.Errorf("limit: %d", len(res.Rows))
	}
}

func TestRulesFired(t *testing.T) {
	f := newFixture(t)
	if res := f.run(t, "rules.fired", nil); len(res.Rows) != 0 {
		t.Fatalf("%+v", res.Rows)
	}
	fire := func(action, rule string) {
		args, _ := json.Marshal(map[string]string{"card_id": f.a.ID, "nick": "alpha"})
		after, _ := json.Marshal(map[string]string{"text": "Moved to Done"})
		if err := f.st.Changes.Create(&domain.Change{WorkspaceID: f.ws.ID, Actor: "rule:" + rule, Action: action, Args: args, Before: json.RawMessage("null"), After: after}); err != nil {
			t.Fatal(err)
		}
	}
	fire(board.ChangeFire, "Finished moves to Done")
	fire(board.ChangeSkip, "Skipped one")
	res := f.run(t, "rules.fired", map[string]any{"since": "today"})
	if len(res.Rows) != 1 || res.Rows[0]["rule"] != "Finished moves to Done" || res.Rows[0]["card"] != "alpha" || res.Rows[0]["text"] != "Moved to Done" {
		t.Errorf("rows = %+v", res.Rows)
	}
}

func TestIntegrationQueries(t *testing.T) {
	f := newFixture(t)
	mk := func(card domain.Card, id, status string) {
		l := domain.Link{WorkspaceID: f.ws.ID, CardID: card.ID, Integration: "jira", ItemID: id, Title: id, Fields: map[string]any{"status": status}}
		if err := f.st.Links.Create(&l); err != nil {
			t.Fatal(err)
		}
	}
	mk(f.a, "ACME-1", "In progress")
	mk(f.b, "ACME-2", "Finished")
	mk(f.c, "ACME-3", "Finished")
	g := groupsOf(f.run(t, "jira.count", map[string]any{"by": "status"}))
	if g["Finished"] != 2 || g["In progress"] != 1 {
		t.Errorf("jira.count = %v", g)
	}
	if g = groupsOf(f.run(t, "jira.count", map[string]any{"by": "jira.status"})); g["Finished"] != 2 {
		t.Errorf("a prefixed field works too: %v", g)
	}
	res := f.run(t, "jira.links", nil)
	if len(res.Rows) != 3 || res.Rows[0]["item_id"] != "ACME-1" || res.Rows[0]["status"] != "In progress" || res.Rows[0]["card"] != "alpha" {
		t.Errorf("jira.links = %+v", res.Rows)
	}
	if _, err := Run(context.Background(), f.env, "jira.count", nil); err == nil || !strings.Contains(err.Error(), "needs by") {
		t.Errorf("count needs by: %v", err)
	}
	// An integration the workspace has not switched on adds nothing.
	if _, err := Run(context.Background(), f.env, "linear.links", nil); err == nil || !strings.Contains(err.Error(), "unknown query") {
		t.Errorf("linear is off here: %v", err)
	}
}

func TestQueryArgumentsAreChecked(t *testing.T) {
	f := newFixture(t)
	for q, args := range map[string]map[string]any{
		"cards.count":    {"nope": 1},
		"cards.list":     {"limit": "ten"},
		"mcp.calls":      {"since": "yesterday"},
		"questions.open": {"limit": float64(0)},
	} {
		if _, err := Run(context.Background(), f.env, q, args); err == nil {
			t.Errorf("%s %v should be refused", q, args)
		}
	}
	if _, err := Run(context.Background(), f.env, "cards.nope", nil); err == nil || !strings.Contains(err.Error(), "cards.count") {
		t.Errorf("an unknown query lists the known ones: %v", err)
	}
}

func TestSinceCutoff(t *testing.T) {
	now := time.Date(2026, 11, 8, 15, 30, 0, 0, time.Local)
	cases := map[string]time.Time{
		"":      {},
		"90m":   now.Add(-90 * time.Minute),
		"24h":   now.Add(-24 * time.Hour),
		"7d":    now.Add(-7 * 24 * time.Hour),
		"today": time.Date(2026, 11, 8, 0, 0, 0, 0, time.Local),
	}
	for in, want := range cases {
		got, err := SinceCutoff(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := SinceCutoff("soon", now); err == nil {
		t.Error("bad since")
	}
}

// ---- validation ----

func lookup(t *testing.T) func(string) (QuerySpec, bool) {
	return newFixture(t).env.Lookup
}

func TestValidateListsEveryProblem(t *testing.T) {
	look := lookup(t)
	specs := []Spec{
		{Type: "bars", Title: "Fine", Source: &Source{Query: "cards.count", Args: map[string]any{"by": "column"}}},
		{Type: "wat", Title: "Unknown type"},
		{Type: "stats", Title: ""},
		{Type: "bars", Title: "Wrong shape", Source: &Source{Query: "cards.list"}},
		{Type: "stats", Title: "Unknown query", Source: &Source{Query: "cards.nope"}},
		{Type: "stats", Title: "Bad arg", Source: &Source{Query: "cards.count", Args: map[string]any{"zzz": 1}}},
		{Type: "custom", Title: "Tall", HTML: "<b>x</b>", Source: &Source{Height: 5000}},
		{Type: "custom", Title: "Short", HTML: "<b>x</b>", Source: &Source{Height: 10}},
		{Type: "prose", Title: "Empty prose"},
		{Type: "planner", Title: "Bad schedule", Schedule: "whenever"},
		{Type: "bars", Title: "Wide", Span: 3, Source: &Source{Query: "cards.count"}},
		{Type: "prose", Title: "Stray html", Text: "x", HTML: "<b>"},
	}
	problems := Validate(specs, look)
	joined := strings.Join(problems, "\n")
	for _, want := range []string{
		`block 2 (Unknown type): type "wat" is not one of`,
		"block 3 (untitled): needs a title",
		"block 3 (untitled): stats blocks need a source query",
		"block 4 (Wrong shape): cards.list returns rows, which a bars block cannot show (use groups)",
		`block 5 (Unknown query): unknown query "cards.nope"`,
		`block 6 (Bad arg): cards.count: unknown argument "zzz"`,
		"block 7 (Tall): height must be between 80 and 1200, got 5000",
		"block 8 (Short): height must be between 80 and 1200, got 10",
		"block 9 (Empty prose): prose blocks need text",
		`block 10 (Bad schedule): schedule "whenever"`,
		"block 11 (Wide): span must be 1",
		"block 12 (Stray html): only custom blocks take html",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing problem %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "block 1 ") {
		t.Errorf("block 1 is fine:\n%s", joined)
	}
	if specs[0].Span != 1 {
		t.Error("span defaults to 1")
	}
}

func TestValidateAcceptsEveryTypeAndTheDefaultDashboard(t *testing.T) {
	look := lookup(t)
	ok := []Spec{
		{Type: "stats", Title: "S", Source: &Source{Query: "cards.count", Args: map[string]any{"by": "column"}}},
		{Type: "bars", Title: "B", Source: &Source{Query: "cards.count", Args: map[string]any{"by": "card.assignee"}}},
		{Type: "table", Title: "T1", Source: &Source{Query: "questions.open", Columns: []Column{{Key: "card", Label: "Card"}}}},
		{Type: "table", Title: "T2", Source: &Source{Query: "cards.count"}},
		{Type: "heatmap", Title: "H", Source: &Source{Query: "mcp.calls", Args: map[string]any{"by": "hour"}}},
		{Type: "prose", Title: "P", Text: "Hello"},
		{Type: "planner", Title: "Pl", Schedule: "daily 08:00"},
		{Type: "planner", Title: "Pl2", Schedule: "hourly"},
		{Type: "planner", Title: "Pl3"},
		{Type: "activity", Title: "A", Source: &Source{Query: "mcp.calls", Args: map[string]any{"limit": float64(7)}}},
		{Type: "custom", Title: "C", HTML: "<b>x</b>", Source: &Source{Height: 300}},
		{Type: "custom", Title: "C2", HTML: "<b>x</b>", Source: &Source{Query: "cards.list"}},
		{Type: "stats", Title: "I", Source: &Source{Query: "jira.count", Args: map[string]any{"by": "status"}}},
	}
	if problems := Validate(ok, look); len(problems) > 0 {
		t.Errorf("valid blocks refused: %v", problems)
	}
	// mcp.calls with by returns groups although its default shape is rows: a heatmap must accept it.
	// (Validation judges by the declared shape, so this is accepted by the table above through
	// ShapeRows for tables, and groups for heatmap via the by arg.)
}

func TestValidateRejectsTheEmptyAndTheHuge(t *testing.T) {
	look := lookup(t)
	if p := Validate(nil, look); len(p) != 1 || !strings.Contains(p[0], "at least one block") {
		t.Errorf("%v", p)
	}
	many := make([]Spec, MaxBlocks+1)
	for i := range many {
		many[i] = Spec{Type: "prose", Title: "x", Text: "y"}
	}
	if p := Validate(many, look); len(p) == 0 || !strings.Contains(p[0], "at most 24 blocks") {
		t.Errorf("%v", p)
	}
	big := Spec{Type: "custom", Title: "Big", HTML: strings.Repeat("a", MaxHTML+1)}
	if p := ValidateOne(&big, look); len(p) != 1 || !strings.Contains(p[0], "keep it under") {
		t.Errorf("%v", p)
	}
}

func TestTheSeededDashboardValidatesAndResolves(t *testing.T) {
	f := newFixture(t)
	blocks, err := f.st.Blocks.List(f.ws.ID)
	if err != nil || len(blocks) != 6 {
		t.Fatalf("the seeded dashboard: %d blocks, %v", len(blocks), err)
	}
	for _, b := range blocks {
		spec := SpecOf(b)
		if p := ValidateOne(&spec, f.env.Lookup); len(p) > 0 {
			t.Errorf("seeded block %q does not validate: %v", b.Title, p)
		}
		if b.Type == TypePlanner && b.Schedule != "" {
			t.Errorf("the default dashboard must not spend tokens: planner block %q has schedule %q", b.Title, b.Schedule)
		}
		src, _ := ParseSource(b.Source)
		if src.Query != "" {
			if _, err := Run(context.Background(), f.env, src.Query, src.Args); err != nil {
				t.Errorf("seeded block %q does not run: %v", b.Title, err)
			}
		}
	}
}

func heatmap(src *Source) Spec {
	return Spec{Type: TypeHeatmap, Title: "Deploys", Source: src}
}

func TestCalendarOptionsAreValidatedAndEveryProblemIsListed(t *testing.T) {
	f := newFixture(t)
	good := []Spec{
		heatmap(&Source{Query: "mcp.calls", Args: map[string]any{"by": "hour"}}),
		heatmap(&Source{Query: "mcp.calls", Args: map[string]any{"by": "hour"}, Layout: LayoutStrip, Unit: "calls"}),
		heatmap(&Source{Query: "mcp.calls", Args: map[string]any{"by": "hour"}, Layout: LayoutCalendar, Weeks: 53, Unit: "deploys"}),
		heatmap(&Source{Query: "mcp.calls", Args: map[string]any{"by": "hour"}, Weeks: 1}),
	}
	if errs := Validate(good, f.env.Lookup); len(errs) != 0 {
		t.Fatalf("valid options rejected: %v", errs)
	}
	bad := []Spec{
		heatmap(&Source{Query: "mcp.calls", Args: map[string]any{"by": "hour"}, Layout: "spiral", Weeks: 54, Unit: strings.Repeat("x", 31)}),
		heatmap(&Source{Query: "mcp.calls", Args: map[string]any{"by": "hour"}, Layout: LayoutStrip, Weeks: 4}),
		heatmap(&Source{Query: "mcp.calls", Args: map[string]any{"by": "hour"}, Weeks: -3, Unit: "a\nb"}),
		{Type: TypeBars, Title: "Bars", Source: &Source{Query: "cards.count", Layout: LayoutCalendar, Weeks: 10, Unit: "cards"}},
	}
	errs := strings.Join(Validate(bad, f.env.Lookup), "\n")
	for _, want := range []string{
		`block 1 (Deploys): layout "spiral" must be strip or calendar`,
		"block 1 (Deploys): weeks must be between 1 and 53, got 54",
		"block 1 (Deploys): the unit is longer than 30 characters",
		"block 2 (Deploys): weeks only applies to the calendar layout",
		"block 3 (Deploys): weeks must be between 1 and 53, got -3",
		"block 3 (Deploys): the unit must be plain text on one line",
		"block 4 (Bars): only heatmap blocks take a layout",
		"block 4 (Bars): only heatmap blocks take weeks",
		"block 4 (Bars): only heatmap blocks take a unit",
	} {
		if !strings.Contains(errs, want) {
			t.Errorf("missing %q in:\n%s", want, errs)
		}
	}
	// The options survive the round trip through a stored block.
	b, err := good[2].Domain(f.ws.ID)
	if err != nil {
		t.Fatal(err)
	}
	back := SpecOf(b)
	if back.Source == nil || back.Source.Layout != LayoutCalendar || back.Source.Weeks != 53 || back.Source.Unit != "deploys" {
		t.Errorf("round trip lost the options: %+v", back.Source)
	}
}

func TestDateKeysAndTheCalendarCheck(t *testing.T) {
	days := []Group{{Key: "2026-11-03"}, {Key: "2026-02-28"}}
	if !AllISODates(days) || CalendarProblem(days) != "" {
		t.Error("ISO dates are calendar-ready")
	}
	for _, bad := range []string{"2026-13-01", "2026-2-3", "03/11/2026", "Mon", "2026-11-03T10:00:00Z", ""} {
		if IsISODate(bad) {
			t.Errorf("%q is not an ISO date", bad)
		}
	}
	mixed := []Group{{Key: "2026-11-03"}, {Key: "tuesday"}}
	if AllISODates(mixed) || AllISODates(nil) || !strings.Contains(CalendarProblem(mixed), `"tuesday"`) {
		t.Error("a non-date key blocks the calendar and is named")
	}
}

// fakeCustom stands in for the integrations' script-defined queries.
type fakeCustom struct{}

func (fakeCustom) QuerySpecs(ws domain.Workspace) []QuerySpec {
	return []QuerySpec{{Name: "ci.deploys_per_day", Shape: ShapeGroups, Doc: "Deploys per day", Args: map[string]string{}, AnyArgs: true}}
}
func (fakeCustom) Query(_ context.Context, _ domain.Workspace, name string, args map[string]any) (Result, error) {
	return Result{Query: name, Shape: ShapeGroups, Groups: []Group{{Key: "2026-11-03", Label: "2026-11-03", Count: 4}}, Pending: args["pending"] == true}, nil
}

func TestScriptDefinedQueriesAreLookedUpAndAcceptAnyArgs(t *testing.T) {
	f := newFixture(t)
	f.env.Workspace.Integrations = []string{"jira", "ci"}
	if _, ok := f.env.Lookup("ci.deploys_per_day"); ok {
		t.Fatal("without a provider the query does not exist")
	}
	f.env.Custom = fakeCustom{}
	q, ok := f.env.Lookup("ci.deploys_per_day")
	if !ok || !q.AnyArgs {
		t.Fatalf("lookup = %+v %v", q, ok)
	}
	// Not in the workspace's integrations: not found.
	f.env.Workspace.Integrations = []string{"jira"}
	if _, ok := f.env.Lookup("ci.deploys_per_day"); ok {
		t.Error("a workspace that does not use the integration cannot use its queries")
	}
	f.env.Workspace.Integrations = []string{"jira", "ci"}
	spec := heatmap(&Source{Query: "ci.deploys_per_day", Args: map[string]any{"repo": "acme/web", "branch": "main"}, Layout: LayoutCalendar})
	if errs := ValidateOne(&spec, f.env.Lookup); len(errs) != 0 {
		t.Fatalf("free-form args are accepted: %v", errs)
	}
	res, err := Run(context.Background(), f.env, "ci.deploys_per_day", map[string]any{"repo": "acme/web"})
	if err != nil || len(res.Groups) != 1 || res.Groups[0].Count != 4 {
		t.Fatalf("run = %+v %v", res, err)
	}
	names := map[string]bool{}
	for _, q := range f.env.AllQueries() {
		names[q.Name] = true
	}
	for _, want := range []string{"cards.count", "jira.count", "ci.links", "ci.deploys_per_day"} {
		if !names[want] {
			t.Errorf("AllQueries lacks %s: %v", want, names)
		}
	}
}
