package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/fields"
	"github.com/GabiHert/hadron/internal/store"
)

// FilterSpec is a saved or live filter: pinned values, a text search and a CEL expression. All must hold.
type FilterSpec struct {
	Vals map[string]string `json:"vals,omitempty" jsonschema:"field path to the value it must equal"`
	Q    string            `json:"q,omitempty" jsonschema:"text the nickname, key or title must contain"`
	Expr string            `json:"expr,omitempty" jsonschema:"a CEL expression"`
}

// filterCards returns the workspace's cards that satisfy the filter, in board order.
func filterCards(c *Call, w domain.Workspace, spec FilterSpec, includeArchived bool) ([]domain.Card, error) {
	cards, err := c.Store.Cards.List(w.ID, storeFilter(includeArchived))
	if err != nil {
		return nil, err
	}
	ev, err := fieldRegistry(c).NewEvaluator(c.Store, w, spec.Expr)
	if err != nil {
		return nil, err
	}
	for path := range spec.Vals {
		if _, ok := fields.Find(ev.Schema(), path); !ok {
			return nil, fmt.Errorf("vals: %q is not a field of this workspace. Valid paths: %s", path, strings.Join(fields.Paths(ev.Schema()), ", "))
		}
	}
	q := strings.ToLower(strings.TrimSpace(spec.Q))
	out := make([]domain.Card, 0, len(cards))
	var first error
	failed := 0
	for _, card := range cards {
		if q != "" && !strings.Contains(strings.ToLower(card.Nick+" "+card.Key+" "+card.Title), q) {
			continue
		}
		if len(spec.Vals) > 0 {
			flat, err := ev.Flat(card)
			if err != nil {
				return nil, err
			}
			ok := true
			for path, want := range spec.Vals {
				if want != "" && fmt.Sprint(flat[path]) != want {
					ok = false
				}
			}
			if !ok {
				continue
			}
		}
		match, err := ev.Match(card)
		if err != nil {
			failed++
			if first == nil {
				first = err
			}
			continue
		}
		if match {
			out = append(out, card)
		}
	}
	if first != nil && len(out) == 0 && failed > 0 {
		return nil, first
	}
	return out, nil
}

// FilterBarSetIn is the input of filter_bar_set.
type FilterBarSetIn struct {
	WorkspaceID string   `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Fields      []string `json:"fields" jsonschema:"the field paths pinned as chips in the filter bar, in order; field_schema_get lists the filterable ones"`
}

// ViewListIn is the input of view_list.
type ViewListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// ViewSaveIn is the input of view_save.
type ViewSaveIn struct {
	WorkspaceID string     `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	ViewID      string     `json:"view_id,omitempty" jsonschema:"update this view; without it, a view with the same name is replaced, else a new one is created"`
	Name        string     `json:"name" jsonschema:"the view's name"`
	Filters     FilterSpec `json:"filters" jsonschema:"the filter to save"`
}

// ViewIDIn is the input of view_apply and view_delete.
type ViewIDIn struct {
	ViewID string `json:"view_id" jsonschema:"the view id"`
}

// ViewApplyOut is the result of view_apply.
type ViewApplyOut struct {
	View    domain.View `json:"view"`
	CardIDs []string    `json:"card_ids"`
}

func registerFilters(r *Registry) {
	Register(r, Spec{Name: "filter_bar_set", Description: "Choose which fields are pinned as chips in the board's filter bar. Only filterable fields can be pinned.",
		Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in FilterBarSetIn) (domain.Workspace, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return w, err
			}
			schema, err := fieldRegistry(c).Schema(c.Store, w)
			if err != nil {
				return w, err
			}
			seen := map[string]bool{}
			for _, p := range in.Fields {
				e, ok := fields.Find(schema, p)
				switch {
				case !ok:
					return w, fmt.Errorf("%q is not a field of workspace %q. Valid paths: %s", p, w.Name, strings.Join(fields.Paths(schema), ", "))
				case !e.Filterable:
					return w, fmt.Errorf("%q cannot be filtered on", p)
				case seen[p]:
					return w, fmt.Errorf("%q is listed twice", p)
				}
				seen[p] = true
			}
			before := w
			w.FilterBar = append([]string{}, in.Fields...)
			if err := c.Store.Workspaces.Update(&w); err != nil {
				return w, err
			}
			c.RecordChange(w.ID, before, w)
			return w, nil
		})

	Register(r, Spec{Name: "view_list", Description: "List a workspace's saved filter views in order."},
		func(ctx context.Context, c *Call, in ViewListIn) ([]domain.View, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			return c.Store.Views.List(w.ID)
		})

	Register(r, Spec{Name: "view_save", Description: "Save the current filter (pinned values, text search, CEL expression) as a named view. The expression must compile. Saving under an existing name replaces that view.",
		Mutates: true, Entities: []string{"views"}},
		func(ctx context.Context, c *Call, in ViewSaveIn) (domain.View, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return domain.View{}, err
			}
			name, err := checkName("view", in.Name)
			if err != nil {
				return domain.View{}, err
			}
			if _, err := filterCards(c, w, in.Filters, false); err != nil {
				return domain.View{}, fmt.Errorf("the filter does not work: %w", err)
			}
			views, err := c.Store.Views.List(w.ID)
			if err != nil {
				return domain.View{}, err
			}
			var v domain.View
			var found bool
			for _, x := range views {
				if (in.ViewID != "" && x.ID == in.ViewID) || (in.ViewID == "" && strings.EqualFold(x.Name, name)) {
					v, found = x, true
				}
			}
			if in.ViewID != "" && !found {
				return v, fmt.Errorf("view %q does not exist. view_list shows the ids", in.ViewID)
			}
			raw, _ := json.Marshal(in.Filters)
			if !found {
				v = domain.View{WorkspaceID: w.ID, Name: name, Filters: raw}
				if err := c.Store.Views.Create(&v); err != nil {
					return v, err
				}
				c.RecordChange(w.ID, nil, v)
				return v, nil
			}
			before := v
			v.Name, v.Filters = name, raw
			if err := c.Store.Views.Update(&v); err != nil {
				return v, err
			}
			c.RecordChange(w.ID, before, v)
			return v, nil
		})

	Register(r, Spec{Name: "view_apply", Description: "Evaluate a saved view against the board now and return it with the ids of the matching cards. The UI keeps the active view itself."},
		func(ctx context.Context, c *Call, in ViewIDIn) (ViewApplyOut, error) {
			v, err := getView(c, in.ViewID)
			if err != nil {
				return ViewApplyOut{}, err
			}
			w, err := c.Workspace(v.WorkspaceID)
			if err != nil {
				return ViewApplyOut{}, err
			}
			var spec FilterSpec
			if err := json.Unmarshal(v.Filters, &spec); err != nil {
				return ViewApplyOut{}, fmt.Errorf("view %q has unreadable filters: %w", v.Name, err)
			}
			cards, err := filterCards(c, w, spec, false)
			if err != nil {
				return ViewApplyOut{}, fmt.Errorf("view %q: %w", v.Name, err)
			}
			ids := make([]string, len(cards))
			for i, card := range cards {
				ids[i] = card.ID
			}
			return ViewApplyOut{View: v, CardIDs: ids}, nil
		})

	Register(r, Spec{Name: "view_delete", Description: "Delete a saved view.",
		Mutates: true, Entities: []string{"views"}},
		func(ctx context.Context, c *Call, in ViewIDIn) (domain.View, error) {
			v, err := getView(c, in.ViewID)
			if err != nil {
				return v, err
			}
			if err := c.Store.Views.Delete(v.ID); err != nil {
				return v, err
			}
			c.RecordChange(v.WorkspaceID, v, nil)
			return v, nil
		})
}

func getView(c *Call, id string) (domain.View, error) {
	v, err := c.Store.Views.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return v, fmt.Errorf("view %q does not exist. view_list shows the ids", id)
	}
	if err == nil {
		c.SetWorkspace(v.WorkspaceID)
	}
	return v, err
}
