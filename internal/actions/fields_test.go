package actions

import (
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/fields/fieldstest"
)

// fieldsHarness is a harness with a fake jira provider registered and a workspace with two cards.
type fieldsHarness struct {
	*harness
	ws   domain.Workspace
	jira *fieldstest.Fake
	a, b domain.Card
}

func newFieldsHarness(t *testing.T) *fieldsHarness {
	h := newHarness(t)
	fh := &fieldsHarness{harness: h, jira: fieldstest.NewJira()}
	h.reg.deps.Fields = fields.NewRegistry()
	if err := h.reg.deps.Fields.Register(fh.jira); err != nil {
		t.Fatal(err)
	}
	fh.ws = h.workspace("W")
	h.do("card_create", m{"nick": "alpha", "start": false}, &fh.a)
	h.do("card_create", m{"nick": "beta", "start": false, "assignee": "Ana"}, &fh.b)
	return fh
}

func (h *fieldsHarness) enableJira() {
	h.do("workspace_update", m{"workspace_id": h.ws.ID, "integrations": []string{"jira"}}, nil)
}

func TestFieldSchemaListsNativeCustomAndEnabledIntegrationFields(t *testing.T) {
	h := newFieldsHarness(t)
	var out FieldSchemaOut
	h.do("field_schema_get", nil, &out)
	if got := strings.Join(fields.Paths(out.Fields), " "); got != "card.assignee card.column card.key card.liveness card.nick card.stage" {
		t.Fatalf("native schema: %s", got)
	}
	if strings.Join(out.Namespaces, ",") != "card,custom" {
		t.Fatalf("namespaces: %v", out.Namespaces)
	}

	h.do("custom_field_create", m{"name": "priority", "type": "enum", "options": []string{"low", "high"}, "card_chip": true}, nil)
	h.enableJira()
	h.do("field_schema_get", nil, &out)
	paths := strings.Join(fields.Paths(out.Fields), " ")
	for _, want := range []string{"custom.priority", "jira.status", "jira.summary"} {
		if !strings.Contains(paths, want) {
			t.Errorf("schema lacks %s: %s", want, paths)
		}
	}
	if strings.Join(out.Namespaces, ",") != "card,custom,jira" {
		t.Fatalf("namespaces: %v", out.Namespaces)
	}
}

func TestCustomFieldLifecycleWritesHistoryAndClearsValues(t *testing.T) {
	h := newFieldsHarness(t)
	h.do("custom_field_create", m{"name": "size", "type": "number"}, nil)
	h.fails("custom_field_create", m{"name": "size"}, "already exists")
	h.fails("custom_field_create", m{"name": "Bad Name"}, "lowercase")
	h.fails("custom_field_create", m{"name": "p", "type": "enum"}, "needs options")

	h.do("card_field_set", m{"card": "alpha", "path": "custom.size", "value": 5}, nil)
	var out CardFieldsOut
	h.do("card_fields_get", m{"card": "alpha"}, &out)
	if out.Fields["custom.size"] != 5.0 {
		t.Fatalf("custom.size = %v", out.Fields["custom.size"])
	}

	var e fields.Entry
	h.do("custom_field_update", m{"name": "size", "label": "Story points", "card_chip": true}, &e)
	if e.Label != "Story points" || !e.CardChip {
		t.Fatalf("%+v", e)
	}
	h.do("custom_field_delete", m{"name": "custom.size"}, nil)
	out = CardFieldsOut{}
	h.do("card_fields_get", m{"card": "alpha"}, &out)
	if _, has := out.Fields["custom.size"]; has {
		t.Fatal("the deleted field still reads")
	}
	var card domain.Card
	h.do("card_get", m{"card_id": h.a.ID}, &card)
	if _, has := card.Fields["size"]; has {
		t.Fatalf("the value stayed on the card: %v", card.Fields)
	}
	var changes []domain.Change
	changes, _ = h.reg.deps.Store.Changes.List(h.ws.ID, 50)
	n := 0
	for _, c := range changes {
		if strings.HasPrefix(c.Action, "custom_field_") {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("want 3 config-history rows for create/update/delete, got %d", n)
	}
}

func TestCardFieldSetNativeAndCustom(t *testing.T) {
	h := newFieldsHarness(t)
	h.do("custom_field_create", m{"name": "priority", "type": "enum", "options": []string{"low", "high"}}, nil)

	var out CardFieldSetOut
	h.do("card_field_set", m{"card": "alpha", "path": "card.stage", "value": "review"}, &out)
	if out.Card.Stage != "review" || out.Old != "" {
		t.Fatalf("%+v", out)
	}
	h.do("card_field_set", m{"card": "alpha", "path": "card.assignee", "value": "Ana"}, nil)
	h.do("card_field_set", m{"card": "alpha", "path": "custom.priority", "value": "high"}, nil)

	cols := h.columns(h.ws.ID)
	done := colByRole(cols, domain.RoleDone)
	h.do("card_field_set", m{"card": "alpha", "path": "card.column", "value": done.Name}, &out)
	if out.Card.ColumnID != done.ID {
		t.Fatalf("card.column must move the card: %+v", out.Card)
	}

	var fields CardFieldsOut
	h.do("card_fields_get", m{"card": "alpha"}, &fields)
	for path, want := range map[string]any{"card.stage": "review", "card.assignee": "Ana", "custom.priority": "high", "card.column": done.Name, "card.nick": "alpha"} {
		if fields.Fields[path] != want {
			t.Errorf("%s = %v, want %v", path, fields.Fields[path], want)
		}
	}

	h.do("card_field_set", m{"card": "alpha", "path": "custom.priority", "value": ""}, nil)
	fields = CardFieldsOut{}
	h.do("card_fields_get", m{"card": "alpha"}, &fields)
	if fields.Fields["custom.priority"] != "" {
		t.Fatalf("an empty string clears the field, got %v", fields.Fields["custom.priority"])
	}

	h.fails("card_field_set", m{"card": "alpha", "path": "custom.priority", "value": "urgent"}, "low, high")
	h.fails("card_field_set", m{"card": "alpha", "path": "card.liveness", "value": "idle"}, "cannot be set", "session")
	h.fails("card_field_set", m{"card": "alpha", "path": "card.column", "value": "Nowhere"}, "no column", "Columns:")
	h.fails("card_field_set", m{"card": "alpha", "path": "custom.nope", "value": "x"}, "no field", "custom.priority")
	h.fails("card_field_set", m{"card": "alpha", "path": "card.nick", "value": "Not Kebab"}, "kebab")
	h.fails("card_field_set", m{"card": "nobody", "path": "card.stage", "value": "x"}, "no card")
}

func TestCardFieldSetOnLinkedFieldNeedsTheIntegration(t *testing.T) {
	h := newFieldsHarness(t)
	h.fails("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "Finished"}, "not available until the", "integration is enabled")
	// An integration nobody registered fails the same way, not with a confusing "no field".
	h.fails("card_field_set", m{"card": "alpha", "path": "linear.state", "value": "Done"}, "not available until the", "linear")

	h.enableJira()
	var out CardFieldSetOut
	h.do("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "In progress"}, &out)
	if out.Value != "In progress" || len(h.jira.Sets) != 1 || h.jira.Sets[0] != "alpha:status=In progress" {
		t.Fatalf("the provider's update op was not called: %+v %v", out, h.jira.Sets)
	}
	h.fails("card_field_set", m{"card": "alpha", "path": "jira.summary", "value": "x"}, "read-only")
	h.fails("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "Cancelled"}, "To do, In progress, Finished")
}

func TestFilterEvalAndCardListExpr(t *testing.T) {
	h := newFieldsHarness(t)
	h.enableJira()
	h.do("card_field_set", m{"card": "alpha", "path": "jira.status", "value": "Finished"}, nil)

	var out FilterEvalOut
	h.do("filter_eval", m{"expr": `jira.status == 'Finished'`}, &out)
	if len(out.CardIDs) != 1 || out.CardIDs[0] != h.a.ID {
		t.Fatalf("%+v", out)
	}
	h.do("filter_eval", m{"expr": `card.assignee == 'Ana'`}, &out)
	if len(out.CardIDs) != 1 || out.CardIDs[0] != h.b.ID {
		t.Fatalf("%+v", out)
	}
	h.do("filter_eval", m{"expr": ""}, &out)
	if len(out.CardIDs) != 2 {
		t.Fatalf("an empty expression matches every card: %+v", out)
	}
	if got := nicks(h.cards(m{"expr": `card.nick == 'beta'`})); got != "beta" {
		t.Fatalf("card_list expr: %q", got)
	}

	// The CEL error reaches the caller untouched.
	err := h.try("filter_eval", m{"expr": `card.nick ==`}, nil)
	if err == nil || !strings.Contains(err.Error(), "ERROR: <input>:1:13: Syntax error") || !strings.Contains(err.Error(), "^") {
		t.Fatalf("compile error must be CEL's own text, got %v", err)
	}
	h.fails("filter_eval", m{"expr": `confluence.page == 'x'`}, "undeclared reference to 'confluence'")
}
