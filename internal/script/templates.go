package script

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed templates
var templateFS embed.FS

// ScriptTemplate is the name of the starter for user scripts (kind script).
const ScriptTemplate = "script"

// TemplateNames lists the integration starter templates: empty, github, jira, linear.
func TemplateNames() []string {
	entries, _ := templateFS.ReadDir("templates")
	var out []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != ScriptTemplate {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Template returns a starter's manifest.json and index.js.
func Template(name string) (manifest, source string, err error) {
	m, e1 := templateFS.ReadFile("templates/" + name + "/manifest.json")
	s, e2 := templateFS.ReadFile("templates/" + name + "/index.js")
	if e1 != nil || e2 != nil {
		return "", "", fmt.Errorf("no template %q. Templates: %s", name, strings.Join(TemplateNames(), ", "))
	}
	return string(m), string(s), nil
}
