package execution

import (
	"context"
)

type toolCallContextKey struct{}

type toolCallContext struct {
	providerCallID string
	executionID    string
	name           string
}

// ContextWithToolCall records provider transcript metadata for direct tool
// callers. Native Agent execution additionally binds a stable execution ID.
func ContextWithToolCall(ctx context.Context, callID, name string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, toolCallContextKey{}, toolCallContext{providerCallID: callID, name: name})
}

func ContextWithToolExecution(ctx context.Context, executionID, providerCallID, name string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, toolCallContextKey{}, toolCallContext{
		executionID: executionID, providerCallID: providerCallID, name: name,
	})
}

// ToolCallID returns the provider transcript call ID. Lifecycle and
// host correlation must use CurrentToolExecutionID instead.
func ToolCallID(ctx context.Context) string {
	metadata, _ := toolCallMetadata(ctx)
	return metadata.providerCallID
}

// ToolName returns the current tool name, or an empty string outside a call.
func ToolName(ctx context.Context) string {
	metadata, _ := toolCallMetadata(ctx)
	return metadata.name
}

func toolCallMetadata(ctx context.Context) (toolCallContext, bool) {
	if ctx == nil {
		return toolCallContext{}, false
	}
	metadata, ok := ctx.Value(toolCallContextKey{}).(toolCallContext)
	return metadata, ok
}
