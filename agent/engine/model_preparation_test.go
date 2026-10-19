package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type changingArtifactResolver struct {
	artifactStorageProbe
	calls int
}

func (resolver *changingArtifactResolver) ResolveToolArtifactPath(context.Context, string) (string, error) {
	resolver.calls++
	return fmt.Sprintf("/runtime/artifacts/projection-%d.txt", resolver.calls), nil
}

type preparationStateObserver struct {
	agentmiddleware.BaseMiddleware
	messages []*agentschema.Message
}

func (observer *preparationStateObserver) AfterAgent(ctx context.Context, state *agentmiddleware.RunState) (context.Context, error) {
	observer.messages = agentschema.CloneMessages(state.Messages)
	return ctx, nil
}

func TestPreparedCompactionFreezesArtifactPathsAndKeepsPortableLoopState(t *testing.T) {
	resolver := &changingArtifactResolver{}
	observer := &preparationStateObserver{}
	answer := agentschema.AssistantMessage("done", nil)
	answer.ResponseMeta = &agentschema.ResponseMeta{Usage: &agentschema.TokenUsage{PromptTokens: 200}}
	model := &scriptedModel{responses: []scriptedModelResponse{{message: answer}}}
	identity := agentschema.CapabilityIdentity{Kind: "test.artifact-projection", Version: 1}
	messages := []*agentschema.Message{
		agentschema.UserMessage("Continue from the checkpoint"),
		agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "saved", Type: "function", Function: agentschema.FunctionCall{Name: "read", Arguments: `{}`}}}),
		agentschema.ToolMessage(agentschema.TextToolResult("Read saved.txt"), "saved", agentschema.WithToolName("read")),
	}
	messages[2].ToolResult.Artifacts = []agentschema.ToolArtifactRef{{ID: "saved", ReadablePath: "saved.txt"}}
	var validated []*agentschema.Message
	loop, err := newModelToolLoop(context.Background(), loopConfig{
		Model: model, Artifacts: resolver, Middlewares: []agentmiddleware.Middleware{observer},
		ModelIdentity: identity,
		modelCallGate: func(_ context.Context, _ *modelCall, metadata *modelStepContext) (*preparedModelCall, error) {
			replacement, err := metadata.prepareCompaction(messages, 0)
			if err == nil {
				validated = replacement.call.Snapshot().Messages()
			}
			return replacement, err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator := loop.Run(context.Background(), &loopInput{Messages: messages})
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	inputs := model.capturedInputs()
	if len(inputs) != 1 || !reflect.DeepEqual(inputs[0], validated) || resolver.calls != 1 {
		t.Fatalf("validated provider projection was rebuilt: inputs=%d resolutions=%d", len(inputs), resolver.calls)
	}
	if got := validated[2].ToolResult.Artifacts[0].ReadablePath; got != "/runtime/artifacts/projection-1.txt" {
		t.Fatalf("provider artifact path=%q", got)
	}
	if observer.messages[2].Content != "Read saved.txt" || observer.messages[2].ToolResult.Artifacts[0].ReadablePath != "saved.txt" {
		t.Fatal("runtime artifact paths entered portable loop state")
	}
	meta := observer.messages[len(observer.messages)-1].ResponseMeta
	if meta == nil || meta.InputEstimate == nil || meta.InputEstimate.Version != agentmodel.InputEstimateVersion || meta.InputEstimate.Model != identity || meta.InputEstimate.Tokens != agentmodel.EstimateRequestTextTokens(validated, []*agentschema.ToolInfo{}) {
		t.Fatalf("input estimate did not use the frozen provider projection: %+v", meta)
	}
}

type rejectedModelCall struct {
	agentmiddleware.BaseMiddleware
	err error
}

func (middleware rejectedModelCall) BeforeModelCall(ctx context.Context, _ *agentmodel.ModelCall, _ *agentmiddleware.ModelContext) (context.Context, *agentmodel.ModelCall, error) {
	return ctx, nil, middleware.err
}

func TestModelPreparationPreservesMiddlewareErrorWithoutCallingProvider(t *testing.T) {
	rejected := errors.New("request rejected by host middleware")
	model := &scriptedModel{}
	loop, err := newModelToolLoop(t.Context(), loopConfig{Model: model, Middlewares: []agentmiddleware.Middleware{&rejectedModelCall{err: rejected}}})
	if err != nil {
		t.Fatal(err)
	}
	iterator := loop.Run(t.Context(), &loopInput{Messages: []*agentschema.Message{agentschema.UserMessage("work")}})
	var failure error
	for {
		event, more := iterator.Next()
		if !more {
			break
		}
		if event.Err != nil {
			failure = event.Err
		}
	}
	if !errors.Is(failure, rejected) {
		t.Fatalf("middleware rejection was replaced: %v", failure)
	}
	if len(model.capturedInputs()) != 0 {
		t.Fatal("rejected request reached the provider")
	}
}
