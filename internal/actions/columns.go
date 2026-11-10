package actions

import (
	"context"
	"errors"
	"fmt"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// ColumnListIn is the input of column_list.
type ColumnListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// ColumnCreateIn is the input of column_create.
type ColumnCreateIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Name        string `json:"name" jsonschema:"the column name"`
	Role        string `json:"role,omitempty" jsonschema:"needs-you, active, blocked, done or backlog; rules use the role, never the name"`
	WIP         int    `json:"wip,omitempty" jsonschema:"work-in-progress limit; 0 means no limit"`
	Color       string `json:"color,omitempty" jsonschema:"a #rrggbb colour"`
}

// ColumnUpdateIn is the input of column_update. Only the fields you pass change; the id never does.
type ColumnUpdateIn struct {
	ColumnID  string  `json:"column_id" jsonschema:"the column id"`
	Name      *string `json:"name,omitempty" jsonschema:"new name"`
	Role      *string `json:"role,omitempty" jsonschema:"new role, or an empty string for none"`
	WIP       *int    `json:"wip,omitempty" jsonschema:"new WIP limit; 0 means no limit"`
	Fold      *bool   `json:"fold,omitempty" jsonschema:"collapse the column to a narrow rail (true) or expand it (false); the same as collapsed"`
	Collapsed *bool   `json:"collapsed,omitempty" jsonschema:"collapse the column to a narrow rail that shows its name and card count (true), or expand it (false)"`
	Color     *string `json:"color,omitempty" jsonschema:"new #rrggbb colour"`
}

// ColumnReorderIn is the input of column_reorder.
type ColumnReorderIn struct {
	WorkspaceID string   `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	ColumnIDs   []string `json:"column_ids" jsonschema:"every column id of the workspace, in the new order"`
}

// ColumnDeleteIn is the input of column_delete.
type ColumnDeleteIn struct {
	ColumnID string `json:"column_id" jsonschema:"the column to delete"`
	MoveTo   string `json:"move_to,omitempty" jsonschema:"the column that takes its cards; defaults to the first other column"`
}

// ColumnDeleteOut is the result of column_delete.
type ColumnDeleteOut struct {
	Deleted   domain.Column `json:"deleted"`
	MovedTo   domain.Column `json:"moved_to"`
	CardCount int           `json:"card_count"`
}

func registerColumns(r *Registry) {
	Register(r, Spec{Name: "column_list", Description: "List a workspace's columns in board order."},
		func(ctx context.Context, c *Call, in ColumnListIn) ([]domain.Column, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			return c.Store.Columns.List(w.ID)
		})

	Register(r, Spec{Name: "column_create", Description: "Add a column at the end of the board.", Mutates: true, Entities: []string{"columns"}},
		func(ctx context.Context, c *Call, in ColumnCreateIn) (domain.Column, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return domain.Column{}, err
			}
			name, err := checkName("column", in.Name)
			if err != nil {
				return domain.Column{}, err
			}
			if err := checkRole(in.Role); err != nil {
				return domain.Column{}, err
			}
			if err := checkColor(in.Color); err != nil {
				return domain.Column{}, err
			}
			if in.WIP < 0 {
				return domain.Column{}, errors.New("wip must be 0 (no limit) or more")
			}
			col := domain.Column{WorkspaceID: w.ID, Name: name, Role: in.Role, WIP: in.WIP, Color: in.Color}
			if err := c.Store.Columns.Create(&col); err != nil {
				return col, err
			}
			c.RecordChange(w.ID, nil, col)
			return col, nil
		})

	Register(r, Spec{Name: "column_update", Description: "Rename a column or change its role, WIP limit, colour, or collapse it to a narrow rail (collapsed, or its older name fold; the column's fold field says whether it is collapsed). A rename keeps the column's id, so rules and cards keep pointing at it.",
		Mutates: true, Entities: []string{"columns"}},
		func(ctx context.Context, c *Call, in ColumnUpdateIn) (domain.Column, error) {
			col, err := getColumn(c, in.ColumnID)
			if err != nil {
				return col, err
			}
			before := col
			if in.Name != nil {
				if col.Name, err = checkName("column", *in.Name); err != nil {
					return col, err
				}
			}
			if in.Role != nil {
				if err := checkRole(*in.Role); err != nil {
					return col, err
				}
				col.Role = *in.Role
			}
			if in.WIP != nil {
				if *in.WIP < 0 {
					return col, errors.New("wip must be 0 (no limit) or more")
				}
				col.WIP = *in.WIP
			}
			if in.Fold != nil && in.Collapsed != nil && *in.Fold != *in.Collapsed {
				return col, errors.New("fold and collapsed name the same setting: pass one of them")
			}
			if in.Fold != nil {
				col.Fold = *in.Fold
			}
			if in.Collapsed != nil {
				col.Fold = *in.Collapsed
			}
			if in.Color != nil {
				if err := checkColor(*in.Color); err != nil {
					return col, err
				}
				col.Color = *in.Color
			}
			if err := c.Store.Columns.Update(&col); err != nil {
				return col, err
			}
			c.SetWorkspace(col.WorkspaceID)
			c.RecordChange(col.WorkspaceID, before, col)
			return col, nil
		})

	Register(r, Spec{Name: "column_reorder", Description: "Set the order of the board's columns. Pass every column id of the workspace exactly once.",
		Mutates: true, Entities: []string{"columns"}},
		func(ctx context.Context, c *Call, in ColumnReorderIn) ([]domain.Column, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			before, err := c.Store.Columns.List(w.ID)
			if err != nil {
				return nil, err
			}
			if err := c.Store.Columns.Reorder(w.ID, in.ColumnIDs); err != nil {
				return nil, err
			}
			after, err := c.Store.Columns.List(w.ID)
			if err != nil {
				return nil, err
			}
			c.RecordChange(w.ID, columnOrder(before), columnOrder(after))
			return after, nil
		})

	Register(r, Spec{Name: "column_delete", Description: "Delete a column. Its cards move to move_to, or to the first other column. A workspace keeps at least one column.",
		Mutates: true, Entities: []string{"columns", "cards"}},
		func(ctx context.Context, c *Call, in ColumnDeleteIn) (ColumnDeleteOut, error) {
			col, err := getColumn(c, in.ColumnID)
			if err != nil {
				return ColumnDeleteOut{}, err
			}
			cols, err := c.Store.Columns.List(col.WorkspaceID)
			if err != nil {
				return ColumnDeleteOut{}, err
			}
			if len(cols) < 2 {
				return ColumnDeleteOut{}, errors.New("a workspace needs at least one column, so the last one cannot be deleted. Add another column first")
			}
			var target domain.Column
			if in.MoveTo != "" {
				for _, x := range cols {
					if x.ID == in.MoveTo {
						target = x
					}
				}
				if target.ID == "" || target.ID == col.ID {
					return ColumnDeleteOut{}, fmt.Errorf("move_to %q must be another column of the same workspace. Call column_list for the ids", in.MoveTo)
				}
			} else {
				for _, x := range cols {
					if x.ID != col.ID {
						target = x
						break
					}
				}
			}
			cards, err := c.Store.Cards.List(col.WorkspaceID, store.CardFilter{ColumnID: col.ID, IncludeArchived: true})
			if err != nil {
				return ColumnDeleteOut{}, err
			}
			ids := make([]string, len(cards))
			for i, cd := range cards {
				ids[i] = cd.ID
			}
			if err := c.Store.Columns.DeleteMoving(col.ID, target.ID); err != nil {
				return ColumnDeleteOut{}, err
			}
			c.RecordChange(col.WorkspaceID, col, map[string]any{"deleted": true, "moved_to": target.ID, "card_ids": ids})
			return ColumnDeleteOut{Deleted: col, MovedTo: target, CardCount: len(ids)}, nil
		})
}

func getColumn(c *Call, id string) (domain.Column, error) {
	col, err := c.Store.Columns.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return col, fmt.Errorf("column %q does not exist. Call column_list for the ids", id)
	}
	if err == nil {
		c.SetWorkspace(col.WorkspaceID)
	}
	return col, err
}

func columnOrder(cols []domain.Column) []string {
	ids := make([]string, len(cols))
	for i, c := range cols {
		ids[i] = c.ID
	}
	return ids
}
