package toolruntime

import (
	"context"
	"strings"
	"testing"

	"denova/config"
	producttools "denova/internal/agents/tools"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestPlanReadOnlyAccessFiltersModelToolSurface(t *testing.T) {
	definitions := []agenttool.ToolDefinition{
		accessPolicyDefinition(t, "read", producttools.BoundedReadDescriptor(agenttool.ToolSourceRead, config.AgentToolFilesystemRead)),
		accessPolicyDefinition(t, "send", accessPolicyDescriptor(agenttool.ToolExecutionChild, agenttool.ToolMutationNone, config.AgentToolDelegation)),
		accessPolicyDefinition(t, "await", accessPolicyDescriptor(agenttool.ToolExecutionInteractiveWait, agenttool.ToolMutationNone, config.AgentToolDelegation)),
		accessPolicyDefinition(t, "ask", accessPolicyDescriptor(agenttool.ToolExecutionInteractiveWait, agenttool.ToolMutationSession, config.AgentToolAsk)),
		accessPolicyDefinition(t, "todo", accessPolicyDescriptor(agenttool.ToolExecutionSessionExclusive, agenttool.ToolMutationSession, config.AgentToolTodo)),
		accessPolicyDefinition(t, "submit_domain_state", accessPolicyDescriptor(agenttool.ToolExecutionSessionExclusive, agenttool.ToolMutationSession, "domain_commit")),
		accessPolicyDefinition(t, "write", producttools.WorkspaceWriteDescriptor(agenttool.ToolSourceWrite, config.AgentToolWorkspaceWrite, agenttool.ToolRecoveryReconcilable)),
		accessPolicyDefinition(t, "apply_config", accessPolicyDescriptor(agenttool.ToolExecutionConfigExclusive, agenttool.ToolMutationConfig, config.AgentToolConfigApply)),
		accessPolicyDefinition(t, "browser", accessPolicyDescriptor(agenttool.ToolExecutionSessionExclusive, agenttool.ToolMutationExternal, config.AgentToolBrowser)),
	}
	original := &agentmiddleware.RunContext{Instruction: "keep", Tools: definitions}
	middleware := NewOrchestratorMiddleware(OrchestratorConfig{})

	_, filtered, err := middleware.BeforeAgent(
		ContextWithToolAccessMode(context.Background(), ToolAccessModePlanReadOnly),
		original,
	)
	if err != nil {
		t.Fatal(err)
	}
	if filtered == original {
		t.Fatal("access filtering must return a run snapshot instead of mutating the caller")
	}
	if len(original.Tools) != len(definitions) {
		t.Fatalf("original tool surface was mutated: got %d tools, want %d", len(original.Tools), len(definitions))
	}
	if got, want := accessPolicyToolNames(t, filtered.Tools), []string{"read", "send", "await", "ask", "todo"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("filtered tools = %v, want %v", got, want)
	}
}

func TestPlanReadOnlyAccessBlocksForgedMutationAtExecution(t *testing.T) {
	middleware := NewOrchestratorMiddleware(OrchestratorConfig{})
	planCtx := ContextWithToolAccessMode(context.Background(), ToolAccessModePlanReadOnly)

	for _, test := range []struct {
		name       string
		descriptor agenttool.ToolDescriptor
		allowed    bool
	}{
		{name: "read", descriptor: producttools.BoundedReadDescriptor(agenttool.ToolSourceRead, config.AgentToolFilesystemRead), allowed: true},
		{name: "ask", descriptor: accessPolicyDescriptor(agenttool.ToolExecutionInteractiveWait, agenttool.ToolMutationSession, config.AgentToolAsk), allowed: true},
		{name: "submit_domain_state", descriptor: accessPolicyDescriptor(agenttool.ToolExecutionSessionExclusive, agenttool.ToolMutationSession, "domain_commit")},
		{name: "write", descriptor: producttools.WorkspaceWriteDescriptor(agenttool.ToolSourceWrite, config.AgentToolWorkspaceWrite, agenttool.ToolRecoveryReconcilable)},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := accessPolicyDefinition(t, test.name, test.descriptor)
			info, err := definition.Tool.Info(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			called := false
			wrapped, err := middleware.WrapToolCall(context.Background(), func(context.Context, string, ...agenttool.ToolOption) (agentschema.ToolResult, error) {
				called = true
				return agentschema.TextToolResult("executed"), nil
			}, &agentmiddleware.ToolContext{
				Name: test.name,
				Definition: agenttool.ToolDefinitionSnapshot{
					Info: info, Descriptor: definition.Descriptor,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			result, err := wrapped(planCtx, `{}`)
			if err != nil {
				t.Fatal(err)
			}
			if test.allowed {
				if !called || result.ModelContent != "executed" {
					t.Fatalf("allowed tool did not execute: called=%t result=%#v", called, result)
				}
				return
			}
			if called {
				t.Fatal("blocked tool reached its endpoint")
			}
			if !strings.Contains(result.ModelContent, "Plan Mode is read-only") {
				t.Fatalf("blocked result must explain the policy in English: %q", result.ModelContent)
			}
		})
	}
}

type accessPolicyTool struct{ name string }

func (tool accessPolicyTool) Info(context.Context) (*agentschema.ToolInfo, error) {
	return &agentschema.ToolInfo{Name: tool.name}, nil
}

func (accessPolicyTool) Run(context.Context, string, ...agenttool.ToolOption) (agentschema.ToolResult, error) {
	return agentschema.TextToolResult("executed"), nil
}

func accessPolicyDefinition(t *testing.T, name string, descriptor agenttool.ToolDescriptor) agenttool.ToolDefinition {
	t.Helper()
	definition, err := producttools.Define(accessPolicyTool{name: name}, descriptor)
	if err != nil {
		t.Fatalf("define %s: %v", name, err)
	}
	return definition
}

func accessPolicyDescriptor(execution agenttool.ToolExecutionClass, mutation agenttool.ToolMutationScope, capability string) agenttool.ToolDescriptor {
	descriptor := agenttool.ToolDescriptor{
		Source: agenttool.ToolSourceOther, Capability: capability,
		Execution: execution, MutationScope: mutation,
		ResultProjection: agentschema.ToolResultBoundedModelContext,
		ResultRetention:  agentschema.ToolResultProtected,
		Steering:         agenttool.SteeringFinishCurrent,
		MaxResultBytes:   1024,
	}
	switch mutation {
	case agenttool.ToolMutationNone:
		descriptor.PostCheck = agenttool.ToolPostCheckNone
		descriptor.Recovery = agenttool.ToolRecoveryReadOnly
	case agenttool.ToolMutationSession:
		descriptor.PostCheck = agenttool.ToolPostCheckSessionState
		descriptor.Recovery = agenttool.ToolRecoveryReconcilable
	case agenttool.ToolMutationConfig:
		descriptor.PostCheck = agenttool.ToolPostCheckConfigRevision
		descriptor.Recovery = agenttool.ToolRecoveryReconcilable
	case agenttool.ToolMutationExternal:
		descriptor.PostCheck = agenttool.ToolPostCheckExternalReceipt
		descriptor.Recovery = agenttool.ToolRecoveryNonIdempotent
	}
	return descriptor
}

func accessPolicyToolNames(t *testing.T, definitions []agenttool.ToolDefinition) []string {
	t.Helper()
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		info, err := definition.Tool.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, info.Name)
	}
	return names
}
