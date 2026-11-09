package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/GabiHert/hadron/internal/domain"
	"github.com/GabiHert/hadron/internal/store"
)

// LaunchProfileListIn is the input of launch_profile_list.
type LaunchProfileListIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
}

// LaunchProfileCreateIn is the input of launch_profile_create.
type LaunchProfileCreateIn struct {
	WorkspaceID string            `json:"workspace_id,omitempty" jsonschema:"the workspace; defaults to the active one"`
	Name        string            `json:"name" jsonschema:"the profile name, unique in the workspace"`
	Command     string            `json:"command,omitempty" jsonschema:"the program to run; defaults to claude. Hadron adds its prompt and MCP flags only when this is claude"`
	Args        []string          `json:"args,omitempty" jsonschema:"extra arguments, after Hadron's own"`
	FirstPrompt string            `json:"first_prompt,omitempty" jsonschema:"typed into the session once it is ready; may use {{nick}}, {{key}} and {{title}}. Empty means none"`
	Env         map[string]string `json:"env,omitempty" jsonschema:"extra environment variables"`
	Worktree    bool              `json:"worktree,omitempty" jsonschema:"run each card in its own git worktree at <root>/.hadron-worktrees/<nick> on branch hadron/<nick>, when the workspace root is a git repository"`
}

// LaunchProfileUpdateIn is the input of launch_profile_update. Only the fields you pass change.
type LaunchProfileUpdateIn struct {
	ProfileID   string             `json:"profile_id" jsonschema:"the launch profile id"`
	Name        *string            `json:"name,omitempty" jsonschema:"new name"`
	Command     *string            `json:"command,omitempty" jsonschema:"new command"`
	Args        *[]string          `json:"args,omitempty" jsonschema:"new arguments"`
	FirstPrompt *string            `json:"first_prompt,omitempty" jsonschema:"new first prompt"`
	Env         *map[string]string `json:"env,omitempty" jsonschema:"new environment"`
	Worktree    *bool              `json:"worktree,omitempty" jsonschema:"run in a git worktree of the card"`
}

// LaunchProfileIDIn is the input of launch_profile_delete.
type LaunchProfileIDIn struct {
	ProfileID string `json:"profile_id" jsonschema:"the launch profile id"`
}

func registerLaunchProfiles(r *Registry) {
	Register(r, Spec{Name: "launch_profile_list", Description: "List a workspace's launch profiles: saved recipes for how a session starts (command, arguments, first prompt, environment). Every workspace starts with brief, orchestrate and plain."},
		func(ctx context.Context, c *Call, in LaunchProfileListIn) ([]domain.LaunchProfile, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return nil, err
			}
			return c.Store.LaunchProfiles.List(w.ID)
		})

	Register(r, Spec{Name: "launch_profile_create", Description: "Create a launch profile.", Mutates: true, Entities: []string{"launch_profiles"}},
		func(ctx context.Context, c *Call, in LaunchProfileCreateIn) (domain.LaunchProfile, error) {
			w, err := c.Workspace(in.WorkspaceID)
			if err != nil {
				return domain.LaunchProfile{}, err
			}
			name, err := checkName("launch profile", in.Name)
			if err != nil {
				return domain.LaunchProfile{}, err
			}
			if err := profileNameFree(c, w.ID, name, ""); err != nil {
				return domain.LaunchProfile{}, err
			}
			p := domain.LaunchProfile{WorkspaceID: w.ID, Name: name, Command: strings.TrimSpace(in.Command), Args: in.Args,
				FirstPrompt: strings.TrimSpace(in.FirstPrompt), Env: in.Env, Worktree: in.Worktree}
			if err := c.Store.LaunchProfiles.Create(&p); err != nil {
				return p, err
			}
			c.RecordChange(w.ID, nil, p)
			return p, nil
		})

	Register(r, Spec{Name: "launch_profile_update", Description: "Change a launch profile. Only the fields you pass change.", Mutates: true, Entities: []string{"launch_profiles"}},
		func(ctx context.Context, c *Call, in LaunchProfileUpdateIn) (domain.LaunchProfile, error) {
			p, err := getProfile(c, in.ProfileID)
			if err != nil {
				return p, err
			}
			before := p
			if in.Name != nil {
				name, err := checkName("launch profile", *in.Name)
				if err != nil {
					return p, err
				}
				if err := profileNameFree(c, p.WorkspaceID, name, p.ID); err != nil {
					return p, err
				}
				p.Name = name
			}
			if in.Command != nil {
				if p.Command = strings.TrimSpace(*in.Command); p.Command == "" {
					p.Command = "claude"
				}
			}
			if in.Args != nil {
				p.Args = *in.Args
			}
			if in.FirstPrompt != nil {
				p.FirstPrompt = strings.TrimSpace(*in.FirstPrompt)
			}
			if in.Env != nil {
				p.Env = *in.Env
			}
			if in.Worktree != nil {
				p.Worktree = *in.Worktree
			}
			if err := c.Store.LaunchProfiles.Update(&p); err != nil {
				return p, err
			}
			c.RecordChange(p.WorkspaceID, before, p)
			return p, nil
		})

	Register(r, Spec{Name: "launch_profile_delete", Description: "Delete a launch profile. Cards that named it must pick another before their next start.", Mutates: true, Entities: []string{"launch_profiles"}},
		func(ctx context.Context, c *Call, in LaunchProfileIDIn) (domain.LaunchProfile, error) {
			p, err := getProfile(c, in.ProfileID)
			if err != nil {
				return p, err
			}
			if err := c.Store.LaunchProfiles.Delete(p.ID); err != nil {
				return p, err
			}
			c.RecordChange(p.WorkspaceID, p, nil)
			return p, nil
		})
}

func getProfile(c *Call, id string) (domain.LaunchProfile, error) {
	p, err := c.Store.LaunchProfiles.Get(id)
	if errors.Is(err, store.ErrNotFound) {
		return p, fmt.Errorf("launch profile %q does not exist. Call launch_profile_list for the ids", id)
	}
	if err == nil {
		c.SetWorkspace(p.WorkspaceID)
	}
	return p, err
}

func profileNameFree(c *Call, workspaceID, name, selfID string) error {
	list, err := c.Store.LaunchProfiles.List(workspaceID)
	if err != nil {
		return err
	}
	for _, p := range list {
		if p.ID != selfID && strings.EqualFold(p.Name, name) {
			return fmt.Errorf("a launch profile named %q already exists in this workspace. Pick another name", name)
		}
	}
	return nil
}
