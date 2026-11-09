package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/chat"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
)

// ModelsListIn is the input of models_list.
type ModelsListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace whose default and planner model to include; defaults to the active one"`
}

// ModelsOut is the result of models_list.
type ModelsOut struct {
	// Models is the list the pickers offer.
	Models []sessions.Model `json:"models"`
	// DefaultModel is the workspace default for cards; PlannerModel is the planner's. Empty means
	// no --model flag, so claude uses its own default.
	WorkspaceID  string `json:"workspace_id"`
	DefaultModel string `json:"default_model"`
	PlannerModel string `json:"planner_model"`
	// Order says how a session's model is chosen.
	Order string `json:"order"`
}

// ModelsSetIn is the input of models_set.
type ModelsSetIn struct {
	Models []sessions.Model `json:"models" jsonschema:"the models the pickers offer, each with the id claude --model takes (opus, sonnet, haiku or a full model name) and a label"`
}

// CardSetModelIn is the input of card_set_model.
type CardSetModelIn struct {
	Card  string `json:"card" jsonschema:"the card: id, nickname or key"`
	Model string `json:"model" jsonschema:"the model id (opus, sonnet, haiku or a full model name); empty goes back to the launch profile's or the workspace default"`
}

// PlannerSetModelIn is the input of planner_set_model.
type PlannerSetModelIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Model       string `json:"model" jsonschema:"the model id; empty goes back to the workspace default for cards, then claude's own default"`
}

// ModelSetOut is the result of card_set_model and planner_set_model.
type ModelSetOut struct {
	Card *domain.Card `json:"card,omitempty"`
	// Model is the model the session uses now, and Source says where it came from.
	Model  string `json:"model"`
	Source string `json:"source"`
	// Typed is true when /model was typed into the running session; otherwise the model applies
	// the next time the session starts.
	Typed bool   `json:"typed"`
	Note  string `json:"note"`
}

const modelOrder = "card, then its launch profile, then the workspace default, then none (claude's own default). The planner uses its own model, then the workspace default."

func registerModels(r *Registry) {
	Register(r, Spec{Name: "models_list", Description: "List the models the pickers offer, and the workspace's default and planner model. A session runs the model of its card, else its launch profile, else the workspace default, else claude's own default."},
		func(ctx context.Context, c *Call, in ModelsListIn) (ModelsOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return ModelsOut{}, err
			}
			return ModelsOut{Models: sessions.Models(c.Store), WorkspaceID: w.ID, DefaultModel: w.DefaultModel, PlannerModel: w.PlannerModel, Order: modelOrder}, nil
		})

	Register(r, Spec{Name: "models_set", Description: "Replace the list of models the pickers offer. Only the user can. A card may still run a model that is not in the list.",
		Mutates: true, Entities: []string{"settings"}},
		func(ctx context.Context, c *Call, in ModelsSetIn) (ModelsOut, error) {
			if c.Actor.Kind != KindYou {
				return ModelsOut{}, errors.New("only the user changes the model list")
			}
			list, err := sessions.CheckModels(in.Models)
			if err != nil {
				return ModelsOut{}, err
			}
			before := sessions.Models(c.Store)
			if err := c.Store.Settings.Set(sessions.SettingModels, json.RawMessage(toJSON(list))); err != nil {
				return ModelsOut{}, err
			}
			c.RecordChange("", map[string]any{"name": "models", "models": before}, map[string]any{"name": "models", "models": list})
			return ModelsOut{Models: list, Order: modelOrder}, nil
		})

	Register(r, Spec{Name: "card_set_model", Description: "Choose the Claude model of a card's session. When the session is running, /model <id> is typed into it; otherwise the model applies the next time it starts. An event on the card says what changed. Empty model goes back to the launch profile's or the workspace default. The user and the planner may; a session may not change its own.",
		Mutates: true, Entities: []string{"cards", "sessions"}},
		func(ctx context.Context, c *Call, in CardSetModelIn) (ModelSetOut, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return ModelSetOut{}, err
			}
			if err := mayChooseModel(c, card.WorkspaceID, &card); err != nil {
				return ModelSetOut{}, err
			}
			model := strings.TrimSpace(in.Model)
			if err := sessions.CheckModelID(model); err != nil {
				return ModelSetOut{}, err
			}
			w, err := c.Workspace(card.WorkspaceID)
			if err != nil {
				return ModelSetOut{}, err
			}
			was := card
			card.Model = model
			if err := c.Store.Cards.Update(&card); err != nil {
				return ModelSetOut{}, err
			}
			c.RecordChange(w.ID, was, card)
			out := announceModel(c, w, &was, &card)
			out.Card = &card
			return out, nil
		})

	Register(r, Spec{Name: "planner_set_model", Description: "Choose the Claude model of the workspace's planner. When the planner is running, /model <id> is typed into it; otherwise it applies the next time it starts. The user may; the planner may not change its own model.",
		Mutates: true, Entities: []string{"workspaces", "sessions"}},
		func(ctx context.Context, c *Call, in PlannerSetModelIn) (ModelSetOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return ModelSetOut{}, err
			}
			if err := mayChooseModel(c, w.ID, nil); err != nil {
				return ModelSetOut{}, err
			}
			model := strings.TrimSpace(in.Model)
			if err := sessions.CheckModelID(model); err != nil {
				return ModelSetOut{}, err
			}
			before := w
			w.PlannerModel = model
			if err := c.Store.Workspaces.Update(&w); err != nil {
				return ModelSetOut{}, err
			}
			c.RecordChange(w.ID, before, w)
			return announceModel(c, w, nil, nil), nil
		})
}

// mayChooseModel says who may pick a model: the user and external clients always, the planner for
// card sessions in its own workspace, never a card session.
func mayChooseModel(c *Call, workspaceID string, card *domain.Card) error {
	switch c.Actor.Kind {
	case KindSession:
		return fmt.Errorf("%s may not change a model. Ask the planner with card_report", c.Actor)
	case KindPlanner:
		if card == nil {
			return errors.New("the planner may not change its own model. Ask the user")
		}
		return sameWorkspace(c, workspaceID)
	}
	return nil
}

// announceModel runs after a model changed. It posts an event on the card (not for the planner),
// and types /model into the running claude session once the call commits. was and now are the
// card before and after; both are nil for the planner.
func announceModel(c *Call, w domain.Workspace, was, now *domain.Card) ModelSetOut {
	var card *domain.Card
	if now != nil {
		card = now
	}
	model, source := sessions.ModelFor(c.Store, w, card)
	out := ModelSetOut{Model: model, Source: source}
	label := model
	if model == "" {
		label = "claude's own default"
	} else if source != "" {
		label = model + " (" + source + ")"
	}
	if card != nil && was != nil {
		prev, prevSource := sessions.ModelFor(c.Store, w, was)
		_ = prevSource
		if prev == "" {
			prev = "claude's own default"
		}
		text := fmt.Sprintf("Model changed: %s to %s", prev, label)
		if t, err := target(c, w.ID, card); err == nil && t.Live != nil && sessions.IsClaude(t.Row.Command) {
			out.Typed = true
			text += ". Typed /model into the running session."
		} else {
			text += ". It applies the next time the session starts."
		}
		ev := domain.Message{WorkspaceID: w.ID, Channel: chat.CardChannel(card.ID), Author: authorOf(c.Actor), Kind: "event", Text: text, Delivered: true}
		_ = c.Store.Messages.Create(&ev)
		c.Touch("messages")
		out.Note = text
	} else if t, err := target(c, w.ID, nil); err == nil && t.Live != nil && sessions.IsClaude(t.Row.Command) {
		out.Typed = true
		out.Note = "Typed /model into the running planner."
	} else {
		out.Note = "It applies the next time the planner starts."
	}
	if out.Typed {
		typed := "/model " + model
		if model == "" {
			typed = "/model default"
		}
		var live interface{ Send(string, bool) error }
		if card != nil {
			if t, err := target(c, w.ID, card); err == nil && t.Live != nil {
				live = t.Live
			}
		} else if t, err := target(c, w.ID, nil); err == nil && t.Live != nil {
			live = t.Live
		}
		if live != nil {
			c.AfterCommit(func() { _ = live.Send(typed, true) })
		}
	}
	return out
}
