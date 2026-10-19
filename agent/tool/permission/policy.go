package permission

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type Mode string

const (
	ModeSafeDefault Mode = "safe_default"
	ModeReadOnly    Mode = "read_only"
	ModeCoding      Mode = "coding"
	ModeFullAccess  Mode = "full_access"
)

type Rule struct {
	Tool       string                      `json:"tool"`
	Source     agenttool.ToolSource        `json:"source"`
	Capability string                      `json:"capability,omitempty"`
	Scope      agenttool.ToolMutationScope `json:"scope"`
}

type RuleStore interface {
	Identity() agentschema.CapabilityIdentity
	Allowed(context.Context, Rule) (bool, error)
	Remember(context.Context, Rule) error
}

type memoryRules struct {
	mu      sync.RWMutex
	allowed map[Rule]struct{}
}

// MemoryRules is a process-local RuleStore for temporary Agents and tests. Its
// identity describes the in-memory storage semantics; rule contents remain
// dynamic and intentionally do not participate in Definition behavior identity.
// Durable Sessions should provide a durable RuleStore instead.
func MemoryRules() RuleStore { return &memoryRules{allowed: make(map[Rule]struct{})} }

func (*memoryRules) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "permission.rules.memory", Version: 1}
}

func (store *memoryRules) Allowed(_ context.Context, rule Rule) (bool, error) {
	store.mu.RLock()
	_, ok := store.allowed[rule]
	store.mu.RUnlock()
	return ok, nil
}

func (store *memoryRules) Remember(_ context.Context, rule Rule) error {
	store.mu.Lock()
	store.allowed[rule] = struct{}{}
	store.mu.Unlock()
	return nil
}

type policy struct {
	mode  Mode
	rules RuleStore
}

func SafeDefault() PermissionPolicy { return newPolicy(ModeSafeDefault, nil) }
func ReadOnly() PermissionPolicy    { return newPolicy(ModeReadOnly, nil) }
func Coding() PermissionPolicy      { return newPolicy(ModeCoding, nil) }
func FullAccess() PermissionPolicy  { return newPolicy(ModeFullAccess, nil) }

// SafeDefaultWithRules and CodingWithRules make the one durable RuleStore
// authority explicit. The prior variadic constructor silently ignored a
// second Store, which made composition order change authorization behavior.
func SafeDefaultWithRules(store RuleStore) (PermissionPolicy, error) {
	return newPolicyWithRules(ModeSafeDefault, store)
}

func CodingWithRules(store RuleStore) (PermissionPolicy, error) {
	return newPolicyWithRules(ModeCoding, store)
}

func newPolicyWithRules(mode Mode, store RuleStore) (PermissionPolicy, error) {
	if store == nil {
		return nil, errors.New("Permission RuleStore is nil")
	}
	identity := store.Identity()
	if strings.TrimSpace(identity.Kind) == "" || identity.Version == 0 {
		return nil, errors.New("Permission RuleStore requires a stable identity")
	}
	return newPolicy(mode, store), nil
}

func newPolicy(mode Mode, store RuleStore) PermissionPolicy {
	return &policy{mode: mode, rules: store}
}

func (policy *policy) Identity() agentschema.CapabilityIdentity {
	identity := struct {
		Mode  Mode
		Rules agentschema.CapabilityIdentity
	}{Mode: policy.mode}
	if policy.rules != nil {
		identity.Rules = policy.rules.Identity()
	}
	encoded, _ := json.Marshal(identity)
	digest := sha256.Sum256(encoded)
	hash := hex.EncodeToString(digest[:])
	return agentschema.CapabilityIdentity{Kind: "permission." + string(policy.mode), Version: 1, ConfigHash: hash}
}

func (policy *policy) Evaluate(ctx context.Context, request PermissionRequest) (PermissionDecision, error) {
	if policy == nil {
		return PermissionDecision{}, errors.New("Permission Policy is nil")
	}
	if criticalToolRequest(request) {
		return PermissionDecision{Kind: PermissionBlock, Reason: criticalReason()}, nil
	}
	rule := ruleFor(request)
	if policy.rules != nil {
		allowed, err := policy.rules.Allowed(ctx, rule)
		if err != nil {
			return PermissionDecision{}, fmt.Errorf("read Permission rule: %w", err)
		}
		if allowed {
			return PermissionDecision{Kind: PermissionAllow}, nil
		}
	}
	switch policy.mode {
	case ModeReadOnly:
		if readOnly(request) {
			return PermissionDecision{Kind: PermissionAllow}, nil
		}
		return PermissionDecision{Kind: PermissionBlock, Reason: protectedReason(request.Tool)}, nil
	case ModeFullAccess:
		return PermissionDecision{Kind: PermissionAllow}, nil
	case ModeSafeDefault, ModeCoding:
		if readOnly(request) || internalIdempotentState(request) {
			return PermissionDecision{Kind: PermissionAllow}, nil
		}
		return PermissionDecision{
			Kind: PermissionAsk, Reason: protectedReason(request.Tool),
			Details: PermissionDetails{
				Mode: string(policy.mode), Risk: "high", RuleID: "protected_resource",
				CanRemember: policy.rules != nil,
			},
		}, nil
	default:
		return PermissionDecision{}, fmt.Errorf("unsupported Permission mode %q", policy.mode)
	}
}

func (policy *policy) Resolve(ctx context.Context, request PermissionResolveRequest) (PermissionResolvedDecision, error) {
	switch request.Resolution.Permission {
	case agentinteraction.PermissionAllowOnce:
		return PermissionResolvedDecision{Allowed: true}, nil
	case agentinteraction.PermissionDeny:
		return PermissionResolvedDecision{}, nil
	case agentinteraction.PermissionRemember:
		if policy.rules == nil {
			return PermissionResolvedDecision{}, errors.New("Permission remember requires a RuleStore")
		}
		if err := policy.rules.Remember(ctx, ruleFor(request.Request)); err != nil {
			return PermissionResolvedDecision{}, fmt.Errorf("persist Permission rule: %w", err)
		}
		return PermissionResolvedDecision{Allowed: true, Remembered: true}, nil
	default:
		return PermissionResolvedDecision{}, errors.New("invalid Permission resolution")
	}
}

func readOnly(request PermissionRequest) bool {
	return request.Descriptor.MutationScope == agenttool.ToolMutationNone && request.Descriptor.Source != agenttool.ToolSourceShell
}

func internalIdempotentState(request PermissionRequest) bool {
	return request.Descriptor.MutationScope == agenttool.ToolMutationSession && request.Descriptor.Recovery == agenttool.ToolRecoveryIdempotent
}

func ruleFor(request PermissionRequest) Rule {
	return Rule{Tool: request.Tool, Source: request.Descriptor.Source, Capability: request.Descriptor.Capability, Scope: request.Descriptor.MutationScope}
}

func protectedReason(tool string) agentinteraction.LocalizedText {
	return agentinteraction.LocalizedText{
		Chinese: fmt.Sprintf("工具 %s 将访问或修改受保护资源。", tool),
		English: fmt.Sprintf("Tool %s will access or modify protected resources.", tool),
	}
}

func criticalReason() agentinteraction.LocalizedText {
	return agentinteraction.LocalizedText{Chinese: "该操作可能破坏系统或大范围删除数据，已被拒绝。", English: "This operation may damage the system or delete data broadly and was denied."}
}

func criticalToolRequest(request PermissionRequest) bool {
	if request.Descriptor.Source != agenttool.ToolSourceShell {
		return false
	}
	text := strings.ToLower(string(request.Arguments))
	text = strings.Join(strings.Fields(text), " ")
	critical := []string{
		"rm -rf /", "rm -fr /", "mkfs.", "format c:",
		"remove-item c:\\ -recurse", "remove-item / -recurse",
		"dd if=/dev/zero of=/dev/", "> /dev/sda", "shutdown -h", "reboot -f",
	}
	for _, pattern := range critical {
		if strings.Contains(text, pattern) {
			return true
		}
	}
	return false
}

var _ PermissionPolicy = (*policy)(nil)
