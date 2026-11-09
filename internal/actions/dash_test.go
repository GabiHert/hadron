package actions

import (
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/dash"
	"github.com/GabiHert/hadron/internal/domain"
)

func TestDashGetResolvesTheDefaultDashboard(t *testing.T) {
	h := newHarness(t)
	h.workspace("W")
	var out DashGetOut
	h.do("dash_get", nil, &out)
	if len(out.Blocks) != 6 {
		t.Fatalf("blocks = %d", len(out.Blocks))
	}
	byTitle := map[string]BlockView{}
	for _, b := range out.Blocks {
		byTitle[b.Title] = b
		if b.Error != "" {
			t.Errorf("block %q: %s", b.Title, b.Error)
		}
	}
	if d := byTitle["Cards by column"].Data; d == nil || d.Shape != "groups" || len(d.Groups) != 5 {
		t.Errorf("stats = %+v", d)
	}
	if byTitle["Planner's summary"].Data != nil || byTitle["Planner's summary"].Schedule != "" {
		t.Error("the default planner block has no data and no schedule: it spends no tokens")
	}
	if len(out.Queries) < 5 {
		t.Errorf("queries = %+v", out.Queries)
	}
}

func TestDashBlockAddUpdateMoveRemoveAndHistory(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	var b domain.Block
	h.do("dash_block_add", m{"type": "bars", "title": "By assignee", "source": m{"query": "cards.count", "args": m{"by": "card.assignee"}}}, &b)
	if b.Type != "bars" || b.Span != 1 || b.Position != 6 {
		t.Fatalf("added = %+v", b)
	}
	h.do("dash_block_add", m{"type": "prose", "title": "Note", "text": "Hello", "span": 2, "position": 0}, &b)
	var out DashGetOut
	h.do("dash_get", nil, &out)
	if out.Blocks[0].Title != "Note" || out.Blocks[0].Span != 2 || len(out.Blocks) != 8 {
		t.Fatalf("position 0: %+v", out.Blocks[0])
	}

	var ordered []domain.Block
	h.do("dash_block_move", m{"block_id": b.ID, "position": 3}, &ordered)
	if ordered[3].ID != b.ID || len(ordered) != 8 {
		t.Errorf("moved to 3: %v", titles(ordered))
	}
	for i, x := range ordered {
		if x.Position != i {
			t.Errorf("positions must be renumbered: %d has %d", i, x.Position)
		}
	}

	var upd domain.Block
	h.do("dash_block_update", m{"block_id": b.ID, "title": "Renamed", "span": 1, "text": "New text"}, &upd)
	if upd.Title != "Renamed" || upd.Span != 1 || upd.Text != "New text" {
		t.Errorf("updated = %+v", upd)
	}
	h.fails("dash_block_update", m{"block_id": b.ID, "text": ""}, "prose blocks need text")
	h.fails("dash_block_update", m{"block_id": b.ID, "span": 5}, "span must be 1")

	h.do("dash_block_remove", m{"block_id": b.ID}, nil)
	h.do("dash_get", nil, &out)
	if len(out.Blocks) != 7 {
		t.Errorf("after remove: %d", len(out.Blocks))
	}
	h.fails("dash_block_remove", m{"block_id": b.ID}, "no block")

	changes, _ := h.reg.Deps().Store.Changes.List(w.ID, 100)
	seen := map[string]bool{}
	for _, c := range changes {
		seen[c.Action] = true
	}
	for _, a := range []string{"dash_block_add", "dash_block_update", "dash_block_move", "dash_block_remove"} {
		if !seen[a] {
			t.Errorf("%s must be in the config history", a)
		}
	}
}

func titles(bs []domain.Block) []string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Title)
	}
	return out
}

func TestDashBlockAddListsEveryProblem(t *testing.T) {
	h := newHarness(t)
	h.workspace("W")
	h.fails("dash_block_add", m{"type": "stats", "title": "  ", "source": m{"query": "cards.nope"}}, "needs a title", "unknown query")
	h.fails("dash_block_add", m{"type": "custom", "title": "C", "html": "<b>x</b>", "source": m{"height": 20}}, "height must be between 80 and 1200")
	h.fails("dash_block_add", m{"type": "bars", "title": "B"}, "need a source query")
}

func TestDashSetReplacesAtomically(t *testing.T) {
	h := newHarness(t)
	h.workspace("W")
	h.fails("dash_set", m{"blocks": []m{{"type": "prose", "title": "ok", "text": "x"}, {"type": "wat", "title": "bad"}}}, `block 2 (bad): type "wat"`)
	var out DashGetOut
	h.do("dash_get", nil, &out)
	if len(out.Blocks) != 6 {
		t.Fatalf("a refused dash_set changes nothing, got %d blocks", len(out.Blocks))
	}
	h.do("dash_set", m{"blocks": []m{
		{"type": "stats", "title": "Count", "span": 2, "source": m{"query": "cards.count"}},
		{"type": "prose", "title": "Note", "text": "x"},
	}}, nil)
	h.do("dash_get", nil, &out)
	if len(out.Blocks) != 2 || out.Blocks[0].Title != "Count" || out.Blocks[0].Data == nil {
		t.Errorf("%+v", out.Blocks)
	}
	h.fails("dash_set", m{"blocks": []m{}}, "at least one block")
}

func TestDashQueryRun(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	var c domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "alpha", "start": false}, &c)
	var res dash.Result
	h.do("dash_query_run", m{"query": "cards.count", "args": m{"by": "column"}}, &res)
	total := 0
	for _, g := range res.Groups {
		total += g.Count
	}
	if total != 1 {
		t.Errorf("%+v", res)
	}
	h.fails("dash_query_run", m{"query": "nope.query"}, "unknown query")
}

func TestSessionsCannotEditTheDashboard(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	var c domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "alpha", "start": false}, &c)
	s := Actor{Kind: KindSession, Name: "alpha", WorkspaceID: w.ID, CardID: c.ID}
	if err := h.as(s, "dash_block_add", m{"type": "prose", "title": "x", "text": "y"}, nil); err == nil || !strings.Contains(err.Error(), "card session cannot") {
		t.Errorf("err = %v", err)
	}
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	if err := h.as(planner, "dash_block_add", m{"type": "prose", "title": "x", "text": "y"}, nil); err != nil {
		t.Errorf("the planner edits the dashboard: %v", err)
	}
}

func TestPlannerBlockRefreshAsksThePlannerAndTheAnswerLandsViaUpdate(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	var out DashGetOut
	h.do("dash_get", nil, &out)
	var pb domain.Block
	for _, b := range out.Blocks {
		if b.Type == "planner" {
			pb = b.Block
		}
	}
	h.fails("dash_block_refresh", m{"block_id": pb.ID}, "could not ask the planner", "planner_start")

	h.do("planner_start", m{}, nil)
	waitUntil(t, "the planner", func() bool { return h.file(".started") != "" })
	h.do("dash_block_refresh", m{"block_id": pb.ID}, nil)
	waitUntil(t, "the instruction", func() bool { return strings.Contains(h.file(".in"), "[dashboard]") })
	in := h.file(".in")
	if !strings.Contains(in, `"block_id":"`+pb.ID+`"`) || !strings.Contains(in, "dash_block_update") {
		t.Errorf("the planner is told which block and which tool: %q", in)
	}

	// The planner answers by calling dash_block_update with text, which stamps the time.
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	var upd domain.Block
	if err := h.as(planner, "dash_block_update", m{"block_id": pb.ID, "text": "Two cards wait on you."}, &upd); err != nil {
		t.Fatal(err)
	}
	if upd.Text != "Two cards wait on you." || upd.TextAt == nil || time.Since(*upd.TextAt) > time.Minute {
		t.Errorf("%+v", upd)
	}

	// A data block refresh reruns its query and sends nothing to the planner.
	before := h.file(".in")
	var stats BlockView
	for _, b := range out.Blocks {
		if b.Type == "stats" {
			h.do("dash_block_refresh", m{"block_id": b.ID}, &stats)
		}
	}
	if stats.Data == nil || h.file(".in") != before {
		t.Errorf("a data block refresh must not touch the planner: %+v", stats)
	}
}

func TestPlannerBlocksRunOnlyOnTheirScheduleAndOnlyWithoutAskingForever(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("W")
	h.do("planner_start", m{}, nil)
	waitUntil(t, "the planner", func() bool { return h.file(".started") != "" })
	var scheduled domain.Block
	h.do("dash_block_add", m{"workspace_id": w.ID, "type": "planner", "title": "Morning", "schedule": "daily 08:00"}, &scheduled)

	base := time.Date(2026, 11, 8, 7, 59, 0, 0, time.Local)
	r := h.reg
	if err := r.TickDashboard(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	if err := r.TickDashboard(t.Context(), base.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.file(".in"), "[dashboard]") {
		t.Fatal("not yet 08:00, and the default planner block has no schedule")
	}
	if err := r.TickDashboard(t.Context(), base.Add(90*time.Second)); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the scheduled summary request", func() bool { return strings.Contains(h.file(".in"), `"block_id":"`+scheduled.ID+`"`) })
	if n := strings.Count(h.file(".in"), "[dashboard]"); n != 1 {
		t.Errorf("only the scheduled block is asked, once: %d requests\n%s", n, h.file(".in"))
	}
	// 30 s later is not another 08:00.
	if err := r.TickDashboard(t.Context(), base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if n := strings.Count(h.file(".in"), "[dashboard]"); n != 1 {
		t.Errorf("asked again: %d", n)
	}
}

func TestScheduleDue(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 11, 8, h, m, 0, 0, time.Local) }
	cases := []struct {
		sched     string
		last, now time.Time
		want      bool
	}{
		{"daily 08:00", at(7, 59), at(8, 0), true},
		{"daily 08:00", at(8, 0), at(8, 1), false},
		{"daily 08:00", at(8, 1), at(23, 0), false},
		{"daily 08:00", at(23, 0), at(23, 0).AddDate(0, 0, 1).Add(-15 * time.Hour), true}, // 08:00 next day
		{"hourly", at(9, 59), at(10, 0), true},
		{"hourly", at(10, 0), at(10, 30), false},
		{"", at(7, 0), at(9, 0), false},
		{"daily 99:99", at(7, 0), at(9, 0), false},
	}
	for _, c := range cases {
		if got := scheduleDue(c.sched, c.last, c.now); got != c.want {
			t.Errorf("%q %v -> %v = %v, want %v", c.sched, c.last.Format("15:04"), c.now.Format("15:04 Jan 2"), got, c.want)
		}
	}
}

func TestWidgetPinMakesACustomBlockOnce(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	var msg domain.Message
	h.do("widget_post", m{"workspace_id": w.ID, "channel": "planner", "title": "Burn-down", "html": "<b>hi</b>"}, &msg)
	var b domain.Block
	h.do("widget_pin", m{"message_id": msg.ID}, &b)
	if b.Type != "custom" || b.Title != "Burn-down" || b.HTML != "<b>hi</b>" || !strings.Contains(string(b.Source), msg.ID) {
		t.Fatalf("pinned = %+v", b)
	}
	var again domain.Block
	h.do("widget_pin", m{"message_id": msg.ID}, &again)
	if again.ID != b.ID {
		t.Error("pinning twice returns the same block")
	}
	var out DashGetOut
	h.do("dash_get", nil, &out)
	if len(out.Blocks) != 7 {
		t.Errorf("blocks = %d", len(out.Blocks))
	}
	var text domain.Message
	h.do("chat_send", m{"workspace_id": w.ID, "channel": "planner", "text": "hello"}, &text)
	h.fails("widget_pin", m{"message_id": text.ID}, "not a widget")
	h.fails("widget_pin", m{"message_id": "nope"}, "no message")
}

func TestOnlyTheUserLetsWidgetsRunScripts(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	if err := h.as(planner, "settings_update", m{"values": m{"widgets.scripts": true}}, nil); err == nil || !strings.Contains(err.Error(), "only the user") {
		t.Errorf("the planner must not enable scripts in the widgets it writes: %v", err)
	}
	h.fails("settings_update", m{"values": m{"widgets.scripts": "yes"}}, "true or false")
	h.do("settings_update", m{"values": m{"widgets.scripts": true}}, nil)
	var got map[string]bool
	h.do("settings_get", m{"key": "widgets.scripts"}, &got)
	if !got["widgets.scripts"] {
		t.Errorf("%v", got)
	}
}
