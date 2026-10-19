package engine

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type toolNameMiddleware struct {
	agentmiddleware.BaseMiddleware
	mu    sync.Mutex
	names []string
}

func (middleware *toolNameMiddleware) WrapToolCall(
	_ context.Context,
	endpoint agentmiddleware.ToolCallEndpoint,
	toolContext *agentmiddleware.ToolContext,
) (agentmiddleware.ToolCallEndpoint, error) {
	return func(ctx context.Context, arguments string, options ...agenttool.ToolOption) (agentschema.ToolResult, error) {
		middleware.mu.Lock()
		middleware.names = append(middleware.names, toolContext.Name)
		middleware.mu.Unlock()
		return endpoint(ctx, arguments, options...)
	}, nil
}

func TestNestedToolsReenterExecutorWithoutAddingTranscriptMessages(t *testing.T) {
	model := &scriptedModel{responses: []scriptedModelResponse{
		{message: agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "outer-provider", Type: "function", Function: agentschema.FunctionCall{Name: "outer", Arguments: `{}`},
		}})},
		{message: agentschema.AssistantMessage("done", nil)},
	}}
	child := schedulerDefinition("child", schedulerReadDescriptor(agenttool.SteeringFinishCurrent), func(_ context.Context, arguments string) (agentschema.ToolResult, error) {
		return agentschema.TextToolResult(arguments), nil
	})
	outer := schedulerDefinition("outer", schedulerChildDescriptor(), func(ctx context.Context, _ string) (agentschema.ToolResult, error) {
		outcomes, err := agenttool.CallNestedTools(ctx, []agenttool.NestedToolCall{
			{Name: "child", Arguments: json.RawMessage(`{"value":1}`)},
			{Name: "missing", Arguments: json.RawMessage(`{}`)},
		})
		if err != nil {
			return agentschema.ToolResult{}, err
		}
		encoded, err := json.Marshal(outcomes)
		if err != nil {
			return agentschema.ToolResult{}, err
		}
		return agentschema.TextToolResult(string(encoded)), nil
	})
	middleware := &toolNameMiddleware{}
	native, err := newModelToolLoop(context.Background(), loopConfig{
		Name: "nested", Model: model, Tools: []agenttool.ToolDefinition{outer, child}, Middlewares: []agentmiddleware.Middleware{middleware},
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator := newLoopRunner(loopRunnerConfig{Agent: native}).Query(context.Background(), "go")
	toolMessages := 0
	var outerID string
	var children []toolExecutionEvent
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Output == nil {
			continue
		}
		if message := event.Output.MessageOutput; message != nil && message.Role == agentschema.ToolRole {
			toolMessages++
		}
		if execution := event.Output.ToolExecution; execution != nil {
			if execution.ToolName == "outer" {
				outerID = execution.ExecutionID
			} else if execution.ParentCallID != "" && execution.Phase == toolExecutionFinished {
				children = append(children, *execution)
			}
		}
	}
	if toolMessages != 1 {
		t.Fatalf("tool transcript messages = %d, want only outer result", toolMessages)
	}
	if len(children) != 2 || outerID == "" || children[0].ParentCallID != outerID || children[1].ParentCallID != outerID {
		t.Fatalf("outer=%q children=%+v", outerID, children)
	}
	if children[0].ExecutionID == children[1].ExecutionID || children[0].Index != 0 || children[1].Index != 1 {
		t.Fatalf("nested identities = %+v", children)
	}
	middleware.mu.Lock()
	names := append([]string(nil), middleware.names...)
	middleware.mu.Unlock()
	if len(names) != 2 || names[0] != "outer" || names[1] != "child" {
		t.Fatalf("middleware calls = %v", names)
	}
}
