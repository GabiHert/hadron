package actions

import (
	"testing"

	"github.com/GabiHert/hadron/internal/dash"
	"github.com/GabiHert/hadron/internal/domain"
)

func groupIDs(v GroupView) []string {
	out := []string{}
	for _, r := range v.Cards {
		out = append(out, r["nick"].(string))
	}
	return out
}

func eqs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func groupHarness(t *testing.T) (*harness, domain.Workspace, map[string]domain.Card) {
	h := newHarness(t)
	w := h.workspace("W")
	cards := map[string]domain.Card{}
	for _, n := range []string{"alpha", "beta", "gamma"} {
		var c domain.Card
		h.do("card_create", m{"workspace_id": w.ID, "nick": n, "key": "ACME-" + n, "start": false}, &c)
		cards[n] = c
	}
	return h, w, cards
}

func TestCardGroupCreateIsADashboardBlockWithLiveRows(t *testing.T) {
	h, w, cards := groupHarness(t)
	var g GroupView
	h.do("card_group_create", m{"workspace_id": w.ID, "title": "In flight", "cards": []string{"alpha", cards["beta"].ID, "ACME-alpha"}, "span": 2}, &g)
	if g.Title != "In flight" || g.Span != 2 || !eqs(groupIDs(g), []string{"alpha", "beta"}) {
		t.Fatalf("group = %+v", g)
	}
	row := g.Cards[0]
	if row["key"] != "ACME-alpha" || row["column"] == "" || row["needs_you"] != false || row["last"] != "" {
		t.Errorf("row = %+v", row)
	}

	var out DashGetOut
	h.do("dash_get", m{"workspace_id": w.ID}, &out)
	var bv *BlockView
	for i := range out.Blocks {
		if out.Blocks[i].ID == g.ID {
			bv = &out.Blocks[i]
		}
	}
	if bv == nil || bv.Type != dash.TypeGroup || bv.Data == nil || len(bv.Data.Rows) != 2 || bv.Error != "" {
		t.Fatalf("dash_get shows the group as a block with rows: %+v", bv)
	}

	h.do("dash_block_update", m{"block_id": g.ID, "title": "Watching"}, nil)
	var list []GroupView
	h.do("card_group_list", m{"workspace_id": w.ID}, &list)
	if len(list) != 1 || list[0].Title != "Watching" || len(list[0].Cards) != 2 {
		t.Errorf("rename keeps the cards: %+v", list)
	}
	h.fails("card_group_create", m{"workspace_id": w.ID, "title": ""}, "title is required")
	h.fails("card_group_create", m{"workspace_id": w.ID, "title": "X", "cards": []string{"nope"}}, "no card \"nope\"")
	h.fails("dash_block_add", m{"workspace_id": w.ID, "type": "bars", "title": "B", "source": m{"query": "cards.count", "args": m{"by": "column"}, "cards": []string{cards["alpha"].ID}}}, "only group blocks take cards")
	h.fails("dash_block_add", m{"workspace_id": w.ID, "type": "group", "title": "G", "source": m{"query": "cards.list"}}, "do not take a source query")
}

func TestCardGroupAddRemoveAndShowTheLastLineAndQuestion(t *testing.T) {
	h, w, cards := groupHarness(t)
	var g GroupView
	h.do("card_group_create", m{"workspace_id": w.ID, "title": "Mine"}, &g)
	h.do("card_group_add", m{"workspace_id": w.ID, "group": "mine", "cards": []string{"alpha", "beta"}}, &g)
	h.do("card_group_add", m{"group": g.ID, "cards": []string{"gamma"}, "position": 0}, &g)
	if !eqs(groupIDs(g), []string{"gamma", "alpha", "beta"}) {
		t.Fatalf("order = %v", groupIDs(g))
	}
	h.do("card_group_add", m{"group": g.ID, "cards": []string{"beta"}, "position": 0}, &g)
	if !eqs(groupIDs(g), []string{"beta", "gamma", "alpha"}) {
		t.Errorf("adding a card already there moves it: %v", groupIDs(g))
	}

	alpha := cards["alpha"]
	session := Actor{Kind: KindSession, Name: alpha.Nick, WorkspaceID: w.ID, CardID: alpha.ID}
	if err := h.as(session, "card_report", m{"kind": "progress", "text": "Wrote the\nmigration"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := h.as(session, "question_ask", m{"kind": "decision", "text": "Ship it?", "options": []m{{"label": "Yes", "description": "go"}, {"label": "No", "description": "wait"}}}, nil); err != nil {
		t.Fatal(err)
	}
	var list []GroupView
	h.do("card_group_list", m{"workspace_id": w.ID, "card": "alpha"}, &list)
	if len(list) != 1 {
		t.Fatalf("filtered = %+v", list)
	}
	var row map[string]any
	for _, r := range list[0].Cards {
		if r["nick"] == "alpha" {
			row = r
		}
	}
	if row["needs_you"] != true || row["question"] != "Ship it?" || row["last"] == "" || row["last_at"] == "" {
		t.Errorf("alpha row = %+v", row)
	}

	h.do("card_group_remove", m{"group": g.ID, "cards": []string{"gamma", alpha.ID}}, &g)
	if !eqs(groupIDs(g), []string{"beta"}) {
		t.Errorf("after remove = %v", groupIDs(g))
	}
	h.fails("card_group_remove", m{"group": g.ID, "cards": []string{"gamma"}}, "is not in the group")
	h.fails("card_group_add", m{"workspace_id": w.ID, "group": "Nope", "cards": []string{"alpha"}}, "no card group \"Nope\"", "Mine")
	if err := h.as(session, "card_group_add", m{"group": g.ID, "cards": []string{"alpha"}}, nil); err == nil {
		t.Error("a card session cannot change groups")
	}
}

func TestCardGroupMoveCopyAndReorderNeverMoveTheBoardColumn(t *testing.T) {
	h, w, cards := groupHarness(t)
	var a, b GroupView
	h.do("card_group_create", m{"workspace_id": w.ID, "title": "A", "cards": []string{"alpha", "beta", "gamma"}}, &a)
	h.do("card_group_create", m{"workspace_id": w.ID, "title": "B"}, &b)
	col := cards["alpha"].ColumnID

	var out []GroupView
	h.do("card_group_move", m{"card": "alpha", "from": a.ID, "to": b.ID}, &out)
	if len(out) != 2 || !eqs(groupIDs(out[0]), []string{"beta", "gamma"}) || !eqs(groupIDs(out[1]), []string{"alpha"}) {
		t.Fatalf("move = %+v", out)
	}
	h.do("card_group_move", m{"card": "beta", "from": "A", "to": "B", "copy": true, "position": 0, "workspace_id": w.ID}, &out)
	if !eqs(groupIDs(out[0]), []string{"beta", "gamma"}) || !eqs(groupIDs(out[1]), []string{"beta", "alpha"}) {
		t.Errorf("copy = %v / %v", groupIDs(out[0]), groupIDs(out[1]))
	}
	h.do("card_group_move", m{"card": "gamma", "from": a.ID, "to": a.ID, "position": 0}, &out)
	if len(out) != 1 || !eqs(groupIDs(out[0]), []string{"gamma", "beta"}) {
		t.Errorf("reorder = %+v", out)
	}
	h.fails("card_group_move", m{"card": "alpha", "from": a.ID, "to": b.ID}, "is not in the group")

	got, _ := h.reg.Deps().Store.Cards.Get(cards["alpha"].ID)
	if got.ColumnID != col {
		t.Error("moving between groups must not move the card on the board")
	}
}

func TestArchivedOrDeletedCardsDropOutOfGroups(t *testing.T) {
	h, w, cards := groupHarness(t)
	var a, b GroupView
	h.do("card_group_create", m{"workspace_id": w.ID, "title": "A", "cards": []string{"alpha", "beta", "gamma"}}, &a)
	h.do("card_group_create", m{"workspace_id": w.ID, "title": "B", "cards": []string{"alpha"}}, &b)

	h.do("card_archive", m{"card_id": cards["alpha"].ID}, nil)
	h.do("card_delete", m{"card_id": cards["beta"].ID}, nil)
	var list []GroupView
	h.do("card_group_list", m{"workspace_id": w.ID}, &list)
	if len(list) != 2 || !eqs(groupIDs(list[0]), []string{"gamma"}) || len(list[1].Cards) != 0 {
		t.Fatalf("after archive and delete = %+v", list)
	}
	blk, _ := h.reg.Deps().Store.Blocks.Get(a.ID)
	if got := groupCards(blk); !eqs(got, []string{cards["gamma"].ID}) {
		t.Errorf("the stored ids are pruned too: %v", got)
	}
	h.do("card_archive", m{"card_id": cards["alpha"].ID, "archived": false}, nil)
	h.fails("card_group_add", m{"group": a.ID, "cards": []string{"ACME-gamma", "ACME-nope"}}, "no card")
}
