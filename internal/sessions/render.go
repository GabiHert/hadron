package sessions

import (
	"errors"
	"fmt"
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
}

// RenderPrompt fills in the planner or session prompt of a workspace. kind is prompts.KindPlanner
// or prompts.KindSession; card is nil for the planner.
func RenderPrompt(st *store.Store, ws domain.Workspace, card *domain.Card, kind string) (string, error) {
	if kind != prompts.KindPlanner && kind != prompts.KindSession {
		return "", fmt.Errorf("prompt kind %q must be planner or session", kind)
	}
	tpl := prompts.Default(kind)
	if p, err := st.Prompts.Get(ws.ID, kind); err == nil && p.Template != nil {
		tpl = *p.Template
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	return RenderTemplate(st, ws, card, tpl)
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
