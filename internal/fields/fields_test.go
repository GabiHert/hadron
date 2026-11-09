package fields_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/fields/fieldstest"
	"github.com/GabiHert/hadron/internal/store"
)

type fx struct {
	st    *store.Store
	reg   *fields.Registry
	ws    domain.Workspace
	cards map[string]domain.Card
	jira  *fieldstest.Fake
}

func setup(t *testing.T, integrations ...string) *fx {
	t.Helper()
	t.Setenv("HADRON_HOME", t.TempDir())
	st, err := store.Open(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &fx{st: st, reg: fields.NewRegistry(), jira: fieldstest.NewJira(), cards: map[string]domain.Card{}}
	if err := f.reg.Register(f.jira); err != nil {
		t.Fatal(err)
	}
	f.ws = domain.Workspace{Name: "W", Root: t.TempDir(), Integrations: integrations}
	if err := st.Workspaces.Create(&f.ws); err != nil {
		t.Fatal(err)
	}
	working := domain.Column{WorkspaceID: f.ws.ID, Name: "Working", Role: domain.RoleActive}
	done := domain.Column{WorkspaceID: f.ws.ID, Name: "Done", Role: domain.RoleDone}
	for _, c := range []*domain.Column{&working, &done} {
		if err := st.Columns.Create(c); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(nick, key, assignee, stage string, col domain.Column, custom map[string]any) {
		c := domain.Card{WorkspaceID: f.ws.ID, ColumnID: col.ID, Nick: nick, Key: key, Assignee: assignee, Stage: stage, Liveness: domain.LivenessWorking, Fields: custom}
		if err := st.Cards.Create(&c); err != nil {
			t.Fatal(err)
		}
		f.cards[nick] = c
	}
	mk("alpha", "ACME-1", "You", "review", working, map[string]any{"priority": "high", "size": 5.0})
	mk("beta", "", "Ana", "", done, nil)
	return f
}

func (f *fx) defineCustom(t *testing.T) {
	t.Helper()
	var defs []fields.Entry
	for _, e := range []fields.Entry{
		{Path: "priority", Type: fields.TypeEnum, Options: []string{"low", "high"}},
		{Path: "size", Type: fields.TypeNumber},
		{Path: "blocked", Type: fields.TypeBool},
	} {
		ce, err := fields.CheckCustom(e)
		if err != nil {
			t.Fatal(err)
		}
		defs = append(defs, ce)
	}
	if err := fields.SaveCustom(f.st, f.ws.ID, defs); err != nil {
		t.Fatal(err)
	}
}

func (f *fx) match(t *testing.T, expr string) string {
	t.Helper()
	cards, err := f.st.Cards.List(f.ws.ID, store.CardFilter{})
	if err != nil {
		t.Fatal(err)
	}
	ids, err := f.reg.Filter(f.st, f.ws, expr, cards)
	if err != nil {
		t.Fatalf("%q: %v", expr, err)
	}
	var nicks []string
	for _, id := range ids {
		for n, c := range f.cards {
			if c.ID == id {
				nicks = append(nicks, n)
			}
		}
	}
	if len(nicks) == 2 && nicks[0] > nicks[1] {
		nicks[0], nicks[1] = nicks[1], nicks[0]
	}
	return strings.Join(nicks, ",")
}

func TestCELOverEveryNamespace(t *testing.T) {
	f := setup(t, "jira")
	f.defineCustom(t)
	f.jira.Put(f.cards["alpha"].ID, "status", "In progress")
	f.jira.Put(f.cards["beta"].ID, "status", "Finished")
	f.jira.Put(f.cards["beta"].ID, "points", 3.0)

	cases := []struct{ expr, want string }{
		{"", "alpha,beta"},
		// card.*
		{`card.column == "Working"`, "alpha"},
		{`card.assignee == "Ana"`, "beta"},
		{`card.liveness == "working"`, "alpha,beta"},
		{`card.key == "ACME-1"`, "alpha"},
		{`card.nick.startsWith("be")`, "beta"},
		{`card.stage == "review"`, "alpha"},
		{`card.role == "done"`, "beta"},
		// custom.*
		{`custom.priority == "high"`, "alpha"},
		{`custom.size > 4.0`, "alpha"},
		{`custom.blocked`, ""},
		{`custom.priority == ""`, "beta"},
		// integration namespace
		{`jira.status == "Finished"`, "beta"},
		{`jira.status != "Finished"`, "alpha"},
		{`jira.points >= 3.0`, "beta"},
		// across namespaces
		{`card.column == "Done" && jira.status == "Finished"`, "beta"},
		{`custom.priority == "high" || jira.status == "Finished"`, "alpha,beta"},
	}
	for _, c := range cases {
		if got := f.match(t, c.expr); got != c.want {
			t.Errorf("%q matched %q, want %q", c.expr, got, c.want)
		}
	}
}

func TestIntegrationNamespaceExistsOnlyWhenEnabled(t *testing.T) {
	f := setup(t) // jira registered, not enabled in the workspace
	_, err := f.reg.NewEvaluator(f.st, f.ws, `jira.status == "Finished"`)
	if err == nil || !strings.Contains(err.Error(), "undeclared reference to 'jira'") {
		t.Fatalf("want CEL's undeclared reference error, got %v", err)
	}
	schema, _ := f.reg.Schema(f.st, f.ws)
	if _, ok := fields.Find(schema, "jira.status"); ok {
		t.Fatal("jira.status must not be in the schema while the integration is off")
	}
}

func TestCompileErrorsAreVerbatim(t *testing.T) {
	f := setup(t)
	for expr, want := range map[string]string{
		`card.nick ==`:      "ERROR: <input>:1:13: Syntax error",
		`nope == 1`:         "undeclared reference to 'nope'",
		`1 + 1`:             "must be true or false",
		`card.nick == 1 &&`: "Syntax error",
	} {
		_, err := f.reg.NewEvaluator(f.st, f.ws, expr)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v lacks %q", expr, err, want)
		}
	}
	// A caret under the mistake comes with CEL's message.
	_, err := f.reg.NewEvaluator(f.st, f.ws, `card.nick ==`)
	if err == nil || !strings.Contains(err.Error(), "^") {
		t.Errorf("CEL's pointer to the column is missing: %v", err)
	}
}

func TestRuntimeErrorWhenEveryCardFails(t *testing.T) {
	f := setup(t)
	cards, _ := f.st.Cards.List(f.ws.ID, store.CardFilter{})
	if _, err := f.reg.Filter(f.st, f.ws, `custom.undefined == "x"`, cards); err == nil || !strings.Contains(err.Error(), "no such key") {
		t.Fatalf("want a no-such-key error, got %v", err)
	}
}

func TestCheckCustomAndCoerce(t *testing.T) {
	if _, err := fields.CheckCustom(fields.Entry{Path: "Bad Name"}); err == nil {
		t.Fatal("a name with a space and capitals must be rejected")
	}
	if _, err := fields.CheckCustom(fields.Entry{Path: "x", Type: "enum"}); err == nil {
		t.Fatal("an enum without options must be rejected")
	}
	e, err := fields.CheckCustom(fields.Entry{Path: "custom.size", Type: "number"})
	if err != nil || e.Path != "custom.size" || !e.Editable {
		t.Fatalf("got %+v %v", e, err)
	}
	if v, err := fields.Coerce(e, 3.0); err != nil || v != 3.0 {
		t.Fatalf("number: %v %v", v, err)
	}
	if _, err := fields.Coerce(e, "three"); err == nil {
		t.Fatal("a string is not a number")
	}
	if v, _ := fields.Coerce(e, ""); v != 0.0 {
		t.Fatalf("an empty string clears a number to 0, got %v", v)
	}
	en := fields.Entry{Path: "custom.p", Type: "enum", Options: []string{"low", "high"}}
	if _, err := fields.Coerce(en, "medium"); err == nil || !strings.Contains(err.Error(), "low, high") {
		t.Fatalf("an enum value outside the options must list them: %v", err)
	}
}

func TestRegisterRejectsReservedNamespaces(t *testing.T) {
	r := fields.NewRegistry()
	f := fieldstest.NewJira()
	f.NS = "card"
	if err := r.Register(f); err == nil {
		t.Fatal("card is reserved")
	}
	f.NS = "Bad-NS"
	if err := r.Register(f); err == nil {
		t.Fatal("namespace must be a lowercase identifier")
	}
}
