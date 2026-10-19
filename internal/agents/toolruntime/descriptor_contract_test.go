package toolruntime_test

import (
	"context"
	"strings"
	"testing"

	"denova/config"
	"denova/internal/agents/toolresult"
	producttools "denova/internal/agents/tools"

	agentschema "github.com/alfredxw/denova/agent/schema"
	sdktool "github.com/alfredxw/denova/agent/tool"
	publictools "github.com/alfredxw/denova/agent/tool/builtin"
)

func TestToolDescriptorDeclaresExecutionAndRecoveryPolicy(t *testing.T) {
	toolset := publictools.Todo()
	definitions, err := toolset.PrepareTools(context.Background(), sdktool.ToolRequest{})
	if err != nil || len(definitions) != 1 {
		t.Fatalf("prepare public todo definition=%#v err=%v", definitions, err)
	}
	definition := definitions[0]
	info, err := definition.Tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	descriptor := definition.Descriptor
	if info.Name != "todo" || descriptor.Capability != config.AgentToolTodo ||
		descriptor.Execution != sdktool.ToolExecutionSessionExclusive ||
		descriptor.Recovery != sdktool.ToolRecoveryIdempotent {
		t.Fatalf("todo definition = info=%+v descriptor=%+v", info, descriptor)
	}
	if descriptor.MutationScope != sdktool.ToolMutationSession || descriptor.PostCheck != sdktool.ToolPostCheckSessionState {
		t.Fatalf("todo must remain session-local: %+v", descriptor)
	}
}

func TestUnknownToolManifestIsConservativeWithoutNameInference(t *testing.T) {
	for _, name := range []string{"write_custom_plugin_state", "search_private_index", "read_side_effecting_api"} {
		descriptor := toolresult.UnknownManifest(name)
		if descriptor.Source != sdktool.ToolSourceOther || descriptor.Capability != "" ||
			descriptor.Execution != sdktool.ToolExecutionWorkspaceExclusive ||
			descriptor.MutationScope != sdktool.ToolMutationExternal ||
			descriptor.Recovery != sdktool.ToolRecoveryNonIdempotent {
			t.Fatalf("unknown %q manifest = %+v", name, descriptor)
		}
	}
}

func TestStructuredToolResultKeepsRecoveryContractOutOfModelText(t *testing.T) {
	descriptor := producttools.WorkspaceWriteDescriptor(sdktool.ToolSourceWrite, config.AgentToolWorkspaceWrite, sdktool.ToolRecoveryReconcilable)
	filtered := toolresult.FilterText("write", descriptor, `{"path":"chapters/ch01.md"}`, "ok", 0)
	if filtered.Result.ModelContent != "ok" {
		t.Fatalf("model content was polluted: %#v", filtered)
	}
	if strings.Contains(filtered.Result.ModelContent, "Denova tool result metadata") || strings.Contains(filtered.Result.ModelContent, "recovery:") {
		t.Fatalf("descriptor leaked into model text: %q", filtered.Result.ModelContent)
	}
	if filtered.Manifest.Execution != sdktool.ToolExecutionWorkspaceExclusive ||
		filtered.Manifest.Recovery != sdktool.ToolRecoveryReconcilable ||
		filtered.Manifest.ResultProjection != agentschema.ToolResultBoundedModelContext {
		t.Fatalf("durable manifest lost recovery contract: %+v", filtered.Manifest)
	}
	if filtered.Result.Metadata.Target != "chapters/ch01.md" || filtered.Result.Metadata.IdempotencyKey == "" {
		t.Fatalf("structured metadata missing target or idempotency key: %+v", filtered.Result.Metadata)
	}
}

func TestStructuredToolResultPreservesEndpointTargetWithoutPathArgument(t *testing.T) {
	descriptor := sdktool.ToolDescriptor{
		Source: sdktool.ToolSourceWeb, Capability: config.AgentToolBrowser,
		Execution: sdktool.ToolExecutionSessionExclusive, MutationScope: sdktool.ToolMutationExternal,
		PostCheck: sdktool.ToolPostCheckExternalReceipt, Recovery: sdktool.ToolRecoveryNonIdempotent,
		ResultProjection: agentschema.ToolResultBoundedModelContext, ResultRetention: agentschema.ToolResultDeferred,
		Steering:       sdktool.SteeringFinishCurrent,
		MaxResultBytes: toolresult.DefaultMaxBytes,
	}
	result := agentschema.TextToolResult(`{"schema":"browser.result.v1"}`)
	result.Metadata.Target = "https://example.com/docs"
	filtered := toolresult.FilterStructured(
		"browser", descriptor, `{"action":"run","tab":"docs","command":"observe"}`, result, 0,
	)
	if filtered.Result.Metadata.Target != "https://example.com/docs" {
		t.Fatalf("browser endpoint target = %q", filtered.Result.Metadata.Target)
	}
}

func TestRegistryRejectsUnclassifiedAndDuplicateTools(t *testing.T) {
	undeclared := sdktool.ToolDefinition{Tool: descriptorTestTool{name: "write_custom_plugin_state"}}
	if _, err := sdktool.NewRegistry(context.Background(), undeclared); err == nil || !strings.Contains(err.Error(), "descriptor") {
		t.Fatalf("expected unclassified definition error, got %v", err)
	}

	first, err := producttools.Define(descriptorTestTool{name: "read"}, producttools.BoundedReadDescriptor(sdktool.ToolSourceRead, config.AgentToolFilesystemRead))
	if err != nil {
		t.Fatal(err)
	}
	second, err := producttools.Define(descriptorTestTool{name: "read"}, producttools.BoundedReadDescriptor(sdktool.ToolSourceRead, config.AgentToolFilesystemRead))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sdktool.NewRegistry(context.Background(), first, second); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate tool error = %v", err)
	}
}

func TestRegistrySnapshotCarriesDescriptorWithoutToolInfoExtra(t *testing.T) {
	definition, err := producttools.Define(descriptorTestTool{name: "read"}, producttools.BoundedReadDescriptor(sdktool.ToolSourceRead, config.AgentToolFilesystemRead))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := sdktool.NewRegistry(context.Background(), definition)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok := registry.Snapshot("read")
	if !ok || snapshot.Info == nil || snapshot.Descriptor.Execution != sdktool.ToolExecutionParallelRead {
		t.Fatalf("snapshot = %#v ok=%t", snapshot, ok)
	}
	if snapshot.Info.Extra != nil {
		t.Fatalf("provider ToolInfo must not carry descriptor metadata: %#v", snapshot.Info.Extra)
	}
}

type descriptorTestTool struct{ name string }

func (tool descriptorTestTool) Info(context.Context) (*agentschema.ToolInfo, error) {
	return &agentschema.ToolInfo{Name: tool.name}, nil
}

func (descriptorTestTool) Run(context.Context, string, ...sdktool.ToolOption) (agentschema.ToolResult, error) {
	return agentschema.TextToolResult("ok"), nil
}
