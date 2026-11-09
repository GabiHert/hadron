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
	Kind      string   `json:"kind"`
	Template  string   `json:"template"`
	Default   string   `json:"default"`
	Custom    bool     `json:"custom"`
	Variables []string `json:"variables"`
	// Unknown lists {{variables}} in the template that Hadron does not fill in. prompt_set returns them as a warning.
	Unknown []string `json:"unknown"`
	Warning string   `json:"warning,omitempty"`
}

// PromptRenderOut is the result of prompt_render.
type PromptRenderOut struct {
	Text    string   `json:"text"`
	Unknown []string `json:"unknown"`
}

func checkPromptKind(kind string) error {
	if !slices.Contains(prompts.Kinds, kind) {
		return fmt.Errorf("prompt kind %q must be one of %s", kind, strings.Join(prompts.Kinds, ", "))
	}
	return nil
}

func promptOf(c *Call, workspaceID, kind string) (PromptOut, error) {
	out := PromptOut{Kind: kind, Default: prompts.Default(kind), Variables: prompts.Variables}
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
			var text string
			if in.Template != "" {
				text, err = sessions.RenderTemplate(c.Store, w, card, in.Template)
			} else {
				text, err = sessions.RenderPrompt(c.Store, w, card, in.Kind)
			}
			if err != nil {
				return PromptRenderOut{}, err
			}
			tpl := in.Template
			if tpl == "" {
				p, _ := promptOf(c, w.ID, in.Kind)
				tpl = p.Template
			}
			return PromptRenderOut{Text: text, Unknown: prompts.Unknown(tpl)}, nil
		})
}
