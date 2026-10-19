package lifecycle

import (
	"context"
	"reflect"
	"testing"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentengine "github.com/alfredxw/denova/agent/engine"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestNestedEventPreservesTypedChildLifecycleEnvelope(t *testing.T) {
	childPayloads := []agentevent.EventPayload{
		agentevent.InteractionRequested{Request: agentinteraction.InteractionRequest{ID: "ask-child", Kind: agentinteraction.InteractionAsk}},
		agentevent.ArtifactProduced{CallID: "child-call", Artifact: agentschema.ToolArtifactRef{ID: "artifact-child", Complete: true}},
		agentevent.CleanupCommitted{State: agenthistory.CleanupState{ID: "cleanup-child", Revision: 1}},
		agentevent.CompactionCommitted{State: agenthistory.CompactionState{ID: "compact-child", Revision: 1}},
		agentevent.RunSettled{Status: agentschema.ResultCompleted},
	}
	tool, err := agenttool.InferTool("delegate", "delegate", func(ctx context.Context, _ struct{}) (agentschema.ToolResult, error) {
		for index, payload := range childPayloads {
			if err := agentevent.ForwardNestedEvent(ctx, agentevent.NestedEvent{
				Source: agentevent.EventSource{
					Name: "researcher", Path: []string{"root", "researcher"},
					InvocationID: "task-session/child-run", InvocationType: "task",
				},
				SessionID: "task-session",
				Child:     agentevent.Event{Cursor: agentevent.Cursor(index + 1), RunID: "child-run", Payload: payload},
			}); err != nil {
				return agentschema.ToolResult{}, err
			}
		}
		return agentschema.TextToolResult("child result"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	definition := testToolDefinition(tool)
	definition.Descriptor.Source = agenttool.ToolSourceOther
	definition.Descriptor.Execution = agenttool.ToolExecutionChild
	model := &lifecycleModel{responses: []*agentschema.Message{
		agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "delegate-call", Type: "function", Function: agentschema.FunctionCall{Name: "delegate", Arguments: `{}`},
		}}),
		agentschema.AssistantMessage("root final", nil),
	}}
	owner, err := New(context.Background(), agentengine.Definition{
		Name: "root", Model: model,
		Tools: mustStaticToolsIdentified(t, agentschema.CapabilityIdentity{Kind: "test.nested.typed", Version: 1}, definition),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	run, err := owner.Run(context.Background(), agentschema.Text("delegate"))
	if err != nil {
		t.Fatal(err)
	}
	if result, waitErr := run.Wait(context.Background()); waitErr != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("result=%#v error=%v", result, waitErr)
	}
	var nested []agentevent.NestedEvent
	for event := range run.Events() {
		if payload, ok := event.Payload.(agentevent.NestedEvent); ok {
			if event.RunID != run.ID() {
				t.Fatalf("outer parent identity changed: %#v", event)
			}
			nested = append(nested, payload)
		}
	}
	if len(nested) != len(childPayloads) {
		t.Fatalf("nested events=%#v", nested)
	}
	for index, event := range nested {
		if event.SessionID != "task-session" || event.Source.InvocationID != "task-session/child-run" ||
			event.Source.InvocationType != "task" || event.Child.RunID != "child-run" ||
			event.Child.Cursor != agentevent.Cursor(index+1) || reflect.TypeOf(event.Child.Payload) != reflect.TypeOf(childPayloads[index]) {
			t.Fatalf("nested event %d = %#v", index, event)
		}
	}
}
