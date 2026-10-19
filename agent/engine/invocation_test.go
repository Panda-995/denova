package engine

import (
	"context"
	"testing"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestToolExecutionIDsAreUniqueAcrossProviderCallIDReuse(t *testing.T) {
	model := &scriptedModel{responses: []scriptedModelResponse{
		{message: agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "provider-reused", Type: "function", Function: agentschema.FunctionCall{Name: "echo", Arguments: `{}`}}})},
		{message: agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "provider-reused", Type: "function", Function: agentschema.FunctionCall{Name: "echo", Arguments: `{}`}}})},
		{message: agentschema.AssistantMessage("done", nil)},
	}}
	tool := testToolDefinition(&functionTool{name: "echo", run: func(context.Context, string) (string, error) { return "ok", nil }})
	native, err := newModelToolLoop(context.Background(), loopConfig{Name: "root", Model: model, Tools: []agenttool.ToolDefinition{tool}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := agentexecution.ContextWithInvocationIdentity(context.Background(), agentexecution.InvocationIdentity{
		Scope: "workspace:one", OperationID: "operation-reuse", Cycle: 2,
	})
	iterator := newLoopRunner(loopRunnerConfig{Agent: native}).Query(ctx, "go")
	var started []toolExecutionEvent
	var results []loopMessage
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
		if execution := event.Output.ToolExecution; execution != nil && execution.Phase == toolExecutionStarted {
			started = append(started, *execution)
		}
		if message := event.Output.MessageOutput; message != nil && message.Role == agentschema.ToolRole {
			results = append(results, *message)
		}
	}
	if len(started) != 2 || len(results) != 2 {
		t.Fatalf("started=%#v results=%#v", started, results)
	}
	if started[0].ProviderCallID != "provider-reused" || started[1].ProviderCallID != "provider-reused" ||
		started[0].ExecutionID == started[1].ExecutionID || started[0].ExecutionID == "provider-reused" {
		t.Fatalf("execution identities = %#v", started)
	}
	for index := range results {
		if results[index].ProviderCallID != "provider-reused" || results[index].ExecutionID != started[index].ExecutionID ||
			results[index].Message == nil || results[index].Message.ToolCallID != "provider-reused" {
			t.Fatalf("result[%d] = %#v, started=%#v", index, results[index], started[index])
		}
	}
}
