package actions

import (
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
)

func argvOf(h *sessionHarness) []string {
	return strings.Split(strings.TrimSuffix(h.file(".argv"), "\x00"), "\x00")
}

func modelFlag(argv []string) string {
	for i, a := range argv {
		if a == "--model" && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

func waitArgv(t *testing.T, h *sessionHarness) []string {
	t.Helper()
	waitUntil(t, "argv", func() bool {
		return strings.Contains(h.file(".argv"), "--mcp-config") && strings.HasSuffix(h.file(".argv"), "\x00")
	})
	return argvOf(h)
}

func TestModelResolutionOrderReachesTheArgv(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	// Nothing chosen: no --model, claude uses its own default.
	var plain domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix"}, &plain)
	if got := modelFlag(waitArgv(t, h)); got != "" {
		t.Fatalf("no model chosen anywhere, but --model %q was passed", got)
	}

	// The workspace default applies to the next card.
	h.do("workspace_update", m{"workspace_id": w.ID, "default_model": "sonnet"}, nil)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "invoice-sync"}, nil)
	waitUntil(t, "the second start", func() bool { return modelFlag(argvOf(h)) == "sonnet" })

	// A launch profile beats the workspace default.
	h.do("launch_profile_create", m{"workspace_id": w.ID, "name": "heavy", "model": "opus"}, nil)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "ledger-audit", "launch_profile": "heavy"}, nil)
	waitUntil(t, "the profile model", func() bool { return modelFlag(argvOf(h)) == "opus" })

	// A card beats its profile.
	h.do("card_create", m{"workspace_id": w.ID, "nick": "tax-report", "launch_profile": "heavy", "model": "haiku"}, nil)
	waitUntil(t, "the card model", func() bool { return modelFlag(argvOf(h)) == "haiku" })

	// The planner has its own, then falls back to the workspace default.
	h.do("planner_start", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "the planner on the default", func() bool { return modelFlag(argvOf(h)) == "sonnet" })
	h.do("planner_set_model", m{"workspace_id": w.ID, "model": "opus"}, nil)
	h.do("planner_restart", m{"workspace_id": w.ID}, nil)
	waitUntil(t, "the planner on its own model", func() bool { return modelFlag(argvOf(h)) == "opus" })

	var out ModelsOut
	h.do("models_list", m{"workspace_id": w.ID}, &out)
	if out.DefaultModel != "sonnet" || out.PlannerModel != "opus" || len(out.Models) != 3 || !strings.Contains(out.Order, "launch profile") {
		t.Fatalf("models_list = %+v", out)
	}
}

func TestCardSetModelTypesIntoARunningSessionAndNotesItOtherwise(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var card, idle domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "launch_profile": "plain"}, &card)
	h.do("card_create", m{"workspace_id": w.ID, "nick": "invoice-sync", "start": false}, &idle)
	waitUntil(t, "billing-fix running", func() bool { return h.status("billing-fix").Running })

	var out ModelSetOut
	h.do("card_set_model", m{"card": "billing-fix", "model": "opus"}, &out)
	if !out.Typed || out.Model != "opus" || out.Source != "card" || out.Card == nil || out.Card.Model != "opus" {
		t.Fatalf("out = %+v", out)
	}
	waitUntil(t, "/model typed", func() bool { return strings.Contains(h.file(".in"), "/model opus") })
	var events []string
	for _, msg := range h.messagesOf(card) {
		if msg.Kind == "event" {
			events = append(events, msg.Text)
		}
	}
	if len(events) == 0 || !strings.Contains(events[len(events)-1], "Model changed: claude's own default to opus (card). Typed /model into the running session.") {
		t.Fatalf("events = %q", events)
	}
	var hist []HistoryEntry
	h.do("config_history", m{"workspace_id": w.ID}, &hist)
	found := false
	for _, e := range hist {
		found = found || (e.Action == "card_set_model" && strings.Contains(e.Summary, "model"))
	}
	if !found {
		t.Fatalf("no history row for the model change: %+v", hist)
	}

	// Clearing falls back to the workspace default, and types it.
	h.do("workspace_update", m{"workspace_id": w.ID, "default_model": "haiku"}, nil)
	h.do("card_update", m{"card_id": card.ID, "model": ""}, &card)
	waitUntil(t, "/model haiku", func() bool { return strings.Contains(h.file(".in"), "/model haiku") })

	// A card with no running session only remembers it, and the next start uses it.
	h.do("card_set_model", m{"card": "invoice-sync", "model": "sonnet"}, &out)
	if out.Typed || !strings.Contains(out.Note, "next time the session starts") {
		t.Fatalf("not running: %+v", out)
	}
	if strings.Contains(h.file(".in"), "/model sonnet") {
		t.Fatal("nothing runs, so nothing is typed")
	}
	h.do("session_start", m{"card": "invoice-sync"}, nil)
	waitUntil(t, "the remembered model", func() bool { return modelFlag(argvOf(h)) == "sonnet" })
}

func (h *sessionHarness) messagesOf(card domain.Card) []domain.Message {
	var msgs []domain.Message
	h.do("chat_read", m{"channel": "card:" + card.ID}, &msgs)
	return msgs
}

func TestModelPermissionsAndValidation(t *testing.T) {
	h := newSessionHarness(t, false)
	w := h.workspace("Acme")
	var card domain.Card
	h.do("card_create", m{"workspace_id": w.ID, "nick": "billing-fix", "start": false}, &card)
	self := Actor{Kind: KindSession, Name: "billing-fix", WorkspaceID: w.ID, CardID: card.ID}
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}

	if err := h.as(self, "card_set_model", m{"card": "billing-fix", "model": "opus"}, nil); err == nil || !strings.Contains(err.Error(), "may not change a model") {
		t.Errorf("a session changing its model: %v", err)
	}
	if err := h.as(self, "card_update", m{"card_id": card.ID, "model": "opus"}, nil); err == nil || !strings.Contains(err.Error(), "may not change a model") {
		t.Errorf("a session through card_update: %v", err)
	}
	if err := h.as(planner, "card_set_model", m{"card": "billing-fix", "model": "opus"}, nil); err != nil {
		t.Errorf("the planner chooses a card's model: %v", err)
	}
	if err := h.as(planner, "planner_set_model", m{"workspace_id": w.ID, "model": "opus"}, nil); err == nil || !strings.Contains(err.Error(), "its own model") {
		t.Errorf("the planner changing its own model: %v", err)
	}
	if err := h.as(Actor{Kind: KindPlanner, WorkspaceID: "elsewhere"}, "card_set_model", m{"card": card.ID, "model": "opus"}, nil); err == nil {
		t.Error("another workspace's planner")
	}
	if err := h.as(planner, "models_set", m{"models": []m{{"id": "opus"}}}, nil); err == nil || !strings.Contains(err.Error(), "only the user") {
		t.Errorf("the planner editing the list: %v", err)
	}
	for _, bad := range []string{"has space", "semi;colon", "$(x)", strings.Repeat("a", 65)} {
		if err := h.try("card_set_model", m{"card": "billing-fix", "model": bad}, nil); err == nil {
			t.Errorf("model %q should be refused", bad)
		}
	}
	h.fails("card_create", m{"workspace_id": w.ID, "nick": "bad-model", "model": "no way", "start": false}, "not valid")
	h.fails("launch_profile_create", m{"workspace_id": w.ID, "name": "bad", "model": "no way"}, "not valid")
	h.fails("workspace_update", m{"workspace_id": w.ID, "default_model": "no way"}, "not valid")
	// A full model name is fine.
	h.do("card_set_model", m{"card": "billing-fix", "model": "claude-opus-5-5"}, nil)
	h.do("card_set_model", m{"card": "billing-fix", "model": "sonnet[1m]"}, nil)

	var list ModelsOut
	h.do("models_set", m{"models": []m{{"id": "opus", "label": "Opus 5"}, {"id": "claude-haiku-5-5"}}}, &list)
	if len(list.Models) != 2 || list.Models[1].Label != "claude-haiku-5-5" {
		t.Fatalf("models_set = %+v", list)
	}
	h.do("models_list", m{"workspace_id": w.ID}, &list)
	if len(list.Models) != 2 || list.Models[0].Label != "Opus 5" {
		t.Fatalf("models_list = %+v", list)
	}
	h.fails("models_set", m{"models": []m{{"id": "opus"}, {"id": "OPUS"}}}, "twice")
	h.fails("models_set", m{"models": []m{{"id": "not valid!"}}}, "invalid id")
}
