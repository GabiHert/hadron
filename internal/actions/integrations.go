package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/integrations"
	"github.com/GabiHert/hadron/internal/script"
	"github.com/GabiHert/hadron/internal/store"
)

// Entities of the integration actions' changed events.
const (
	EntityScripts = "scripts"
	EntityLinks   = "links"
)

func integ(c *Call) (*integrations.Manager, error) {
	if c.Deps.Integrations == nil {
		return nil, errors.New("integrations are not available in this build of the app")
	}
	return c.Deps.Integrations, nil
}

// ScriptView is an integration or script as callers see it. It never carries a secret value.
type ScriptView struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Kind        string             `json:"kind"`
	Status      string             `json:"status"`
	WorkspaceID string             `json:"workspace_id"`
	Global      bool               `json:"global"`
	Version     string             `json:"version"`
	Description string             `json:"description"`
	UsedHere    bool               `json:"used_here" jsonschema:"the workspace has switched this integration on"`
	Fields      []script.FieldDef  `json:"fields"`
	Ops         []string           `json:"ops"`
	Permissions script.Permissions `json:"permissions"`
	NewDomains  []string           `json:"new_domains" jsonschema:"network access, MCP servers and binaries this asks for that no approved integration in the workspace has"`
	Config      []ConfigView       `json:"config"`
	Secrets     []SecretView       `json:"secrets"`
	Tests       int                `json:"tests"`
	TestResult  *script.TestResult `json:"test_result"`
	PollSeconds int                `json:"poll_seconds"`
	Problems    []string           `json:"problems"`
	ApprovedBy  string             `json:"approved_by"`
	ApprovedAt  *time.Time         `json:"approved_at"`
	Sessions    bool               `json:"sessions" jsonschema:"declares remote sessions"`
	Links       int                `json:"links"`
}

// ConfigView is one setting and its current value.
type ConfigView struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Value       any    `json:"value"`
}

// SecretView says whether a secret the integration uses is set. Never the value.
type SecretView struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

func approvedPermsKey(id string) string { return "script_approved_perms:" + id }

func approvedPerms(st *store.Store, id string) (script.Permissions, bool) {
	raw, err := st.Settings.Get(approvedPermsKey(id))
	if err != nil {
		return script.Permissions{}, false
	}
	var p script.Permissions
	return p, json.Unmarshal(raw, &p) == nil
}

func scriptView(c *Call, w domain.Workspace, sc domain.Script) ScriptView {
	v := ScriptView{ID: sc.ID, Name: sc.Name, Kind: sc.Kind, Status: sc.Status, WorkspaceID: sc.WorkspaceID, Global: sc.WorkspaceID == "",
		ApprovedBy: sc.ApprovedBy, ApprovedAt: sc.ApprovedAt, Fields: []script.FieldDef{}, Ops: []string{}, Config: []ConfigView{}, Secrets: []SecretView{}, Problems: []string{}, NewDomains: []string{}}
	man, problems := script.ParseManifest(sc.Manifest)
	if len(problems) > 0 {
		v.Problems = problems
		return v
	}
	v.Version, v.Description, v.Permissions, v.PollSeconds = man.Version, man.Description, man.Permissions, man.PollSeconds
	if man.Fields != nil {
		v.Fields = man.Fields
	}
	v.Ops, v.Tests = man.OpNames(), len(man.Tests)
	v.Sessions = man.Ops.Sessions != nil
	v.UsedHere = contains(w.Integrations, sc.Name)
	if len(sc.TestResult) > 0 && string(sc.TestResult) != "null" {
		var tr script.TestResult
		if json.Unmarshal(sc.TestResult, &tr) == nil {
			v.TestResult = &tr
		}
	}
	cfg := integrations.Config(c.Store, sc.Name)
	for _, k := range script.SortedKeys(man.Config) {
		d := man.Config[k]
		val, set := cfg[k]
		if !set {
			val = d.Default
		}
		label := d.Label
		if label == "" {
			label = k
		}
		v.Config = append(v.Config, ConfigView{Key: k, Label: label, Description: d.Description, Type: d.Type, Required: d.Required, Value: val})
	}
	if c.Deps.Integrations != nil && c.Deps.Integrations.Secrets != nil {
		for _, n := range man.Permissions.Secrets {
			_, ok, _ := c.Deps.Integrations.Secrets.Get(script.SecretKey(sc.Name, n))
			v.Secrets = append(v.Secrets, SecretView{Name: n, Set: ok})
		}
	}
	if sc.Status != integrations.StatusEnabled {
		v.NewDomains = newGrants(c.Store, w, sc, man.Permissions)
	}
	if links, err := c.Store.Links.ListByIntegration(sc.Name); err == nil {
		for _, l := range links {
			if l.WorkspaceID == w.ID {
				v.Links++
			}
		}
	}
	return v
}

// newGrants lists the network entries and binaries a script asks for that no other approved
// script visible to the workspace already holds.
func newGrants(st *store.Store, w domain.Workspace, sc domain.Script, p script.Permissions) []string {
	known := map[string]bool{}
	list, _ := st.Scripts.List(w.ID)
	for _, o := range list {
		if o.ID == sc.ID || o.ApprovedAt == nil {
			continue
		}
		if om, problems := script.ParseManifest(o.Manifest); len(problems) == 0 {
			for _, d := range om.Permissions.Domains() {
				known[d] = true
			}
			for _, e := range om.Permissions.Exec {
				known["exec:"+e] = true
			}
		}
	}
	var out []string
	for _, d := range p.Domains() {
		if !known[d] {
			out = append(out, d)
		}
	}
	for _, e := range p.Exec {
		if !known["exec:"+e] {
			out = append(out, "exec:"+e)
		}
	}
	sort.Strings(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// ---- inputs ----

// ScriptListIn is the input of integration_list and script_list.
type ScriptListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// ScriptNameIn names one integration or script.
type ScriptNameIn struct {
	Name        string `json:"name" jsonschema:"the integration or script name"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// ScaffoldIn is the input of integration_scaffold and script_scaffold.
type ScaffoldIn struct {
	Name        string `json:"name" jsonschema:"lowercase name, letters digits and underscores; for an integration it is also the field namespace (jira gives jira.status)"`
	Template    string `json:"template,omitempty" jsonschema:"integration_scaffold: linear (through the Linear MCP server), jira (REST), github (the gh CLI) or empty; default empty"`
	Global      bool   `json:"global,omitempty" jsonschema:"make it available to every workspace instead of only this one"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// WriteFileIn is the input of integration_write_file and script_write_file.
type WriteFileIn struct {
	Name        string `json:"name" jsonschema:"the integration or script name"`
	Path        string `json:"path" jsonschema:"manifest.json or index.js"`
	Content     string `json:"content" jsonschema:"the whole new file"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// ReadFileIn is the input of integration_read_file and script_read_file.
type ReadFileIn struct {
	Name        string `json:"name" jsonschema:"the integration or script name"`
	Path        string `json:"path" jsonschema:"manifest.json or index.js"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// FileOut is one file of an integration.
type FileOut struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ValidateOut is the result of integration_validate.
type ValidateOut struct {
	OK       bool     `json:"ok"`
	Problems []string `json:"problems"`
}

// RunOpIn is the input of integration_run_op and script_run_op.
type RunOpIn struct {
	Name        string `json:"name" jsonschema:"the integration or script name"`
	Op          string `json:"op" jsonschema:"search, get, update, poll, actions.<name> or sessions.<list|output|send|start|stop>"`
	Input       any    `json:"input,omitempty" jsonschema:"the op's input object"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// RunOpOut is the result of integration_run_op.
type RunOpOut struct {
	Output     any      `json:"output"`
	Logs       []string `json:"logs"`
	DurationMS int      `json:"duration_ms"`
}

// ScriptLogsIn is the input of integration_logs and script_logs.
type ScriptLogsIn struct {
	Name  string `json:"name,omitempty" jsonschema:"only this integration or script; empty for all"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many rows, newest first; default 100"`
}

// RemoveOut is the result of integration_remove.
type RemoveOut struct {
	Removed      string `json:"removed"`
	LinksRemoved int    `json:"links_removed"`
}

// TemplateInfo describes a starter.
type TemplateInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ConfigSetIn is the input of integration_config_set.
type ConfigSetIn struct {
	Name   string         `json:"name" jsonschema:"the integration name"`
	Config map[string]any `json:"config" jsonschema:"setting values by key; keys not listed keep their value"`
}

// SecretSetIn is the input of integration_secret_set.
type SecretSetIn struct {
	Name   string `json:"name" jsonschema:"the integration name"`
	Secret string `json:"secret" jsonschema:"the secret's name, as listed in the manifest's permissions.secrets"`
	Value  string `json:"value" jsonschema:"the secret value. Stored in the macOS keychain, never shown again"`
}

// SecretClearIn is the input of integration_secret_clear.
type SecretClearIn struct {
	Name   string `json:"name" jsonschema:"the integration name"`
	Secret string `json:"secret" jsonschema:"the secret's name"`
}

// UseIn is the input of integration_use.
type UseIn struct {
	Name        string `json:"name" jsonschema:"the integration name"`
	Enabled     bool   `json:"enabled" jsonschema:"switch it on or off for the workspace"`
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func registerIntegrations(r *Registry) {
	registerScriptFamily(r, "integration", integrations.KindIntegration)
	registerScriptFamily(r, "script", integrations.KindScript)

	Register(r, Spec{Name: "integration_templates", Description: "List the starter templates integration_scaffold accepts."},
		func(ctx context.Context, c *Call, in struct{}) ([]TemplateInfo, error) {
			out := []TemplateInfo{}
			for _, n := range script.TemplateNames() {
				man, _, _ := script.Template(n)
				m, _ := script.ParseManifest([]byte(man))
				out = append(out, TemplateInfo{Name: n, Description: m.Description})
			}
			return out, nil
		})

	Register(r, Spec{Name: "integration_config_set", Description: "Set an integration's settings (the config keys its manifest lists, such as a Jira site). Secrets such as API tokens go through integration_secret_set instead.",
		Mutates: true, Entities: []string{EntityScripts}},
		func(ctx context.Context, c *Call, in ConfigSetIn) (ScriptView, error) {
			if err := userOrPlanner(c, "change an integration's settings"); err != nil {
				return ScriptView{}, err
			}
			w, sc, man, err := loadScript(c, "", in.Name, integrations.KindIntegration)
			if err != nil {
				return ScriptView{}, err
			}
			cur := integrations.Config(c.Store, sc.Name)
			for k, v := range in.Config {
				d, ok := man.Config[k]
				if !ok {
					return ScriptView{}, fmt.Errorf("%s has no setting %q. Its settings: %s", sc.Name, k, strings.Join(script.SortedKeys(man.Config), ", "))
				}
				if err := checkConfigType(k, d.Type, v); err != nil {
					return ScriptView{}, err
				}
				cur[k] = v
			}
			if err := c.Store.Settings.Set(integrations.ConfigKey(sc.Name), json.RawMessage(toJSON(cur))); err != nil {
				return ScriptView{}, err
			}
			return scriptView(c, w, sc), nil
		})

	Register(r, Spec{Name: "integration_secret_set", Description: "Store a secret an integration uses (an API token). It goes to the macOS login keychain under the service \"Hadron secrets\" and is never shown or returned again. User-only: see mcp.user_actions.",
		Mutates: true, Sensitive: true, Entities: []string{EntityScripts}},
		func(ctx context.Context, c *Call, in SecretSetIn) (ScriptView, error) {
			if err := c.UserOnly("integration_secret_set"); err != nil {
				return ScriptView{}, err
			}
			m, err := integ(c)
			if err != nil {
				return ScriptView{}, err
			}
			w, sc, man, err := loadScript(c, "", in.Name, integrations.KindIntegration)
			if err != nil {
				return ScriptView{}, err
			}
			if !contains(man.Permissions.Secrets, in.Secret) {
				return ScriptView{}, fmt.Errorf("%s does not declare the secret %q. Its secrets: %s", sc.Name, in.Secret, strings.Join(man.Permissions.Secrets, ", "))
			}
			if strings.TrimSpace(in.Value) == "" {
				return ScriptView{}, errors.New("the value is empty")
			}
			if err := m.Secrets.Set(script.SecretKey(sc.Name, in.Secret), strings.TrimSpace(in.Value)); err != nil {
				return ScriptView{}, err
			}
			return scriptView(c, w, sc), nil
		})

	Register(r, Spec{Name: "integration_secret_clear", Description: "Delete a stored secret of an integration.",
		Mutates: true, Entities: []string{EntityScripts}},
		func(ctx context.Context, c *Call, in SecretClearIn) (ScriptView, error) {
			if err := c.UserOnly("integration_secret_clear"); err != nil {
				return ScriptView{}, err
			}
			m, err := integ(c)
			if err != nil {
				return ScriptView{}, err
			}
			w, sc, _, err := loadScript(c, "", in.Name, integrations.KindIntegration)
			if err != nil {
				return ScriptView{}, err
			}
			if err := m.Secrets.Delete(script.SecretKey(sc.Name, in.Secret)); err != nil {
				return ScriptView{}, err
			}
			return scriptView(c, w, sc), nil
		})

	Register(r, Spec{Name: "integration_use", Description: "Switch an approved integration on or off for one workspace. Its fields appear in filters, rules and cards only where it is on.",
		Mutates: true, Entities: []string{EntityScripts, "workspaces", "fields"}},
		func(ctx context.Context, c *Call, in UseIn) (ScriptView, error) {
			w, sc, _, err := loadScript(c, in.WorkspaceID, in.Name, integrations.KindIntegration)
			if err != nil {
				return ScriptView{}, err
			}
			if in.Enabled && sc.Status != integrations.StatusEnabled {
				return ScriptView{}, fmt.Errorf("%s is %s. Approve it before switching it on", sc.Name, sc.Status)
			}
			before := w
			w.Integrations = withIntegration(w.Integrations, sc.Name, in.Enabled)
			if err := c.Store.Workspaces.Update(&w); err != nil {
				return ScriptView{}, err
			}
			c.RecordChange(w.ID, before, w)
			return scriptView(c, w, sc), nil
		})
}

func withIntegration(list []string, name string, on bool) []string {
	out := []string{}
	for _, x := range list {
		if x != name {
			out = append(out, x)
		}
	}
	if on {
		out = append(out, name)
	}
	return out
}

func checkConfigType(key, typ string, v any) error {
	ok := false
	switch typ {
	case "string":
		_, ok = v.(string)
	case "number":
		_, ok = v.(float64)
	case "bool":
		_, ok = v.(bool)
	}
	if !ok {
		return fmt.Errorf("setting %s is a %s, got %v", key, typ, v)
	}
	return nil
}

// userOrPlanner refuses card sessions, which have no business changing integrations.
func userOrPlanner(c *Call, what string) error {
	if c.Actor.Kind == KindSession {
		return fmt.Errorf("a card session cannot %s. Ask the planner or the user", what)
	}
	return nil
}

// loadScript finds the script a workspace sees under name, of the given kind.
func loadScript(c *Call, workspaceID, name, kind string) (domain.Workspace, domain.Script, script.Manifest, error) {
	w, err := c.Workspace(workspaceID)
	if err != nil {
		return w, domain.Script{}, script.Manifest{}, err
	}
	sc, err := integrations.Find(c.Store, w.ID, name)
	if err != nil {
		return w, sc, script.Manifest{}, err
	}
	if sc.Kind != kind {
		other := "integration_*"
		if kind == integrations.KindIntegration {
			other = "script_*"
		}
		return w, sc, script.Manifest{}, fmt.Errorf("%q is a %s, not a %s. Use the %s actions for it", name, sc.Kind, kind, other)
	}
	man, _ := script.ParseManifest(sc.Manifest)
	return w, sc, man, nil
}

func registerScriptFamily(r *Registry, prefix, kind string) {
	noun := "integration"
	if kind == integrations.KindScript {
		noun = "script"
	}
	n := func(op string) string { return prefix + "_" + op }
	ents := []string{EntityScripts, "workspaces", "fields"}

	Register(r, Spec{Name: n("list"), Description: "List the " + noun + "s visible to a workspace (its own and the global ones) with status (pending, enabled, disabled, rejected), permissions, what they ask for that nothing approved has, test results, settings and whether each secret is set. Secret values are never returned."},
		func(ctx context.Context, c *Call, in ScriptListIn) ([]ScriptView, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			list, err := c.Store.Scripts.List(w.ID)
			if err != nil {
				return nil, err
			}
			out := []ScriptView{}
			for _, sc := range list {
				if sc.Kind == kind {
					out = append(out, scriptView(c, w, sc))
				}
			}
			return out, nil
		})

	Register(r, Spec{Name: n("scaffold"), Description: "Create a new " + noun + " from a starter template, as pending. It does nothing until its tests pass and it is approved. Edit it with " + n("write_file") + ".",
		Mutates: true, Entities: ents},
		func(ctx context.Context, c *Call, in ScaffoldIn) (ScriptView, error) {
			if err := userOrPlanner(c, "create "+noun+"s"); err != nil {
				return ScriptView{}, err
			}
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return ScriptView{}, err
			}
			if !nameRe.MatchString(in.Name) {
				return ScriptView{}, fmt.Errorf("name %q must be lowercase letters, digits and underscores, starting with a letter", in.Name)
			}
			if kind == integrations.KindIntegration && (in.Name == fields.NSCard || in.Name == fields.NSCustom) {
				return ScriptView{}, fmt.Errorf("%q is reserved for the built-in fields", in.Name)
			}
			existing, err := c.Store.Scripts.List(w.ID)
			if err != nil {
				return ScriptView{}, err
			}
			for _, e := range existing {
				if e.Name == in.Name {
					return ScriptView{}, fmt.Errorf("%q already exists in this workspace as a %s. Pick another name, or edit it with %s_write_file", in.Name, e.Kind, prefix)
				}
			}
			tpl := in.Template
			if kind == integrations.KindScript {
				tpl = script.ScriptTemplate
			} else if tpl == "" {
				tpl = "empty"
			}
			man, src, err := script.Template(tpl)
			if err != nil {
				return ScriptView{}, err
			}
			var generic map[string]json.RawMessage
			_ = json.Unmarshal([]byte(man), &generic)
			generic["name"] = json.RawMessage(toJSON(in.Name))
			pretty, _ := json.MarshalIndent(generic, "", "  ")
			sc := domain.Script{Name: in.Name, Kind: kind, Manifest: pretty, Source: src, Status: integrations.StatusPending}
			if !in.Global {
				sc.WorkspaceID = w.ID
			}
			if err := c.Store.Scripts.Create(&sc); err != nil {
				return ScriptView{}, err
			}
			c.RecordChange(w.ID, nil, sc)
			return scriptView(c, w, sc), nil
		})

	Register(r, Spec{Name: n("write_file"), Description: "Replace manifest.json or index.js of a " + noun + ". The manifest is checked against its JSON Schema (every problem is listed) and the code must parse. An edit that keeps the same permissions re-runs the recorded tests and, if they pass, leaves an approved " + noun + " enabled; an edit that asks for more makes it pending again and needs approval.",
		Mutates: true, Entities: ents},
		func(ctx context.Context, c *Call, in WriteFileIn) (ScriptView, error) {
			if err := userOrPlanner(c, "edit "+noun+"s"); err != nil {
				return ScriptView{}, err
			}
			w, sc, _, err := loadScript(c, in.WorkspaceID, in.Name, kind)
			if err != nil {
				return ScriptView{}, err
			}
			before := sc
			switch in.Path {
			case "manifest.json":
				man, problems := script.ParseManifest([]byte(in.Content))
				if len(problems) > 0 {
					return ScriptView{}, fmt.Errorf("manifest.json has problems:\n- %s", strings.Join(problems, "\n- "))
				}
				if man.Name != sc.Name {
					return ScriptView{}, fmt.Errorf("manifest.json must keep name %q (it is %q). To rename, scaffold a new one", sc.Name, man.Name)
				}
				sc.Manifest = json.RawMessage(in.Content)
			case "index.js":
				if err := script.CheckSyntax(in.Content); err != nil {
					return ScriptView{}, err
				}
				sc.Source = in.Content
			default:
				return ScriptView{}, fmt.Errorf("path %q must be manifest.json or index.js", in.Path)
			}
			man, _ := script.ParseManifest(sc.Manifest)
			sc.TestResult = json.RawMessage("null")
			switch {
			case sc.Status == integrations.StatusDisabled:
			case sc.ApprovedAt != nil && sameApproved(c.Store, sc, man.Permissions):
				res := script.RunTests(ctx, sc.Name, man, sc.Source, nil)
				sc.TestResult = json.RawMessage(toJSON(res))
				sc.Status = integrations.StatusPending
				if res.OK && len(script.CheckOps(man, sc.Source)) == 0 {
					sc.Status = integrations.StatusEnabled
				}
			default:
				sc.Status = integrations.StatusPending
			}
			if err := c.Store.Scripts.Update(&sc); err != nil {
				return ScriptView{}, err
			}
			c.RecordChange(w.ID, before, sc)
			syncAfter(c)
			return scriptView(c, w, sc), nil
		})

	Register(r, Spec{Name: n("read_file"), Description: "Read manifest.json or index.js of a " + noun + "."},
		func(ctx context.Context, c *Call, in ReadFileIn) (FileOut, error) {
			_, sc, _, err := loadScript(c, in.WorkspaceID, in.Name, kind)
			if err != nil {
				return FileOut{}, err
			}
			switch in.Path {
			case "manifest.json":
				return FileOut{Path: in.Path, Content: string(sc.Manifest)}, nil
			case "index.js":
				return FileOut{Path: in.Path, Content: sc.Source}, nil
			}
			return FileOut{}, fmt.Errorf("path %q must be manifest.json or index.js", in.Path)
		})

	Register(r, Spec{Name: n("validate"), Description: "Check a " + noun + " without running it: the manifest against its JSON Schema, the code parses, and every op the manifest declares is defined in the code."},
		func(ctx context.Context, c *Call, in ScriptNameIn) (ValidateOut, error) {
			_, sc, _, err := loadScript(c, in.WorkspaceID, in.Name, kind)
			if err != nil {
				return ValidateOut{}, err
			}
			problems := validateScript(sc)
			return ValidateOut{OK: len(problems) == 0, Problems: problems}, nil
		})

	Register(r, Spec{Name: n("test"), Description: "Run the " + noun + "'s recorded tests (manifest.tests). Each test calls an op with the network, exec and MCP answered by its fixtures, never the real thing, and compares the output with expect. The result is saved on the " + noun + ".",
		Mutates: true, Entities: []string{EntityScripts}},
		func(ctx context.Context, c *Call, in ScriptNameIn) (script.TestResult, error) {
			_, sc, man, err := loadScript(c, in.WorkspaceID, in.Name, kind)
			if err != nil {
				return script.TestResult{}, err
			}
			if problems := validateScript(sc); len(problems) > 0 {
				return script.TestResult{}, fmt.Errorf("fix these first:\n- %s", strings.Join(problems, "\n- "))
			}
			res := script.RunTests(ctx, sc.Name, man, sc.Source, nil)
			sc.TestResult = json.RawMessage(toJSON(res))
			return res, c.Store.Scripts.Update(&sc)
		})

	Register(r, Spec{Name: n("run_op"), Description: "Run one op of an enabled " + noun + " for real (this uses its network access, secrets and binaries). Input and output are checked against the op's schema.",
		Mutates: true, Entities: []string{EntityScripts, EntityLinks, "cards"}},
		func(ctx context.Context, c *Call, in RunOpIn) (RunOpOut, error) {
			m, err := integ(c)
			if err != nil {
				return RunOpOut{}, err
			}
			w, sc, _, err := loadScript(c, in.WorkspaceID, in.Name, kind)
			if err != nil {
				return RunOpOut{}, err
			}
			if sc.Status != integrations.StatusEnabled {
				return RunOpOut{}, fmt.Errorf("%s is %s. Only an enabled %s runs for real; %s_test runs its recorded tests", sc.Name, sc.Status, noun, prefix)
			}
			run, err := m.Op(ctx, c.Store, w, sc, in.Op, in.Input)
			if err != nil {
				return RunOpOut{}, err
			}
			if err := fireEvents(c, run.Events); err != nil {
				return RunOpOut{}, err
			}
			var out any
			_ = json.Unmarshal(run.Result.Output, &out)
			logs := run.Result.Logs
			if logs == nil {
				logs = []string{}
			}
			return RunOpOut{Output: out, Logs: logs, DurationMS: int(run.Result.Duration.Milliseconds())}, nil
		})

	Register(r, Spec{Name: n("approve"), Description: "Approve a " + noun + ": its tests must pass, then it runs with the permissions its manifest lists. The user may always approve. The planner may unless the planner policy is decisions_only or ask_for_prd and the " + noun + " asks for network access, an MCP server or a binary that nothing approved already has. A card session never approves.",
		Mutates: true, Entities: ents},
		func(ctx context.Context, c *Call, in ScriptNameIn) (ScriptView, error) {
			w, sc, man, err := loadScript(c, in.WorkspaceID, in.Name, kind)
			if err != nil {
				return ScriptView{}, err
			}
			switch c.Actor.Kind {
			case KindYou:
			case KindPlanner, KindExternal:
				if err := sameWorkspace(c, w.ID); err != nil {
					return ScriptView{}, err
				}
			default:
				return ScriptView{}, fmt.Errorf("a card session cannot approve a %s", noun)
			}
			if sc.Status == integrations.StatusEnabled {
				return scriptView(c, w, sc), nil
			}
			if problems := validateScript(sc); len(problems) > 0 {
				return ScriptView{}, fmt.Errorf("cannot approve yet. Fix these:\n- %s", strings.Join(problems, "\n- "))
			}
			res := script.RunTests(ctx, sc.Name, man, sc.Source, nil)
			sc.TestResult = json.RawMessage(toJSON(res))
			if !res.OK {
				_ = c.Store.Scripts.Update(&sc)
				var bad []string
				for _, cs := range res.Cases {
					if !cs.OK {
						bad = append(bad, cs.Name+": "+cs.Error)
					}
				}
				return ScriptView{}, fmt.Errorf("cannot approve: %d of %d tests fail.\n- %s", res.Failed, res.Passed+res.Failed, strings.Join(bad, "\n- "))
			}
			if c.Actor.Kind != KindYou {
				if p := plannerPolicy(c.Store, w.ID); p != PolicyAllowAll {
					if grants := newGrants(c.Store, w, sc, man.Permissions); len(grants) > 0 {
						return ScriptView{}, fmt.Errorf("the planner policy is %s, so the planner cannot approve new network access, MCP servers or binaries (%s). Ask the user to approve %s in Settings, Integrations", p, strings.Join(grants, ", "), sc.Name)
					}
				}
			}
			before := sc
			now := time.Now().UTC()
			sc.Status, sc.ApprovedBy, sc.ApprovedAt = integrations.StatusEnabled, c.Actor.String(), &now
			if err := c.Store.Scripts.Update(&sc); err != nil {
				return ScriptView{}, err
			}
			if err := c.Store.Settings.Set(approvedPermsKey(sc.ID), json.RawMessage(toJSON(man.Permissions))); err != nil {
				return ScriptView{}, err
			}
			if kind == integrations.KindIntegration {
				wb := w
				w.Integrations = withIntegration(w.Integrations, sc.Name, true)
				if err := c.Store.Workspaces.Update(&w); err != nil {
					return ScriptView{}, err
				}
				c.RecordChange(w.ID, wb, w)
			}
			c.RecordChange(w.ID, before, sc)
			syncAfter(c)
			return scriptView(c, w, sc), nil
		})

	Register(r, Spec{Name: n("disable"), Description: "Switch an enabled " + noun + " off (it keeps its files and links), or reject a pending one.",
		Mutates: true, Entities: ents},
		func(ctx context.Context, c *Call, in ScriptNameIn) (ScriptView, error) {
			if err := userOrPlanner(c, "disable "+noun+"s"); err != nil {
				return ScriptView{}, err
			}
			w, sc, _, err := loadScript(c, in.WorkspaceID, in.Name, kind)
			if err != nil {
				return ScriptView{}, err
			}
			before := sc
			switch sc.Status {
			case integrations.StatusEnabled:
				sc.Status = integrations.StatusDisabled
			case integrations.StatusPending:
				sc.Status = integrations.StatusRejected
			default:
				return scriptView(c, w, sc), nil
			}
			if err := c.Store.Scripts.Update(&sc); err != nil {
				return ScriptView{}, err
			}
			if kind == integrations.KindIntegration {
				if err := dropFromWorkspaces(c, sc); err != nil {
					return ScriptView{}, err
				}
			}
			c.RecordChange(w.ID, before, sc)
			syncAfter(c)
			w, _ = c.Workspace(w.ID)
			return scriptView(c, w, sc), nil
		})

	Register(r, Spec{Name: n("remove"), Description: "Delete a " + noun + ", its logs, its stored secrets and the links to its items. This cannot be undone through the UI; config_revert restores the files only.",
		Mutates: true, Entities: append([]string{EntityLinks, "cards"}, ents...)},
		func(ctx context.Context, c *Call, in ScriptNameIn) (RemoveOut, error) {
			if err := userOrPlanner(c, "remove "+noun+"s"); err != nil {
				return RemoveOut{}, err
			}
			w, sc, man, err := loadScript(c, in.WorkspaceID, in.Name, kind)
			if err != nil {
				return RemoveOut{}, err
			}
			out := RemoveOut{Removed: sc.Name}
			if kind == integrations.KindIntegration {
				if err := dropFromWorkspaces(c, sc); err != nil {
					return out, err
				}
				links, _ := c.Store.Links.ListByIntegration(sc.Name)
				for _, l := range links {
					if sc.WorkspaceID == "" || l.WorkspaceID == sc.WorkspaceID {
						if err := c.Store.Links.Delete(l.ID); err != nil {
							return out, err
						}
						out.LinksRemoved++
					}
				}
				if m := c.Deps.Integrations; m != nil && m.Secrets != nil {
					for _, s := range man.Permissions.Secrets {
						_ = m.Secrets.Delete(script.SecretKey(sc.Name, s))
					}
				}
			}
			if err := c.Store.Scripts.Delete(sc.ID); err != nil {
				return out, err
			}
			_ = c.Store.ScriptLogs.DeleteByName(sc.Name)
			c.RecordChange(w.ID, sc, nil)
			syncAfter(c)
			return out, nil
		})

	Register(r, Spec{Name: n("logs"), Description: "What " + noun + "s did lately, newest first: each op run with its duration, whether it worked, the error if not, and the lines the code logged. Secret values never appear."},
		func(ctx context.Context, c *Call, in ScriptLogsIn) ([]domain.ScriptLog, error) {
			return c.Store.ScriptLogs.List(in.Name, in.Limit)
		})
}

func sameApproved(st *store.Store, sc domain.Script, p script.Permissions) bool {
	old, ok := approvedPerms(st, sc.ID)
	return ok && script.SamePermissions(old, p)
}

func validateScript(sc domain.Script) []string {
	man, problems := script.ParseManifest(sc.Manifest)
	if len(problems) > 0 {
		return problems
	}
	if err := script.CheckSyntax(sc.Source); err != nil {
		return []string{err.Error()}
	}
	return script.CheckOps(man, sc.Source)
}

func syncAfter(c *Call) {
	if m := c.Deps.Integrations; m != nil {
		c.AfterCommit(func() {
			if err := m.Sync(); err != nil {
				fmt.Println("integrations sync:", err)
			}
		})
	}
}

// dropFromWorkspaces takes an integration out of every workspace that had it on.
func dropFromWorkspaces(c *Call, sc domain.Script) error {
	list, err := c.Store.Workspaces.List(true)
	if err != nil {
		return err
	}
	for _, w := range list {
		if (sc.WorkspaceID == "" || sc.WorkspaceID == w.ID) && contains(w.Integrations, sc.Name) {
			before := w
			w.Integrations = withIntegration(w.Integrations, sc.Name, false)
			if err := c.Store.Workspaces.Update(&w); err != nil {
				return err
			}
			c.RecordChange(w.ID, before, w)
		}
	}
	return nil
}

// fireEvents runs the rules for the changes a script made.
func fireEvents(c *Call, events []board.Event) error {
	for _, ev := range events {
		if err := runEvent(c, ev); err != nil {
			return err
		}
	}
	if len(events) > 0 {
		c.Touch("cards", "messages")
	}
	return nil
}
