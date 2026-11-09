package actions

import (
	"context"
	"fmt"

	"github.com/GabiHert/hadron/internal/domain"
)

// MCPInfoOut is what mcp_info reports: where the MCP server listens and how to connect to it.
type MCPInfoOut struct {
	Running   bool   `json:"running"`
	URL       string `json:"url"`
	Port      int    `json:"port"`
	Token     string `json:"token"`
	ToolCount int    `json:"tool_count"`
	// ConnectCommand adds Hadron to your own Claude Code. It has no session header, so the actor is "external".
	ConnectCommand string `json:"connect_command"`
}

// MCPCallsIn is the input of mcp_calls.
type MCPCallsIn struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"only this workspace's calls; empty lists every workspace"`
	Limit       int    `json:"limit,omitempty" jsonschema:"how many of the newest calls to return (default 100, at most 500)"`
}

func registerMCP(r *Registry) {
	Register(r, Spec{Name: "mcp_info", Description: "Report where the Hadron MCP server listens, its bearer token, how many tools it serves and the command that connects your own Claude Code to it."},
		func(ctx context.Context, c *Call, in struct{}) (MCPInfoOut, error) {
			out := MCPInfoOut{ToolCount: r.Len()}
			if c.Deps.MCP != nil {
				out = c.Deps.MCP()
				out.ToolCount = r.Len()
			}
			if out.Running {
				out.ConnectCommand = fmt.Sprintf(`claude mcp add --transport http hadron %s --header "Authorization: Bearer %s"`, out.URL, out.Token)
			}
			return out, nil
		})

	Register(r, Spec{Name: "mcp_calls", Description: "List the newest tool calls, newest first: who called (you, planner, session:<nick>, external, rule:<name>), which tool, the arguments, the outcome and the duration. Reads from the UI are not logged."},
		func(ctx context.Context, c *Call, in MCPCallsIn) ([]domain.MCPCall, error) {
			if in.Limit < 0 || in.Limit > 500 {
				return nil, fmt.Errorf("limit must be between 1 and 500, got %d", in.Limit)
			}
			return c.Store.MCPCalls.List(in.WorkspaceID, in.Limit)
		})
}
