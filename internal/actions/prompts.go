package actions

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/prompts"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
)

// maxPromptBytes caps a prompt template.
const maxPromptBytes = 64 * 1024

// PromptKindIn is the input of prompt_get and prompt_reset.
type PromptKindIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Kind        string `json:"kind" jsonschema:"planner, session, handoff or handoff_planner"`
}

// PromptSetIn is the input of prompt_set.
type PromptSetIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Kind        string `json:"kind" jsonschema:"planner, session, handoff or handoff_planner"`
	Template    string `json:"template" jsonschema:"the prompt text; {{workspace.name}}, {{columns}} and the other variables are filled in when a session starts"`
}

// PromptRenderIn is the input of prompt_render.
type PromptRenderIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Kind        string `json:"kind" jsonschema:"planner, session, handoff or handoff_planner"`
	Card        string `json:"card,omitempty" jsonschema:"render as this card's session sees it (id, nickname or key); default is a sample card for the session prompt"`
	Template    string `json:"template,omitempty" jsonschema:"render this text instead of the saved one, to preview an edit"`
}

// PromptOut is a prompt as Settings shows it.
type PromptOut struct {
	Kind     string `json:"kind"`
	Template string `json:"template"`
	Default  string `json:"default"`
	// Core is the built-in harness text under this kind's workspace prompt (empty for handoffs). It is read-only.
	Core        string   `json:"core"`
	CoreVersion int      `json:"core_version"`
	Custom      bool     `json:"custom"`
	Variables   []string `json:"variables"`
	// Unknown lists {{variables}} in the template that Hadron does not fill in. prompt_set returns them as a warning.
	Unknown []string `json:"unknown"`
	Warning string   `json:"warning,omitempty"`
}

// PromptLayersIn is the input of prompt_layers_get.
type PromptLayersIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Kind        string `json:"kind" jsonschema:"planner, session, handoff or handoff_planner"`
	Card        string `json:"card,omitempty" jsonschema:"show the layers as this card's session gets them (id, nickname or key); default is a sample card"`
}

// PromptLayersOut is the result of prompt_layers_get.
type PromptLayersOut struct {
	Kind        string          `json:"kind"`
	CoreVersion int             `json:"core_version"`
	Layers      []prompts.Layer `json:"layers"`
	Text        string          `json:"text"`
}

// GuideIn is the input of hadron_guide.
type GuideIn struct {
	Topic string `json:"topic,omitempty" jsonschema:"one of the guide topics; default overview"`
}

// GuideOut is the result of hadron_guide.
type GuideOut struct {
	Topic       string   `json:"topic"`
	Text        string   `json:"text"`
	Topics      []string `json:"topics"`
	CoreVersion int      `json:"core_version"`
}

// PromptRenderOut is the result of prompt_render.
type PromptRenderOut struct {
	Text    string   `json:"text"`
	Unknown []string `json:"unknown"`
}

// checkLayerPrompt validates the text of a card or launch profile prompt layer.
func checkLayerPrompt(what, text string) error {
	if len(text) > maxPromptBytes {
		return fmt.Errorf("the %s is %d KB; keep it under %d KB", what, len(text)/1024, maxPromptBytes/1024)
	}
	return nil
}

func checkPromptKind(kind string) error {
	if !slices.Contains(prompts.Kinds, kind) {
		return fmt.Errorf("prompt kind %q must be one of %s", kind, strings.Join(prompts.Kinds, ", "))
	}
	return nil
}

func promptOf(c *Call, workspaceID, kind string) (PromptOut, error) {
	out := PromptOut{Kind: kind, Default: prompts.Default(kind), Core: prompts.Core(kind), CoreVersion: prompts.CoreVersion, Variables: prompts.Variables}
	out.Template = out.Default
	p, err := c.Store.Prompts.Get(workspaceID, kind)
	switch {
	case err == nil && p.Template != nil:
		out.Template, out.Custom = *p.Template, true
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return out, err
	}
	out.Unknown = prompts.Unknown(out.Template)
	return out, nil
}

func warningFor(unknown []string) string {
	if len(unknown) == 0 {
		return ""
	}
	names := make([]string, len(unknown))
	for i, u := range unknown {
		names[i] = "{{" + u + "}}"
	}
	return fmt.Sprintf("unknown variable(s) %s will be left as written. Valid variables: {{%s}}", strings.Join(names, ", "), strings.Join(prompts.Variables, "}}, {{"))
}

func registerPrompts(r *Registry) {
	Register(r, Spec{Name: "prompt_get", Description: "Read the planner or session prompt of a workspace: the template in effect, the default, whether it is customised, and the variables you can use."},
		func(ctx context.Context, c *Call, in PromptKindIn) (PromptOut, error) {
			if err := checkPromptKind(in.Kind); err != nil {
				return PromptOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return PromptOut{}, err
			}
			return promptOf(c, w.ID, in.Kind)
		})

	Register(r, Spec{Name: "prompt_set", Description: "Replace the planner or session prompt of a workspace. It applies to sessions that start afterwards. Variables Hadron does not know are kept as written, and listed in the warning.",
		Mutates: true, Entities: []string{"prompts"}},
		func(ctx context.Context, c *Call, in PromptSetIn) (PromptOut, error) {
			if err := checkPromptKind(in.Kind); err != nil {
				return PromptOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return PromptOut{}, err
			}
			if strings.TrimSpace(in.Template) == "" {
				return PromptOut{}, errors.New("the template is empty. To go back to the default, call prompt_reset")
			}
			if len(in.Template) > maxPromptBytes {
				return PromptOut{}, fmt.Errorf("the template is %d KB; keep it under %d KB", len(in.Template)/1024, maxPromptBytes/1024)
			}
			before, err := promptOf(c, w.ID, in.Kind)
			if err != nil {
				return before, err
			}
			if err := c.Store.Prompts.Set(w.ID, in.Kind, &in.Template); err != nil {
				return before, err
			}
			out, err := promptOf(c, w.ID, in.Kind)
			if err != nil {
				return out, err
			}
			out.Warning = warningFor(out.Unknown)
			c.RecordChange(w.ID, map[string]any{"kind": in.Kind, "custom": before.Custom, "template": before.Template}, map[string]any{"kind": in.Kind, "custom": true, "template": in.Template})
			return out, nil
		})

	Register(r, Spec{Name: "prompt_reset", Description: "Go back to Hadron's default planner or session prompt.",
		Mutates: true, Entities: []string{"prompts"}},
		func(ctx context.Context, c *Call, in PromptKindIn) (PromptOut, error) {
			if err := checkPromptKind(in.Kind); err != nil {
				return PromptOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return PromptOut{}, err
			}
			before, err := promptOf(c, w.ID, in.Kind)
			if err != nil {
				return before, err
			}
			if err := c.Store.Prompts.Set(w.ID, in.Kind, nil); err != nil {
				return before, err
			}
			c.RecordChange(w.ID, map[string]any{"kind": in.Kind, "custom": before.Custom, "template": before.Template}, map[string]any{"kind": in.Kind, "custom": false, "template": prompts.Default(in.Kind)})
			return promptOf(c, w.ID, in.Kind)
		})

	Register(r, Spec{Name: "prompt_render", Description: "Show a prompt as a session would receive it, with the workspace's columns, rules, integrations and policy filled in. Pass template to preview text that is not saved yet."},
		func(ctx context.Context, c *Call, in PromptRenderIn) (PromptRenderOut, error) {
			if err := checkPromptKind(in.Kind); err != nil {
				return PromptRenderOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return PromptRenderOut{}, err
			}
			var card *domain.Card
			if prompts.UsesCard(in.Kind) {
				if in.Card != "" {
					cd, err := cardByRef(c, in.Card)
					if err != nil {
						return PromptRenderOut{}, err
					}
					card = &cd
				} else {
					card = &domain.Card{Nick: "example-card", Key: "KEY-1"}
				}
			}
			var override *string
			if in.Template != "" {
				override = &in.Template
			}
			var profile *domain.LaunchProfile
			if card != nil && card.LaunchProfile != "" {
				if p, err := sessions.FindProfile(c.Store, w.ID, card.LaunchProfile); err == nil {
					profile = &p
				}
			}
			layers, err := sessions.PromptLayers(c.Store, w, card, profile, in.Kind, override)
			if err != nil {
				return PromptRenderOut{}, err
			}
			text := prompts.Compose(layers)
			tpl := in.Template
			if tpl == "" {
				p, _ := promptOf(c, w.ID, in.Kind)
				tpl = p.Template
			}
			return PromptRenderOut{Text: text, Unknown: prompts.Unknown(tpl)}, nil
		})

	Register(r, Spec{Name: "prompt_layers_get", Description: "Show every layer of a session's prompt, in the order they are composed: the core harness (built in, read-only), the workspace prompt, the launch profile's prompt and the card's prompt, each as a template and as rendered text, plus the composed result. Use it to see why a session behaves as it does."},
		func(ctx context.Context, c *Call, in PromptLayersIn) (PromptLayersOut, error) {
			if err := checkPromptKind(in.Kind); err != nil {
				return PromptLayersOut{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return PromptLayersOut{}, err
			}
			var card *domain.Card
			var profile *domain.LaunchProfile
			if prompts.UsesCard(in.Kind) {
				if in.Card != "" {
					cd, err := cardByRef(c, in.Card)
					if err != nil {
						return PromptLayersOut{}, err
					}
					card = &cd
					if cd.LaunchProfile != "" {
						if p, err := sessions.FindProfile(c.Store, w.ID, cd.LaunchProfile); err == nil {
							profile = &p
						}
					}
				} else {
					card = &domain.Card{Nick: "example-card", Key: "KEY-1"}
				}
			}
			layers, err := sessions.PromptLayers(c.Store, w, card, profile, in.Kind, nil)
			if err != nil {
				return PromptLayersOut{}, err
			}
			return PromptLayersOut{Kind: in.Kind, CoreVersion: prompts.CoreVersion, Layers: layers, Text: prompts.Compose(layers)}, nil
		})

	Register(r, Spec{Name: "hadron_guide", Description: "Read how a part of Hadron works, written for you: overview, chat, questions, reports, widgets (with HTML examples that use the theme's CSS variables), dashboard, fields-links, rules-filters, integrations, prompts, keyboard, feedback, debugging. Call it before guessing how a feature behaves. With no topic it returns the overview and the topic list."},
		func(ctx context.Context, c *Call, in GuideIn) (GuideOut, error) {
			text, err := prompts.Guide(in.Topic)
			if err != nil {
				return GuideOut{}, err
			}
			topic := in.Topic
			if topic == "" {
				topic = "overview"
			}
			return GuideOut{Topic: topic, Text: text, Topics: prompts.GuideTopics(), CoreVersion: prompts.CoreVersion}, nil
		})
}
