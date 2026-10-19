// Package agentprofile projects one active user-owned Agent definition onto
// runtime capabilities without teaching the core Agent engine product config.
package agentprofile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"denova/config"

	agentcontext "github.com/alfredxw/denova/agent/context"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

const generalPurposeAgentID = "general-purpose"

func ContextSource(cfg *config.Config, runtimeKind string) agentcontext.ContextSource {
	definition, ok := activeDefinition(cfg, runtimeKind)
	if !ok || len(definition.ContextBindings) == 0 {
		return nil
	}
	fragments := make([]agentschema.ContextFragment, 0, len(definition.ContextBindings))
	sourceIdentity := identity("denova.custom_agent.context", struct {
		ID       string
		Bindings []config.AgentContextBinding
	}{definition.ID, definition.ContextBindings})
	for _, binding := range definition.ContextBindings {
		if strings.TrimSpace(binding.Content) == "" {
			continue
		}
		fragment := agentschema.ContextFragment{
			Source: "Custom Agent", Purpose: binding.Purpose,
			Resource:  fmt.Sprintf("agent://%s/context/%s", definition.ID, binding.ID),
			Revision:  identity("denova.custom_agent.context.fragment", binding).ConfigHash,
			Rendering: agentschema.ContextRenderAttributed, Content: binding.Content, HardLimit: binding.HardLimitBytes,
		}
		switch binding.Slot {
		case config.AgentContextSlotSession:
			fragment.Stability = agentschema.ContextSessionState
			fragment.Placement = agentschema.ContextStateMessage
			fragment.StateID = "custom-agent:" + definition.ID + ":" + binding.ID
		case config.AgentContextSlotTurn:
			fragment.Stability = agentschema.ContextTurn
			fragment.Placement = agentschema.ContextFinalUserPrefix
		default:
			fragment.Stability = agentschema.ContextStablePrefix
			fragment.Placement = agentschema.ContextLeadingMessage
			fragment.Role = agentschema.System
		}
		fragments = append(fragments, fragment)
	}
	if len(fragments) == 0 {
		return nil
	}
	return staticContextSource{
		identity:  sourceIdentity,
		fragments: fragments,
	}
}

// ApplyToolGuidance wraps only provider-visible descriptions. Implementations,
// schemas, descriptors, permissions, and the enabled set stay unchanged.
func ApplyToolGuidance(ctx context.Context, cfg *config.Config, runtimeKind string, definitions []agenttool.ToolDefinition) ([]agenttool.ToolDefinition, error) {
	profile, ok := activeDefinition(cfg, runtimeKind)
	if !ok || len(profile.ToolGuidance) == 0 {
		return append([]agenttool.ToolDefinition(nil), definitions...), nil
	}
	limit := config.ResolveAgentContext(cfg, runtimeKind).MaxFragmentBytes
	result := make([]agenttool.ToolDefinition, len(definitions))
	for index, definition := range definitions {
		result[index] = definition
		if definition.Tool == nil {
			continue
		}
		info, err := definition.Tool.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("read tool description for custom Agent %s: %w", profile.ID, err)
		}
		if info == nil {
			continue
		}
		guidance := strings.TrimSpace(profile.ToolGuidance[info.Name])
		if guidance == "" {
			continue
		}
		if len(guidance) > limit {
			return nil, fmt.Errorf("custom Agent %s tool guidance for %s exceeds %d-byte context limit", profile.ID, info.Name, limit)
		}
		result[index].Tool = describedTool{Tool: definition.Tool, guidance: guidance}
	}
	return result, nil
}

func FilterSubAgents(cfg *config.Config, runtimeKind string, values []config.SubAgentConfig) []config.SubAgentConfig {
	profile, ok := activeDefinition(cfg, runtimeKind)
	if !ok || profile.Delegation.Mode == config.AgentDelegationCompatible {
		return append([]config.SubAgentConfig(nil), values...)
	}
	if profile.Delegation.Mode == config.AgentDelegationDisabled {
		return nil
	}
	allowed := make(map[string]bool, len(profile.Delegation.AgentIDs))
	for _, id := range profile.Delegation.AgentIDs {
		allowed[id] = true
	}
	result := make([]config.SubAgentConfig, 0, len(values))
	for _, value := range values {
		if allowed[config.NormalizeSubAgentID(value.ID)] {
			result = append(result, value)
		}
	}
	return result
}

// IncludeGeneralSubAgent resolves the shared fixed-Agent switch through the
// active custom Agent's delegation policy. Selected is exact and therefore
// may opt into General even when the fixed Agent switch is off.
func IncludeGeneralSubAgent(cfg *config.Config, runtimeKind string, configured bool) bool {
	profile, ok := activeDefinition(cfg, runtimeKind)
	if !ok || profile.Delegation.Mode == config.AgentDelegationCompatible {
		return configured
	}
	if profile.Delegation.Mode == config.AgentDelegationDisabled {
		return false
	}
	for _, id := range profile.Delegation.AgentIDs {
		if id == generalPurposeAgentID {
			return true
		}
	}
	return false
}

func activeDefinition(cfg *config.Config, runtimeKind string) (config.CustomAgentConfig, bool) {
	definition, ok := config.FindActiveCustomAgent(cfg)
	return definition, ok && config.CustomAgentRuntimeKind(definition) == strings.TrimSpace(runtimeKind)
}

type describedTool struct {
	agenttool.Tool
	guidance string
}

func (tool describedTool) Info(ctx context.Context) (*agentschema.ToolInfo, error) {
	info, err := tool.Tool.Info(ctx)
	if err != nil || info == nil {
		return info, err
	}
	cloned := *info
	canonical := strings.TrimSpace(info.Desc)
	if canonical == "" {
		cloned.Desc = "Agent-specific guidance:\n" + tool.guidance
	} else {
		cloned.Desc = canonical + "\n\nAgent-specific guidance:\n" + tool.guidance
	}
	return &cloned, nil
}

type staticContextSource struct {
	identity  agentschema.CapabilityIdentity
	fragments []agentschema.ContextFragment
}

func (source staticContextSource) Identity() agentschema.CapabilityIdentity { return source.identity }

func (source staticContextSource) Materialize(context.Context, agentcontext.ContextRequest) ([]agentschema.ContextFragment, error) {
	return append([]agentschema.ContextFragment(nil), source.fragments...), nil
}

func identity(kind string, configuration any) agentschema.CapabilityIdentity {
	encoded, _ := json.Marshal(configuration)
	digest := sha256.Sum256(encoded)
	return agentschema.CapabilityIdentity{Kind: kind, Version: 1, ConfigHash: hex.EncodeToString(digest[:])}
}
