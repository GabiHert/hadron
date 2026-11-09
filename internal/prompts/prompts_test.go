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

func TestCoreHarnessTeachesExplainingWithWidgets(t *testing.T) {
	if CoreVersion < 2 {
		t.Errorf("CoreVersion = %d; the widgets guidance is version 2", CoreVersion)
	}
	shared := []string{
		"Explain with widgets",
		"Use widget_post",
		"a comparison table, a before/after, a small flow or architecture diagram in inline SVG, a timeline, a bar or sparkline of numbers, a checklist of a plan's steps, a status board",
		"Pair the widget with one or two lines of text",
		"Do not repeat the widget's content in prose.",
		"no network, under 256 KB",
		"so it works in light and dark",
		"Do not post a widget for a trivial or one-line answer.",
		"Widgets in chat are for explaining.",
		`hadron_guide with topic "widgets"`,
	}
	for kind, extra := range map[string][]string{
		KindPlanner: {"Add one to the dashboard (widget_pin) only when the user asks."},
		KindSession: {"Use widget_post on your card", "Adding one to the dashboard is the user's choice"},
	} {
		text := Core(kind)
		for _, want := range append(shared, extra...) {
			if !strings.Contains(text, want) {
				t.Errorf("core %s prompt lacks %q", kind, want)
			}
		}
		for _, gone := range []string{"Show, don't dump", "worth keeping, pin it", "pin a good widget"} {
			if strings.Contains(text, gone) {
				t.Errorf("core %s prompt still says %q", kind, gone)
			}
		}
	}
}

func TestWidgetGuideTeachesAndKeepsPinningSeparate(t *testing.T) {
	w, err := Guide("widgets")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## A flow diagram (inline SVG, with arrows)", "<marker", "marker-end=",
		"## Before and after", "## A timeline", "## A plan's steps, with their states",
		"## A status table", "## A bar chart", "## A sparkline",
		"Widgets in chat are for explaining, not for the dashboard.", "only when the user asks", "Sessions never pin.",
	} {
		if !strings.Contains(w, want) {
			t.Errorf("widgets topic lacks %q", want)
		}
	}
	if strings.Count(w, "```html") != 7 || strings.Count(w, "```")%2 != 0 {
		t.Errorf("widgets topic has %d html examples and unbalanced fences", strings.Count(w, "```html"))
	}
	for topic, want := range map[string]string{
		"chat":      "Widgets in chat are for explaining",
		"dashboard": "A chat widget reaches the dashboard only when the user decides",
	} {
		if text, _ := Guide(topic); !strings.Contains(text, want) {
			t.Errorf("%s topic lacks %q", topic, want)
		}
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
