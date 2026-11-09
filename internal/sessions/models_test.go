package sessions

import (
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
)

func TestResolveModelOrder(t *testing.T) {
	ws := domain.Workspace{DefaultModel: "sonnet", PlannerModel: "opus"}
	prof := domain.LaunchProfile{Model: "haiku"}
	card := &domain.Card{Model: "claude-opus-5-5"}
	for _, tc := range []struct {
		name         string
		ws           domain.Workspace
		card         *domain.Card
		profile      domain.LaunchProfile
		model, where string
	}{
		{"card wins", ws, card, prof, "claude-opus-5-5", ModelFromCard},
		{"profile next", ws, &domain.Card{}, prof, "haiku", ModelFromProfile},
		{"workspace next", ws, &domain.Card{}, domain.LaunchProfile{}, "sonnet", ModelFromWorkspace},
		{"nothing chosen", domain.Workspace{}, &domain.Card{}, domain.LaunchProfile{}, "", ModelFromNone},
		{"planner own model", ws, nil, prof, "opus", ModelFromPlanner},
		{"planner falls back to the workspace", domain.Workspace{DefaultModel: "sonnet"}, nil, prof, "sonnet", ModelFromWorkspace},
		{"planner ignores profiles", domain.Workspace{}, nil, prof, "", ModelFromNone},
	} {
		if got, where := ResolveModel(tc.ws, tc.card, tc.profile); got != tc.model || where != tc.where {
			t.Errorf("%s: got %q from %q, want %q from %q", tc.name, got, where, tc.model, tc.where)
		}
	}
}

func TestCheckModels(t *testing.T) {
	if err := CheckModelID(""); err != nil {
		t.Error("empty means not chosen")
	}
	for _, ok := range []string{"opus", "claude-opus-5-5", "sonnet[1m]", "us.anthropic.claude-sonnet-5-5:0"} {
		if err := CheckModelID(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{" opus", "a b", "-x", "a;b", "a/b"} {
		if CheckModelID(bad) == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
	if _, err := CheckModels([]Model{{ID: "opus", Label: string(make([]rune, 41))}}); err == nil {
		t.Error("a long label is refused")
	}
}
