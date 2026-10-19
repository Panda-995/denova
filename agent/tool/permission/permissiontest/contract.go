// Package permissiontest provides reusable behavioral checks for Permission
// Policy implementations.
package permissiontest

import (
	"context"
	"strings"
	"testing"

	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

type Factory func(testing.TB) agentpermission.PermissionPolicy

func RunPolicyContract(t *testing.T, factory Factory) {
	t.Helper()
	policy := factory(t)
	identity := policy.Identity()
	if strings.TrimSpace(identity.Kind) == "" || identity.Version == 0 {
		t.Fatalf("identity = %#v", identity)
	}
	requests := []agentpermission.PermissionRequest{
		{Tool: "read", Descriptor: agenttool.ToolDescriptor{Source: agenttool.ToolSourceRead, MutationScope: agenttool.ToolMutationNone}},
		{Tool: "write", Descriptor: agenttool.ToolDescriptor{Source: agenttool.ToolSourceWrite, MutationScope: agenttool.ToolMutationWorkspace}},
	}
	for _, request := range requests {
		decision, err := policy.Evaluate(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		switch decision.Kind {
		case agentpermission.PermissionAllow, agentpermission.PermissionAsk, agentpermission.PermissionBlock:
		default:
			t.Fatalf("decision = %#v", decision)
		}
	}
	allowed, err := policy.Resolve(context.Background(), agentpermission.PermissionResolveRequest{
		Request: requests[1], Resolution: agentinteraction.InteractionResolution{Permission: agentinteraction.PermissionAllowOnce},
	})
	if err != nil || !allowed.Allowed || allowed.Remembered {
		t.Fatalf("allow-once = %#v error = %v", allowed, err)
	}
	denied, err := policy.Resolve(context.Background(), agentpermission.PermissionResolveRequest{
		Request: requests[1], Resolution: agentinteraction.InteractionResolution{Permission: agentinteraction.PermissionDeny},
	})
	if err != nil || denied.Allowed {
		t.Fatalf("deny = %#v error = %v", denied, err)
	}
}
