package actions

import (
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
)

func historyIDs(h *harness, in m) []HistoryEntry {
	var out []HistoryEntry
	h.do("config_history", in, &out)
	return out
}

func TestAPlannerColumnRenameIsRevertedByYou(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	cols := h.columns(w.ID)
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	if err := h.as(planner, "column_update", m{"column_id": cols[1].ID, "name": "Doing"}, nil); err != nil {
		t.Fatal(err)
	}
	hist := historyIDs(h, m{"workspace_id": w.ID, "actor": "planner"})
	if len(hist) != 1 || hist[0].Actor != "planner" || hist[0].Kind != "column" || !hist[0].Revertible || !strings.Contains(hist[0].Summary, `name Working → Doing`) {
		t.Fatalf("history = %+v", hist)
	}
	var diff DiffOut
	h.do("config_diff", m{"change_id": hist[0].ID}, &diff)
	if len(diff.Diff) != 1 || diff.Diff[0].Field != "name" || diff.Diff[0].Before != "Working" || diff.Diff[0].After != "Doing" {
		t.Fatalf("diff = %+v", diff)
	}

	var out RevertOut
	h.do("config_revert", m{"change_id": hist[0].ID}, &out)
	if got := h.columns(w.ID)[1].Name; got != "Working" {
		t.Fatalf("column = %q after the revert", got)
	}
	all := historyIDs(h, m{"workspace_id": w.ID})
	if all[0].Action != "config_revert" || all[0].Actor != "you" {
		t.Errorf("the revert is itself a change by the caller: %+v", all[0])
	}
	if pl := historyIDs(h, m{"workspace_id": w.ID, "actor": "planner"}); len(pl) != 1 || !pl[0].Reverted || pl[0].Revertible {
		t.Errorf("the original is marked reverted: %+v", pl)
	}
	h.fails("config_revert", m{"change_id": hist[0].ID}, "already reverted")
}

func TestRevertIsRefusedAfterALaterEdit(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	col := h.columns(w.ID)[1]
	h.do("column_update", m{"column_id": col.ID, "name": "Doing"}, nil)
	first := historyIDs(h, m{"workspace_id": w.ID})[0]
	h.do("column_update", m{"column_id": col.ID, "name": "Active"}, nil)
	h.fails("config_revert", m{"change_id": first.ID}, "changed since", "later changes first")
	if h.columns(w.ID)[1].Name != "Active" {
		t.Error("a refused revert changes nothing")
	}
	// Unwinding newest first works.
	second := historyIDs(h, m{"workspace_id": w.ID})[0]
	h.do("config_revert", m{"change_id": second.ID}, nil)
	h.do("config_revert", m{"change_id": first.ID}, nil)
	if h.columns(w.ID)[1].Name != "Working" {
		t.Errorf("name = %q", h.columns(w.ID)[1].Name)
	}
}

func TestHistoryFiltersByActorAndSkipsRuleLogRows(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	col := h.columns(w.ID)[1]
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	_ = h.as(planner, "column_update", m{"column_id": col.ID, "wip": 3}, nil)
	h.do("column_update", m{"column_id": col.ID, "wip": 4}, nil)
	if got := historyIDs(h, m{"workspace_id": w.ID, "actor": "planner"}); len(got) != 1 {
		t.Errorf("planner: %+v", got)
	}
	if got := historyIDs(h, m{"workspace_id": w.ID, "actor": "you"}); len(got) < 2 {
		t.Errorf("you made the workspace and an edit: %+v", got)
	}
	if got := historyIDs(h, m{"workspace_id": w.ID, "actor": "session"}); len(got) != 0 {
		t.Errorf("session: %+v", got)
	}
	if got := historyIDs(h, m{"workspace_id": w.ID, "limit": 1}); len(got) != 1 {
		t.Errorf("limit: %+v", got)
	}
	for _, e := range historyIDs(h, m{"workspace_id": w.ID}) {
		if e.Action == "rule_fire" || e.Action == "rule_skip" {
			t.Errorf("rule log rows are not config history: %+v", e)
		}
	}
}

func TestRevertCoversRulesViewsPromptsProfilesAndTheDashboard(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	last := func() HistoryEntry { return historyIDs(h, m{"workspace_id": w.ID})[0] }

	var rule domain.Rule
	h.do("rule_create", m{"name": "R", "trigger": m{"on": "card_created"}, "actions": []m{{"type": "notify", "text": "hi"}}}, &rule)
	h.do("rule_update", m{"rule_id": rule.ID, "name": "R2"}, nil)
	h.do("config_revert", m{"change_id": last().ID}, nil)
	var rules []domain.Rule
	h.do("rule_list", nil, &rules)
	found := false
	for _, r := range rules {
		if r.ID == rule.ID && r.Name == "R" {
			found = true
		}
	}
	if !found {
		t.Errorf("rule name not restored: %+v", rules)
	}
	h.do("rule_delete", m{"rule_id": rule.ID}, nil)
	h.do("config_revert", m{"change_id": last().ID}, nil) // brings the deleted rule back
	h.do("rule_list", nil, &rules)
	back := false
	for _, r := range rules {
		back = back || r.ID == rule.ID
	}
	if !back {
		t.Error("a deleted rule is recreated")
	}

	h.do("prompt_set", m{"kind": "planner", "template": "Custom {{workspace.name}}"}, nil)
	h.do("config_revert", m{"change_id": last().ID}, nil)
	var p PromptOut
	h.do("prompt_get", m{"kind": "planner"}, &p)
	if p.Custom {
		t.Error("the prompt is back to the default")
	}

	var view domain.View
	h.do("view_save", m{"name": "Mine", "filters": m{"q": "a"}}, &view)
	h.do("view_save", m{"view_id": view.ID, "name": "Mine2", "filters": m{"q": "a"}}, nil)
	h.do("config_revert", m{"change_id": last().ID}, nil)
	var views []domain.View
	h.do("view_list", nil, &views)
	if len(views) != 1 || views[0].Name != "Mine" {
		t.Errorf("views = %+v", views)
	}

	var dashBefore DashGetOut
	h.do("dash_get", nil, &dashBefore)
	h.do("dash_set", m{"blocks": []m{{"type": "prose", "title": "Only", "text": "x"}}}, nil)
	h.do("config_revert", m{"change_id": last().ID}, nil)
	var dashAfter DashGetOut
	h.do("dash_get", nil, &dashAfter)
	if len(dashAfter.Blocks) != len(dashBefore.Blocks) || dashAfter.Blocks[0].ID != dashBefore.Blocks[0].ID {
		t.Errorf("dashboard not restored: %d vs %d blocks", len(dashAfter.Blocks), len(dashBefore.Blocks))
	}

	cols := h.columns(w.ID)
	ids := []string{}
	for i := len(cols) - 1; i >= 0; i-- {
		ids = append(ids, cols[i].ID)
	}
	h.do("column_reorder", m{"workspace_id": w.ID, "column_ids": ids}, nil)
	h.do("config_revert", m{"change_id": last().ID}, nil)
	for i, c := range h.columns(w.ID) {
		if c.ID != cols[i].ID {
			t.Errorf("column order not restored at %d", i)
		}
	}
}

func TestRevertRefusesNonConfigAndSessions(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("W")
	var c domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "alpha", "start": false}, &c)
	st := h.reg.Deps().Store
	ch := domain.Change{WorkspaceID: w.ID, Actor: "rule:x", Action: "rule_fire", Args: []byte(`{}`), Before: []byte(`null`), After: []byte(`{"text":"y"}`)}
	if err := st.Changes.Create(&ch); err != nil {
		t.Fatal(err)
	}
	h.fails("config_revert", m{"change_id": ch.ID}, "not a config change")
	h.fails("config_revert", m{"change_id": "nope"}, "no change")
	col := h.columns(w.ID)[0]
	h.do("column_update", m{"column_id": col.ID, "wip": 2}, nil)
	id := historyIDs(h, m{"workspace_id": w.ID})[0].ID
	s := Actor{Kind: KindSession, Name: "alpha", WorkspaceID: w.ID, CardID: c.ID}
	if err := h.as(s, "config_revert", m{"change_id": id}, nil); err == nil || !strings.Contains(err.Error(), "card session") {
		t.Errorf("err = %v", err)
	}
	h.fails("config_revert", m{"change_id": historyIDs(h, m{"workspace_id": w.ID, "limit": 50})[len(historyIDs(h, m{"workspace_id": w.ID, "limit": 50}))-1].ID}, "")
}
