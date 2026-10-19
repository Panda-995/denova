package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

func TestDefinitionEngineResolvesPersistedAskBeforeMaterializedDefinitionCheck(t *testing.T) {
	ctx := context.Background()
	definition := Definition{
		Key: "persisted-ask-definition", Name: "persisted-ask",
		Model: &scriptedModel{}, ModelIdentity: agentschema.CapabilityIdentity{Kind: "test.model", Version: 1},
	}
	prepared, err := prepareDefinitionBase(ctx, definition, PrepareRequest{
		Session: agentschema.SessionView{Key: agentsession.Named("persisted-ask")},
		Run:     agentschema.RunView{ID: "operation-1", CommandID: "command-1", Cycle: 1},
		Input:   agentschema.Text("continue"), Reason: TurnReasonInteraction,
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(engineTranscript{
		Version: engineTranscriptVersion, DefinitionKey: prepared.definitionKey, BehaviorKey: prepared.behaviorKey,
		PreparationStage: enginePreparationMaterialized, MaterializedFingerprint: "previous-materialized-definition",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Presentation fields deliberately use the former localized shape. Answer
	// resolution only depends on the stable IDs and selection constraints that
	// were persisted when the interaction was emitted.
	request := json.RawMessage(`{
		"id":"ask-1","kind":"ask","allow_other":true,
		"questions":[{"id":"scope","prompt":{"zh":"选择范围","en":"Choose scope"},"options":[
			{"value":"small","label":{"zh":"小","en":"Small"},"recommended":true},
			{"value":"large","label":{"zh":"大","en":"Large"}}
		]}]
	}`)
	response := json.RawMessage(`{"answers":[{"question_id":"scope","values":["small"]}]}`)
	engine := &Engine{source: definition, key: agentsession.Named("persisted-ask")}
	encoded, err := engine.ResolveInteraction(ctx, InteractionResolveRequest{
		Snapshot: TurnSnapshot{
			CommandID: "command-1", OperationID: "operation-1", Cycle: 1,
			Input: UserInput{Text: "continue"}, State: state,
		},
		Interaction: InteractionSnapshot{
			ID: "ask-1", OperationID: "operation-1", Cycle: 1, Request: request,
		},
		Response: response,
	})
	if err != nil {
		t.Fatal(err)
	}
	var resolution agentinteraction.InteractionResolution
	if err := json.Unmarshal(encoded, &resolution); err != nil {
		t.Fatal(err)
	}
	if len(resolution.Answers) != 1 || resolution.Answers[0].QuestionID != "scope" ||
		len(resolution.Answers[0].Values) != 1 || resolution.Answers[0].Values[0] != "small" {
		t.Fatalf("resolution = %#v", resolution)
	}
}

func TestDefinitionEngineKeepsMaterializedDefinitionFenceForPermission(t *testing.T) {
	ctx := context.Background()
	definition := Definition{
		Key: "persisted-permission-definition", Name: "persisted-permission",
		Model: &scriptedModel{}, ModelIdentity: agentschema.CapabilityIdentity{Kind: "test.model", Version: 1},
	}
	prepared, err := prepareDefinitionBase(ctx, definition, PrepareRequest{
		Session: agentschema.SessionView{Key: agentsession.Named("persisted-permission")},
		Run:     agentschema.RunView{ID: "operation-1", CommandID: "command-1", Cycle: 1},
		Input:   agentschema.Text("continue"), Reason: TurnReasonInteraction,
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal(engineTranscript{
		Version: engineTranscriptVersion, DefinitionKey: prepared.definitionKey, BehaviorKey: prepared.behaviorKey,
		PreparationStage: enginePreparationMaterialized, MaterializedFingerprint: "previous-materialized-definition",
	})
	if err != nil {
		t.Fatal(err)
	}
	engine := &Engine{source: definition, key: agentsession.Named("persisted-permission")}
	_, err = engine.ResolveInteraction(ctx, InteractionResolveRequest{
		Snapshot: TurnSnapshot{
			CommandID: "command-1", OperationID: "operation-1", Cycle: 1,
			Input: UserInput{Text: "continue"}, State: state,
		},
		Interaction: InteractionSnapshot{
			ID: "permission-1", OperationID: "operation-1", Cycle: 1,
			Request: json.RawMessage(`{"id":"permission-1","kind":"permission"}`),
		},
		Response: json.RawMessage(`{"permission":"deny"}`),
	})
	if !errors.Is(err, agentschema.ErrDefinitionMismatch) || !strings.Contains(err.Error(), "materialized Definition changed") {
		t.Fatalf("error = %v, want materialized Definition mismatch", err)
	}
}
