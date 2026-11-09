package mcpserver

import (
	"github.com/GabiHert/hadron/internal/actions"
	"github.com/google/jsonschema-go/jsonschema"
)

// inputSchema derives a tool's input schema from the action's In struct. A type the inferrer
// cannot describe falls back to a free-form object, so the tool still lists and the registry
// validates the call.
func inputSchema(info actions.Info) any {
	s, err := jsonschema.ForType(info.In, nil)
	if err != nil || s == nil || s.Type != "object" {
		return map[string]any{"type": "object"}
	}
	// The registry rejects unknown fields itself, with a message that names the valid ones.
	s.AdditionalProperties = nil
	return s
}
