package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	productsession "denova/internal/agents/session"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
	agenttool "github.com/alfredxw/denova/agent/tool"
	publictools "github.com/alfredxw/denova/agent/tool/builtin"
	"github.com/invopop/jsonschema"
)

type productCanonical struct{ product *productsession.Session }

func (productCanonical) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "denova.platform.canonical", Version: 1}
}

func commitIdentity(identity agentcanonical.CommitIdentity) productsession.DomainCommitIdentity {
	return productsession.DomainCommitIdentity{CommandID: identity.CommandID, OperationID: identity.RunID, Cycle: identity.Cycle}
}

func (c productCanonical) MaterializeInput(ctx context.Context, request agentcanonical.InputCommitRequest) (agentcanonical.CommitReceipt, error) {
	message := &agentschema.Message{Role: agentschema.User, Content: request.Input.Text}
	intent, err := productsession.NewDomainCommitIntent(commitIdentity(request.Identity), message, productsession.MessageMetadata{RunID: request.Identity.RunID})
	if err != nil {
		return agentcanonical.CommitReceipt{}, err
	}
	intent.Checkpoint = request.Checkpoint
	receipt, err := c.product.CommitDomainMessageContext(ctx, intent)
	return agentcanonical.CommitReceipt{Revision: fmt.Sprint(receipt.ContextRevision)}, err
}

func (c productCanonical) CommitOutput(ctx context.Context, request agentcanonical.OutputCommitRequest) (agentcanonical.OutputCommitReceipt, error) {
	intent, err := productsession.NewDomainCommitIntent(commitIdentity(request.Identity), &request.Message, productsession.MessageMetadata{RunID: request.Identity.RunID})
	if err != nil {
		return agentcanonical.OutputCommitReceipt{}, err
	}
	intent.Checkpoint = request.Checkpoint
	receipt, err := c.product.CommitDomainMessageContext(ctx, intent)
	return agentcanonical.OutputCommitReceipt{Revision: fmt.Sprint(receipt.ContextRevision)}, err
}

func (c productCanonical) CommitContext(ctx context.Context, request agentcanonical.ContextCommitRequest) (agentcanonical.CommitReceipt, error) {
	snapshot, err := c.product.SnapshotContext()
	if err != nil {
		return agentcanonical.CommitReceipt{}, err
	}
	messages := make([]*agentschema.Message, len(request.Messages))
	for index := range request.Messages {
		messages[index] = &request.Messages[index]
	}
	receipt, err := c.product.CommitContextBatch(ctx, snapshot.Cursor, commitIdentity(request.Identity), request.Sequence, messages, request.Checkpoint)
	return agentcanonical.CommitReceipt{Revision: fmt.Sprint(receipt.ContextRevision)}, err
}

type httpAgentTool struct {
	invoke func(context.Context, json.RawMessage) (ToolResult, error)
	info   *agentschema.ToolInfo
}

func (t *httpAgentTool) Info(context.Context) (*agentschema.ToolInfo, error) { return t.info, nil }
func (t *httpAgentTool) Run(ctx context.Context, arguments string, _ ...agenttool.ToolOption) (agentschema.ToolResult, error) {
	// Use Agent's argument repair before the same strict public execution seam.
	input, err := agenttool.NormalizeToolArguments(t.info, arguments)
	if err != nil {
		return agentschema.ToolResult{}, err
	}
	result, err := t.invoke(ctx, json.RawMessage(input))
	if err != nil {
		return agentschema.ToolResult{}, err
	}
	content := result.Content
	if len(result.Data) > 0 {
		content += "\n\nStructured result:\n" + string(result.Data)
	}
	return agentschema.TextToolResult(content), nil
}

// agentTool is the single HTTP adapter for host Agents and game-private NPCs.
// It preserves the existing Agent permission, scheduling and recovery contract.
func (m *Manager) agentTool(release Release, tool Tool, settings map[string]any, invoke func(context.Context, json.RawMessage) (ToolResult, error)) (agenttool.ToolDefinition, error) {
	var definition ToolDefinition
	if err := readJSON(filepath.Join(m.releasePath(release.Ref), filepath.FromSlash(tool.Definition)), &definition); err != nil {
		return agenttool.ToolDefinition{}, err
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		return agenttool.ToolDefinition{}, err
	}
	info := &agentschema.ToolInfo{Name: agentToolName(release.Ref.Package.Kind, tool.ID), Desc: definition.Description, ParamsOneOf: agentschema.NewParamsOneOfByJSONSchema(&schema)}
	descriptor := agenttool.ToolDescriptor{Source: agenttool.ToolSourceOther, Execution: agenttool.ToolExecutionParallelRead, MutationScope: agenttool.ToolMutationNone, PostCheck: agenttool.ToolPostCheckNone, Recovery: agenttool.ToolRecoveryReadOnly, ResultProjection: agentschema.ToolResultBoundedModelContext, ResultRetention: agentschema.ToolResultDeferred, Steering: agenttool.SteeringFinishCurrent, MaxResultBytes: MaxDefinitionBytes}
	switch definition.Effect {
	case "pure", "read":
	case "propose", "write":
		descriptor.Execution, descriptor.MutationScope, descriptor.Recovery = agenttool.ToolExecutionSessionExclusive, agenttool.ToolMutationExternal, agenttool.ToolRecoveryNonIdempotent
	default:
		return agenttool.ToolDefinition{}, failure("INVALID_ARGUMENT", "Unsupported tool effect %s", definition.Effect)
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return agenttool.ToolDefinition{}, err
	}
	return agenttool.ToolDefinition{Tool: &httpAgentTool{info: info, invoke: invoke}, Descriptor: descriptor, ImplementationIdentity: agentschema.CapabilityIdentity{Kind: "denova.platform.http_tool", Version: 1, ConfigHash: stableID(release.Digest, string(raw))}}, nil
}

func (s *AgentService) agentTools(runtime *Runtime, caller *activation, definition AgentDefinition) (agenttool.Toolset, error) {
	resolve := func(reference string) (*activation, string, error) {
		if strings.HasPrefix(reference, "local:") {
			if caller.release.Ref.Package.Kind != Game {
				return nil, "", failure("PERMISSION_DENIED", "Private reference outside a game")
			}
			return caller, strings.TrimPrefix(reference, "local:"), nil
		}
		providerID, id, external := strings.Cut(reference, "/")
		if !external {
			return caller, reference, nil
		}
		provider := runtime.providers[providerID]
		if provider == nil || !runtime.visible(caller, provider, id) {
			return nil, "", failure("PERMISSION_DENIED", "Capability %s is not selected", reference)
		}
		return provider, id, nil
	}
	refs := append([]string{}, definition.Tools...)
	for _, reference := range definition.Toolsets {
		provider, id, err := resolve(reference)
		if err != nil {
			return nil, err
		}
		found := false
		for _, set := range provider.release.Manifest.contributions().Toolsets {
			if set.ID == id {
				found = true
				for _, tool := range set.Tools {
					if provider.release.Ref.Package.Kind == Game {
						refs = append(refs, "local:"+tool)
					} else {
						refs = append(refs, provider.release.Manifest.ID+"/"+tool)
					}
				}
			}
		}
		if !found {
			return nil, failure("DEPENDENCY_UNAVAILABLE", "Selected capability %s is not a toolset", reference)
		}
	}
	definitions := []agenttool.ToolDefinition{}
	seen := map[string]bool{}
	for _, reference := range refs {
		providerID, id, external := strings.Cut(reference, "/")
		provider := caller
		if strings.HasPrefix(reference, "local:") {
			id = strings.TrimPrefix(reference, "local:")
		} else if external {
			provider = runtime.providers[providerID]
		} else {
			id = reference
		}
		if provider == nil {
			return nil, failure("DEPENDENCY_UNAVAILABLE", "Tool provider is unavailable")
		}
		key := string(provider.release.Ref.Package.Kind) + "/" + provider.release.Manifest.ID + "/" + id
		if seen[key] {
			continue
		}
		seen[key] = true
		found := false
		for _, tool := range provider.release.Manifest.contributions().Tools {
			if tool.ID != id {
				continue
			}
			found = true
			providerID := provider.release.Manifest.ID
			if provider.release.Ref.Package.Kind == Game {
				providerID = "local"
			}
			definition, err := s.manager.agentTool(provider.release, tool, provider.context.Settings, func(ctx context.Context, input json.RawMessage) (ToolResult, error) {
				return runtime.invoke(ctx, caller, providerID, tool.ID, input)
			})
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
		if !found {
			return nil, failure("DEPENDENCY_UNAVAILABLE", "Selected capability %s is not a tool", reference)
		}
	}
	selected, err := agenttool.StaticTools(definitions...)
	if err != nil {
		return nil, err
	}
	// Ordinary questions use the Native runtime's durable interaction protocol.
	// Consumers may answer them; permission decisions remain host-owned.
	return agenttool.CombineToolsets(publictools.Ask(), selected)
}
