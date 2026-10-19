package lifecycle

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

type permissionResolutionInvariantPolicy struct {
	decision agentpermission.PermissionResolvedDecision
	resolve  atomic.Int32
}

func (*permissionResolutionInvariantPolicy) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "permission.resolution-invariant-test", Version: 1}
}

func (*permissionResolutionInvariantPolicy) Evaluate(context.Context, agentpermission.PermissionRequest) (agentpermission.PermissionDecision, error) {
	return agentpermission.PermissionDecision{
		Kind: agentpermission.PermissionAsk,
		Reason: agentinteraction.LocalizedText{
			Chinese: "需要确认测试权限。",
			English: "Confirm the test permission.",
		},
		Details: agentpermission.PermissionDetails{CanRemember: true},
	}, nil
}

func (policy *permissionResolutionInvariantPolicy) Resolve(
	_ context.Context,
	_ agentpermission.PermissionResolveRequest,
) (agentpermission.PermissionResolvedDecision, error) {
	policy.resolve.Add(1)
	return policy.decision, nil
}

func TestPermissionResolutionRequiresExactAllowedAndRememberedDecision(t *testing.T) {
	tests := []struct {
		name     string
		choice   agentinteraction.PermissionChoice
		decision agentpermission.PermissionResolvedDecision
		want     string
	}{
		{
			name: "allow once cannot claim a persisted rule", choice: agentinteraction.PermissionAllowOnce,
			decision: agentpermission.PermissionResolvedDecision{Allowed: true, Remembered: true}, want: "allow-once",
		},
		{
			name: "remember must be durable before success", choice: agentinteraction.PermissionRemember,
			decision: agentpermission.PermissionResolvedDecision{Allowed: true}, want: "remembered rule was durable",
		},
		{
			name: "deny cannot authorize execution", choice: agentinteraction.PermissionDeny,
			decision: agentpermission.PermissionResolvedDecision{Allowed: true}, want: "deny decision",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := &permissionResolutionInvariantPolicy{decision: test.decision}
			run, toolRuns := startPermissionResolutionInvariantRun(t, policy)
			interactionID := waitPermissionInteractionID(t, run)
			err := run.Respond(context.Background(), interactionID, agentinteraction.InteractionResponse{Permission: test.choice})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Respond error=%v, want text %q", err, test.want)
			}
			if policy.resolve.Load() != 1 {
				t.Fatalf("Permission Resolve calls=%d, want 1", policy.resolve.Load())
			}
			if toolRuns.Load() != 0 {
				t.Fatalf("tool ran %d time(s) before a valid durable permission decision", toolRuns.Load())
			}
			if _, abortErr := run.Abort(context.Background(), agentevent.AbortRequest{Reason: "finish invariant test"}); abortErr != nil {
				t.Fatal(abortErr)
			}
			if result, waitErr := run.Wait(context.Background()); waitErr != nil || result.Status != agentschema.ResultAborted {
				t.Fatalf("aborted result=%#v error=%v", result, waitErr)
			}
		})
	}
}

func TestCancelledPermissionDoesNotInvokePolicyResolution(t *testing.T) {
	policy := &permissionResolutionInvariantPolicy{decision: agentpermission.PermissionResolvedDecision{Allowed: true, Remembered: true}}
	run, toolRuns := startPermissionResolutionInvariantRun(t, policy)
	interactionID := waitPermissionInteractionID(t, run)
	if err := run.Respond(context.Background(), interactionID, agentinteraction.InteractionResponse{Cancelled: true}); err != nil {
		t.Fatal(err)
	}
	if result, err := run.Wait(context.Background()); err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("cancelled permission result=%#v error=%v", result, err)
	}
	if policy.resolve.Load() != 0 {
		t.Fatalf("cancelled permission invoked policy Resolve %d time(s)", policy.resolve.Load())
	}
	if toolRuns.Load() != 0 {
		t.Fatalf("cancelled permission executed tool %d time(s)", toolRuns.Load())
	}
}

func startPermissionResolutionInvariantRun(
	t *testing.T,
	policy agentpermission.PermissionPolicy,
	middlewares ...agentmiddleware.Middleware,
) (*Run, *atomic.Int32) {
	t.Helper()
	var toolRuns atomic.Int32
	tool, err := agenttool.InferTool("permission_invariant", "exercise the permission fence", func(context.Context, struct {
		Value string `json:"value,omitempty"`
	}) (string, error) {
		toolRuns.Add(1)
		return "ran", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	model := &lifecycleModel{responses: []*agentschema.Message{
		agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "permission-invariant-call", Type: "function",
			Function: agentschema.FunctionCall{Name: "permission_invariant", Arguments: `{}`},
		}}),
		agentschema.AssistantMessage("done", nil),
	}}
	owner, err := New(context.Background(), agentengine.Definition{
		Model: model,
		Tools: mustStaticToolsIdentified(t, agentschema.CapabilityIdentity{Kind: "tools.permission-invariant", Version: 1}, agenttool.ToolDefinition{
			Tool: tool,
			Descriptor: agenttool.ToolDescriptor{
				Source: agenttool.ToolSourceWrite, Execution: agenttool.ToolExecutionWorkspaceExclusive,
				MutationScope: agenttool.ToolMutationWorkspace, PostCheck: agenttool.ToolPostCheckWorkspaceChange,
				Recovery: agenttool.ToolRecoveryIdempotent, ResultProjection: agentschema.ToolResultBoundedModelContext,
				ResultRetention: agentschema.ToolResultProtected, Steering: agenttool.SteeringFinishCurrent,
				MaxResultBytes: 64 << 10,
			},
		}),
		Permission:  policy,
		Middlewares: middlewares,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	session, err := owner.Session(context.Background(), agentsession.Named("permission-resolution-invariant-"+newPublicID("test")))
	if err != nil {
		t.Fatal(err)
	}
	run, err := session.Run(context.Background(), agentschema.Text("test permission resolution"))
	if err != nil {
		t.Fatal(err)
	}
	return run, &toolRuns
}

func waitPermissionInteractionID(t *testing.T, run *Run) string {
	t.Helper()
	for event := range run.Events() {
		if request, ok := event.Payload.(agentevent.InteractionRequested); ok {
			return request.Request.ID
		}
	}
	t.Fatal(errors.New("Permission Interaction was not requested"))
	return ""
}
