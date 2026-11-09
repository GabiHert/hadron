package prompts

import (
	"strings"
	"testing"
)

func TestCoreHarnessCarriesTheRoleAndTheTools(t *testing.T) {
	p, s := Core(KindPlanner), Core(KindSession)
	for _, want := range []string{`You are the planner for the "{{workspace.name}}" workspace in Hadron.`, "Planner policy: {{policy}}", "- Rules: {{rules}}", "hadron_guide", "widget_post"} {
		if !strings.Contains(p, want) {
			t.Errorf("core planner prompt lacks %q", want)
		}
	}
	for _, want := range []string{`working on the card "{{card.nick}}"{{card.key}}`, "{{card.links}}", "a request, never an approval.", "question_ask", "card_report", "hadron_guide"} {
		if !strings.Contains(s, want) {
			t.Errorf("core session prompt lacks %q", want)
		}
	}
	if Core(KindHandoff) != "" || Core("nope") != "" || HasCore(KindHandoff) || !HasCore(KindPlanner) {
		t.Error("only the planner and session kinds have a core")
	}
	if strings.HasSuffix(p, "\n") || strings.HasSuffix(s, "\n") {
		t.Error("core prompts are trimmed")
	}
}

func TestWorkspaceLayerDefaultsToEmpty(t *testing.T) {
	if Default(KindPlanner) != "" || Default(KindSession) != "" || Default(KindHandoff) == "" {
		t.Error("planner and session have an empty workspace layer by default; the handoffs keep their text")
	}
}

func TestEveryVariableInTheCoreAndHandoffsIsKnown(t *testing.T) {
	for _, text := range []string{Core(KindPlanner), Core(KindSession), Default(KindHandoff), Default(KindHandoffPlanner)} {
		if _, unknown := Render(text, map[string]string{}); len(unknown) != 0 {
			t.Errorf("unknown variables %v", unknown)
		}
	}
}

func TestComposeOrdersLayersAndSkipsEmptyOnes(t *testing.T) {
	out := Compose([]Layer{
		{Title: "A", Rendered: "first"},
		{Title: "B", Rendered: "  "},
		{Title: "C", Rendered: "third\n"},
		{Title: "", Rendered: "bare"},
	})
	if out != "## A\n\nfirst\n\n## C\n\nthird\n\nbare" {
		t.Fatalf("composed = %q", out)
	}
}

func TestGuideTopics(t *testing.T) {
	topics := GuideTopics()
	want := []string{"overview", "chat", "questions", "reports", "widgets", "dashboard", "fields-links", "rules-filters", "integrations", "prompts", "feedback", "debugging"}
	if strings.Join(topics, ",") != strings.Join(want, ",") {
		t.Fatalf("topics = %v", topics)
	}
	for _, topic := range topics {
		text, err := Guide(topic)
		if err != nil || len(text) < 200 {
			t.Errorf("%s: %d bytes, %v", topic, len(text), err)
		}
	}
	w, _ := Guide("widgets")
	for _, v := range []string{"var(--fg)", "var(--ok)", "var(--border)", "<svg", "<table"} {
		if !strings.Contains(w, v) {
			t.Errorf("widgets topic lacks %q", v)
		}
	}
	if o, err := Guide(""); err != nil || !strings.HasPrefix(o, "# Hadron overview") {
		t.Errorf("no topic = the overview, got %v", err)
	}
	if _, err := Guide("nope"); err == nil || !strings.Contains(err.Error(), "topics: overview") {
		t.Errorf("unknown topic error = %v", err)
	}
}

func TestRenderFillsKnownAndKeepsUnknownVariables(t *testing.T) {
	out, unknown := Render("Hi {{workspace.name}}, {{nope}} and {{card.key}}.", map[string]string{"workspace.name": "Acme"})
	if out != "Hi Acme, {{nope}} and {{card.key}}." {
		t.Fatalf("out = %q", out)
	}
	if len(unknown) != 1 || unknown[0] != "nope" {
		t.Fatalf("unknown = %v; want [nope] (card.key is known, just not provided)", unknown)
	}
}
