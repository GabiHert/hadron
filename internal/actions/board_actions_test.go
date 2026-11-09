package actions

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/prompts"
	"github.com/GabiHert/hadron/internal/store"
)

type harness struct {
	t   *testing.T
	reg *Registry
	rec *recorder
}

func newHarness(t *testing.T) *harness {
	deps, rec := testDeps(t)
	r := NewRegistry(deps)
	RegisterAll(r)
	return &harness{t: t, reg: r, rec: rec}
}

// do invokes an action as you and decodes the result into out (may be nil).
func (h *harness) do(name string, args any, out any) {
	h.t.Helper()
	if err := h.try(name, args, out); err != nil {
		h.t.Fatalf("%s: %v", name, err)
	}
}

func (h *harness) try(name string, args any, out any) error {
	h.t.Helper()
	var raw string
	switch a := args.(type) {
	case nil:
	case string:
		raw = a
	default:
		b, _ := json.Marshal(a)
		raw = string(b)
	}
	res, err := h.reg.Invoke(context.Background(), You, name, raw)
	if err == nil && out != nil {
		if jerr := json.Unmarshal([]byte(res), out); jerr != nil {
			h.t.Fatalf("%s: bad result %s: %v", name, res, jerr)
		}
	}
	return err
}

// fails asserts the action fails with an error containing every want.
func (h *harness) fails(name string, args any, wants ...string) {
	h.t.Helper()
	err := h.try(name, args, nil)
	if err == nil {
		h.t.Fatalf("%s(%v) succeeded, want an error containing %v", name, args, wants)
	}
	for _, w := range wants {
		if !strings.Contains(err.Error(), w) {
			h.t.Errorf("%s error %q lacks %q", name, err.Error(), w)
		}
	}
}

type m = map[string]any

func (h *harness) workspace(name string) domain.Workspace {
	var w domain.Workspace
	h.do("workspace_create", m{"name": name, "root": h.t.TempDir()}, &w)
	return w
}

func (h *harness) columns(ws string) []domain.Column {
	var cols []domain.Column
	h.do("column_list", m{"workspace_id": ws}, &cols)
	return cols
}

func (h *harness) cards(args m) []domain.Card {
	var cards []domain.Card
	h.do("card_list", args, &cards)
	return cards
}

func colByRole(cols []domain.Column, role string) domain.Column {
	for _, c := range cols {
		if c.Role == role {
			return c
		}
	}
	return domain.Column{}
}

func nicks(cards []domain.Card) string {
	n := make([]string, len(cards))
	for i, c := range cards {
		n[i] = c.Nick
	}
	return strings.Join(n, ",")
}

// ---- workspaces ----

func TestWorkspaceCreateSeedsEverything(t *testing.T) {
	h := newHarness(t)
	root := t.TempDir()
	var w domain.Workspace
	h.do("workspace_create", m{"name": "  Acme  ", "root": root, "integrations": []string{"linear"}}, &w)
	if w.Name != "Acme" || w.Root != root || w.Color != DefaultColors[0] || w.Integrations[0] != "linear" {
		t.Fatalf("workspace = %+v", w)
	}

	cols := h.columns(w.ID)
	want := []struct{ name, role string }{{"Needs you", "needs-you"}, {"Working", "active"}, {"Blocked", "blocked"}, {"Done", "done"}, {"Backlog", "backlog"}}
	if len(cols) != len(want) {
		t.Fatalf("columns = %d, want %d", len(cols), len(want))
	}
	for i, wc := range want {
		if cols[i].Name != wc.name || cols[i].Role != wc.role || cols[i].Position != i {
			t.Errorf("column %d = %s/%s/%d, want %s/%s/%d", i, cols[i].Name, cols[i].Role, cols[i].Position, wc.name, wc.role, i)
		}
	}

	st := h.reg.Deps().Store
	rules, _ := st.Rules.List(w.ID)
	if len(rules) != 2 || !rules[0].Builtin || !rules[1].Builtin || !rules[0].Enabled {
		t.Fatalf("rules = %+v", rules)
	}
	blocks, _ := st.Blocks.List(w.ID)
	types := make([]string, len(blocks))
	for i, b := range blocks {
		types[i] = b.Type
		if b.Type == "planner" && b.Schedule != "" {
			t.Errorf("the default planner block must not run on a schedule, got %q", b.Schedule)
		}
	}
	if strings.Join(types, ",") != "stats,table,bars,planner,custom,activity" {
		t.Fatalf("dashboard blocks = %v", types)
	}
	for _, kind := range []string{prompts.KindPlanner, prompts.KindSession} {
		p, err := st.Prompts.Get(w.ID, kind)
		if err != nil || p.Template != nil {
			t.Errorf("prompt %s = %+v, %v; want a row that points at the default", kind, p, err)
		}
	}
	active, _ := st.Settings.Get(SettingActiveWorkspace)
	if string(active) != `"`+w.ID+`"` {
		t.Fatalf("the first workspace should become active, got %s", active)
	}

	var second domain.Workspace
	h.do("workspace_create", m{"name": "Second", "template": "simple", "root": root}, &second)
	if second.Color != DefaultColors[1] {
		t.Errorf("second color = %s, want the next swatch", second.Color)
	}
	if cols := h.columns(second.ID); len(cols) != 3 || cols[0].Name != "To do" {
		t.Errorf("simple template columns = %+v", cols)
	}
	active, _ = st.Settings.Get(SettingActiveWorkspace)
	if string(active) != `"`+w.ID+`"` {
		t.Errorf("a second workspace must not steal the active one, got %s", active)
	}

	if len(h.rec.got) == 0 {
		t.Fatal("no changed event")
	}
	last := h.rec.got[len(h.rec.got)-1].payload.(ChangedEvent)
	if last.WorkspaceID != second.ID {
		t.Errorf("event workspace = %s, want %s", last.WorkspaceID, second.ID)
	}
}

func TestWorkspaceCreateCopiesColumns(t *testing.T) {
	h := newHarness(t)
	src := h.workspace("src")
	var col domain.Column
	h.do("column_create", m{"workspace_id": src.ID, "name": "Review", "role": "active", "wip": 2}, &col)
	var cp domain.Workspace
	h.do("workspace_create", m{"name": "copy", "template": "copy", "copy_from": src.ID, "root": t.TempDir()}, &cp)
	got, want := h.columns(cp.ID), h.columns(src.ID)
	if len(got) != len(want) || got[5].Name != "Review" || got[5].WIP != 2 || got[5].ID == col.ID {
		t.Fatalf("copied columns = %+v", got)
	}
	h.fails("workspace_create", m{"name": "x", "template": "copy"}, "copy_from")
	h.fails("workspace_create", m{"name": "x", "template": "copy", "copy_from": "nope"}, "no columns to copy")
}

func TestWorkspaceCreateValidation(t *testing.T) {
	h := newHarness(t)
	file := filepath.Join(t.TempDir(), "afile")
	_ = os.WriteFile(file, []byte("x"), 0o600)
	for _, tc := range []struct {
		name string
		args m
		want string
	}{
		{"blank name", m{"name": "   "}, "name must not be empty"},
		{"long name", m{"name": strings.Repeat("a", 61)}, "61 characters"},
		{"bad color", m{"name": "x", "color": "blue"}, "#24408e"},
		{"bad template", m{"name": "x", "template": "weird"}, "planner, simple, copy"},
		{"root is a file", m{"name": "x", "root": file}, "is a file, not a folder"},
	} {
		t.Run(tc.name, func(t *testing.T) { h.fails("workspace_create", tc.args, tc.want) })
	}
	if ws, _ := h.reg.Deps().Store.Workspaces.List(true); len(ws) != 0 {
		t.Fatalf("a rejected create left %d workspaces", len(ws))
	}
}

func TestWorkspaceCreateExpandsTildeRoot(t *testing.T) {
	h := newHarness(t)
	home, _ := os.UserHomeDir()
	var w domain.Workspace
	h.do("workspace_create", m{"name": "home", "root": "~/Documents/hadron-test-not-created"}, &w)
	if w.Root != filepath.Join(home, "Documents", "hadron-test-not-created") {
		t.Fatalf("root = %s", w.Root)
	}
	var blank domain.Workspace
	h.do("workspace_create", m{"name": "blank"}, &blank)
	if blank.Root != home {
		t.Fatalf("empty root = %s, want the home folder", blank.Root)
	}
}

func TestWorkspaceUpdateArchiveSwitchDelete(t *testing.T) {
	h := newHarness(t)
	a, b := h.workspace("a"), h.workspace("b")

	var up domain.Workspace
	h.do("workspace_update", m{"workspace_id": a.ID, "name": "Alpha", "filter_bar": []string{"card.assignee"}}, &up)
	if up.Name != "Alpha" || up.FilterBar[0] != "card.assignee" || up.Color != a.Color || up.Root != a.Root {
		t.Fatalf("partial update changed too much: %+v", up)
	}
	h.fails("workspace_update", m{"workspace_id": a.ID, "name": ""}, "must not be empty")
	h.fails("workspace_update", m{"workspace_id": "nope", "name": "x"}, "does not exist")

	var arch domain.Workspace
	h.do("workspace_archive", m{"workspace_id": b.ID}, &arch)
	if !arch.Archived {
		t.Fatal("not archived")
	}
	var live, all []domain.Workspace
	h.do("workspace_list", nil, &live)
	h.do("workspace_list", m{"include_archived": true}, &all)
	if len(live) != 1 || len(all) != 2 {
		t.Fatalf("list = %d live, %d all", len(live), len(all))
	}
	h.fails("workspace_switch", m{"workspace_id": b.ID}, "is archived")
	h.do("workspace_archive", m{"workspace_id": b.ID, "archived": false}, &arch)
	if arch.Archived {
		t.Fatal("not restored")
	}
	h.do("workspace_switch", m{"workspace_id": b.ID}, nil)
	var got domain.Workspace
	h.do("workspace_get", m{"workspace_id": b.ID}, &got)
	if got.ID != b.ID {
		t.Fatal("workspace_get returned the wrong workspace")
	}
	active, _ := h.reg.Deps().Store.Settings.Get(SettingActiveWorkspace)
	if string(active) != `"`+b.ID+`"` {
		t.Fatalf("active = %s", active)
	}

	h.do("workspace_delete", m{"workspace_id": b.ID}, nil)
	active, _ = h.reg.Deps().Store.Settings.Get(SettingActiveWorkspace)
	if string(active) != `"`+a.ID+`"` {
		t.Fatalf("after deleting the active workspace, active = %s, want %s", active, a.ID)
	}
	h.fails("workspace_get", m{"workspace_id": b.ID}, "does not exist")
	if cols, _ := h.reg.Deps().Store.Columns.List(b.ID); len(cols) != 0 {
		t.Fatal("columns survived the workspace")
	}
}

func TestWorkspaceActionsRecordConfigHistory(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("old")
	h.do("workspace_update", m{"workspace_id": w.ID, "name": "new"}, nil)
	h.do("workspace_delete", m{"workspace_id": w.ID}, nil)
	changes, _ := h.reg.Deps().Store.Changes.List(w.ID, 10)
	if len(changes) != 3 {
		t.Fatalf("changes = %d, want create, update, delete", len(changes))
	}
	del, upd, cre := changes[0], changes[1], changes[2]
	if del.Action != "workspace_delete" || !strings.Contains(string(del.Before), `"new"`) || string(del.After) != "null" {
		t.Errorf("delete change: before=%s after=%s", del.Before, del.After)
	}
	if !strings.Contains(string(upd.Before), `"old"`) || !strings.Contains(string(upd.After), `"new"`) {
		t.Errorf("update change: before=%s after=%s", upd.Before, upd.After)
	}
	if string(cre.Before) != "null" || !strings.Contains(string(cre.After), `"old"`) {
		t.Errorf("create change: before=%s after=%s", cre.Before, cre.After)
	}
}

// ---- columns ----

func TestColumnCreateUpdateKeepsIdAndValidates(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	var col domain.Column
	h.do("column_create", m{"workspace_id": w.ID, "name": "Review", "role": "active", "wip": 3, "color": "#b07a2a"}, &col)
	if col.Position != 5 || col.WIP != 3 || col.Role != "active" || col.Color != "#b07a2a" {
		t.Fatalf("column = %+v", col)
	}
	var renamed domain.Column
	h.do("column_update", m{"column_id": col.ID, "name": "Code review"}, &renamed)
	if renamed.ID != col.ID || renamed.Name != "Code review" || renamed.WIP != 3 || renamed.Role != "active" {
		t.Fatalf("rename lost or changed something: %+v", renamed)
	}
	var folded domain.Column
	h.do("column_update", m{"column_id": col.ID, "fold": true, "wip": 0, "role": ""}, &folded)
	if !folded.Fold || folded.WIP != 0 || folded.Role != "" || folded.Name != "Code review" {
		t.Fatalf("update = %+v", folded)
	}

	for _, tc := range []struct {
		action string
		args   m
		want   string
	}{
		{"column_create", m{"workspace_id": w.ID, "name": ""}, "name is required"},
		{"column_create", m{"workspace_id": w.ID, "name": "x", "role": "boss"}, `role "boss" is not one of`},
		{"column_create", m{"workspace_id": w.ID, "name": "x", "wip": -1}, "wip must be"},
		{"column_create", m{"workspace_id": w.ID, "name": "x", "color": "red"}, "#24408e"},
		{"column_create", m{"workspace_id": "nope", "name": "x"}, "does not exist"},
		{"column_update", m{"column_id": "nope", "name": "x"}, `column "nope" does not exist`},
		{"column_update", m{"column_id": col.ID, "wip": -4}, "wip must be"},
		{"column_update", m{"column_id": col.ID, "role": "boss"}, "is not one of"},
		{"column_update", m{"column_id": col.ID, "name": "   "}, "must not be empty"},
	} {
		h.fails(tc.action, tc.args, tc.want)
	}
}

func TestColumnUpdateRecordsBeforeAndAfter(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	col := h.columns(w.ID)[1]
	h.do("column_update", m{"column_id": col.ID, "name": "Doing", "wip": 4}, nil)
	changes, _ := h.reg.Deps().Store.Changes.List(w.ID, 1)
	var before, after domain.Column
	_ = json.Unmarshal(changes[0].Before, &before)
	_ = json.Unmarshal(changes[0].After, &after)
	if changes[0].Action != "column_update" || changes[0].Actor != "you" || before.Name != "Working" || after.Name != "Doing" || before.WIP != 0 || after.WIP != 4 || before.ID != after.ID {
		t.Fatalf("change = %+v / before %+v / after %+v", changes[0], before, after)
	}
}

func TestColumnReorder(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	cols := h.columns(w.ID)
	order := []string{cols[4].ID, cols[0].ID, cols[1].ID, cols[2].ID, cols[3].ID}
	var out []domain.Column
	h.do("column_reorder", m{"workspace_id": w.ID, "column_ids": order}, &out)
	for i, id := range order {
		if out[i].ID != id || out[i].Position != i {
			t.Fatalf("position %d = %s, want %s", i, out[i].Name, id)
		}
	}
	h.fails("column_reorder", m{"workspace_id": w.ID, "column_ids": order[:3]}, "must list all 5 columns")
	h.fails("column_reorder", m{"workspace_id": w.ID, "column_ids": []string{order[0], order[0], order[1], order[2], order[3]}}, "listed twice")
	changes, _ := h.reg.Deps().Store.Changes.List(w.ID, 1)
	if changes[0].Action != "column_reorder" || !strings.Contains(string(changes[0].Before), cols[0].ID) {
		t.Fatalf("reorder change = %+v", changes[0])
	}
}

func TestColumnDeleteMovesItsCards(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	cols := h.columns(w.ID)
	needs, working, blocked := cols[0], cols[1], cols[2]
	for _, n := range []string{"a", "b"} {
		h.do("card_create", m{"workspace_id": w.ID, "nick": n, "column_id": working.ID}, nil)
	}
	h.do("card_create", m{"workspace_id": w.ID, "nick": "c", "column_id": needs.ID}, nil)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "z", "column_id": blocked.ID}, nil)

	var out ColumnDeleteOut
	h.do("column_delete", m{"column_id": working.ID}, &out)
	if out.Deleted.ID != working.ID || out.MovedTo.ID != needs.ID || out.CardCount != 2 {
		t.Fatalf("out = %+v; want the first other column to take 2 cards", out)
	}
	if got := nicks(h.cards(m{"workspace_id": w.ID, "column_id": needs.ID})); got != "c,a,b" {
		t.Fatalf("cards in the first column = %s, want c,a,b", got)
	}
	if len(h.columns(w.ID)) != 4 {
		t.Fatal("column not deleted")
	}

	h.fails("column_delete", m{"column_id": needs.ID, "move_to": needs.ID}, "must be another column")
	h.fails("column_delete", m{"column_id": needs.ID, "move_to": "nope"}, "must be another column")
	h.do("column_delete", m{"column_id": needs.ID, "move_to": blocked.ID}, &out)
	if got := nicks(h.cards(m{"workspace_id": w.ID, "column_id": blocked.ID})); got != "z,c,a,b" {
		t.Fatalf("cards after move_to = %s, want z,c,a,b", got)
	}
	changes, _ := h.reg.Deps().Store.Changes.List(w.ID, 1)
	if !strings.Contains(string(changes[0].Before), needs.ID) || !strings.Contains(string(changes[0].After), `"moved_to":"`+blocked.ID+`"`) {
		t.Fatalf("delete change = %s -> %s", changes[0].Before, changes[0].After)
	}

	// Leave one column: the last cannot go.
	rest := h.columns(w.ID)
	h.do("column_delete", m{"column_id": rest[0].ID}, nil)
	h.do("column_delete", m{"column_id": rest[1].ID}, nil)
	last := h.columns(w.ID)
	h.fails("column_delete", m{"column_id": last[0].ID}, "needs at least one column")
	if len(h.cards(m{"workspace_id": w.ID})) != 4 {
		t.Fatal("cards were lost while deleting columns")
	}
}

// ---- cards ----

func TestCardCreateDefaultsAndValidation(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "key": " ACME-541 ", "title": "Fix billing"}, &card)
	working := colByRole(h.columns(w.ID), "active")
	if card.ColumnID != working.ID || card.Key != "ACME-541" || card.Assignee != "You" || card.Liveness != "idle" || card.Position != 0 || card.Title != "Fix billing" {
		t.Fatalf("card = %+v", card)
	}
	h.do("card_create", m{"workspace_id": w.ID, "nick": "second", "start": false}, &card)
	if card.Position != 1 {
		t.Fatalf("second card position = %d, want 1", card.Position)
	}
	for _, tc := range []struct {
		args m
		want string
	}{
		{m{"workspace_id": w.ID, "nick": "Billing Fix"}, "must be kebab-case"},
		{m{"workspace_id": w.ID, "nick": "double--dash"}, "must be kebab-case"},
		{m{"workspace_id": w.ID, "nick": "-lead"}, "must be kebab-case"},
		{m{"workspace_id": w.ID, "nick": strings.Repeat("a", 61)}, "longer than 60"},
		{m{"workspace_id": w.ID, "nick": ""}, "nick is required"},
		{m{"workspace_id": w.ID, "nick": "billing-fix"}, `already exists in workspace "w"`},
		{m{"workspace_id": w.ID, "nick": "ok", "column_id": "nope"}, "is not in workspace"},
	} {
		h.fails("card_create", tc.args, tc.want)
	}
	other := h.workspace("other")
	h.do("card_create", m{"workspace_id": other.ID, "nick": "billing-fix"}, nil) // the same nick in another workspace is fine
}

func TestCardListFilters(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	cols := h.columns(w.ID)
	var a, b domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "a", "column_id": cols[0].ID}, &a)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "b", "column_id": cols[1].ID}, &b)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "c", "column_id": cols[1].ID}, nil)
	h.do("card_archive", m{"card_id": b.ID}, nil)

	for _, tc := range []struct {
		name string
		args m
		want string
	}{
		{"all, archived hidden", m{"workspace_id": w.ID}, "a,c"},
		{"with archived", m{"workspace_id": w.ID, "include_archived": true}, "a,b,c"},
		{"by column", m{"workspace_id": w.ID, "column_id": cols[1].ID}, "c"},
		{"by role", m{"workspace_id": w.ID, "role": "needs-you"}, "a"},
		{"by liveness", m{"workspace_id": w.ID, "liveness": "working"}, ""},
	} {
		if got := nicks(h.cards(tc.args)); got != tc.want {
			t.Errorf("%s: cards = %q, want %q", tc.name, got, tc.want)
		}
	}
	if got := nicks(h.cards(m{"workspace_id": w.ID, "expr": `card.nick == "a"`})); got != "a" {
		t.Errorf("expr filter: cards = %q, want a", got)
	}
	h.fails("card_list", m{"workspace_id": w.ID, "expr": `card.nick ==`}, "Syntax error")
	h.fails("card_list", m{"workspace_id": w.ID, "role": "boss"}, "is not one of")
	var got domain.Card
	h.do("card_get", m{"card_id": a.ID}, &got)
	if got.ID != a.ID {
		t.Fatal("card_get returned the wrong card")
	}
	h.fails("card_get", m{"card_id": "nope"}, `card "nope" does not exist`)
}

func TestCardUpdate(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	var a, b domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "a"}, &a)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "b"}, &b)
	var up domain.Card
	h.do("card_update", m{"card_id": a.ID, "nick": "alpha", "assignee": "Ana", "stage": " review "}, &up)
	if up.Nick != "alpha" || up.Assignee != "Ana" || up.Stage != "review" || up.ColumnID != a.ColumnID {
		t.Fatalf("card = %+v", up)
	}
	h.fails("card_update", m{"card_id": a.ID, "nick": "b"}, `a card named "b" already exists`)
	h.fails("card_update", m{"card_id": a.ID, "nick": "Not Kebab"}, "must be kebab-case")
	h.fails("card_update", m{"card_id": "nope", "title": "x"}, "does not exist")
}

func TestCardMovePlacesAndRenumbers(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	cols := h.columns(w.ID)
	src, dst := cols[1], cols[0]
	ids := map[string]string{}
	for _, n := range []string{"a", "b", "c"} {
		var cd domain.Card
		h.do("card_create", m{"workspace_id": w.ID, "nick": n, "column_id": src.ID}, &cd)
		ids[n] = cd.ID
	}
	var d domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "d", "column_id": dst.ID}, &d)

	var moved domain.Card
	h.do("card_move", m{"card_id": ids["b"], "column_id": dst.ID}, &moved)
	if moved.ColumnID != dst.ID || moved.Position != 1 {
		t.Fatalf("moved = %+v", moved)
	}
	if got := nicks(h.cards(m{"workspace_id": w.ID, "column_id": src.ID})); got != "a,c" {
		t.Fatalf("source = %s, want a,c", got)
	}
	left := h.cards(m{"workspace_id": w.ID, "column_id": src.ID})
	if left[0].Position != 0 || left[1].Position != 1 {
		t.Fatalf("source positions = %d, %d; want 0, 1", left[0].Position, left[1].Position)
	}
	h.do("card_move", m{"card_id": ids["c"], "column_id": dst.ID, "position": 0}, nil)
	if got := nicks(h.cards(m{"workspace_id": w.ID, "column_id": dst.ID})); got != "c,d,b" {
		t.Fatalf("destination = %s, want c,d,b", got)
	}
	h.do("card_move", m{"card_id": ids["b"], "column_id": dst.ID, "position": 0}, nil) // reorder within a column
	if got := nicks(h.cards(m{"workspace_id": w.ID, "column_id": dst.ID})); got != "b,c,d" {
		t.Fatalf("after reorder = %s, want b,c,d", got)
	}
	h.do("card_move", m{"card_id": ids["b"], "column_id": dst.ID, "position": 99}, nil)
	if got := nicks(h.cards(m{"workspace_id": w.ID, "column_id": dst.ID})); got != "c,d,b" {
		t.Fatalf("position past the end = %s, want c,d,b", got)
	}

	other := h.workspace("other")
	h.fails("card_move", m{"card_id": ids["a"], "column_id": h.columns(other.ID)[0].ID}, "belongs to another workspace")
	h.fails("card_move", m{"card_id": ids["a"], "column_id": "nope"}, `column "nope" does not exist`)
	h.fails("card_move", m{"card_id": "nope", "column_id": dst.ID}, "does not exist")
	h.fails("card_move", m{"card_id": ids["a"], "column_id": dst.ID, "position": -2}, "position must be")
}

func TestCardArchiveAndDelete(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	var a domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "a"}, &a)
	var got domain.Card
	h.do("card_archive", m{"card_id": a.ID}, &got)
	if !got.Archived || len(h.cards(m{"workspace_id": w.ID})) != 0 {
		t.Fatal("archive did not hide the card")
	}
	h.do("card_archive", m{"card_id": a.ID, "archived": false}, &got)
	if got.Archived || len(h.cards(m{"workspace_id": w.ID})) != 1 {
		t.Fatal("restore did not bring the card back")
	}
	h.do("card_delete", m{"card_id": a.ID}, nil)
	h.fails("card_get", m{"card_id": a.ID}, "does not exist")
	h.fails("card_delete", m{"card_id": a.ID}, "does not exist")
}

func TestCardActionsDoNotWriteConfigHistory(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	before, _ := h.reg.Deps().Store.Changes.List(w.ID, 100)
	var a domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "a"}, &a)
	h.do("card_move", m{"card_id": a.ID, "column_id": h.columns(w.ID)[0].ID}, nil)
	after, _ := h.reg.Deps().Store.Changes.List(w.ID, 100)
	if len(after) != len(before) {
		t.Fatalf("cards are data, not config: %d new change rows", len(after)-len(before))
	}
	calls, _ := h.reg.Deps().Store.MCPCalls.List(w.ID, 100)
	tools := map[string]bool{}
	for _, c := range calls {
		tools[c.Tool] = true
	}
	if !tools["card_create"] || !tools["card_move"] {
		t.Fatalf("mcp_calls tools = %v; card actions are still logged", tools)
	}
}

// ---- ui and settings ----

func TestUIActionsPersistInTheWorkspace(t *testing.T) {
	h := newHarness(t)
	w := h.workspace("w")
	var got domain.Workspace
	h.do("ui_panel_collapse", m{"workspace_id": w.ID, "panel": "planner", "collapsed": true}, &got)
	h.do("ui_main_view", m{"workspace_id": w.ID, "view": "dashboard"}, &got)
	h.do("ui_fullscreen", m{"workspace_id": w.ID, "panel": "card"}, &got)
	if got.UI["main_view"] != "dashboard" || got.UI["full"] != "card" || got.UI["collapsed"].(map[string]any)["planner"] != true {
		t.Fatalf("ui = %+v", got.UI)
	}
	got = domain.Workspace{} // json.Unmarshal merges into an existing map, so decode into a fresh value
	h.do("ui_panel_collapse", m{"workspace_id": w.ID, "panel": "card", "collapsed": true}, &got)
	if _, still := got.UI["full"]; still {
		t.Fatalf("collapsing the full-screen panel should leave full screen: %+v", got.UI)
	}
	h.do("ui_fullscreen", m{"workspace_id": w.ID, "panel": "planner"}, &got)
	if got.UI["collapsed"].(map[string]any)["planner"] != false {
		t.Fatalf("full screen should expand the panel: %+v", got.UI)
	}
	got = domain.Workspace{}
	h.do("ui_fullscreen", m{"workspace_id": w.ID, "panel": ""}, &got)
	if _, still := got.UI["full"]; still {
		t.Fatal("empty panel should leave full screen")
	}
	h.fails("ui_main_view", m{"workspace_id": w.ID, "view": "map"}, "must be board or dashboard")
	h.fails("ui_panel_collapse", m{"workspace_id": w.ID, "panel": "left", "collapsed": true}, "must be planner or card")
	var back domain.Workspace
	h.do("workspace_get", m{"workspace_id": w.ID}, &back)
	if back.UI["main_view"] != "dashboard" {
		t.Fatalf("ui did not persist: %+v", back.UI)
	}
}

func TestSettings(t *testing.T) {
	h := newHarness(t)
	var all map[string]json.RawMessage
	h.do("settings_update", m{"values": m{"theme": "dark", "planner.autostart": true}}, &all)
	if string(all["theme"]) != `"dark"` || string(all["planner.autostart"]) != "true" {
		t.Fatalf("settings = %v", all)
	}
	var one map[string]json.RawMessage
	h.do("settings_get", m{"key": "theme"}, &one)
	if len(one) != 1 || string(one["theme"]) != `"dark"` {
		t.Fatalf("settings_get = %v", one)
	}
	one = nil
	h.do("settings_get", m{"key": "missing"}, &one)
	if len(one) != 0 {
		t.Fatalf("a missing key should return nothing, got %v", one)
	}
	h.fails("settings_update", m{"values": m{"theme": "purple"}}, "theme must be")
	h.fails("settings_update", m{"values": m{"Bad Key": 1}}, "must be lowercase")
}

// ---- the registry as a whole ----

func TestEveryActionHasADescriptionAndSnakeCaseName(t *testing.T) {
	h := newHarness(t)
	for _, info := range h.reg.Infos() {
		if len(info.Description) < 20 {
			t.Errorf("%s has too short a description: %q", info.Name, info.Description)
		}
		for _, r := range info.Name {
			if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
				t.Errorf("action name %q is not snake_case", info.Name)
				break
			}
		}
		if info.Mutates && len(info.Entities) == 0 {
			t.Errorf("%s mutates but names no entities for the changed event", info.Name)
		}
	}
}

var _ = store.ErrNotFound

// ---- updates ----

func TestUpdateActionsWorkWithoutAnUpdaterAndHideTheToken(t *testing.T) {
	h := newHarness(t)
	var st UpdateStatus
	h.do("get_update_status", nil, &st)
	if st.Version != "test" || st.State != "idle" || st.Lines == nil {
		t.Fatalf("status = %+v", st)
	}
	h.fails("check_for_update", nil, "updates are not available")
	h.fails("start_update", nil, "updates are not available")
	h.fails("set_update_token", m{"token": "ghp_supersecret"}, "updates are not available")

	calls, _ := h.reg.Deps().Store.MCPCalls.List("", 10)
	for _, c := range calls {
		if strings.Contains(string(c.Args), "ghp_supersecret") {
			t.Fatalf("the token leaked into mcp_calls: %s", c.Args)
		}
	}
	if len(calls) == 0 || string(calls[0].Args) != `"[redacted]"` {
		t.Fatalf("set_update_token args should be redacted, got %+v", calls)
	}
}
