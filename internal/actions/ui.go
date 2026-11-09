package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
)

// UIPanelCollapseIn is the input of ui_panel_collapse.
type UIPanelCollapseIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Panel       string `json:"panel" jsonschema:"planner or card"`
	Collapsed   bool   `json:"collapsed" jsonschema:"true to collapse the panel to a rail"`
}

// UIFullscreenIn is the input of ui_fullscreen.
type UIFullscreenIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Panel       string `json:"panel,omitempty" jsonschema:"planner or card to make full screen; empty to leave full screen"`
}

// UIMainViewIn is the input of ui_main_view.
type UIMainViewIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	View        string `json:"view" jsonschema:"board or dashboard"`
}

// SettingsGetIn is the input of settings_get.
type SettingsGetIn struct {
	Key string `json:"key,omitempty" jsonschema:"one setting; omit for all"`
}

// SettingsUpdateIn is the input of settings_update.
type SettingsUpdateIn struct {
	Values map[string]any `json:"values" jsonschema:"setting keys and their new JSON values, for example {\"theme\": \"dark\"}"`
}

var settingKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)

func registerUI(r *Registry) {
	Register(r, Spec{Name: "ui_panel_collapse", Description: "Collapse or expand the planner or card panel of a workspace.", Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in UIPanelCollapseIn) (domain.Workspace, error) {
			if err := checkPanel(in.Panel, false); err != nil {
				return domain.Workspace{}, err
			}
			return setUI(c, in.WorkspaceID, func(ui map[string]any) {
				col, _ := ui["collapsed"].(map[string]any)
				if col == nil {
					col = map[string]any{}
				}
				col[in.Panel] = in.Collapsed
				ui["collapsed"] = col
				if in.Collapsed && ui["full"] == in.Panel {
					delete(ui, "full")
				}
			})
		})

	Register(r, Spec{Name: "ui_fullscreen", Description: "Make the planner or card panel full screen, or leave full screen with an empty panel.", Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in UIFullscreenIn) (domain.Workspace, error) {
			if err := checkPanel(in.Panel, true); err != nil {
				return domain.Workspace{}, err
			}
			return setUI(c, in.WorkspaceID, func(ui map[string]any) {
				if in.Panel == "" {
					delete(ui, "full")
					return
				}
				ui["full"] = in.Panel
				col, _ := ui["collapsed"].(map[string]any)
				if col != nil {
					col[in.Panel] = false
				}
			})
		})

	Register(r, Spec{Name: "ui_main_view", Description: "Switch a workspace's main area between the board and the dashboard.", Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in UIMainViewIn) (domain.Workspace, error) {
			if in.View != "board" && in.View != "dashboard" {
				return domain.Workspace{}, fmt.Errorf("view %q must be board or dashboard", in.View)
			}
			return setUI(c, in.WorkspaceID, func(ui map[string]any) { ui["main_view"] = in.View })
		})

	Register(r, Spec{Name: "settings_get", Description: "Read global settings: one by key, or all."},
		func(ctx context.Context, c *Call, in SettingsGetIn) (map[string]json.RawMessage, error) {
			if in.Key != "" {
				v, err := c.Store.Settings.Get(in.Key)
				if err != nil {
					return map[string]json.RawMessage{}, nil
				}
				return map[string]json.RawMessage{in.Key: v}, nil
			}
			return c.Store.Settings.All()
		})

	Register(r, Spec{Name: "settings_update", Description: "Change global settings, such as theme (light, dark or system). Returns all settings.", Mutates: true, Entities: []string{"settings"}},
		func(ctx context.Context, c *Call, in SettingsUpdateIn) (map[string]json.RawMessage, error) {
			for k, v := range in.Values {
				if !settingKeyRe.MatchString(k) {
					return nil, fmt.Errorf("setting key %q must be lowercase letters, digits, dots and underscores, starting with a letter", k)
				}
				if k == "planner.policy" {
					if c.Actor.Kind != KindYou {
						return nil, errors.New("only the user sets the planner policy")
					}
					if p, _ := v.(string); !contains(PlannerPolicies, p) {
						return nil, fmt.Errorf("planner.policy must be one of %s, got %v", strings.Join(PlannerPolicies, ", "), v)
					}
				}
				if k == "widgets.scripts" {
					if c.Actor.Kind != KindYou {
						return nil, errors.New("only the user lets widgets run scripts: a widget the planner writes would otherwise run code")
					}
					if _, ok := v.(bool); !ok {
						return nil, fmt.Errorf("widgets.scripts must be true or false, got %v", v)
					}
				}
				if k == "theme" && v != "light" && v != "dark" && v != "system" {
					return nil, fmt.Errorf("theme must be light, dark or system, got %v", v)
				}
				if err := c.Store.Settings.Set(k, json.RawMessage(toJSON(v))); err != nil {
					return nil, err
				}
			}
			return c.Store.Settings.All()
		})
}

func checkPanel(panel string, allowEmpty bool) error {
	if panel == "planner" || panel == "card" || (allowEmpty && panel == "") {
		return nil
	}
	return fmt.Errorf("panel %q must be planner or card", panel)
}

func setUI(c *Call, workspaceID string, fn func(ui map[string]any)) (domain.Workspace, error) {
	w, err := c.Workspace(workspaceID)
	if err != nil {
		return w, err
	}
	if w.UI == nil {
		w.UI = map[string]any{}
	}
	fn(w.UI)
	return w, c.Store.Workspaces.Update(&w)
}
