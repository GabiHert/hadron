package sessions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// SettingModels is the setting that holds the list of models the pickers offer.
const SettingModels = "models"

// Model is one entry of the model list: the id claude --model takes (an alias such as opus, or a
// full model name) and the label the pickers show.
type Model struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// DefaultModels is the list a fresh install offers.
var DefaultModels = []Model{
	{ID: "opus", Label: "Opus"},
	{ID: "sonnet", Label: "Sonnet"},
	{ID: "haiku", Label: "Haiku"},
}

const maxModels = 30

var modelIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:\[\]-]{0,63}$`)

// CheckModelID reports whether id can be passed to claude --model. Empty is fine: it means "not
// chosen here".
func CheckModelID(id string) error {
	if id == "" || modelIDRe.MatchString(id) {
		return nil
	}
	return fmt.Errorf("model %q is not valid: use an alias such as opus or sonnet, or a model name, with letters, digits and . _ : [ ] - only", id)
}

// CheckModels validates a model list and returns it cleaned.
func CheckModels(list []Model) ([]Model, error) {
	if len(list) > maxModels {
		return nil, fmt.Errorf("%d models is too many; keep at most %d", len(list), maxModels)
	}
	seen := map[string]bool{}
	out := make([]Model, 0, len(list))
	for i, m := range list {
		id, label := strings.TrimSpace(m.ID), strings.TrimSpace(m.Label)
		if id == "" || !modelIDRe.MatchString(id) {
			return nil, fmt.Errorf("model %d has an invalid id %q: use an alias such as opus or sonnet, or a model name", i+1, m.ID)
		}
		if seen[strings.ToLower(id)] {
			return nil, fmt.Errorf("model %q appears twice", id)
		}
		seen[strings.ToLower(id)] = true
		if label == "" {
			label = id
		}
		if len([]rune(label)) > 40 {
			return nil, fmt.Errorf("the label of %q is longer than 40 characters", id)
		}
		out = append(out, Model{ID: id, Label: label})
	}
	return out, nil
}

// Models returns the model list: the `models` setting, else DefaultModels.
func Models(st *store.Store) []Model {
	raw, err := st.Settings.Get(SettingModels)
	if err != nil {
		return append([]Model(nil), DefaultModels...)
	}
	var list []Model
	if json.Unmarshal(raw, &list) != nil {
		return append([]Model(nil), DefaultModels...)
	}
	return list
}

// Where a resolved model came from.
const (
	ModelFromCard      = "card"
	ModelFromProfile   = "launch profile"
	ModelFromWorkspace = "workspace default"
	ModelFromPlanner   = "planner"
	ModelFromNone      = ""
)

// ResolveModel picks the model a session runs: the card's own, else its launch profile's, else the
// workspace default. A planner uses the workspace's planner model, else the workspace default. An
// empty result means no --model flag, so claude uses its own default. The second result says
// where the model came from.
func ResolveModel(ws domain.Workspace, card *domain.Card, profile domain.LaunchProfile) (model, source string) {
	if card == nil {
		if ws.PlannerModel != "" {
			return ws.PlannerModel, ModelFromPlanner
		}
		if ws.DefaultModel != "" {
			return ws.DefaultModel, ModelFromWorkspace
		}
		return "", ModelFromNone
	}
	switch {
	case card.Model != "":
		return card.Model, ModelFromCard
	case profile.Model != "":
		return profile.Model, ModelFromProfile
	case ws.DefaultModel != "":
		return ws.DefaultModel, ModelFromWorkspace
	}
	return "", ModelFromNone
}

// ModelFor resolves the model of a card (or of the planner when card is nil) from the store,
// following the card's launch profile.
func ModelFor(st *store.Store, ws domain.Workspace, card *domain.Card) (model, source string) {
	var profile domain.LaunchProfile
	if card != nil && card.LaunchProfile != "" {
		if p, err := FindProfile(st, ws.ID, card.LaunchProfile); err == nil {
			profile = p
		}
	}
	return ResolveModel(ws, card, profile)
}
