package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/store"
)

// fieldRegistry is the registry of the call, or an empty one (native and custom fields only).
func fieldRegistry(c *Call) *fields.Registry {
	if c.Deps.Fields != nil {
		return c.Deps.Fields
	}
	return noProviders
}

var noProviders = fields.NewRegistry()

// FieldSchemaGetIn is the input of field_schema_get.
type FieldSchemaGetIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// FieldSchemaOut is the result of field_schema_get.
type FieldSchemaOut struct {
	Fields     []fields.Entry `json:"fields"`
	Namespaces []string       `json:"namespaces"`
}

// CardFieldsGetIn is the input of card_fields_get.
type CardFieldsGetIn struct {
	Card string `json:"card" jsonschema:"the card: id, nickname or key"`
}

// CardFieldsOut is the result of card_fields_get.
type CardFieldsOut struct {
	CardID string         `json:"card_id"`
	Fields map[string]any `json:"fields"`
}

// CardFieldSetIn is the input of card_field_set.
type CardFieldSetIn struct {
	Card  string `json:"card,omitempty" jsonschema:"the card: id, nickname or key; a session may leave it out for its own card"`
	Path  string `json:"path" jsonschema:"the field: card.assignee, card.stage, card.key, card.nick, card.column, custom.<name> or <integration>.<field>; field_schema_get lists them"`
	Value any    `json:"value" jsonschema:"the new value: a string, number or true/false matching the field's type; an empty string clears it"`
}

// CardFieldSetOut is the result of card_field_set.
type CardFieldSetOut struct {
	Card  domain.Card `json:"card"`
	Path  string      `json:"path"`
	Old   any         `json:"old"`
	Value any         `json:"value"`
}

// ChipOut is one chip on a card face.
type ChipOut struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Value any    `json:"value"`
}

// FilterEvalIn is the input of filter_eval.
type FilterEvalIn struct {
	WorkspaceID     string            `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Expr            string            `json:"expr,omitempty" jsonschema:"a CEL expression, such as card.assignee == 'You' && custom.priority == 'high'; field_schema_get lists the fields"`
	Vals            map[string]string `json:"vals,omitempty" jsonschema:"pinned filters: each field path mapped to the value it must equal"`
	Q               string            `json:"q,omitempty" jsonschema:"only cards whose nickname, key or title contains this text"`
	IncludeArchived bool              `json:"include_archived,omitempty" jsonschema:"also test archived cards"`
}

// FilterEvalOut is the result of filter_eval.
type FilterEvalOut struct {
	CardIDs []string `json:"card_ids"`
}

// CustomFieldCreateIn is the input of custom_field_create.
type CustomFieldCreateIn struct {
	WorkspaceID string   `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Name        string   `json:"name" jsonschema:"lowercase name, letters digits and underscores; the field becomes custom.<name>"`
	Label       string   `json:"label,omitempty" jsonschema:"the name shown in the UI"`
	Type        string   `json:"type,omitempty" jsonschema:"text, number, enum, user, date or bool; default text"`
	Options     []string `json:"options,omitempty" jsonschema:"the allowed values of an enum"`
	CardChip    bool     `json:"card_chip,omitempty" jsonschema:"show the value as a chip on the card"`
}

// CustomFieldUpdateIn is the input of custom_field_update.
type CustomFieldUpdateIn struct {
	WorkspaceID string    `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Name        string    `json:"name" jsonschema:"the field's name, with or without the custom. prefix"`
	Label       *string   `json:"label,omitempty" jsonschema:"new label"`
	Options     *[]string `json:"options,omitempty" jsonschema:"new enum options"`
	CardChip    *bool     `json:"card_chip,omitempty" jsonschema:"show the value as a chip on the card"`
}

// CustomFieldDeleteIn is the input of custom_field_delete.
type CustomFieldDeleteIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Name        string `json:"name" jsonschema:"the field's name, with or without the custom. prefix"`
}

func registerFields(r *Registry) {
	Register(r, Spec{Name: "field_schema_get", Description: "List every field a card can have in a workspace: native card.* fields, the workspace's custom.* fields, and the fields of its enabled integrations. Filters and rules use these paths in CEL, such as card.stage == 'review'."},
		func(ctx context.Context, c *Call, in FieldSchemaGetIn) (FieldSchemaOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return FieldSchemaOut{}, err
			}
			schema, err := fieldRegistry(c).Schema(c.Store, w)
			if err != nil {
				return FieldSchemaOut{}, err
			}
			env, err := fieldRegistry(c).NewEnv(w)
			if err != nil {
				return FieldSchemaOut{}, err
			}
			return FieldSchemaOut{Fields: schema, Namespaces: env.Namespaces}, nil
		})

	Register(r, Spec{Name: "card_fields_get", Description: "Read every field value of a card, keyed by path (card.column, custom.priority, jira.status...). Fields with no value yet read as an empty string, 0 or false."},
		func(ctx context.Context, c *Call, in CardFieldsGetIn) (CardFieldsOut, error) {
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return CardFieldsOut{}, err
			}
			w, err := c.Workspace(card.WorkspaceID)
			if err != nil {
				return CardFieldsOut{}, err
			}
			vars, err := cardVars(c, w, card)
			if err != nil {
				return CardFieldsOut{}, err
			}
			return CardFieldsOut{CardID: card.ID, Fields: fields.Flat(vars)}, nil
		})

	Register(r, Spec{Name: "card_field_set", Description: "Set one field of a card. A card session may set only its own card's card.stage, custom.* fields and linked-item fields. Native fields (card.assignee, card.stage, card.key, card.nick, card.column) and custom.* fields are written here; a field of a linked integration is written through the integration. Rules that watch the field run afterwards.",
		Mutates: true, Entities: []string{"cards"}},
		func(ctx context.Context, c *Call, in CardFieldSetIn) (CardFieldSetOut, error) {
			if c.Actor.Kind == KindSession && in.Card == "" {
				in.Card = c.Actor.CardID
			}
			if in.Card == "" {
				return CardFieldSetOut{}, errors.New("pass card: the id, nickname or key of the card")
			}
			card, err := cardByRef(c, in.Card)
			if err != nil {
				return CardFieldSetOut{}, err
			}
			if c.Actor.Kind == KindSession {
				if card.ID != c.Actor.CardID {
					return CardFieldSetOut{}, fmt.Errorf("%s may set fields only on its own card, not on %q. Ask the planner with card_report", c.Actor, card.Nick)
				}
				if strings.HasPrefix(in.Path, "card.") && in.Path != "card.stage" {
					return CardFieldSetOut{}, fmt.Errorf("a session may set card.stage, custom.* and the fields of its own linked items, not %s. The planner or the user changes that", in.Path)
				}
			}
			return setField(ctx, c, card, in.Path, in.Value)
		})

	Register(r, Spec{Name: "card_chips", Description: "For every card of a workspace, the values of the fields that are set to show as chips on the card face (custom fields and integration fields with card_chip on, such as jira.status). Cards with nothing to show are left out."},
		func(ctx context.Context, c *Call, in FieldSchemaGetIn) (map[string][]ChipOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			ev, err := fieldRegistry(c).NewEvaluator(c.Store, w, "")
			if err != nil {
				return nil, err
			}
			cards, err := c.Store.Cards.List(w.ID, storeFilter(false))
			if err != nil {
				return nil, err
			}
			out := map[string][]ChipOut{}
			for _, card := range cards {
				flat, err := ev.Flat(card)
				if err != nil {
					return nil, err
				}
				for _, e := range ev.Schema() {
					if !e.CardChip || e.Source == "native" {
						continue
					}
					v := flat[e.Path]
					if s := fmt.Sprint(v); v == nil || s == "" || s == "0" || s == "false" {
						continue
					}
					out[card.ID] = append(out[card.ID], ChipOut{Path: e.Path, Label: e.Label, Value: v})
				}
			}
			return out, nil
		})

	Register(r, Spec{Name: "filter_eval", Description: "Return the ids of the cards that satisfy a CEL expression, and optionally pinned field values (vals) and a text search (q), all of which must hold. A compile error comes back verbatim, with the position. An empty filter matches every card."},
		func(ctx context.Context, c *Call, in FilterEvalIn) (FilterEvalOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return FilterEvalOut{}, err
			}
			cards, err := filterCards(c, w, FilterSpec{Vals: in.Vals, Q: in.Q, Expr: in.Expr}, in.IncludeArchived)
			if err != nil {
				return FilterEvalOut{}, err
			}
			ids := make([]string, len(cards))
			for i, card := range cards {
				ids[i] = card.ID
			}
			return FilterEvalOut{CardIDs: ids}, nil
		})

	Register(r, Spec{Name: "custom_field_list", Description: "List a workspace's custom fields."},
		func(ctx context.Context, c *Call, in FieldSchemaGetIn) ([]fields.Entry, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			return fields.LoadCustom(c.Store, w.ID)
		})

	Register(r, Spec{Name: "custom_field_create", Description: "Add a custom field to a workspace. It appears as custom.<name> in filters, rules and on cards.",
		Mutates: true, Entities: []string{"fields"}},
		func(ctx context.Context, c *Call, in CustomFieldCreateIn) (fields.Entry, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return fields.Entry{}, err
			}
			e, err := fields.CheckCustom(fields.Entry{Path: in.Name, Label: in.Label, Type: in.Type, Options: in.Options, CardChip: in.CardChip})
			if err != nil {
				return e, err
			}
			list, err := fields.LoadCustom(c.Store, w.ID)
			if err != nil {
				return e, err
			}
			for _, x := range list {
				if x.Path == e.Path {
					return e, fmt.Errorf("%s already exists in workspace %q. Use custom_field_update to change it", e.Path, w.Name)
				}
			}
			if err := fields.SaveCustom(c.Store, w.ID, append(list, e)); err != nil {
				return e, err
			}
			c.RecordChange(w.ID, nil, e)
			return e, nil
		})

	Register(r, Spec{Name: "custom_field_update", Description: "Change a custom field's label, enum options or card chip. The type cannot change; delete the field and create it again for that.",
		Mutates: true, Entities: []string{"fields"}},
		func(ctx context.Context, c *Call, in CustomFieldUpdateIn) (fields.Entry, error) {
			w, list, i, err := findCustom(c, in.WorkspaceID, in.Name)
			if err != nil {
				return fields.Entry{}, err
			}
			before := list[i]
			e := before
			if in.Label != nil {
				e.Label = strings.TrimSpace(*in.Label)
			}
			if in.Options != nil {
				e.Options = *in.Options
			}
			if in.CardChip != nil {
				e.CardChip = *in.CardChip
			}
			e, err = fields.CheckCustom(e)
			if err != nil {
				return e, err
			}
			list[i] = e
			if err := fields.SaveCustom(c.Store, w.ID, list); err != nil {
				return e, err
			}
			c.RecordChange(w.ID, before, e)
			return e, nil
		})

	Register(r, Spec{Name: "custom_field_delete", Description: "Delete a custom field and clear its value on every card. This cannot be undone through the UI; config_revert restores the definition, not the values.",
		Mutates: true, Entities: []string{"fields", "cards"}},
		func(ctx context.Context, c *Call, in CustomFieldDeleteIn) (fields.Entry, error) {
			w, list, i, err := findCustom(c, in.WorkspaceID, in.Name)
			if err != nil {
				return fields.Entry{}, err
			}
			gone := list[i]
			if err := fields.SaveCustom(c.Store, w.ID, append(list[:i:i], list[i+1:]...)); err != nil {
				return gone, err
			}
			_, name := fields.SplitPath(gone.Path)
			cards, err := c.Store.Cards.List(w.ID, storeAll)
			if err != nil {
				return gone, err
			}
			for _, card := range cards {
				if _, has := card.Fields[name]; has {
					delete(card.Fields, name)
					if err := c.Store.Cards.Update(&card); err != nil {
						return gone, err
					}
				}
			}
			c.RecordChange(w.ID, gone, nil)
			return gone, nil
		})
}

func findCustom(c *Call, workspaceID, name string) (domain.Workspace, []fields.Entry, int, error) {
	w, err := c.Workspace(workspaceID)
	if err != nil {
		return w, nil, 0, err
	}
	list, err := fields.LoadCustom(c.Store, w.ID)
	if err != nil {
		return w, nil, 0, err
	}
	path := fields.NSCustom + "." + strings.TrimPrefix(name, fields.NSCustom+".")
	for i, e := range list {
		if e.Path == path {
			return w, list, i, nil
		}
	}
	return w, nil, 0, fmt.Errorf("workspace %q has no custom field %q. custom_field_list shows them", w.Name, path)
}

// cardVars builds the CEL variables of one card.
func cardVars(c *Call, w domain.Workspace, card domain.Card) (map[string]any, error) {
	reg := fieldRegistry(c)
	schema, err := reg.Schema(c.Store, w)
	if err != nil {
		return nil, err
	}
	cols, err := fields.ColumnMap(c.Store, w.ID)
	if err != nil {
		return nil, err
	}
	return reg.Values(c.Store, w, schema, cols, card)
}

// setField writes one field and runs the rules that watch it.
func setField(ctx context.Context, c *Call, card domain.Card, path string, value any) (CardFieldSetOut, error) {
	w, err := c.Workspace(card.WorkspaceID)
	if err != nil {
		return CardFieldSetOut{}, err
	}
	res, err := board.SetField(ctx, c.Store, fieldRegistry(c), w, card, path, value, false)
	if err != nil {
		return CardFieldSetOut{}, nilNick(err, card.Nick)
	}
	card, err = c.Store.Cards.Get(card.ID)
	if err != nil {
		return CardFieldSetOut{}, err
	}
	if res.Old != res.New {
		if err := runEvent(c, board.Event{Trigger: board.OnFieldChanged, CardID: card.ID, Field: path, Old: res.Old, New: res.New}); err != nil {
			return CardFieldSetOut{}, err
		}
		if res.Moved {
			if err := runEvent(c, board.Event{Trigger: board.OnColumnEntered, CardID: card.ID, ColumnID: res.ToID}); err != nil {
				return CardFieldSetOut{}, err
			}
		}
		card, _ = c.Store.Cards.Get(card.ID)
	}
	return CardFieldSetOut{Card: card, Path: path, Old: res.Old, Value: res.New}, nil
}

func nilNick(err error, nick string) error { return nickError(err, nick, "") }

var storeAll = store.CardFilter{IncludeArchived: true}

func storeFilter(includeArchived bool) store.CardFilter {
	return store.CardFilter{IncludeArchived: includeArchived}
}
