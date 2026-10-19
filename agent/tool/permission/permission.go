package permission

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type PermissionDecisionKind string

const (
	PermissionAllow PermissionDecisionKind = "allow"
	PermissionAsk   PermissionDecisionKind = "ask"
	PermissionBlock PermissionDecisionKind = "deny"
)

type PermissionRequest struct {
	Session    agentschema.SessionView
	Run        agentschema.RunView
	CallID     string
	Tool       string
	Arguments  json.RawMessage
	Descriptor agenttool.ToolDescriptor
	// Attachments are the application-owned file copies visible in the active
	// model transcript. Hosts may treat their exact paths as user-authorized
	// input without granting access to the surrounding user-data directory.
	Attachments []agentschema.Attachment
}

type PermissionDecision struct {
	Kind    PermissionDecisionKind
	Reason  agentinteraction.LocalizedText
	Details PermissionDetails
}

// PermissionDetails is policy-owned audit and presentation metadata. Agent
// supplies the exact tool, call, canonical arguments, and argument hash so a
// host never needs to reconstruct authorization evidence from product state.
type PermissionDetails struct {
	Mode               string `json:"mode,omitempty"`
	Command            string `json:"command,omitempty"`
	Details            string `json:"details,omitempty"`
	Cwd                string `json:"cwd,omitempty"`
	Risk               string `json:"risk,omitempty"`
	RuleID             string `json:"rule_id,omitempty"`
	CanRemember        bool   `json:"can_remember,omitempty"`
	RuleMatcherVersion int    `json:"rule_matcher_version,omitempty"`
	RuleMatchKey       string `json:"rule_match_key,omitempty"`
	RuleDisplayPattern string `json:"rule_display_pattern,omitempty"`
}

type PermissionResolveRequest struct {
	Request    PermissionRequest
	Resolution agentinteraction.InteractionResolution
}

type PermissionResolvedDecision struct {
	Allowed    bool
	Remembered bool
}

type PermissionPolicy interface {
	Identity() agentschema.CapabilityIdentity
	Evaluate(context.Context, PermissionRequest) (PermissionDecision, error)
	// Resolve must persist a remembered rule before returning success. Agent
	// commits InteractionResolved only after this method completes.
	Resolve(context.Context, PermissionResolveRequest) (PermissionResolvedDecision, error)
}

type SafeDefaultPermissionPolicy struct{}

func (SafeDefaultPermissionPolicy) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "permission.safe_default", Version: 1}
}

func (SafeDefaultPermissionPolicy) Evaluate(_ context.Context, request PermissionRequest) (PermissionDecision, error) {
	if request.Descriptor.MutationScope == agenttool.ToolMutationNone && request.Descriptor.Source != agenttool.ToolSourceShell ||
		request.Descriptor.MutationScope == agenttool.ToolMutationSession && request.Descriptor.Recovery == agenttool.ToolRecoveryIdempotent {
		return PermissionDecision{Kind: PermissionAllow}, nil
	}
	return PermissionDecision{
		Kind: PermissionAsk,
		Reason: agentinteraction.LocalizedText{
			Chinese: fmt.Sprintf("工具 %s 将访问或修改受保护资源。", request.Tool),
			English: fmt.Sprintf("Tool %s will access or modify protected resources.", request.Tool),
		},
	}, nil
}

func (SafeDefaultPermissionPolicy) Resolve(_ context.Context, request PermissionResolveRequest) (PermissionResolvedDecision, error) {
	switch request.Resolution.Permission {
	case agentinteraction.PermissionAllowOnce:
		return PermissionResolvedDecision{Allowed: true}, nil
	case agentinteraction.PermissionDeny:
		return PermissionResolvedDecision{}, nil
	case agentinteraction.PermissionRemember:
		return PermissionResolvedDecision{}, errors.New("safe default Permission Policy has no durable rule store")
	default:
		return PermissionResolvedDecision{}, errors.New("permission resolution is invalid")
	}
}

func EffectivePermissionPolicy(policy PermissionPolicy) PermissionPolicy {
	if policy == nil {
		return SafeDefaultPermissionPolicy{}
	}
	return policy
}

const permissionDetailsMaxBytes = 16 << 10

func PermissionPresentation(request PermissionRequest, decision PermissionDecision) agentinteraction.PermissionPresentation {
	details := decision.Details
	if strings.TrimSpace(details.Mode) == "" {
		details.Mode = "custom"
	}
	if strings.TrimSpace(details.Risk) == "" {
		details.Risk = "high"
	}
	if strings.TrimSpace(details.RuleID) == "" {
		details.RuleID = "permission_required"
	}
	if strings.TrimSpace(details.Command) == "" && strings.TrimSpace(details.Details) == "" {
		details.Details = truncatePermissionDetails(string(request.Arguments))
	}
	digest := sha256.Sum256(request.Arguments)
	return agentinteraction.PermissionPresentation{
		Tool: request.Tool, CallID: request.CallID,
		Arguments: append(json.RawMessage(nil), request.Arguments...), Reason: decision.Reason,
		Mode: details.Mode, Command: details.Command, Details: details.Details, Cwd: details.Cwd,
		Risk: details.Risk, RuleID: details.RuleID, ArgsHash: fmt.Sprintf("%x", digest[:]),
		CanRemember: details.CanRemember, RuleMatcherVersion: details.RuleMatcherVersion,
		RuleMatchKey: details.RuleMatchKey, RuleDisplayPattern: details.RuleDisplayPattern,
		Options: permissionOptions(details.CanRemember),
	}
}

func truncatePermissionDetails(value string) string {
	value = strings.TrimSpace(strings.ToValidUTF8(value, "\uFFFD"))
	if len(value) <= permissionDetailsMaxBytes {
		return value
	}
	const marker = "\n… [details truncated]"
	value = value[:permissionDetailsMaxBytes-len(marker)]
	for len(value) > 0 && !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + marker
}

func permissionOptions(canRemember bool) []agentinteraction.PermissionOption {
	options := []agentinteraction.PermissionOption{{
		Value: string(agentinteraction.PermissionAllowOnce),
		Label: agentinteraction.LocalizedText{Chinese: "仅允许这一次", English: "Allow once"},
		Description: agentinteraction.LocalizedText{
			Chinese: "仅执行当前调用，不保存权限规则。",
			English: "Execute only this call without saving a permission rule.",
		},
	}}
	if canRemember {
		options = append(options, agentinteraction.PermissionOption{
			Value: string(agentinteraction.PermissionRemember),
			Label: agentinteraction.LocalizedText{Chinese: "在当前工作区始终允许", English: "Always allow here"},
			Description: agentinteraction.LocalizedText{
				Chinese: "保存当前展示的命令匹配规则。",
				English: "Save the displayed command matching rule for this workspace.",
			},
		})
	}
	return append(options, agentinteraction.PermissionOption{
		Value: string(agentinteraction.PermissionDeny),
		Label: agentinteraction.LocalizedText{Chinese: "拒绝", English: "Deny"},
		Description: agentinteraction.LocalizedText{
			Chinese: "阻止这次调用，让 Agent 选择其他方案。",
			English: "Block this call and let the Agent choose another approach.",
		},
	})
}
