package actions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/integrations"
	"github.com/GabiHert/hadron/internal/script"
)

func (h *integHarness) cardsIn(columnID string) []domain.Card {
	var out []domain.Card
	for _, c := range h.cards(m{"column_id": columnID}) {
		out = append(out, c)
	}
	return out
}

func TestDiscoveryFillsTheBacklogOnceAndNeverRemakesAnArchivedCard(t *testing.T) {
	h := newIntegHarness(t)
	v := h.jiraEnabled()
	if v.Discover == nil || v.Discover.Enabled {
		t.Fatalf("the jira template declares discover, off by default: %+v", v.Discover)
	}
	backlog := colByRole(h.columns(h.ws.ID), domain.RoleBacklog)
	h.jira.sprint = [][2]string{{"ACME-20", "Fix billing"}, {"ACME-21", "Export invoices"}, {"ACME-22", "Dark mode"}}

	// Off: a poll makes nothing.
	if err := h.mgr.Poll(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := len(h.cardsIn(backlog.ID)); n != 0 {
		t.Fatalf("discovery is off but %d cards appeared", n)
	}

	h.do("integration_discover_set", m{"name": "jira", "enabled": true, "column": "backlog", "max_per_run": 2}, &v)
	if !v.Discover.Enabled || v.Discover.MaxPerRun != 2 || v.Discover.Column != "backlog" {
		t.Fatalf("discover = %+v", v.Discover)
	}
	var res integrations.DiscoverResult
	h.do("integration_discover_run", m{"name": "jira"}, &res)
	if len(res.Created) != 2 || res.Left != 1 || res.Known != 0 {
		t.Fatalf("first run = %+v", res)
	}
	c := res.Created[0]
	if c.Nick != "acme-20" || c.Key != "ACME-20" || c.Title != "Fix billing" || c.ColumnID != backlog.ID {
		t.Fatalf("card = %+v", c)
	}
	var links []domain.Link
	h.do("link_list", m{"card": "acme-20"}, &links)
	if len(links) != 1 || links[0].Integration != "jira" || links[0].ItemID != "ACME-20" || links[0].URL != "https://acme.atlassian.net/browse/ACME-20" {
		t.Fatalf("links = %+v", links)
	}
	if st := h.reg.Deps().Store; true {
		if _, err := st.Sessions.Latest(h.ws.ID, c.ID); err == nil {
			t.Fatal("a discovered card must not start a session")
		}
	}

	// The next run takes the one left over; the run after that has nothing new.
	h.do("integration_discover_run", m{"name": "jira"}, &res)
	if len(res.Created) != 1 || res.Created[0].Nick != "acme-22" || res.Known != 2 {
		t.Fatalf("second run = %+v", res)
	}
	h.do("integration_discover_run", m{"name": "jira"}, &res)
	if len(res.Created) != 0 || res.Known != 3 {
		t.Fatalf("third run = %+v", res)
	}

	// Archived, and even deleted, cards do not come back.
	h.do("card_archive", m{"card_id": c.ID}, nil)
	h.do("card_delete", m{"card_id": res2ID(h, "acme-21")}, nil)
	h.do("integration_discover_run", m{"name": "jira"}, &res)
	if len(res.Created) != 0 {
		t.Fatalf("an archived or deleted card's item came back: %+v", res.Created)
	}
	// forget lets the deleted one come back; the archived card still holds its item.
	h.do("integration_discover_set", m{"name": "jira", "forget": true}, nil)
	h.do("integration_discover_run", m{"name": "jira"}, &res)
	if len(res.Created) != 1 || res.Created[0].Key != "ACME-21" {
		t.Fatalf("after forget = %+v", res.Created)
	}

	var list []ScriptView
	h.do("integration_list", nil, &list)
	d := list[0].Discover
	if d == nil || d.LastRunAt == nil || d.LastCreated != 1 || d.LastError != "" || d.Discovered != 3 {
		t.Fatalf("discover view = %+v", d)
	}
}

func res2ID(h *integHarness, nick string) string {
	c, err := h.reg.Deps().Store.Cards.ByNick(h.ws.ID, nick)
	if err != nil {
		h.t.Fatal(err)
	}
	return c.ID
}

func TestDiscoveryRunsOnThePollAndSkipsAnItemLinkedByHand(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()
	h.jira.sprint = [][2]string{{"ACME-12", "Fix billing"}, {"ACME-30", "New thing"}}
	h.do("link_add", m{"card": "alpha", "integration": "jira", "item_id": "ACME-12"}, nil)
	h.do("integration_discover_set", m{"name": "jira", "enabled": true}, nil)
	if err := h.mgr.Poll(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	backlog := colByRole(h.columns(h.ws.ID), domain.RoleBacklog)
	got := h.cardsIn(backlog.ID)
	if len(got) != 1 || got[0].Key != "ACME-30" {
		t.Fatalf("backlog = %+v", got)
	}
}

func TestDiscoverySettingsAreChecked(t *testing.T) {
	h := newIntegHarness(t)
	h.jiraEnabled()
	h.fails("integration_discover_set", m{"name": "jira", "max_per_run": 0}, "between 1 and 50")
	h.fails("integration_discover_set", m{"name": "jira", "column": "Nowhere"}, `no column "Nowhere"`)
	h.do("integration_use", m{"name": "jira", "enabled": false}, nil)
	h.fails("integration_discover_set", m{"name": "jira", "enabled": true}, "not switched on")
	if err := h.as(Actor{Kind: KindSession, Name: "alpha", WorkspaceID: h.ws.ID, CardID: h.a.ID}, "integration_discover_run", m{"name": "jira"}, nil); err == nil {
		t.Fatal("a session ran discovery")
	}
}

func TestManifestDiscoverNeedsCardCreateAndAPoll(t *testing.T) {
	_, problems := script.ParseManifest([]byte(`{"name":"x","version":"1","ops":{"discover":{}}}`))
	all := strings.Join(problems, "\n")
	if !strings.Contains(all, `permissions.api must list "card.create"`) || !strings.Contains(all, "set poll_seconds") {
		t.Fatalf("problems = %v", problems)
	}
	_, problems = script.ParseManifest([]byte(`{"name":"x","version":"1","ops":{"discover":{}},"permissions":{"api":["card.create"]},"poll_seconds":600}`))
	if len(problems) != 0 {
		t.Fatalf("a valid discover manifest has problems: %v", problems)
	}
}

const makerManifest = `{
  "name": "maker", "version": "1.0.0",
  "ops": {"get": {}, "actions": {"make": {"description": "make a card"}}},
  "permissions": {"api": ["card.create"]},
  "tests": [{"name": "get", "op": "get", "input": {"item_id": "1"}, "expect": {"item_id": "1"}}]
}`

const makerSource = `var ops = { get: function (input) { return { item_id: input.item_id, title: "one" }; }, actions: { make: function (input) {
  var a = host.card.create({ key: "BETA-7", title: "Ship the export", column: "Working", item_id: "7", url: "https://example.com/7" });
  var again = host.card.create({ key: "BETA-7", title: "Ship the export", item_id: "7" });
  var named = host.card.create({ nick: "release-notes", title: "Write release notes" });
  return { first: a, again: again, named: named };
} } };`

func TestHostCardCreateMakesALinkedSessionlessCardOnce(t *testing.T) {
	h := newIntegHarness(t)
	h.do("integration_scaffold", m{"name": "maker", "template": "empty"}, nil)
	h.do("integration_write_file", m{"name": "maker", "path": "manifest.json", "content": makerManifest}, nil)
	h.do("integration_write_file", m{"name": "maker", "path": "index.js", "content": makerSource}, nil)
	h.do("integration_approve", m{"name": "maker"}, nil)
	var out struct {
		Output struct {
			First, Again, Named struct {
				CardID  string `json:"card_id"`
				Nick    string `json:"nick"`
				Created bool   `json:"created"`
			}
		} `json:"output"`
	}
	h.do("integration_run_op", m{"name": "maker", "op": "actions.make", "input": m{}}, &out)
	o := out.Output
	if !o.First.Created || o.First.Nick != "beta-7" || o.Again.Created || o.Again.CardID != o.First.CardID || !o.Named.Created || o.Named.Nick != "release-notes" {
		t.Fatalf("output = %+v", o)
	}
	var card domain.Card
	h.do("card_get", m{"card_id": res2ID(h, "beta-7")}, &card)
	if card.ColumnID != h.working.ID || card.Title != "Ship the export" {
		t.Fatalf("card = %+v", card)
	}
	// Without the permission the call is refused.
	h.do("integration_write_file", m{"name": "maker", "path": "manifest.json", "content": strings.Replace(makerManifest, `"card.create"`, ``, 1)}, nil)
	h.do("integration_approve", m{"name": "maker"}, nil)
	h.fails("integration_run_op", m{"name": "maker", "op": "actions.make", "input": m{}}, `permissions.api`)
}

func TestNickForKeysAndTitles(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"PROJ-123", "x"}:               "proj-123",
		{"", "Fix the login page!"}:     "fix-the-login-page",
		{"", "42 is the answer"}:        "item-42-is-the-answer",
		{"", "   "}:                     "item",
		{"", strings.Repeat("ab ", 30)}: "ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-ab-a",
	} {
		if got := integrations.NickFor(in[0], in[1]); got != want {
			t.Errorf("NickFor(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}
