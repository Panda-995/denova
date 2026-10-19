package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agenttoolresult "github.com/alfredxw/denova/agent/tool/result"
)

type resultProcessorProbe struct {
	mu       sync.Mutex
	requests []agenttoolresult.ToolResultProcessRequest
}

func (*resultProcessorProbe) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.tool_result_processor", Version: 1}
}

func (processor *resultProcessorProbe) Process(_ context.Context, request agenttoolresult.ToolResultProcessRequest) (agentschema.ToolResult, error) {
	processor.mu.Lock()
	processor.requests = append(processor.requests, request)
	processor.mu.Unlock()
	if request.Result.ModelContent != "raw endpoint result" {
		return request.Result, fmt.Errorf("processor observed %q", request.Result.ModelContent)
	}
	request.Result.ModelContent = "processed result"
	request.Result.DisplayContent = "processed display"
	return request.Result, nil
}

func (processor *resultProcessorProbe) snapshot() []agenttoolresult.ToolResultProcessRequest {
	processor.mu.Lock()
	defer processor.mu.Unlock()
	return append([]agenttoolresult.ToolResultProcessRequest(nil), processor.requests...)
}

func TestLoopRunsFixedResultProcessorBeforeEventsAndNextModelCall(t *testing.T) {
	model := &scriptedModel{responses: []scriptedModelResponse{
		{message: agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "provider-call", Type: "function",
			Function: agentschema.FunctionCall{Name: "read", Arguments: `{"path":"chapter.md"}`},
		}, {ID: "provider-call-2", Type: "function", Function: agentschema.FunctionCall{Name: "read", Arguments: `{"path":"chapter.md"}`}}})},
		{message: agentschema.AssistantMessage("done", nil)},
	}}
	processor := &resultProcessorProbe{}
	tool := &functionTool{name: "read", run: func(context.Context, string) (string, error) {
		return "raw endpoint result", nil
	}}
	loop, err := newModelToolLoop(context.Background(), loopConfig{
		Name: "fixed-result-stage", Model: model, Tools: []agenttool.ToolDefinition{testToolDefinition(tool)},
		ResultProcessor: processor,
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator := loop.Run(context.Background(), &loopInput{Messages: []*agentschema.Message{agentschema.UserMessage("go")}})
	var finished *agentschema.ToolResult
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Output != nil && event.Output.ToolExecution != nil &&
			event.Output.ToolExecution.Phase == toolExecutionFinished {
			finished = event.Output.ToolExecution.Result
		}
	}
	requests := processor.snapshot()
	if len(requests) != 2 || requests[0].ToolName != "read" ||
		requests[0].Arguments != `{"path":"chapter.md"}` || requests[0].BatchSize != 2 || requests[1].BatchSize != 2 {
		t.Fatalf("processor requests = %#v", requests)
	}
	if finished == nil || finished.ModelContent != "processed result" || finished.DisplayContent != "processed display" {
		t.Fatalf("finished result = %#v", finished)
	}
	inputs := model.capturedInputs()
	if len(inputs) != 2 || len(inputs[1]) == 0 {
		t.Fatalf("model inputs = %#v", inputs)
	}
	last := inputs[1][len(inputs[1])-1]
	if last.Role != agentschema.ToolRole || last.Content != "processed result" {
		t.Fatalf("next model tool result = %#v", last)
	}
}

type artifactStorageProbe struct{ began bool }

func (*artifactStorageProbe) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.artifact_storage", Version: 1}
}

func (storage *artifactStorageProbe) BeginToolArtifact(context.Context, agenttool.ToolArtifactRequest) (agenttool.ToolArtifactWriter, error) {
	storage.began = true
	return nil, fmt.Errorf("unused")
}

func (*artifactStorageProbe) VerifyToolArtifact(context.Context, agentschema.ToolArtifactRef, agenttool.ToolArtifactRequest) error {
	return nil
}

type artifactOverrideMiddleware struct {
	agentmiddleware.BaseMiddleware
	storage agenttool.ToolArtifactStorage
}

func (middleware *artifactOverrideMiddleware) BeforeAgent(
	ctx context.Context,
	runContext *agentmiddleware.RunContext,
) (context.Context, *agentmiddleware.RunContext, error) {
	ctx = agenttool.ContextWithToolArtifactBackend(ctx, middleware.storage)
	return ctx, runContext, nil
}

func (middleware *artifactOverrideMiddleware) WrapToolCall(
	_ context.Context,
	endpoint agentmiddleware.ToolCallEndpoint,
	_ *agentmiddleware.ToolContext,
) (agentmiddleware.ToolCallEndpoint, error) {
	return func(ctx context.Context, arguments string, options ...agenttool.ToolOption) (agentschema.ToolResult, error) {
		ctx = agenttool.ContextWithToolArtifactBackend(ctx, middleware.storage)
		return endpoint(ctx, arguments, options...)
	}, nil
}

type artifactAuthorityProcessor struct {
	expected agenttool.ToolArtifactStorage
	seen     bool
}

func (*artifactAuthorityProcessor) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.artifact-authority-processor", Version: 1}
}

func (processor *artifactAuthorityProcessor) Process(ctx context.Context, request agenttoolresult.ToolResultProcessRequest) (agentschema.ToolResult, error) {
	processor.seen = agenttool.ToolArtifactStoreFromContext(ctx) == processor.expected &&
		agenttool.ToolArtifactVerifierFromContext(ctx) == processor.expected
	if !processor.seen {
		return request.Result, errors.New("processor observed non-Definition artifact storage")
	}
	return request.Result, nil
}

func TestLoopInstallsDefinitionArtifactStorageForToolsAndProcessor(t *testing.T) {
	model := &scriptedModel{responses: []scriptedModelResponse{
		{message: agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "call", Type: "function", Function: agentschema.FunctionCall{Name: "probe", Arguments: `{}`}}})},
		{message: agentschema.AssistantMessage("done", nil)},
	}}
	storage := &artifactStorageProbe{}
	processor := &resultProcessorProbe{}
	tool := &functionTool{name: "probe", run: func(ctx context.Context, _ string) (string, error) {
		if agenttool.ToolArtifactStoreFromContext(ctx) != storage {
			return "", fmt.Errorf("tool artifact storage is not installed")
		}
		return "raw endpoint result", nil
	}}
	loop, err := newModelToolLoop(context.Background(), loopConfig{
		Name: "artifact-storage", Model: model, Tools: []agenttool.ToolDefinition{testToolDefinition(tool)},
		ResultProcessor: processor, Artifacts: storage,
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator := loop.Run(context.Background(), &loopInput{Messages: []*agentschema.Message{agentschema.UserMessage("go")}})
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil && !strings.Contains(event.Err.Error(), "scripted model") {
			t.Fatal(event.Err)
		}
	}
	if len(processor.snapshot()) != 1 {
		t.Fatalf("processor calls = %d", len(processor.snapshot()))
	}
}

func TestDefinitionArtifactStorageCannotBeOverriddenByMiddleware(t *testing.T) {
	model := &scriptedModel{responses: []scriptedModelResponse{
		{message: agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "call", Type: "function", Function: agentschema.FunctionCall{Name: "probe", Arguments: `{}`}}})},
		{message: agentschema.AssistantMessage("done", nil)},
	}}
	definitionStorage := &artifactStorageProbe{}
	middlewareStorage := &artifactStorageProbe{}
	processor := &artifactAuthorityProcessor{expected: definitionStorage}
	toolSawDefinition := false
	tool := &functionTool{name: "probe", run: func(ctx context.Context, _ string) (string, error) {
		toolSawDefinition = agenttool.ToolArtifactStoreFromContext(ctx) == definitionStorage &&
			agenttool.ToolArtifactVerifierFromContext(ctx) == definitionStorage
		return "raw endpoint result", nil
	}}
	loop, err := newModelToolLoop(context.Background(), loopConfig{
		Name: "artifact-authority", Model: model, Tools: []agenttool.ToolDefinition{testToolDefinition(tool)},
		ResultProcessor: processor, Artifacts: definitionStorage,
		Middlewares: []agentmiddleware.Middleware{&artifactOverrideMiddleware{storage: middlewareStorage}},
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator := loop.Run(context.Background(), &loopInput{Messages: []*agentschema.Message{agentschema.UserMessage("go")}})
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if !toolSawDefinition || !processor.seen {
		t.Fatalf("Definition artifact authority: tool=%t processor=%t", toolSawDefinition, processor.seen)
	}
}

type partialFailureProcessor struct{}

func (partialFailureProcessor) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.partial-failure-processor", Version: 1}
}

func (partialFailureProcessor) Process(_ context.Context, request agenttoolresult.ToolResultProcessRequest) (agentschema.ToolResult, error) {
	request.Result.ModelContent = "retained partial model output"
	request.Result.DisplayContent = "retained partial display output"
	request.Result.Details = []byte(`{"partial":true}`)
	request.Result.Metadata.Target = "chapters/partial.md"
	request.Result.Artifacts = []agentschema.ToolArtifactRef{{
		ID: "partial-artifact", Purpose: agentschema.ToolArtifactPurposeAttachment,
		ReadablePath: ".agent/artifacts/partial.log", ContentType: "text/plain", Complete: true,
	}}
	request.Result.Effects = []agentschema.Effect{{Kind: "test.partial-effect", Data: []byte(`{"retained":true}`)}}
	return request.Result, errors.New("projection backend unavailable")
}

func TestLoopRetainsPartialToolResultWhenProcessorFails(t *testing.T) {
	model := &scriptedModel{responses: []scriptedModelResponse{
		{message: agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "call", Type: "function", Function: agentschema.FunctionCall{Name: "probe", Arguments: `{}`}}})},
		{message: agentschema.AssistantMessage("done", nil)},
	}}
	tool := &functionTool{name: "probe", run: func(context.Context, string) (string, error) {
		return "raw endpoint result", nil
	}}
	loop, err := newModelToolLoop(context.Background(), loopConfig{
		Name: "partial-processor-failure", Model: model, Tools: []agenttool.ToolDefinition{testToolDefinition(tool)},
		ResultProcessor: partialFailureProcessor{},
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator := loop.Run(context.Background(), &loopInput{Messages: []*agentschema.Message{agentschema.UserMessage("go")}})
	var finished *agentschema.ToolResult
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
		if event.Output != nil && event.Output.ToolExecution != nil && event.Output.ToolExecution.Phase == toolExecutionFinished {
			finished = event.Output.ToolExecution.Result
		}
	}
	if finished == nil || finished.Status != agentschema.ToolResultError ||
		!strings.Contains(finished.ModelContent, "retained partial model output") ||
		!strings.Contains(finished.ModelContent, "projection backend unavailable") ||
		!strings.Contains(finished.DisplayContent, "retained partial display output") ||
		string(finished.Details) != `{"partial":true}` || finished.Metadata.Target != "chapters/partial.md" ||
		len(finished.Artifacts) != 1 || finished.Artifacts[0].ID != "partial-artifact" ||
		len(finished.Effects) != 1 || finished.Effects[0].Kind != "test.partial-effect" {
		t.Fatalf("retained processor failure = %#v", finished)
	}
	inputs := model.capturedInputs()
	if len(inputs) != 2 || len(inputs[1]) == 0 || !strings.Contains(inputs[1][len(inputs[1])-1].Content, "retained partial model output") ||
		!strings.Contains(inputs[1][len(inputs[1])-1].Content, "projection backend unavailable") {
		t.Fatalf("next model input lost partial processor output: %#v", inputs)
	}
}

type identityResultProcessor struct {
	identity agentschema.CapabilityIdentity
}

func (processor identityResultProcessor) Identity() agentschema.CapabilityIdentity {
	return processor.identity
}

func (identityResultProcessor) Process(_ context.Context, request agenttoolresult.ToolResultProcessRequest) (agentschema.ToolResult, error) {
	return request.Result, nil
}

func TestResultProcessorAndArtifactStorageIdentityAffectRestoreButNotStablePrefix(t *testing.T) {
	prepare := func(processorHash, storageHash, runID string) preparedDefinition {
		t.Helper()
		storage, err := agenttool.IdentifyToolArtifactStorage(&artifactStorageProbe{}, agentschema.CapabilityIdentity{
			Kind: "test.artifact-storage-identity", Version: 1, ConfigHash: storageHash,
		})
		if err != nil {
			t.Fatal(err)
		}
		prepared, err := prepareDefinition(context.Background(), Definition{
			Key: "result-identity", Model: &lifecycleModel{},
			ModelIdentity: agentschema.CapabilityIdentity{Kind: "model.result-identity", Version: 1},
			ResultProcessor: identityResultProcessor{identity: agentschema.CapabilityIdentity{
				Kind: "test.result-processor-identity", Version: 1, ConfigHash: processorHash,
			}},
			Artifacts: storage,
		}, PrepareRequest{
			Session: agentschema.SessionView{Key: agentsession.Named("result-identity-session"), Revision: 9},
			Run:     agentschema.RunView{ID: runID, CommandID: "command-" + runID, Cycle: 1}, Reason: TurnReasonStart,
		})
		if err != nil {
			t.Fatal(err)
		}
		return prepared
	}
	first := prepare("processor-one", "storage-one", "run-one")
	same := prepare("processor-one", "storage-one", "run-two")
	processorChanged := prepare("processor-two", "storage-one", "run-three")
	storageChanged := prepare("processor-one", "storage-two", "run-four")
	if first.behaviorKey != same.behaviorKey || first.prefixFingerprint != same.prefixFingerprint {
		t.Fatalf("run identity polluted result capability identity: first=%#v same=%#v", first, same)
	}
	if first.behaviorKey == processorChanged.behaviorKey || first.behaviorKey == storageChanged.behaviorKey {
		t.Fatal("result processor or artifact storage identity did not change restore semantics")
	}
	if first.prefixFingerprint != processorChanged.prefixFingerprint || first.prefixFingerprint != storageChanged.prefixFingerprint {
		t.Fatal("model-invisible result storage policy polluted the stable provider prefix")
	}
}
