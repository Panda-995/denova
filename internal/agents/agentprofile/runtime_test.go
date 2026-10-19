package agentprofile

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"denova/config"

	agentcontext "github.com/alfredxw/denova/agent/context"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestContextSourceMapsStableSessionAndTurnBindings(t *testing.T) {
	cfg := activeWritingConfig(config.CustomAgentConfig{
		ID: "writer", Name: "Writer", Contract: config.AgentContractWritingPrimary,
		ContextBindings: []config.AgentContextBinding{
			{ID: "style", Purpose: "apply stable style", Slot: config.AgentContextSlotStable, Content: "restrained", HardLimitBytes: 1024},
			{ID: "state", Purpose: "apply session state", Slot: config.AgentContextSlotSession, Content: "chapter 3", HardLimitBytes: 1024},
			{ID: "request", Purpose: "apply turn note", Slot: config.AgentContextSlotTurn, Content: "short", HardLimitBytes: 1024},
			{ID: "draft", Purpose: "keep editable draft", Slot: config.AgentContextSlotStable, Content: "", HardLimitBytes: 1024},
		},
	})

	source := ContextSource(cfg, config.AgentKindIDE)
	if source == nil {
		t.Fatal("custom Agent context source is nil")
	}
	fragments, err := source.Materialize(context.Background(), agentcontext.ContextRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) != 3 {
		t.Fatalf("context fragments = %#v", fragments)
	}
	assertContextSlot(t, fragments[0], agentschema.ContextStablePrefix, agentschema.ContextLeadingMessage, "")
	assertContextSlot(t, fragments[1], agentschema.ContextSessionState, agentschema.ContextStateMessage, "custom-agent:writer:state")
	assertContextSlot(t, fragments[2], agentschema.ContextTurn, agentschema.ContextFinalUserPrefix, "")
}

func TestApplyToolGuidanceChangesOnlyDescription(t *testing.T) {
	original := &profileTestTool{info: &agentschema.ToolInfo{
		Name: "read", Desc: "Read canonical content.",
		ParamsOneOf: agentschema.NewParamsOneOfByParams(map[string]*agentschema.ParameterInfo{"path": {Type: "string"}}),
	}}
	definition := agenttool.ToolDefinition{
		Tool:                   original,
		Descriptor:             agenttool.ToolDescriptor{Source: agenttool.ToolSourceRead, Capability: config.AgentToolFilesystemRead},
		ImplementationIdentity: agentschema.CapabilityIdentity{Kind: "test.read", Version: 1, ConfigHash: "stable"},
	}
	cfg := activeWritingConfig(config.CustomAgentConfig{
		ID: "writer", Name: "Writer", Contract: config.AgentContractWritingPrimary,
		ToolGuidance: map[string]string{"read": "Read the outline before prose files."},
	})

	resolved, err := ApplyToolGuidance(context.Background(), cfg, config.AgentKindIDE, []agenttool.ToolDefinition{definition})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0].Tool == original {
		t.Fatalf("guided definitions = %#v", resolved)
	}
	if !reflect.DeepEqual(resolved[0].Descriptor, definition.Descriptor) || resolved[0].ImplementationIdentity != definition.ImplementationIdentity {
		t.Fatal("tool guidance changed the execution contract")
	}
	info, err := resolved[0].Tool.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "read" || info.ParamsOneOf != original.info.ParamsOneOf || !strings.Contains(info.Desc, "Read canonical content.") || !strings.Contains(info.Desc, "Read the outline before prose files.") {
		t.Fatalf("guided tool info = %#v", info)
	}
	if original.info.Desc != "Read canonical content." {
		t.Fatalf("canonical tool description was mutated: %q", original.info.Desc)
	}
}

func TestDelegationPolicyFiltersSelectionsIndependently(t *testing.T) {
	cfg := activeWritingConfig(config.CustomAgentConfig{
		ID: "writer", Name: "Writer", Contract: config.AgentContractWritingPrimary,
		Delegation: config.AgentDelegationPolicy{Mode: config.AgentDelegationSelected, AgentIDs: []string{"continuity", generalPurposeAgentID, "missing"}},
	})
	values := []config.SubAgentConfig{{ID: "continuity"}, {ID: "research"}}
	filtered := FilterSubAgents(cfg, config.AgentKindIDE, values)
	if len(filtered) != 1 || filtered[0].ID != "continuity" {
		t.Fatalf("filtered SubAgents = %#v", filtered)
	}
	if !IncludeGeneralSubAgent(cfg, config.AgentKindIDE, false) {
		t.Fatal("selected General SubAgent was rejected")
	}
}

func activeWritingConfig(definition config.CustomAgentConfig) *config.Config {
	return &config.Config{CustomAgents: []config.CustomAgentConfig{definition}, ActiveCustomAgentID: definition.ID}
}

func assertContextSlot(t *testing.T, fragment agentschema.ContextFragment, stability agentschema.ContextStability, placement agentschema.ContextPlacement, stateID string) {
	t.Helper()
	if fragment.Stability != stability || fragment.Placement != placement || fragment.StateID != stateID {
		t.Fatalf("context slot = %#v, want stability=%q placement=%q state_id=%q", fragment, stability, placement, stateID)
	}
}

type profileTestTool struct{ info *agentschema.ToolInfo }

func (tool *profileTestTool) Info(context.Context) (*agentschema.ToolInfo, error) {
	return tool.info, nil
}

func (tool *profileTestTool) Run(context.Context, string, ...agenttool.ToolOption) (agentschema.ToolResult, error) {
	return agentschema.TextToolResult("ok"), nil
}
