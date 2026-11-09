package actions

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/store"
)

// SettingUserActions is the settings key that says who may call the actions that are the user's
// alone, such as planner_policy_set or chat_send.
const SettingUserActions = "mcp.user_actions"

// Values of mcp.user_actions.
const (
	// UserActionsExternal lets MCP token holders with no session header (the user's own clients)
	// call user-only actions. It is the default.
	UserActionsExternal = "external"
	// UserActionsAll lets the planner and card sessions call them too.
	UserActionsAll = "all"
	// UserActionsOff leaves them to the app's own UI.
	UserActionsOff = "off"
)

// UserActionModes lists the values of mcp.user_actions.
var UserActionModes = []string{UserActionsExternal, UserActionsAll, UserActionsOff}

// UserActionsMode reads the setting; an unset or invalid value is external.
func UserActionsMode(st *store.Store) string {
	raw, err := st.Settings.Get(SettingUserActions)
	if err != nil {
		return UserActionsExternal
	}
	var v string
	if json.Unmarshal(raw, &v) != nil || !contains(UserActionModes, v) {
		return UserActionsExternal
	}
	return v
}

// mayUseUserActions says whether the caller may call a user-only action under the current setting.
// The UI always may.
func (c *Call) mayUseUserActions() bool {
	switch c.Actor.Kind {
	case KindYou:
		return true
	case KindExternal:
		return UserActionsMode(c.Store) != UserActionsOff
	}
	return UserActionsMode(c.Store) == UserActionsAll
}

// UserOnly is the one gate for the actions that belong to the user. It returns nil for the UI,
// for external clients unless mcp.user_actions is off, and for the planner and sessions only when
// it is all. A call that passes over MCP is written to the config history with the actor, so the
// user sees it.
func (c *Call) UserOnly(what string) error {
	if c.Actor.Kind == KindYou {
		return nil
	}
	if !c.mayUseUserActions() {
		return fmt.Errorf("only the user may use %s, and %s may not. The user can allow it in Settings, MCP server (%s is %s)", what, c.Actor, SettingUserActions, UserActionsMode(c.Store))
	}
	c.noteUserAction(what)
	return nil
}

// noteUserAction records that a non-UI caller used a user-only action.
func (c *Call) noteUserAction(what string) {
	ws := c.Actor.WorkspaceID
	if ws == "" {
		if v, err := c.Store.Settings.Get(SettingActiveWorkspace); err == nil {
			_ = json.Unmarshal(v, &ws)
		}
	}
	c.RecordChange(ws, nil, map[string]any{
		"name": "used over MCP by " + c.Actor.String(), "action": what, "actor": c.Actor.String(), SettingUserActions: UserActionsMode(c.Store),
	})
}

// userActionsOverride is for an action that refuses some callers by role (a planner may not stop
// itself): it returns true, and notes the call, when mcp.user_actions is all.
func (c *Call) userActionsOverride(what string) bool {
	if c.Actor.Kind == KindYou || UserActionsMode(c.Store) != UserActionsAll {
		return false
	}
	c.noteUserAction(what)
	return true
}

func checkUserActionsValue(v any) error {
	s, _ := v.(string)
	if !contains(UserActionModes, s) {
		return fmt.Errorf("%s must be one of %s, got %v", SettingUserActions, strings.Join(UserActionModes, ", "), v)
	}
	return nil
}
