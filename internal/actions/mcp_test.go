package actions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/GabiHert/hadron/internal/domain"
)

func TestMCPInfoReportsTheServerAndAConnectCommand(t *testing.T) {
	deps, _ := testDeps(t)
	deps.MCP = func() MCPInfoOut {
		return MCPInfoOut{Running: true, URL: "http://127.0.0.1:47820/mcp", Port: 47820, Token: "tok123"}
	}
	r := NewRegistry(deps)
	RegisterAll(r)
	h := &harness{t: t, reg: r}
	var info MCPInfoOut
	h.do("mcp_info", nil, &info)
	if info.ToolCount != r.Len() || info.ToolCount == 0 {
		t.Errorf("tool_count = %d, actions = %d", info.ToolCount, r.Len())
	}
	if !strings.Contains(info.ConnectCommand, "claude mcp add --transport http hadron http://127.0.0.1:47820/mcp") || !strings.Contains(info.ConnectCommand, "Bearer tok123") {
		t.Errorf("connect command = %q", info.ConnectCommand)
	}
}

func TestMCPCallsListsTheLogNewestFirstAndSkipsUIReads(t *testing.T) {
	h := newHarness(t)
	var w domain.Workspace
	h.do("workspace_create", m{"name": "Acme", "root": t.TempDir()}, &w)
	h.do("card_list", nil, nil) // a UI read: not logged
	planner := Actor{Kind: KindPlanner, WorkspaceID: w.ID}
	if _, err := h.reg.Invoke(context.Background(), planner, "card_list", `{}`); err != nil {
		t.Fatal(err)
	}
	var calls []domain.MCPCall
	h.do("mcp_calls", m{"workspace_id": w.ID, "limit": 5}, &calls)
	if len(calls) < 2 || calls[0].Tool != "card_list" || calls[0].Actor != "planner" || !calls[0].OK {
		b, _ := json.Marshal(calls)
		t.Fatalf("calls = %s", b)
	}
	for _, c := range calls {
		if c.Tool == "card_list" && c.Actor == "you" {
			t.Error("a UI read was logged")
		}
	}
	if err := h.try("mcp_calls", m{"limit": 9999}, nil); err == nil {
		t.Error("an absurd limit should be refused")
	}
}

func TestACallLogEntryEmitsChangedForMcpCalls(t *testing.T) {
	deps, rec := testDeps(t)
	r := NewRegistry(deps)
	RegisterAll(r)
	before := rec.count()
	_, _ = r.Invoke(context.Background(), Actor{Kind: KindExternal}, "app_status", `{}`)
	if rec.count() != before+1 {
		t.Fatalf("a logged read should emit one changed event, got %d", rec.count()-before)
	}
}
