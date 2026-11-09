package actions

import (
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/prompts"
)

func TestFilterBarSetValidatesFields(t *testing.T) {
	h := newFieldsHarness(t)
	h.enableJira()
	var w domain.Workspace
	h.do("filter_bar_set", m{"fields": []string{"card.assignee", "jira.status"}}, &w)
	if strings.Join(w.FilterBar, ",") != "card.assignee,jira.status" {
		t.Fatalf("%v", w.FilterBar)
	}
	h.fails("filter_bar_set", m{"fields": []string{"card.nope"}}, "not a field", "card.assignee")
	h.fails("filter_bar_set", m{"fields": []string{"card.stage", "card.stage"}}, "twice")
	h.do("filter_bar_set", m{"fields": []string{}}, &w)
	if len(w.FilterBar) != 0 {
		t.Fatalf("an empty list unpins everything: %v", w.FilterBar)
	}
}

func TestFilterEvalWithPinnedValuesAndSearch(t *testing.T) {
	h := newFieldsHarness(t)
	h.enableJira()
	h.do("card_update", m{"card_id": h.a.ID, "title": "Fix the invoice rounding"}, nil)
	h.do("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "Finished"}, nil)
	h.do("card_field_set", m{"card": "beta", "path": "card.stage", "value": "review"}, nil)

	var out FilterEvalOut
	for _, tc := range []struct {
		name string
		args m
		want string
	}{
		{"assignee chip", m{"vals": m{"card.assignee": "Ana"}}, h.b.ID},
		{"integration chip", m{"vals": m{"jira.status": "Finished"}}, h.a.ID},
		{"all chip value", m{"vals": m{"card.assignee": ""}}, h.a.ID + "," + h.b.ID},
		{"text search by title", m{"q": "invoice"}, h.a.ID},
		{"text search by nick", m{"q": "BET"}, h.b.ID},
		{"chips and expression together", m{"vals": m{"card.assignee": "Ana"}, "expr": "card.stage == 'review'"}, h.b.ID},
		{"chips exclude what the expression keeps", m{"vals": m{"card.assignee": "You"}, "expr": "card.stage == 'review'"}, ""},
	} {
		h.do("filter_eval", tc.args, &out)
		if got := strings.Join(out.CardIDs, ","); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
	h.fails("filter_eval", m{"vals": m{"card.nope": "x"}}, "not a field")
}

func TestViewsSaveApplyReplaceDelete(t *testing.T) {
	h := newFieldsHarness(t)
	h.do("card_field_set", m{"card": "beta", "path": "card.stage", "value": "review"}, nil)

	var v domain.View
	h.do("view_save", m{"name": "Ana's reviews", "filters": m{"vals": m{"card.assignee": "Ana"}, "expr": "card.stage == 'review'"}}, &v)
	if v.ID == "" || !strings.Contains(string(v.Filters), `"card.assignee":"Ana"`) || !strings.Contains(string(v.Filters), "review") {
		t.Fatalf("%+v %s", v, v.Filters)
	}
	var views []domain.View
	h.do("view_list", nil, &views)
	if len(views) != 1 {
		t.Fatalf("%+v", views)
	}

	var applied ViewApplyOut
	h.do("view_apply", m{"view_id": v.ID}, &applied)
	if len(applied.CardIDs) != 1 || applied.CardIDs[0] != h.b.ID || applied.View.ID != v.ID {
		t.Fatalf("%+v", applied)
	}

	// The same name replaces the view, whatever the case.
	var again domain.View
	h.do("view_save", m{"name": "ana's REVIEWS", "filters": m{"q": "alpha"}}, &again)
	if again.ID != v.ID {
		t.Fatal("saving under an existing name must update that view")
	}
	h.do("view_list", nil, &views)
	if len(views) != 1 {
		t.Fatalf("%d views", len(views))
	}
	h.do("view_apply", m{"view_id": v.ID}, &applied)
	if len(applied.CardIDs) != 1 || applied.CardIDs[0] != h.a.ID {
		t.Fatalf("%+v", applied)
	}

	// A broken expression is refused before it is stored.
	h.fails("view_save", m{"name": "broken", "filters": m{"expr": "card.nick =="}}, "does not work", "Syntax error")
	h.fails("view_save", m{"name": " ", "filters": m{}}, "name")
	h.fails("view_apply", m{"view_id": "nope"}, "does not exist")

	h.do("view_delete", m{"view_id": v.ID}, nil)
	h.do("view_list", nil, &views)
	if len(views) != 0 {
		t.Fatal("not deleted")
	}
	changes, _ := h.reg.deps.Store.Changes.List(h.ws.ID, 100)
	n := 0
	for _, c := range changes {
		if strings.HasPrefix(c.Action, "view_") {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("view_save x2 and view_delete write config history, got %d rows", n)
	}
}

func TestPromptActions(t *testing.T) {
	h := newFieldsHarness(t)
	var p PromptOut
	h.do("prompt_get", m{"kind": "planner"}, &p)
	if p.Custom || p.Template != prompts.Default("planner") || p.Template != p.Default || len(p.Variables) != len(prompts.Variables) {
		t.Fatalf("a new workspace uses the default: custom=%v", p.Custom)
	}
	h.fails("prompt_get", m{"kind": "robot"}, "planner or session")

	tpl := "You run {{workspace.name}} with columns {{columns}}. Hello {{who}} and {{ card.nick }}."
	h.do("prompt_set", m{"kind": "planner", "template": tpl}, &p)
	if !p.Custom || p.Template != tpl || len(p.Unknown) != 1 || p.Unknown[0] != "who" || !strings.Contains(p.Warning, "{{who}}") {
		t.Fatalf("prompt_set must warn about unknown variables: %+v", p)
	}
	p = PromptOut{}
	h.do("prompt_set", m{"kind": "session", "template": "Card {{card.nick}}{{card.key}}"}, &p)
	if p.Warning != "" || len(p.Unknown) != 0 {
		t.Fatalf("no warning for known variables: %+v", p)
	}
	h.fails("prompt_set", m{"kind": "planner", "template": "  "}, "prompt_reset")

	var r PromptRenderOut
	h.do("prompt_render", m{"kind": "planner"}, &r)
	if !strings.Contains(r.Text, "You run W with columns Needs you (needs-you)") || !strings.Contains(r.Text, "{{who}}") || len(r.Unknown) != 1 {
		t.Fatalf("render: %q %v", r.Text, r.Unknown)
	}
	h.do("prompt_render", m{"kind": "session", "card": "alpha"}, &r)
	if r.Text != "Card alpha" {
		t.Fatalf("session render for a card: %q", r.Text)
	}
	h.do("prompt_render", m{"kind": "session"}, &r)
	if r.Text != "Card example-card (KEY-1)" {
		t.Fatalf("sample card: %q", r.Text)
	}
	// An unsaved edit previews without being stored.
	h.do("prompt_render", m{"kind": "session", "template": "draft for {{card.nick}} in {{workspace.name}}"}, &r)
	if r.Text != "draft for example-card in W" {
		t.Fatalf("%q", r.Text)
	}
	h.do("prompt_get", m{"kind": "session"}, &p)
	if p.Template != "Card {{card.nick}}{{card.key}}" {
		t.Fatal("preview must not save")
	}

	h.do("prompt_reset", m{"kind": "planner"}, &p)
	if p.Custom || p.Template != prompts.Default("planner") {
		t.Fatalf("reset: %+v", p)
	}
	changes, _ := h.reg.deps.Store.Changes.List(h.ws.ID, 100)
	n := 0
	for _, c := range changes {
		if strings.HasPrefix(c.Action, "prompt_") {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("prompt_set x2 and prompt_reset write config history, got %d", n)
	}
}
