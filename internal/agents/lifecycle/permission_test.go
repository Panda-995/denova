package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"denova/config"

	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

func TestDenovaPermissionPolicyPersistsRememberedRuleBeforeAllowing(t *testing.T) {
	var persisted config.AgentApprovalRule
	policy, err := NewPermissionPolicy(PermissionConfig{
		Mode: config.AgentApprovalAsk, ProjectID: "project-1", Workspace: t.TempDir(), GOOS: "linux",
		PersistRule: func(_ context.Context, rule config.AgentApprovalRule) error {
			persisted = rule
			return nil
		},
		clock: func() time.Time { return time.Unix(100, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}
	arguments := json.RawMessage(`{"command":"go test ./..."}`)
	request := agentpermission.PermissionRequest{
		Tool: "bash", Arguments: arguments,
		Descriptor: agenttool.ToolDescriptor{
			Source: agenttool.ToolSourceShell, MutationScope: agenttool.ToolMutationWorkspace,
			Recovery: agenttool.ToolRecoveryNonIdempotent,
		},
	}
	decision, err := policy.Evaluate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != agentpermission.PermissionAsk || decision.Reason.Chinese == "" || decision.Reason.English == "" {
		t.Fatalf("decision=%#v", decision)
	}
	resolved, err := policy.Resolve(context.Background(), agentpermission.PermissionResolveRequest{
		Request: request, Resolution: agentinteraction.InteractionResolution{Permission: agentinteraction.PermissionRemember},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Allowed || !resolved.Remembered || persisted.ID == "" || persisted.ToolName != "bash" || persisted.ApprovedArgsHash == "" {
		t.Fatalf("resolved=%#v persisted=%#v", resolved, persisted)
	}
}

func TestDenovaPermissionPolicyBlocksCriticalShellCommand(t *testing.T) {
	policy, err := NewPermissionPolicy(PermissionConfig{
		Mode: config.AgentApprovalFullAccess, Workspace: t.TempDir(), GOOS: "linux",
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := policy.Evaluate(context.Background(), agentpermission.PermissionRequest{
		Tool: "bash", Arguments: json.RawMessage(`{"command":"rm -rf /"}`),
		Descriptor: agenttool.ToolDescriptor{Source: agenttool.ToolSourceShell, MutationScope: agenttool.ToolMutationExternal},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != agentpermission.PermissionBlock {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestNonInteractivePermissionPolicyBlocksApprovalPrompt(t *testing.T) {
	policy, err := NewPermissionPolicy(PermissionConfig{
		Mode: config.AgentApprovalAsk, Workspace: t.TempDir(), GOOS: "linux", NonInteractive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := policy.Evaluate(context.Background(), permissionTestRequest(`{"command":"go test ./..."}`))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Kind != agentpermission.PermissionBlock || decision.Details.CanRemember ||
		decision.Reason.Chinese == "" || decision.Reason.English == "" {
		t.Fatalf("decision=%#v", decision)
	}
}

func TestDenovaPermissionRememberIsVisibleImmediatelyAndDoesNotChangeIdentity(t *testing.T) {
	workspace := t.TempDir()
	var durable []config.AgentApprovalRule
	load := func(context.Context) ([]config.AgentApprovalRule, error) {
		return append([]config.AgentApprovalRule(nil), durable...), nil
	}
	persist := func(_ context.Context, rule config.AgentApprovalRule) error {
		durable = config.NormalizeAgentApprovalRules(append(durable, rule))
		return nil
	}
	policy, err := NewPermissionPolicy(PermissionConfig{
		Mode: config.AgentApprovalAsk, ProjectID: "project-1", Workspace: workspace, GOOS: "linux",
		LoadRules: load, PersistRule: persist,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := permissionTestRequest(`{"command":"go test ./..."}`)
	before := policy.Identity()
	decision, err := policy.Evaluate(context.Background(), request)
	if err != nil || decision.Kind != agentpermission.PermissionAsk {
		t.Fatalf("initial decision=%#v err=%v", decision, err)
	}
	resolved, err := policy.Resolve(context.Background(), agentpermission.PermissionResolveRequest{
		Request: request, Resolution: agentinteraction.InteractionResolution{Permission: agentinteraction.PermissionRemember},
	})
	if err != nil || !resolved.Allowed || !resolved.Remembered {
		t.Fatalf("resolved=%#v err=%v", resolved, err)
	}
	decision, err = policy.Evaluate(context.Background(), request)
	if err != nil || decision.Kind != agentpermission.PermissionAllow {
		t.Fatalf("same-run decision=%#v err=%v", decision, err)
	}
	if after := policy.Identity(); after != before {
		t.Fatalf("remember changed Definition identity: before=%#v after=%#v", before, after)
	}

	reopened, err := NewPermissionPolicy(PermissionConfig{
		Mode: config.AgentApprovalAsk, ProjectID: "project-1", Workspace: workspace, GOOS: "linux",
		Rules: durable, LoadRules: load, PersistRule: persist,
	})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Identity() != before {
		t.Fatalf("persisted rules changed cold Definition identity: before=%#v reopened=%#v", before, reopened.Identity())
	}
	decision, err = reopened.Evaluate(context.Background(), request)
	if err != nil || decision.Kind != agentpermission.PermissionAllow {
		t.Fatalf("reopened decision=%#v err=%v", decision, err)
	}
}

func TestDenovaPermissionPersistFailureDoesNotAuthorizeProcessState(t *testing.T) {
	policy, err := NewPermissionPolicy(PermissionConfig{
		Mode: config.AgentApprovalAsk, ProjectID: "project-1", Workspace: t.TempDir(), GOOS: "linux",
		PersistRule: func(context.Context, config.AgentApprovalRule) error { return errors.New("disk unavailable") },
	})
	if err != nil {
		t.Fatal(err)
	}
	request := permissionTestRequest(`{"command":"go test ./..."}`)
	if _, err := policy.Resolve(context.Background(), agentpermission.PermissionResolveRequest{
		Request: request, Resolution: agentinteraction.InteractionResolution{Permission: agentinteraction.PermissionRemember},
	}); err == nil {
		t.Fatal("remember unexpectedly succeeded")
	}
	decision, err := policy.Evaluate(context.Background(), request)
	if err != nil || decision.Kind != agentpermission.PermissionAsk {
		t.Fatalf("decision after failed persist=%#v err=%v", decision, err)
	}
}

func TestDenovaPermissionRejectsConflictingRememberedRuleBeforePersistence(t *testing.T) {
	workspace := t.TempDir()
	request := permissionTestRequest(`{"command":"go test ./..."}`)
	var generated config.AgentApprovalRule
	seed, err := NewPermissionPolicy(PermissionConfig{
		Mode: config.AgentApprovalAsk, ProjectID: "project-1", Workspace: workspace, GOOS: "linux",
		PersistRule: func(_ context.Context, rule config.AgentApprovalRule) error {
			generated = rule
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Resolve(context.Background(), agentpermission.PermissionResolveRequest{
		Request: request, Resolution: agentinteraction.InteractionResolution{Permission: agentinteraction.PermissionRemember},
	}); err != nil {
		t.Fatal(err)
	}
	generated.ProjectID = "another-project"
	persisted := false
	policy, err := NewPermissionPolicy(PermissionConfig{
		Mode: config.AgentApprovalAsk, ProjectID: "project-1", Workspace: workspace, GOOS: "linux",
		Rules: []config.AgentApprovalRule{generated},
		PersistRule: func(context.Context, config.AgentApprovalRule) error {
			persisted = true
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := policy.Resolve(context.Background(), agentpermission.PermissionResolveRequest{
		Request: request, Resolution: agentinteraction.InteractionResolution{Permission: agentinteraction.PermissionRemember},
	}); err == nil {
		t.Fatal("conflicting deterministic rule id unexpectedly succeeded")
	}
	if persisted {
		t.Fatal("conflicting rule reached durable store")
	}
}

func TestTrajectoryResourcesAreLowRiskReadOnlyEvidence(t *testing.T) {
	policy, err := NewPermissionPolicy(PermissionConfig{
		Mode: config.AgentApprovalFullAccess, AgentKind: config.AgentKindGeneral,
	})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := policy.Evaluate(context.Background(), agentpermission.PermissionRequest{
		Tool: "read", Arguments: json.RawMessage(`{"path":"trajectory://index"}`),
	})
	if err != nil || decision.Kind != agentpermission.PermissionAllow {
		t.Fatalf("trajectory decision=%#v err=%v", decision, err)
	}
}

func permissionTestRequest(arguments string) agentpermission.PermissionRequest {
	return agentpermission.PermissionRequest{
		Tool: "bash", Arguments: json.RawMessage(arguments),
		Descriptor: agenttool.ToolDescriptor{
			Source: agenttool.ToolSourceShell, MutationScope: agenttool.ToolMutationWorkspace,
			Recovery: agenttool.ToolRecoveryNonIdempotent,
		},
	}
}
