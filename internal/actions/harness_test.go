package actions

import (
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/prompts"
)

func argvPrompt(t *testing.T, h *sessionHarness) string {
	t.Helper()
	waitUntil(t, "the argv", func() bool { return strings.Contains(h.file(".argv"), "--mcp-config") })
	argv := strings.Split(strings.TrimSuffix(h.file(".argv"), "\x00"), "\x00")
	for i, a := range argv {
		if a == "--append-system-prompt" && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	t.Fatalf("no --append-system-prompt in %q", argv)
	return ""
}

func TestLayersComposeInOrderWithVariablesFilledIn(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	h.do("prompt_set", m{"workspace_id": w.ID, "kind": "session", "template": "Workspace rule for {{workspace.name}}: write tests."}, nil)
	var p domain.LaunchProfile
	h.do("launch_profile_create", m{"workspace_id": w.ID, "name": "careful", "prompt": "Profile rule: ask before deleting anything in {{card.nick}}."}, &p)
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "key": "ACME-508", "launch_profile": "careful", "prompt": "Card rule: only touch the invoices module{{card.key}}."}, &card)

	text := argvPrompt(t, h)
	order := []string{"## Hadron harness (built in)", "## Workspace instructions", "Workspace rule for Acme: write tests.", "## Launch profile instructions", "Profile rule: ask before deleting anything in billing-fix.", "## Card instructions", "Card rule: only touch the invoices module (ACME-508)."}
	last := -1
	for _, want := range order {
		i := strings.Index(text, want)
		if i < 0 {
			t.Fatalf("the prompt lacks %q:\n%s", want, text)
		}
		if i < last {
			t.Fatalf("%q is out of order in:\n%s", want, text)
		}
		last = i
	}
	if strings.Contains(text, "{{") {
		t.Errorf("a variable was not filled in:\n%s", text)
	}
}

func TestPromptSetCannotRemoveTheCore(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	h.do("prompt_set", m{"workspace_id": w.ID, "kind": "planner", "template": "Be terse."}, nil)
	if err := h.try("prompt_set", m{"workspace_id": w.ID, "kind": "planner", "template": "  "}, nil); err == nil {
		t.Fatal("an empty workspace prompt is refused; reset is the way back")
	}
	if err := h.try("prompt_set", m{"workspace_id": w.ID, "kind": "core", "template": "x"}, nil); err == nil {
		t.Fatal("there is no core kind to set")
	}
	var out PromptLayersOut
	h.do("prompt_layers_get", m{"workspace_id": w.ID, "kind": "planner"}, &out)
	if len(out.Layers) != 2 || out.Layers[0].Name != prompts.LayerCore || out.Layers[0].Editable {
		t.Fatalf("layers = %+v", out.Layers)
	}
	if !strings.Contains(out.Text, "You are the planner for the \"Acme\" workspace") || !strings.Contains(out.Text, "Be terse.") {
		t.Errorf("composed text:\n%s", out.Text)
	}
	h.do("prompt_reset", m{"workspace_id": w.ID, "kind": "planner"}, nil)
	h.do("prompt_layers_get", m{"workspace_id": w.ID, "kind": "planner"}, &out)
	if !strings.Contains(out.Text, "You are the planner") || strings.Contains(out.Text, "Be terse.") {
		t.Errorf("after reset:\n%s", out.Text)
	}
}

func TestPromptLayersGetForACardAndPromptRenderReturnTheComposedText(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "invoice-sync", "start": false, "prompt": "Card layer."}, &card)
	var out PromptLayersOut
	h.do("prompt_layers_get", m{"workspace_id": w.ID, "kind": "session", "card": "invoice-sync"}, &out)
	if len(out.Layers) != 4 || out.Layers[3].Template != "Card layer." || out.CoreVersion != prompts.CoreVersion {
		t.Fatalf("layers = %+v", out.Layers)
	}
	var r PromptRenderOut
	h.do("prompt_render", m{"workspace_id": w.ID, "kind": "session", "card": "invoice-sync"}, &r)
	if r.Text != out.Text || !strings.Contains(r.Text, "## Card instructions\n\nCard layer.") {
		t.Errorf("render:\n%s", r.Text)
	}
	h.do("prompt_render", m{"workspace_id": w.ID, "kind": "session", "card": "invoice-sync", "template": "Previewed {{workspace.name}}"}, &r)
	if !strings.Contains(r.Text, "Previewed Acme") {
		t.Errorf("a template previews as the workspace layer:\n%s", r.Text)
	}
}

func TestACardPromptIsEditableAndShownToTheNextStart(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix"}, &card)
	waitUntil(t, "the first start", func() bool { return h.file(".argv") != "" })
	if strings.Contains(argvPrompt(t, h), "Card instructions") {
		t.Fatal("no card layer yet")
	}
	h.do("card_update", m{"card_id": card.ID, "prompt": "Only touch invoices."}, &card)
	if card.Prompt != "Only touch invoices." {
		t.Fatalf("prompt = %q", card.Prompt)
	}
	if err := h.try("card_update", m{"card_id": card.ID, "prompt": strings.Repeat("x", maxPromptBytes+1)}, nil); err == nil {
		t.Error("an oversized card prompt is refused")
	}
}

func TestHadronGuideTopics(t *testing.T) {
	h := newSessionHarness(t, false)
	var g GuideOut
	h.do("hadron_guide", m{}, &g)
	if g.Topic != "overview" || len(g.Topics) != 12 || !strings.Contains(g.Text, "Hadron overview") {
		t.Fatalf("overview = %+v", g)
	}
	h.do("hadron_guide", m{"topic": "widgets"}, &g)
	if !strings.Contains(g.Text, "var(--ok)") {
		t.Errorf("widgets topic:\n%s", g.Text)
	}
	err := h.try("hadron_guide", m{"topic": "nope"}, nil)
	if err == nil || !strings.Contains(err.Error(), `unknown guide topic "nope"`) || !strings.Contains(err.Error(), "debugging") {
		t.Fatalf("unknown topic error = %v", err)
	}
}
