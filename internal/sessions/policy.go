package sessions

import (
	"encoding/json"

	"github.com/GabiHert/hadron/internal/store"
)

// SettingWorkspacePolicyPrefix starts the key of a workspace's own planner policy. The key is the
// prefix plus the workspace id; an empty value means "use the global policy". The ids hold hyphens,
// so settings_update cannot write these keys: only planner_policy_set does.
const SettingWorkspacePolicyPrefix = "planner.policy.workspace."

// Where a planner policy comes from.
const (
	PolicyFromWorkspace = "workspace"
	PolicyFromGlobal    = "global"
	PolicyFromDefault   = "default"
)

// PlannerPolicy returns the policy in force for a workspace and where it came from: the
// workspace's own, else the global setting, else allow_all.
func PlannerPolicy(st *store.Store, workspaceID string) (policy, source string) {
	if workspaceID != "" {
		if p := policySetting(st, SettingWorkspacePolicyPrefix+workspaceID); p != "" {
			return p, PolicyFromWorkspace
		}
	}
	if p := policySetting(st, SettingPlannerPolicy); p != "" {
		return p, PolicyFromGlobal
	}
	return "allow_all", PolicyFromDefault
}

// policySetting reads one policy setting; an unknown or empty value reads as unset.
func policySetting(st *store.Store, key string) string {
	raw, err := st.Settings.Get(key)
	if err != nil {
		return ""
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	if _, ok := policyText[v]; !ok {
		return ""
	}
	return v
}
