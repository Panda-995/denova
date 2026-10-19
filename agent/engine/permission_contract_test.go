package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentcontext "github.com/alfredxw/denova/agent/context"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

type permissionContractTools struct{ definitions []agenttool.ToolDefinition }

func (*permissionContractTools) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.permission-contract-tools", Version: 1}
}

func (tools *permissionContractTools) PrepareTools(context.Context, agenttool.ToolRequest) ([]agenttool.ToolDefinition, error) {
	return append([]agenttool.ToolDefinition(nil), tools.definitions...), nil
}

type permissionContractContext struct{ unavailable bool }

func (*permissionContractContext) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.permission-contract-context", Version: 1}
}

func (source *permissionContractContext) Materialize(context.Context, agentcontext.ContextRequest) ([]agentschema.ContextFragment, error) {
	if source.unavailable {
		return nil, errors.New("referenced context is no longer readable")
	}
	return []agentschema.ContextFragment{{Source: "test", Purpose: "referenced document", Resource: "chapter.md", Revision: "1",
		Stability: agentschema.ContextTurn, Placement: agentschema.ContextFinalUserPrefix, Content: "Original chapter", HardLimit: 1024}}, nil
}

func TestPermissionContractPreservesAuthorizationFences(t *testing.T) {
	for _, scenario := range []string{"context_unavailable", "schema_changed", "descriptor_changed", "tool_removed", "policy_changed", "legacy_context_changed", "legacy_unchanged"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			tool, err := agenttool.InferTool("inspect", "Inspect a chapter", func(context.Context, struct{}) (string, error) {
				return "inspected", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			tools := &permissionContractTools{definitions: []agenttool.ToolDefinition{testToolDefinition(tool)}}
			contextSource := &permissionContractContext{}
			policy := &permissionResolutionInvariantPolicy{decision: agentpermission.PermissionResolvedDecision{Allowed: true}}
			definition := Definition{Key: "permission-contract", Name: "test", Model: &lifecycleModel{},
				Tools: tools, Context: contextSource, Permission: policy}
			key := agentsession.Named("permission-contract")
			prepared, err := prepareDefinition(ctx, definition, PrepareRequest{
				Session: agentschema.SessionView{Key: key}, Run: agentschema.RunView{ID: "run", CommandID: "command", Cycle: 1},
				Input: agentschema.Text("inspect"), Reason: TurnReasonInteraction,
			})
			if err != nil {
				t.Fatal(err)
			}
			prepared.materializedFingerprint, err = materializedDefinitionFingerprint(prepared)
			if err != nil {
				t.Fatal(err)
			}
			prepared.preparationStage = enginePreparationMaterialized
			state, err := encodeEngineTranscript(prepared, nil)
			if err != nil {
				t.Fatal(err)
			}
			presentation := agentpermission.PermissionPresentation(agentpermission.PermissionRequest{Tool: "inspect", CallID: "call", Arguments: json.RawMessage(`{}`)},
				agentpermission.PermissionDecision{Reason: agentinteraction.LocalizedText{Chinese: "需要确认", English: "Approval required"}})
			presentation.ToolDefinitionHash, err = agentschema.HashCanonical(prepared.toolSnapshots[0])
			if err != nil {
				t.Fatal(err)
			}
			wantError := true
			switch scenario {
			case "context_unavailable":
				contextSource.unavailable, wantError = true, false
			case "schema_changed":
				changed, err := agenttool.InferTool("inspect", "Inspect a chapter", func(context.Context, struct {
					Path string `json:"path"`
				}) (string, error) {
					return "changed", nil
				})
				if err != nil {
					t.Fatal(err)
				}
				tools.definitions[0].Tool = changed
			case "descriptor_changed":
				tools.definitions[0].Descriptor.MaxResultBytes++
			case "tool_removed":
				tools.definitions = nil
			case "policy_changed":
				definition.Permission = agentpermission.SafeDefaultPermissionPolicy{}
			case "legacy_context_changed":
				presentation.ToolDefinitionHash = ""
				contextSource.unavailable = true
			case "legacy_unchanged":
				presentation.ToolDefinitionHash, wantError = "", false
			}
			if strings.HasPrefix(scenario, "legacy_") {
				var legacy engineTranscript
				if err := json.Unmarshal(state, &legacy); err != nil {
					t.Fatal(err)
				}
				legacy.PreparedContext = nil
				state, err = json.Marshal(legacy)
				if err != nil {
					t.Fatal(err)
				}
			}
			encodedRequest, err := json.Marshal(agentinteraction.InteractionRequest{ID: "permission-call", Kind: agentinteraction.InteractionPermission, Permission: &presentation})
			if err != nil {
				t.Fatal(err)
			}
			engine := &Engine{source: definition, key: key}
			encoded, err := engine.ResolveInteraction(ctx, InteractionResolveRequest{
				Snapshot: TurnSnapshot{CommandID: "command", OperationID: "run", Cycle: 1,
					Input: UserInput{Text: "inspect"}, State: state},
				Interaction: InteractionSnapshot{ID: "permission-call", OperationID: "run", Cycle: 1, ToolCallID: "call", Request: encodedRequest},
				Response:    json.RawMessage(`{"permission":"allow_once"}`),
			})
			if wantError {
				if err == nil || scenario != "legacy_context_changed" && !errors.Is(err, agentschema.ErrDefinitionMismatch) {
					t.Fatalf("error=%v, want authorization rejection", err)
				}
				if policy.resolve.Load() != 0 {
					t.Fatal("changed authorization reached PermissionPolicy.Resolve")
				}
				return
			}
			var resolution agentinteraction.InteractionResolution
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &resolution); err != nil || resolution.Permission != agentinteraction.PermissionAllowOnce || policy.resolve.Load() != 1 {
				t.Fatalf("resolution=%#v error=%v policy calls=%d", resolution, err, policy.resolve.Load())
			}
		})
	}
}
