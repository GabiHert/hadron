// Package prompts holds Hadron's built-in harness (the core planner and session prompts, and the
// guide topics), the default handoff prompts, and renders {{variables}}.
//
// A session's system prompt is built from layers: the core harness (embedded, read-only), the
// workspace layer (the planner or session prompt a user edits), the launch profile's prompt, and
// the card's prompt.
package prompts

import (
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// CoreVersion is bumped whenever the core prompts or guide topics change in a way a user should
// know about. It is shown in Settings → Harness.
const CoreVersion = 2

// Kinds of prompt.
const (
	KindPlanner = "planner"
	KindSession = "session"
	// KindHandoff is typed into an adopted card's session once it resumes.
	KindHandoff = "handoff"
	// KindHandoffPlanner is typed into an adopted planner conversation once it resumes.
	KindHandoffPlanner = "handoff_planner"
)

// Kinds lists every prompt kind.
var Kinds = []string{KindPlanner, KindSession, KindHandoff, KindHandoffPlanner}

// UsesCard says whether a kind is written for a card, so its preview needs a sample card.
func UsesCard(kind string) bool { return kind == KindSession || kind == KindHandoff }

//go:embed core_planner.md
var corePlanner string

//go:embed core_session.md
var coreSession string

//go:embed guide/*.md
var guideFS embed.FS

//go:embed default_handoff.md
var defaultHandoff string

//go:embed default_handoff_planner.md
var defaultHandoffPlanner string

// Variables are the names a template may use.
var Variables = []string{"workspace.name", "workspace.root", "columns", "integrations", "rules", "policy", "card.nick", "card.key", "card.links"}

// Core returns the built-in harness prompt of a kind: the planner and session kinds have one, the
// handoff kinds do not. It is never stored and never edited.
func Core(kind string) string {
	switch kind {
	case KindPlanner:
		return strings.TrimRight(corePlanner, "\n")
	case KindSession:
		return strings.TrimRight(coreSession, "\n")
	}
	return ""
}

// HasCore says whether a kind has a core layer.
func HasCore(kind string) bool { return Core(kind) != "" }

// Layer names, in composition order.
const (
	LayerCore      = "core"
	LayerWorkspace = "workspace"
	LayerProfile   = "profile"
	LayerCard      = "card"
)

// Layer is one part of a composed prompt.
type Layer struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Editable bool   `json:"editable"`
	Template string `json:"template"`
	Rendered string `json:"rendered,omitempty"`
}

// LayerTitle is the line that introduces a layer in the composed prompt, so a session can tell
// where each instruction came from.
func LayerTitle(name string) string {
	switch name {
	case LayerCore:
		return "Hadron harness (built in)"
	case LayerWorkspace:
		return "Workspace instructions"
	case LayerProfile:
		return "Launch profile instructions"
	case LayerCard:
		return "Card instructions"
	}
	return name
}

// Compose joins the non-empty layers, core first, each under its title. Empty layers are left out.
func Compose(layers []Layer) string {
	var parts []string
	for _, l := range layers {
		text := l.Rendered
		if strings.TrimSpace(text) == "" {
			continue
		}
		if l.Title == "" {
			parts = append(parts, strings.TrimSpace(text))
			continue
		}
		parts = append(parts, fmt.Sprintf("## %s\n\n%s", l.Title, strings.TrimSpace(text)))
	}
	return strings.Join(parts, "\n\n")
}

// GuideTopics lists the hadron_guide topics, in the order they are taught.
func GuideTopics() []string {
	entries, _ := guideFS.ReadDir("guide")
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".md"))
	}
	order := []string{"overview", "chat", "questions", "reports", "widgets", "dashboard", "fields-links", "rules-filters", "integrations", "prompts", "keyboard", "feedback", "debugging"}
	rank := map[string]int{}
	for i, n := range order {
		rank[n] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, iok := rank[out[i]]
		rj, jok := rank[out[j]]
		if iok != jok {
			return iok
		}
		if iok {
			return ri < rj
		}
		return out[i] < out[j]
	})
	return out
}

// Guide returns one guide topic, or an error naming the valid topics.
func Guide(topic string) (string, error) {
	if topic == "" {
		topic = "overview"
	}
	b, err := guideFS.ReadFile("guide/" + topic + ".md")
	if err != nil {
		return "", fmt.Errorf("unknown guide topic %q; topics: %s", topic, strings.Join(GuideTopics(), ", "))
	}
	return strings.TrimRight(string(b), "\n"), nil
}

// Default returns the default template of a kind, or "" for an unknown kind. The planner and
// session kinds default to empty: that is their workspace layer, and the core harness sits
// underneath it.
func Default(kind string) string {
	switch kind {
	case KindPlanner, KindSession:
		return ""
	case KindHandoff:
		return defaultHandoff
	case KindHandoffPlanner:
		return defaultHandoffPlanner
	}
	return ""
}

var varRe = regexp.MustCompile(`\{\{([a-z.]+)\}\}`)

// Render fills in the {{variables}} it has values for. An unknown variable is left as it is, and
// listed in the second result so prompt_set can warn about it.
func Render(template string, vars map[string]string) (string, []string) {
	unknown := map[string]bool{}
	out := varRe.ReplaceAllStringFunc(template, func(m string) string {
		name := varRe.FindStringSubmatch(m)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		if !isKnown(name) {
			unknown[name] = true
		}
		return m
	})
	list := make([]string, 0, len(unknown))
	for n := range unknown {
		list = append(list, n)
	}
	sort.Strings(list)
	return out, list
}

func isKnown(name string) bool {
	for _, v := range Variables {
		if v == name {
			return true
		}
	}
	return false
}

// Unknown lists the {{variables}} of a template that Hadron does not fill in, sorted.
func Unknown(template string) []string {
	_, unknown := Render(template, nil)
	return unknown
}
