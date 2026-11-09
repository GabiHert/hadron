// Package prompts holds the two default system prompts and renders their {{variables}}.
package prompts

import (
	_ "embed"
	"regexp"
	"sort"
)

// Kinds of prompt.
const (
	KindPlanner = "planner"
	KindSession = "session"
)

//go:embed default_planner.md
var defaultPlanner string

//go:embed default_session.md
var defaultSession string

// Variables are the names a template may use.
var Variables = []string{"workspace.name", "workspace.root", "columns", "integrations", "rules", "policy", "card.nick", "card.key", "card.links"}

// Default returns the default template of a kind, or "" for an unknown kind.
func Default(kind string) string {
	switch kind {
	case KindPlanner:
		return defaultPlanner
	case KindSession:
		return defaultSession
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
