package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
)

// WorkspaceIDIn is the input of the actions that need only a workspace.
type WorkspaceIDIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"the workspace id"`
}

// WorkspaceListIn is the input of workspace_list.
type WorkspaceListIn struct {
	IncludeArchived bool `json:"include_archived,omitempty" jsonschema:"also list archived workspaces"`
}

// WorkspaceCreateIn is the input of workspace_create.
type WorkspaceCreateIn struct {
	Name         string   `json:"name" jsonschema:"the workspace name, up to 60 characters"`
	Root         string   `json:"root,omitempty" jsonschema:"the root folder; ~ is expanded. Defaults to the home folder"`
	Color        string   `json:"color,omitempty" jsonschema:"a #rrggbb colour; defaults to the next swatch"`
	Template     string   `json:"template,omitempty" jsonschema:"column template: planner (default), simple or copy"`
	CopyFrom     string   `json:"copy_from,omitempty" jsonschema:"with template copy, the workspace whose columns to copy"`
	Integrations []string `json:"integrations,omitempty" jsonschema:"integration ids to enable"`
}

// WorkspaceUpdateIn is the input of workspace_update. Only the fields you pass change.
type WorkspaceUpdateIn struct {
	WorkspaceID  string    `json:"workspace_id" jsonschema:"the workspace id"`
	Name         *string   `json:"name,omitempty" jsonschema:"new name"`
	Root         *string   `json:"root,omitempty" jsonschema:"new root folder"`
	Color        *string   `json:"color,omitempty" jsonschema:"new #rrggbb colour"`
	Integrations *[]string `json:"integrations,omitempty" jsonschema:"the enabled integration ids"`
	FilterBar    *[]string `json:"filter_bar,omitempty" jsonschema:"the field paths pinned in the filter bar"`
}

// WorkspaceArchiveIn is the input of workspace_archive.
type WorkspaceArchiveIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"the workspace id"`
	Archived    *bool  `json:"archived,omitempty" jsonschema:"true (default) to archive, false to restore"`
}

func registerWorkspaces(r *Registry) {
	Register(r, Spec{Name: "workspace_list", Description: "List workspaces in order. Archived ones are left out unless include_archived is true."},
		func(ctx context.Context, c *Call, in WorkspaceListIn) ([]domain.Workspace, error) {
			return c.Store.Workspaces.List(in.IncludeArchived)
		})

	Register(r, Spec{Name: "workspace_get", Description: "Get one workspace by id, with its integrations, filter bar and UI state."},
		func(ctx context.Context, c *Call, in WorkspaceIDIn) (domain.Workspace, error) {
			return c.Workspace(in.WorkspaceID)
		})

	Register(r, Spec{Name: "workspace_create", Description: "Create a workspace with its columns, the built-in rules, the default dashboard and prompts. The first workspace becomes the active one.",
		Mutates: true, Entities: []string{"workspaces", "columns", "rules"}},
		func(ctx context.Context, c *Call, in WorkspaceCreateIn) (domain.Workspace, error) {
			name, err := checkName("workspace", in.Name)
			if err != nil {
				return domain.Workspace{}, err
			}
			if err := checkColor(in.Color); err != nil {
				return domain.Workspace{}, err
			}
			root, err := resolveRoot(in.Root)
			if err != nil {
				return domain.Workspace{}, err
			}
			existing, err := c.Store.Workspaces.List(true)
			if err != nil {
				return domain.Workspace{}, err
			}
			color := in.Color
			if color == "" {
				color = DefaultColors[len(existing)%len(DefaultColors)]
			}
			cols, err := columnsForTemplate(c, in.Template, in.CopyFrom)
			if err != nil {
				return domain.Workspace{}, err
			}
			w := domain.Workspace{Name: name, Root: root, Color: color, Integrations: in.Integrations, FilterBar: []string{"card.assignee"}}
			if err := c.Store.Workspaces.Create(&w); err != nil {
				return w, err
			}
			if _, err := board.SeedWorkspace(c.Store, w, cols); err != nil {
				return w, err
			}
			if len(existing) == 0 {
				if err := c.Store.Settings.Set(SettingActiveWorkspace, json.RawMessage(toJSON(w.ID))); err != nil {
					return w, err
				}
			}
			c.RecordChange(w.ID, nil, w)
			if len(existing) == 0 {
				autostartPlanner(c, w)
			}
			return w, nil
		})

	Register(r, Spec{Name: "workspace_update", Description: "Change a workspace's name, root folder, colour, enabled integrations or pinned filter fields. Only the fields you pass change.",
		Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in WorkspaceUpdateIn) (domain.Workspace, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return w, err
			}
			before := w
			if in.Name != nil {
				if w.Name, err = checkName("workspace", *in.Name); err != nil {
					return w, err
				}
			}
			if in.Root != nil {
				if w.Root, err = resolveRoot(*in.Root); err != nil {
					return w, err
				}
			}
			if in.Color != nil {
				if err := checkColor(*in.Color); err != nil {
					return w, err
				}
				w.Color = *in.Color
			}
			if in.Integrations != nil {
				w.Integrations = *in.Integrations
			}
			if in.FilterBar != nil {
				w.FilterBar = *in.FilterBar
			}
			if err := c.Store.Workspaces.Update(&w); err != nil {
				return w, err
			}
			c.RecordChange(w.ID, before, w)
			return w, nil
		})

	Register(r, Spec{Name: "workspace_archive", Description: "Archive a workspace so it leaves the tab bar, or restore it with archived: false. Nothing is deleted.",
		Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in WorkspaceArchiveIn) (domain.Workspace, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return w, err
			}
			before := w
			w.Archived = in.Archived == nil || *in.Archived
			if err := c.Store.Workspaces.Update(&w); err != nil {
				return w, err
			}
			if w.Archived {
				stopWorkspaceSessions(c, w.ID)
			}
			c.RecordChange(w.ID, before, w)
			return w, nil
		})

	Register(r, Spec{Name: "workspace_delete", Description: "Delete a workspace and everything in it: columns, cards, chats, rules, dashboard. This cannot be undone; prefer workspace_archive.",
		Mutates: true, Entities: []string{"workspaces", "columns", "cards"}},
		func(ctx context.Context, c *Call, in WorkspaceIDIn) (domain.Workspace, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return w, err
			}
			stopWorkspaceSessions(c, w.ID)
			if err := c.Store.Workspaces.Delete(w.ID); err != nil {
				return w, err
			}
			if v, err := c.Store.Settings.Get(SettingActiveWorkspace); err == nil {
				var active string
				_ = json.Unmarshal(v, &active)
				if active == w.ID {
					if rest, _ := c.Store.Workspaces.List(false); len(rest) > 0 {
						_ = c.Store.Settings.Set(SettingActiveWorkspace, json.RawMessage(toJSON(rest[0].ID)))
					} else {
						_ = c.Store.Settings.Set(SettingActiveWorkspace, json.RawMessage("null"))
					}
				}
			}
			c.RecordChange(w.ID, w, nil)
			return w, nil
		})

	Register(r, Spec{Name: "workspace_switch", Description: "Make a workspace the active one. Actions that omit workspace_id then act on it.",
		Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in WorkspaceIDIn) (domain.Workspace, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return w, err
			}
			if w.Archived {
				return w, fmt.Errorf("workspace %q is archived. Restore it with workspace_archive (archived: false) first", w.Name)
			}
			if err := c.Store.Settings.Set(SettingActiveWorkspace, json.RawMessage(toJSON(w.ID))); err != nil {
				return w, err
			}
			autostartPlanner(c, w)
			return w, nil
		})
}

func columnsForTemplate(c *Call, template, copyFrom string) ([]board.ColumnSpec, error) {
	if template == board.TemplateCopy {
		if copyFrom == "" {
			return nil, errors.New("template copy needs copy_from: the id of the workspace whose columns to copy")
		}
		src, err := c.Store.Columns.List(copyFrom)
		if err != nil {
			return nil, err
		}
		if len(src) == 0 {
			return nil, fmt.Errorf("workspace %q has no columns to copy. Call workspace_list for the ids", copyFrom)
		}
		out := make([]board.ColumnSpec, len(src))
		for i, s := range src {
			out[i] = board.ColumnSpec{Name: s.Name, Role: s.Role, WIP: s.WIP}
		}
		return out, nil
	}
	cols, ok := board.Templates(template)
	if !ok {
		return nil, fmt.Errorf("template %q is not one of planner, simple, copy", template)
	}
	return cols, nil
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// stopWorkspaceSessions hangs up every running session of a workspace once the call commits.
func stopWorkspaceSessions(c *Call, workspaceID string) {
	if c.Deps.Sessions == nil {
		return
	}
	for _, stop := range c.Deps.Sessions.Running(c.Store, workspaceID) {
		c.AfterCommit(stop)
	}
}
