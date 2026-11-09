package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/GabiHert/hadron/internal/run"
	"github.com/GabiHert/hadron/internal/store"
)

// writeMCPConfig writes <Home>/run/<session-id>.mcp.json (spec section 4): the Hadron MCP server,
// the bearer token, and the session header the server maps to the card and the actor. The server
// itself arrives with the MCP work; the file only names where it will listen.
func (s *Service) writeMCPConfig(tx *store.Store, sessionID string) (string, error) {
	token, err := run.EnsureMCPToken(filepath.Join(s.cfg.Home, "mcp-token"))
	if err != nil {
		return "", fmt.Errorf("could not create the MCP token: %w", err)
	}
	dir := filepath.Join(s.cfg.Home, "run")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	cfg := map[string]any{"mcpServers": map[string]any{"hadron": map[string]any{
		"type": "http",
		"url":  fmt.Sprintf("http://127.0.0.1:%d/mcp", s.port(tx)),
		"headers": map[string]string{
			"Authorization":    "Bearer " + token,
			"X-Hadron-Session": sessionID,
		},
	}}}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, sessionID+".mcp.json")
	if err := os.WriteFile(path, append(b, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
