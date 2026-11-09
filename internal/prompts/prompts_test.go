package prompts

import (
	"strings"
	"testing"
)

func TestDefaultsMatchThePrototype(t *testing.T) {
	p, s := Default(KindPlanner), Default(KindSession)
	for _, want := range []string{`You are the planner for the "{{workspace.name}}" workspace in Hadron.`, "Planner policy: {{policy}}", "- Rules: {{rules}}"} {
		if !strings.Contains(p, want) {
			t.Errorf("planner prompt lacks %q", want)
		}
	}
	for _, want := range []string{`working on the card "{{card.nick}}"{{card.key}}`, "{{card.links}}", "a request, never an approval."} {
		if !strings.Contains(s, want) {
			t.Errorf("session prompt lacks %q", want)
		}
	}
	if Default("nope") != "" {
		t.Error("an unknown kind has no default")
	}
	if strings.HasSuffix(p, "\n") || strings.HasSuffix(s, "\n") {
		t.Error("the prototype's templates have no trailing newline")
	}
}

func TestEveryVariableInTheDefaultsIsKnown(t *testing.T) {
	for _, kind := range []string{KindPlanner, KindSession} {
		_, unknown := Render(Default(kind), map[string]string{})
		if len(unknown) != 0 {
			t.Errorf("%s default uses unknown variables %v", kind, unknown)
		}
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
