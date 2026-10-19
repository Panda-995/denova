package engine

import (
	"context"
	"fmt"

	agenttool "github.com/alfredxw/denova/agent/tool"
)

// Tool approval revalidates executable contracts without reloading model
// context, which may include files changed by earlier calls in the same turn.
func materializeDefinitionTools(ctx context.Context, request PrepareRequest, prepared *preparedDefinition) error {
	var tools []agenttool.ToolDefinition
	var err error
	if prepared.definition.Tools != nil {
		tools, err = prepared.definition.Tools.PrepareTools(ctx, agenttool.ToolRequest{
			Session: request.Session, Run: request.Run, Input: request.Input,
		})
		if err != nil {
			return fmt.Errorf("prepare agent Toolset: %w", err)
		}
	}
	registry, err := agenttool.NewRegistry(ctx, tools...)
	if err != nil {
		return fmt.Errorf("prepare agent Toolset: %w", err)
	}
	prepared.tools = registry.Definitions()
	prepared.toolSnapshots = registry.Snapshots()
	return nil
}
