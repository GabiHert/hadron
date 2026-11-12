package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/board"
	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/sessions"
	"github.com/GabiHert/hadron/internal/store"
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
	DefaultModel *string   `json:"default_model,omitempty" jsonschema:"the Claude model cards use when neither the card nor its launch profile names one (opus, sonnet, haiku or a full model name); empty means claude's own default. Applies to sessions that start afterwards"`
	PlannerCwd   *string   `json:"planner_cwd,omitempty" jsonschema:"the folder the planner's session runs in: an absolute path to an existing folder, ~ is expanded. Empty means the root. Applies when the planner next starts"`
	Position     *int      `json:"position,omitempty" jsonschema:"move the workspace to this place in the tab bar, the order workspace_list returns: 0 is first; a number past the end means last. Archived workspaces are not counted"`
}

// WorkspaceDeleteIn is the input of workspace_delete.
type WorkspaceDeleteIn struct {
	WorkspaceID string `json:"workspace_id" jsonschema:"the workspace id"`
	DryRun      bool   `json:"dry_run,omitempty" jsonschema:"only count what would be deleted; nothing changes"`
}

// WorkspaceDeleteOut is the result of workspace_delete: the workspace and what went with it.
type WorkspaceDeleteOut struct {
	Workspace domain.Workspace `json:"workspace"`
	Deleted   bool             `json:"deleted"`
	// ActiveID is the active workspace afterwards (unchanged on a dry run).
	ActiveID string `json:"active_id"`
	store.Contents
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

	Register(r, Spec{Name: "workspace_update", Description: "Change a workspace's name, root folder, colour, enabled integrations, pinned filter fields, the planner's folder (planner_cwd, used from the planner's next start) or its place in the tab bar (position, 0 is first). Only the fields you pass change.",
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
			if in.DefaultModel != nil {
				if c.Actor.Kind == KindSession {
					return w, fmt.Errorf("%s may not change a model. Ask the planner with card_report", c.Actor)
				}
				w.DefaultModel = strings.TrimSpace(*in.DefaultModel)
				if err := sessions.CheckModelID(w.DefaultModel); err != nil {
					return w, err
				}
			}
			if in.PlannerCwd != nil {
				folder, err := resolveFolder("planner_cwd", *in.PlannerCwd)
				if err != nil {
					return w, err
				}
				w.PlannerCwd = storedFolder(w, folder)
			}
			if in.Position != nil {
				if w.Archived {
					return w, fmt.Errorf("workspace %q is archived, so it has no place in the tab bar. Restore it with workspace_archive (archived: false) first", w.Name)
				}
				if *in.Position < 0 {
					return w, errors.New("position must be 0 or more: 0 is the first workspace")
				}
			}
			if err := c.Store.Workspaces.Update(&w); err != nil {
				return w, err
			}
			if in.Position != nil {
				if err := c.Store.Workspaces.Move(w.ID, *in.Position); err != nil {
					return w, err
				}
				if w, err = c.Store.Workspaces.Get(w.ID); err != nil {
					return w, err
				}
			}
			c.RecordChange(w.ID, before, w)
			return w, nil
		})

	Register(r, Spec{Name: "workspace_archive", Description: "Archive a workspace so it leaves the tab bar, or restore it with archived: false. Its running sessions stop; nothing is deleted. The last workspace in the tab bar cannot be archived; archiving the active one makes the next one active.",
		Mutates: true, Entities: []string{"workspaces"}},
		func(ctx context.Context, c *Call, in WorkspaceArchiveIn) (domain.Workspace, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return w, err
			}
			before := w
			w.Archived = in.Archived == nil || *in.Archived
			if w.Archived && !before.Archived {
				if _, err := otherLive(c, w); err != nil {
					return w, err
				}
				// Chosen while w still holds its place in the tab bar.
				if err := leaveActive(c, w); err != nil {
					return w, err
				}
			}
			if err := c.Store.Workspaces.Update(&w); err != nil {
				return w, err
			}
			if w.Archived {
				stopWorkspaceSessions(c, w.ID)
			}
			c.RecordChange(w.ID, before, w)
			return w, nil
		})

	Register(r, Spec{Name: "workspace_delete", Description: "Delete a workspace and everything Hadron keeps for it: columns, cards, sessions, chats, questions, rules, dashboard. Its running sessions stop first. Files in its root folder and worktrees on disk are not touched. This cannot be undone; prefer workspace_archive. The last workspace in the tab bar cannot be deleted; deleting the active one makes the next one active. dry_run: true only counts what would go.",
		Mutates: true, Entities: []string{"workspaces", "columns", "cards"}},
		func(ctx context.Context, c *Call, in WorkspaceDeleteIn) (WorkspaceDeleteOut, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return WorkspaceDeleteOut{}, err
			}
			out := WorkspaceDeleteOut{Workspace: w, ActiveID: activeID(c)}
			if out.Contents, err = c.Store.Workspaces.Contents(w.ID); err != nil {
				return out, err
			}
			if !w.Archived {
				if _, err := otherLive(c, w); err != nil {
					return out, err
				}
			}
			if in.DryRun {
				return out, nil
			}
			stopWorkspaceSessions(c, w.ID)
			if err := leaveActive(c, w); err != nil {
				return out, err
			}
			if err := c.Store.Workspaces.Delete(w.ID); err != nil {
				return out, err
			}
			out.Deleted, out.ActiveID = true, activeID(c)
			c.RecordChange(w.ID, w, nil)
			return out, nil
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

func activeID(c *Call) string {
	var id string
	if v, err := c.Store.Settings.Get(SettingActiveWorkspace); err == nil {
		_ = json.Unmarshal(v, &id)
	}
	return id
}

// otherLive returns the workspace that takes w's place in the tab bar: the next one, else the one
// before. It fails when w is the only workspace there, since Hadron always shows one.
func otherLive(c *Call, w domain.Workspace) (domain.Workspace, error) {
	live, err := c.Store.Workspaces.List(false)
	if err != nil {
		return domain.Workspace{}, err
	}
	at := -1
	for i, x := range live {
		if x.ID == w.ID {
			at = i
		}
	}
	for _, i := range []int{at + 1, at - 1, 0} {
		if i >= 0 && i < len(live) && live[i].ID != w.ID {
			return live[i], nil
		}
	}
	return domain.Workspace{}, fmt.Errorf("workspace %q is the only one in the tab bar, and Hadron needs one. Create another workspace first", w.Name)
}

// leaveActive makes another workspace active when w is the active one and is going away. Call it
// before w is archived or deleted, so the next one is chosen from w's place.
func leaveActive(c *Call, w domain.Workspace) error {
	if activeID(c) != w.ID {
		return nil
	}
	next, err := otherLive(c, w)
	if err != nil {
		return c.Store.Settings.Set(SettingActiveWorkspace, json.RawMessage("null"))
	}
	if err := c.Store.Settings.Set(SettingActiveWorkspace, json.RawMessage(toJSON(next.ID))); err != nil {
		return err
	}
	autostartPlanner(c, next)
	return nil
}
