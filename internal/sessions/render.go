package sessions

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/prompts"
	"github.com/GabiHert/hadron/internal/store"
)

// policyText is how each planner policy reads in the prompt.
var policyText = map[string]string{
	"allow_all":      "may answer every question and approval itself, then tells the user what it decided",
	"decisions_only": "may answer decisions; brings approvals to the user",
	"ask_for_prd":    "may answer decisions and approvals that target DEV; asks the user for anything that targets PRD",
	"user_only":      "answers and clears nothing itself; asks the user, then relays their answer with question_answer on_behalf_of",
}

// RenderPrompt builds the prompt of a planner or session (or the text of a handoff) for a
// workspace: the core harness, the workspace layer, the card's launch profile layer and the card's
// own layer, each with its {{variables}} filled in. kind is one of prompts.Kinds; card is nil for
// the planner.
func RenderPrompt(st *store.Store, ws domain.Workspace, card *domain.Card, kind string) (string, error) {
	var profile *domain.LaunchProfile
	if card != nil && card.LaunchProfile != "" {
		if p, err := FindProfile(st, ws.ID, card.LaunchProfile); err == nil {
			profile = &p
		}
	}
	return RenderPromptWith(st, ws, card, profile, kind)
}

// RenderPromptWith is RenderPrompt with the launch profile chosen by the caller (a start can
// override the card's).
func RenderPromptWith(st *store.Store, ws domain.Workspace, card *domain.Card, profile *domain.LaunchProfile, kind string) (string, error) {
	layers, err := PromptLayers(st, ws, card, profile, kind, nil)
	if err != nil {
		return "", err
	}
	return prompts.Compose(layers), nil
}

// PromptLayers returns the layers of a prompt, in order, each with its template and its rendered
// text. workspaceOverride, when not nil, stands in for the saved workspace layer (to preview an
// edit). The handoff kinds have a single workspace layer and no core.
func PromptLayers(st *store.Store, ws domain.Workspace, card *domain.Card, profile *domain.LaunchProfile, kind string, workspaceOverride *string) ([]prompts.Layer, error) {
	if !slices.Contains(prompts.Kinds, kind) {
		return nil, fmt.Errorf("prompt kind %q must be one of %s", kind, strings.Join(prompts.Kinds, ", "))
	}
	tpl := prompts.Default(kind)
	if p, err := st.Prompts.Get(ws.ID, kind); err == nil && p.Template != nil {
		tpl = *p.Template
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if workspaceOverride != nil {
		tpl = *workspaceOverride
	}
	var layers []prompts.Layer
	if prompts.HasCore(kind) {
		layers = append(layers, prompts.Layer{Name: prompts.LayerCore, Title: prompts.LayerTitle(prompts.LayerCore), Template: prompts.Core(kind)})
		layers = append(layers, prompts.Layer{Name: prompts.LayerWorkspace, Title: prompts.LayerTitle(prompts.LayerWorkspace), Editable: true, Template: tpl})
		if kind == prompts.KindSession {
			pt, ct := "", ""
			if profile != nil {
				pt = profile.Prompt
			}
			if card != nil {
				ct = card.Prompt
			}
			layers = append(layers,
				prompts.Layer{Name: prompts.LayerProfile, Title: prompts.LayerTitle(prompts.LayerProfile), Editable: true, Template: pt},
				prompts.Layer{Name: prompts.LayerCard, Title: prompts.LayerTitle(prompts.LayerCard), Editable: true, Template: ct})
		}
	} else {
		layers = append(layers, prompts.Layer{Name: prompts.LayerWorkspace, Title: "", Editable: true, Template: tpl})
	}
	for i := range layers {
		out, err := RenderTemplate(st, ws, card, layers[i].Template)
		if err != nil {
			return nil, err
		}
		layers[i].Rendered = out
	}
	return layers, nil
}

// RenderTemplate fills in the {{variables}} of any template for a workspace, and for a card when
// card is not nil. Settings → Prompts uses it to preview a template that is not saved yet.
func RenderTemplate(st *store.Store, ws domain.Workspace, card *domain.Card, tpl string) (string, error) {
	cols, err := st.Columns.List(ws.ID)
	if err != nil {
		return "", err
	}
	colText := make([]string, 0, len(cols))
	for _, c := range cols {
		role := c.Role
		if role == "" {
			role = "no role"
		}
		colText = append(colText, c.Name+" ("+role+")")
	}
	rules, err := st.Rules.List(ws.ID)
	if err != nil {
		return "", err
	}
	var ruleNames []string
	for _, r := range rules {
		if r.Enabled {
			ruleNames = append(ruleNames, r.Name)
		}
	}
	policy := "allow_all"
	if raw, err := st.Settings.Get(SettingPlannerPolicy); err == nil {
		var v string
		if jsonString(raw, &v) && v != "" {
			policy = v
		}
	}
	vars := map[string]string{
		"workspace.name": ws.Name,
		"workspace.root": ws.Root,
		"columns":        strings.Join(colText, ", "),
		"integrations":   orNone(strings.Join(ws.Integrations, ", ")),
		"rules":          orNone(strings.Join(ruleNames, "; ")),
		"policy":         policyOf(policy),
		"card.nick":      "",
		"card.key":       "",
		"card.links":     "no linked items yet",
	}
	if card != nil {
		vars["card.nick"] = card.Nick
		if card.Key != "" {
			vars["card.key"] = " (" + card.Key + ")"
		}
		links, err := st.Links.ListByCard(card.ID)
		if err != nil {
			return "", err
		}
		var l []string
		for _, k := range links {
			l = append(l, k.Integration+" "+k.ItemID)
		}
		if len(l) > 0 {
			vars["card.links"] = strings.Join(l, ", ")
		}
	}
	out, _ := prompts.Render(tpl, vars)
	return out, nil
}

func policyOf(name string) string {
	if t, ok := policyText[name]; ok {
		return t
	}
	return name
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

// renderFirstPrompt fills a launch profile's first prompt: {{nick}}, {{key}} (the key, else the
// nickname) and {{title}}.
func renderFirstPrompt(tpl string, card domain.Card) string {
	key := card.Key
	if key == "" {
		key = card.Nick
	}
	r := strings.NewReplacer("{{nick}}", card.Nick, "{{key}}", key, "{{title}}", card.Title)
	return strings.TrimSpace(r.Replace(tpl))
}
