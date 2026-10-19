package engine

import (
	"encoding/json"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// persistedTool confirms individual effects without making a second message
// history. The canonical transcript still owns the complete call/result batch.
type PersistedTool struct {
	RunID          string                    `json:"run_id"`
	Cycle          int                       `json:"cycle"`
	CallID         string                    `json:"call_id"`
	ProviderCallID string                    `json:"provider_call_id"`
	Name           string                    `json:"name"`
	Index          int                       `json:"index"`
	Arguments      json.RawMessage           `json:"arguments"`
	Descriptor     *agenttool.ToolDescriptor `json:"descriptor,omitempty"`
	Started        bool                      `json:"started"`
	Result         *agentschema.ToolResult   `json:"result,omitempty"`
	Source         string                    `json:"source,omitempty"`
}
