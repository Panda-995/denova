package toolruntime

import (
	"context"
	"errors"
	"strings"

	"denova/config"
	agenttool "denova/internal/agents/tool"
	"denova/internal/agents/toolresult"
	producttools "denova/internal/agents/tools"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentschema "github.com/alfredxw/denova/agent/schema"
	sdktool "github.com/alfredxw/denova/agent/tool"
)

type testTextToolEndpoint func(context.Context, string, ...sdktool.ToolOption) (string, error)

func wrapTextToolCallForTest(middleware agentmiddleware.Middleware, endpoint testTextToolEndpoint, toolCtx *agentmiddleware.ToolContext) (testTextToolEndpoint, error) {
	wrapped, err := middleware.WrapToolCall(
		context.Background(),
		func(ctx context.Context, arguments string, options ...sdktool.ToolOption) (agentschema.ToolResult, error) {
			content, runErr := endpoint(ctx, arguments, options...)
			return agentschema.TextToolResult(content), runErr
		},
		toolCtx,
	)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, arguments string, options ...sdktool.ToolOption) (string, error) {
		result, runErr := wrapped(ctx, arguments, options...)
		return result.ModelContent, runErr
	}, nil
}

func testToolContext(name, callID string) *agentmiddleware.ToolContext {
	var descriptor sdktool.ToolDescriptor
	switch name {
	case "read", "grep", "search_story_history":
		descriptor = producttools.BoundedReadDescriptor(sdktool.ToolSourceRead, config.AgentToolFilesystemRead)
		if name == "search_story_history" {
			descriptor = producttools.BoundedReadDescriptor(sdktool.ToolSourceHistory, "")
		}
	case "write", "edit":
		descriptor = producttools.WorkspaceWriteDescriptor(sdktool.ToolSourceWrite, config.AgentToolWorkspaceWrite, sdktool.ToolRecoveryReconcilable)
	case "bash", "pwsh":
		descriptor = sdktool.ToolDescriptor{
			Source: sdktool.ToolSourceShell, Capability: config.AgentToolShell,
			Execution: sdktool.ToolExecutionWorkspaceExclusive, MutationScope: sdktool.ToolMutationExternal,
			PostCheck: sdktool.ToolPostCheckExternalReceipt, Recovery: sdktool.ToolRecoveryNonIdempotent,
			ResultProjection: agentschema.ToolResultBoundedModelContext, ResultRetention: agentschema.ToolResultDeferred,
			Steering: sdktool.SteeringFinishCurrent, MaxResultBytes: toolresult.DefaultMaxBytes,
		}
	default:
		return &agentmiddleware.ToolContext{Name: name, ProviderCallID: callID}
	}
	return &agentmiddleware.ToolContext{
		Name: name, ProviderCallID: callID,
		Definition: sdktool.ToolDefinitionSnapshot{Info: &agentschema.ToolInfo{Name: name}, Descriptor: descriptor},
	}
}

func processorShellTestDecision() agenttool.Decision {
	return agenttool.Decision{
		ToolName: "bash", ProviderCallID: "call-shell", ExecutionID: "exec-shell",
		Descriptor: sdktool.ToolDescriptor{
			Source: sdktool.ToolSourceShell, Execution: sdktool.ToolExecutionWorkspaceExclusive,
			MutationScope: sdktool.ToolMutationExternal, PostCheck: sdktool.ToolPostCheckExternalReceipt,
			Recovery: sdktool.ToolRecoveryNonIdempotent, ResultProjection: agentschema.ToolResultBoundedModelContext,
			ResultRetention: agentschema.ToolResultProtected, Steering: sdktool.SteeringFinishCurrent, MaxResultBytes: 1024,
		},
	}
}

type processorArtifactStore struct {
	request         sdktool.ToolArtifactRequest
	content         strings.Builder
	beginErr        error
	beginCalls      int
	returnedPurpose agentschema.ToolArtifactPurpose
	verified        bool
}

func (store *processorArtifactStore) BeginToolArtifact(_ context.Context, request sdktool.ToolArtifactRequest) (sdktool.ToolArtifactWriter, error) {
	store.beginCalls++
	store.request = request
	if store.beginErr != nil {
		return nil, store.beginErr
	}
	store.content.Reset()
	return &processorArtifactWriter{store: store}, nil
}

func (store *processorArtifactStore) VerifyToolArtifact(_ context.Context, _ agentschema.ToolArtifactRef, _ sdktool.ToolArtifactRequest) error {
	if !store.verified {
		return errors.New("artifact was not issued by this store")
	}
	return nil
}

type processorArtifactWriter struct {
	store    *processorArtifactStore
	terminal bool
}

func (writer *processorArtifactWriter) Write(data []byte) (int, error) {
	if writer.terminal {
		return 0, errors.New("writer is closed")
	}
	return writer.store.content.Write(data)
}

func (writer *processorArtifactWriter) Commit() (agentschema.ToolArtifactRef, error) {
	writer.terminal = true
	purpose := writer.store.returnedPurpose
	if purpose == "" {
		purpose = writer.store.request.Purpose
	}
	return agentschema.ToolArtifactRef{
		ID: "artifact-call-42", Purpose: purpose,
		ReadablePath: ".denova/artifacts/session/call-42.log",
		ContentType:  "text/plain; charset=utf-8", EstimatedBytes: int64(writer.store.content.Len()),
		EstimatedTokens: (writer.store.content.Len() + 3) / 4, Complete: true,
	}, nil
}

func (writer *processorArtifactWriter) Abort() error {
	writer.terminal = true
	return nil
}
