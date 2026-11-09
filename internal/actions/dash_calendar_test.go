package actions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/integrations"
)

const calendarScript = `var ops = { queries: { deploys_per_day: function (args) {
  return { groups: [ { key: "2026-11-03", count: 4 }, { key: args.bad ? "monday" : "2026-11-04", label: "Tue", count: 1 } ] };
} } };`

func (h *integHarness) withCalendarIntegration() {
	manifest, _ := json.Marshal(map[string]any{"name": "ci", "version": "1",
		"ops": map[string]any{"queries": map[string]any{"deploys_per_day": map[string]any{"description": "Successful runs per day"}}}})
	sc := domain.Script{Name: "ci", Kind: integrations.KindIntegration, Manifest: manifest, Source: calendarScript, Status: integrations.StatusEnabled}
	if err := h.reg.Deps().Store.Scripts.Create(&sc); err != nil {
		h.t.Fatal(err)
	}
	h.do("workspace_update", m{"workspace_id": h.ws.ID, "integrations": []string{"ci"}}, nil)
	if err := h.mgr.Sync(); err != nil {
		h.t.Fatal(err)
	}
}

func TestADashboardShowsAScriptQueryAsACalendarHeatmap(t *testing.T) {
	h := newIntegHarness(t)
	h.withCalendarIntegration()

	var got DashGetOut
	h.do("dash_get", m{}, &got)
	found := false
	for _, q := range got.Queries {
		found = found || (q.Name == "ci.deploys_per_day" && q.Doc == "Successful runs per day" && q.AnyArgs)
	}
	if !found {
		t.Fatalf("dash_get lists the integration's own queries: %+v", got.Queries)
	}

	// Calendar options are validated with the block.
	h.fails("dash_block_add", m{"type": "heatmap", "title": "Deploys", "source": m{"query": "ci.deploys_per_day", "layout": "spiral", "weeks": 99}}, "layout", "weeks must be between 1 and 53")
	var blk domain.Block
	h.do("dash_block_add", m{"type": "heatmap", "title": "Deploys", "span": 2,
		"source": m{"query": "ci.deploys_per_day", "args": m{"repo": "acme/web"}, "layout": "calendar", "weeks": 26, "unit": "deploys"}}, &blk)

	// The first render does not wait for the script; once it has run, the data is there.
	var view BlockView
	h.do("dash_block_refresh", m{"block_id": blk.ID}, &view)
	if view.Data == nil || !view.Data.Pending {
		t.Fatalf("first render: %+v", view)
	}
	h.mgr.WaitQueries()
	h.do("dash_block_refresh", m{"block_id": blk.ID}, &view)
	if view.Error != "" || view.Data == nil || len(view.Data.Groups) != 2 || view.Data.Groups[0].Count != 4 || view.Data.Groups[1].Label != "Tue" {
		t.Fatalf("second render: %+v", view)
	}

	// A calendar over keys that are not dates says so on the block.
	var bad domain.Block
	h.do("dash_block_add", m{"type": "heatmap", "title": "Odd", "source": m{"query": "ci.deploys_per_day", "args": m{"bad": true}, "layout": "calendar"}}, &bad)
	h.do("dash_block_refresh", m{"block_id": bad.ID}, &view)
	h.mgr.WaitQueries()
	h.do("dash_block_refresh", m{"block_id": bad.ID}, &view)
	if !strings.Contains(view.Error, `"monday"`) || !strings.Contains(view.Error, "YYYY-MM-DD") {
		t.Errorf("error = %q", view.Error)
	}
}
