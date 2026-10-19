package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	agentcontext "github.com/alfredxw/denova/agent/context"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type mutablePreparedContext struct {
	body        string
	unavailable bool
	calls       int
}

func (*mutablePreparedContext) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "test.prepared-context", Version: 1}
}

func (source *mutablePreparedContext) Materialize(context.Context, agentcontext.ContextRequest) ([]agentschema.ContextFragment, error) {
	source.calls++
	if source.unavailable {
		return nil, errors.New("source unavailable")
	}
	return []agentschema.ContextFragment{{Source: "project", Purpose: "accepted project instruction", Resource: "AGENTS.md",
		Stability: agentschema.ContextStablePrefix, Placement: agentschema.ContextLeadingMessage, Content: source.body, HardLimit: 4096}}, nil
}

func TestPreparedContextRematerializationUpdatesRecoveryIdentity(t *testing.T) {
	source := &mutablePreparedContext{body: "Original instruction"}
	request := PrepareRequest{Session: agentschema.SessionView{Key: agentsession.Named("refresh")}, Input: agentschema.Text("work"), Reason: TurnReasonStart}
	prepared, err := prepareDefinition(t.Context(), Definition{Name: "test", Model: &lifecycleModel{}, Context: source}, request)
	if err != nil {
		t.Fatal(err)
	}
	prepared.preparationStage = enginePreparationMaterialized
	prepared.materializedFingerprint, err = materializedDefinitionFingerprint(prepared)
	if err != nil {
		t.Fatal(err)
	}
	previous := prepared.materializedFingerprint
	source.body = "Accepted post-compaction instruction"
	if err := rematerializeDefinitionContext(t.Context(), request, &prepared); err != nil {
		t.Fatal(err)
	}
	raw, err := encodeEngineTranscript(prepared, nil)
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeEngineTranscript(raw)
	if err != nil {
		t.Fatal(err)
	}
	if previous == state.MaterializedFingerprint || state.PreparedContext == nil || !strings.Contains(state.PreparedContext.Fragments[0].Content, "post-compaction") {
		t.Fatalf("context refresh did not update recovery baseline: %+v", state)
	}
	restored := prepared
	source.unavailable = true
	engine := &Engine{}
	if err := engine.materializeCycleCapabilities(t.Context(), request, TurnSnapshot{}, state.PreparedContext, &restored); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := materializedDefinitionFingerprint(restored)
	if err != nil || fingerprint != state.MaterializedFingerprint {
		t.Fatalf("refreshed context could not restore: %s %v", fingerprint, err)
	}
}

func TestPreparedContextRepeatedSuspendPreservesActiveInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	model := &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("done", nil)}}
	contextSource := &mutablePreparedContext{body: "Accepted project instruction"}
	definition := Definition{Name: "test", Model: model, Context: contextSource}
	key := agentsession.Named("repeated-preparation-suspend")
	prepared, err := prepareDefinition(ctx, definition, PrepareRequest{Session: agentschema.SessionView{Key: key}, Input: agentschema.Text("original input"), Reason: TurnReasonStart})
	if err != nil {
		t.Fatal(err)
	}
	prepared.definitionOperationID, prepared.definitionCommandID, prepared.definitionCycle = "run", "command", 1
	prepared.preparationStage = enginePreparationMaterialized
	prepared.materializedFingerprint, err = materializedDefinitionFingerprint(prepared)
	if err != nil {
		t.Fatal(err)
	}
	original := agentschema.UserMessage("original input")
	raw, err := encodeActiveEngineTranscript(prepared, []*agentschema.Message{original}, original, 0)
	if err != nil {
		t.Fatal(err)
	}
	contextSource.unavailable = true
	var preparationContext context.Context
	engine := &Engine{key: key, cacheKeys: agentsession.CanonicalKey, source: SourceFunc(func(ctx context.Context, _ PrepareRequest) (Definition, error) {
		preparationContext = ctx
		return definition, nil
	})}
	snapshot := TurnSnapshot{OperationID: "run", CommandID: "command", Cycle: 1, Delivery: DeliveryStart,
		Input: UserInput{Text: "original input"}, State: raw}
	for attempt := 0; attempt < 2; attempt++ {
		controls := make(chan Control, 1)
		interrupted := false
		result, err := engine.Run(ctx, Request{Snapshot: snapshot, Controls: controls}, func(event Event) error {
			if update, ok := event.(TranscriptUpdated); ok {
				snapshot.State = append(json.RawMessage(nil), update.State...)
				if !interrupted {
					interrupted = true
					controls <- Control{Kind: ControlSuspend}
					<-preparationContext.Done()
					return preparationContext.Err()
				}
			}
			return nil
		})
		if err != nil || result.Status != Suspended {
			t.Fatalf("suspend %d: result=%+v err=%v", attempt, result, err)
		}
		state, err := decodeEngineTranscript(snapshot.State)
		if err != nil || state.ActiveModelUser == nil || state.ActiveUserIndex != 0 || !reflect.DeepEqual(state.Messages, []*agentschema.Message{original}) {
			t.Fatalf("suspend %d lost accepted input boundary: state=%+v err=%v", attempt, state, err)
		}
	}
	result, err := engine.Run(ctx, Request{Snapshot: snapshot}, func(Event) error { return nil })
	if err != nil || result.Status != Completed {
		t.Fatalf("resume: result=%+v err=%v", result, err)
	}
	want := append(leadingContextMessages(prepared.fragments), original)
	if calls := model.calls(); len(calls) != 1 || !reflect.DeepEqual(calls[0], want) {
		t.Fatalf("repeated suspension changed model input: %#v", calls)
	}
}

func TestPreparedContextResumePreservesContextAndExecutionFences(t *testing.T) {
	for _, scenario := range []string{"changed_context", "unavailable_context", "tool_schema", "tool_descriptor", "tool_removed", "behavior", "legacy_unchanged", "legacy_changed", "invalid_version", "invalid_bound", "tampered_context"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			source := &mutablePreparedContext{body: "Original project instruction"}
			model := &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("done", nil)}}
			tool, err := agenttool.InferTool("inspect", "Inspect a chapter", func(context.Context, struct{}) (string, error) {
				t.Error("resume executed an unrequested tool")
				return "", nil
			})
			if err != nil {
				t.Fatal(err)
			}
			tools := &permissionContractTools{definitions: []agenttool.ToolDefinition{testToolDefinition(tool)}}
			definition := Definition{Name: "test", Model: model, Context: source, Tools: tools}
			key := agentsession.Named("restore-context")
			request := PrepareRequest{Session: agentschema.SessionView{Key: key}, Run: agentschema.RunView{ID: "run", CommandID: "command", Cycle: 1}, Input: agentschema.Text("original input"), Reason: TurnReasonStart}
			prepared, err := prepareDefinition(ctx, definition, request)
			if err != nil {
				t.Fatal(err)
			}
			prepared.definitionOperationID, prepared.definitionCommandID, prepared.definitionCycle = "run", "command", 1
			prepared.preparationStage = enginePreparationMaterialized
			prepared.materializedFingerprint, err = materializedDefinitionFingerprint(prepared)
			if err != nil {
				t.Fatal(err)
			}
			original := agentschema.UserMessage("original input")
			raw, err := encodeActiveEngineTranscript(prepared, []*agentschema.Message{original}, original, 0)
			if err != nil {
				t.Fatal(err)
			}
			state, err := decodeEngineTranscript(raw)
			if err != nil {
				t.Fatal(err)
			}
			wantError := true
			switch scenario {
			case "changed_context":
				source.body, wantError = "Changed project instruction", false
			case "unavailable_context":
				source.unavailable, wantError = true, false
			case "tool_schema":
				changed, err := agenttool.InferTool("inspect", "Inspect a chapter", func(context.Context, struct {
					Path string `json:"path"`
				}) (string, error) {
					return "", nil
				})
				if err != nil {
					t.Fatal(err)
				}
				tools.definitions[0].Tool = changed
			case "tool_descriptor":
				tools.definitions[0].Descriptor.MaxResultBytes++
			case "tool_removed":
				tools.definitions = nil
			case "behavior":
				definition.Instructions = "Changed executable behavior"
			case "legacy_unchanged":
				state.PreparedContext, wantError = nil, false
			case "legacy_changed":
				state.PreparedContext, source.body = nil, "Changed project instruction"
			case "invalid_version":
				state.PreparedContext.Version++
			case "invalid_bound":
				state.PreparedContext.Fragments[0].HardLimit = 1
			case "tampered_context":
				state.PreparedContext.Fragments[0].Content = "Unaccepted context"
			}
			raw, err = json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			source.calls = 0
			engine := &Engine{source: definition, key: key, cacheKeys: agentsession.CanonicalKey}
			var checkpoints []engineTranscript
			result, err := engine.Run(ctx, Request{Snapshot: TurnSnapshot{
				OperationID: "run", CommandID: "command", Cycle: 1, Delivery: DeliveryStart,
				Input: UserInput{Text: "original input"}, State: raw,
			}}, func(event Event) error {
				if update, ok := event.(TranscriptUpdated); ok {
					decoded, decodeErr := decodeEngineTranscript(update.State)
					if decodeErr != nil {
						return decodeErr
					}
					checkpoints = append(checkpoints, decoded)
				}
				return nil
			})
			if wantError {
				if err == nil || len(model.calls()) != 0 {
					t.Fatalf("changed contract reached model: result=%+v err=%v", result, err)
				}
				if len(checkpoints) != 0 {
					t.Fatal("failed resume replaced the last valid recovery checkpoint")
				}
				return
			}
			if err != nil || result.Status != Completed {
				t.Fatalf("resume=%+v error=%v", result, err)
			}
			calls := model.calls()
			expected := append(leadingContextMessages(prepared.fragments), original)
			if len(calls) != 1 || !reflect.DeepEqual(calls[0], expected) {
				t.Fatalf("resume changed accepted context: %#v", calls)
			}
			wantReads := 0
			if scenario == "legacy_unchanged" {
				wantReads = 1
			}
			if source.calls != wantReads {
				t.Fatalf("source reads=%d, want %d", source.calls, wantReads)
			}
			if len(checkpoints) == 0 || checkpoints[0].PreparedContext == nil {
				t.Fatal("successful resume omitted prepared context")
			}
		})
	}
}
