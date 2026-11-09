package sessions

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

func jsonString(raw json.RawMessage, out *string) bool { return json.Unmarshal(raw, out) == nil }

// FindProfile returns a workspace's launch profile by id or name (case-insensitive).
func FindProfile(st *store.Store, workspaceID, ref string) (domain.LaunchProfile, error) {
	list, err := st.LaunchProfiles.List(workspaceID)
	if err != nil {
		return domain.LaunchProfile{}, err
	}
	for _, p := range list {
		if p.ID == ref {
			return p, nil
		}
	}
	for _, p := range list {
		if strings.EqualFold(p.Name, ref) {
			return p, nil
		}
	}
	names := make([]string, len(list))
	for i, p := range list {
		names[i] = p.Name
	}
	sort.Strings(names) // a stable message, whatever order the rows come back in
	return domain.LaunchProfile{}, fmt.Errorf("launch profile %q does not exist (profiles: %s). Call launch_profile_list", ref, strings.Join(names, ", "))
}

// profile picks the launch profile of a start: the request's, else the card's, else a bare claude.
// A planner never uses one.
func (s *Service) profile(st *store.Store, ws domain.Workspace, card *domain.Card, override string) (domain.LaunchProfile, error) {
	ref := override
	if ref == "" && card != nil {
		ref = card.LaunchProfile
	}
	if card == nil {
		// The planner uses the workspace's own launch profile, when one is set and still exists.
		if name := PlannerProfileName(st, ws.ID); name != "" {
			if p, err := FindProfile(st, ws.ID, name); err == nil {
				p.FirstPrompt = "" // a planner never gets a first prompt
				return p, nil
			}
		}
		return domain.LaunchProfile{Command: "claude"}, nil
	}
	if ref == "" {
		return domain.LaunchProfile{Command: "claude"}, nil
	}
	return FindProfile(st, ws.ID, ref)
}

// SettingPlannerProfilePrefix starts the key of a workspace's planner launch profile setting.
const SettingPlannerProfilePrefix = "planner.launch_profile."

// PlannerProfileName is the launch profile the workspace's planner starts with, or "".
func PlannerProfileName(st *store.Store, workspaceID string) string {
	raw, err := st.Settings.Get(SettingPlannerProfilePrefix + workspaceID)
	if err != nil {
		return ""
	}
	var v string
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return v
}
