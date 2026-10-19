package engine

import (
	"context"
	"sync/atomic"

	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
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
