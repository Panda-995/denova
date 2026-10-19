package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

const turnToolRecord = "turn.tool"

func (run *Run) recordToolStart(event agentengine.ToolStarted) error {
	if event.ExecutionAuthorized {
		if err := run.admitWork(context.Background()); err != nil {
			return err
		}
	}
	if event.CallID == "" {
		return errors.New("tool intent requires an execution ID")
	}
	fact := agentengine.PersistedTool{RunID: run.id, Cycle: run.cycleValue(), CallID: event.CallID, ProviderCallID: event.ProviderCallID,
		Name: event.Name, Index: event.Index, Arguments: append(json.RawMessage(nil), event.Arguments...),
		Descriptor: agenttool.DecodeExecutionMetadata(event.Metadata), Started: event.ExecutionAuthorized}
	if len(fact.Arguments) == 0 {
		fact.Arguments = json.RawMessage(`{}`)
	}
	run.session.mu.Lock()
	defer run.session.mu.Unlock()
	if previous, found := run.tools[fact.CallID]; found && previous.Started {
		return errors.New("tool execution ID was already started")
	}
	if err := run.session.appendRecordLocked(context.Background(), turnToolRecord, fact); err != nil {
		return err
	}
	run.tools[fact.CallID] = fact
	return nil
}

func (run *Run) recordToolResult(event agentengine.ToolFinished, result *agentschema.ToolResult) error {
	run.session.mu.Lock()
	defer run.session.mu.Unlock()
	fact, found := run.tools[event.CallID]
	if !found {
		return errors.New("tool result has no durable intent")
	}
	if result == nil {
		value := agentschema.ToolResult{Status: agentschema.ToolResultSuccess, ModelContent: event.Result, DisplayContent: event.Result}
		if event.IsError {
			value.Status = agentschema.ToolResultError
		}
		result = &value
	}
	// Interrupted Ask/permission calls have no completed result yet. Their
	// durable request and response survive the process waiter.
	run.mu.RLock()
	for _, interaction := range run.interactions {
		if interaction.snapshot.ToolCallID == event.CallID && run.suspendReason != "" && result.Status != agentschema.ToolResultSuccess {
			run.mu.RUnlock()
			return nil
		}
	}
	run.mu.RUnlock()
	fact.Result, fact.Source = result, "tool"
	if err := run.session.appendRecordLocked(context.Background(), turnToolRecord, fact); err != nil {
		return err
	}
	run.tools[event.CallID] = fact
	return nil
}

func (session *Session) replayTool(data json.RawMessage) error {
	var fact agentengine.PersistedTool
	if err := json.Unmarshal(data, &fact); err != nil {
		return err
	}
	run := session.runs[fact.RunID]
	if run == nil || fact.CallID == "" || fact.Name == "" || !json.Valid(fact.Arguments) {
		return errors.New("invalid persisted tool fact")
	}
	run.tools[fact.CallID] = fact
	if fact.Started && fact.Result == nil {
		run.openTools[fact.CallID] = agentevent.OpenToolSnapshot{CallID: fact.CallID, Name: fact.Name, RunID: run.id, Cycle: fact.Cycle}
	}
	if fact.Result != nil {
		delete(run.openTools, fact.CallID)
	}
	return nil
}

func (run *Run) restoreEffectInteractions() error {
	run.session.mu.Lock()
	defer run.session.mu.Unlock()
	ids := make([]string, 0, len(run.tools))
	for id := range run.tools {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fact := run.tools[id]
		if !fact.Started || fact.Result != nil || (fact.Descriptor != nil && fact.Descriptor.MutationScope == agenttool.ToolMutationNone) {
			continue
		}
		interactionID := "verify-" + id
		run.mu.RLock()
		_, pending := run.interactions[interactionID]
		run.mu.RUnlock()
		if pending {
			continue
		}
		request := agentinteraction.InteractionRequest{ID: interactionID, Kind: agentinteraction.InteractionAsk,
			Questions: []agentinteraction.InteractionQuestion{{ID: "effect", Prompt: "Did this tool operation take effect before execution stopped?",
				Options: []agentinteraction.InteractionOption{{Value: "executed", Label: "It took effect"}, {Value: "not_executed", Label: "It did not take effect"}, {Value: "unknown", Label: "I cannot determine yet", Recommended: true}}}},
			Verification: &agentinteraction.ToolEffectVerification{ExecutionID: id, Tool: fact.Name, Arguments: append(json.RawMessage(nil), fact.Arguments...)},
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			return err
		}
		stored := persistedInteraction{RunID: run.id, Cycle: fact.Cycle, InteractionID: interactionID, ToolCallID: id, Request: encoded}
		if err := run.session.appendRecordLocked(context.Background(), turnInteractionRecord, stored); err != nil {
			return err
		}
		run.mu.Lock()
		run.interactions[interactionID] = stored.pending(request)
		run.mu.Unlock()
	}
	return nil
}

// awaitRecoveryInput keeps the whole execution entrance closed until saved Ask
// requests and unknown effects are resolved. Queue and Steer cannot bypass it.
func (run *Run) awaitRecoveryInput() (agentengine.Status, error) {
	if err := run.restoreEffectInteractions(); err != nil {
		return "", err
	}
	for _, request := range run.pendingInteractionRequests() {
		run.publish(agentevent.InteractionRequested{Request: request})
	}
	for len(run.pendingInteractionRequests()) > 0 {
		select {
		case control := <-run.controls:
			switch control.Kind {
			case agentengine.ControlAbort:
				return agentengine.Aborted, nil
			case agentengine.ControlSuspend:
				return agentengine.Suspended, nil
			case agentengine.ControlPreempt:
			case agentengine.ControlInteractionResolved:
			default:
				return "", fmt.Errorf("unsupported recovery control %q", control.Kind)
			}
		case <-run.ctx.Done():
			return "", run.ctx.Err()
		}
	}
	return "", nil
}

// toolFacts returns a detached recovery snapshot; callers never share the
// lifecycle lock or mutable result bodies with an executing tool.
func (run *Run) toolFacts() map[string]agentengine.PersistedTool {
	run.session.mu.RLock()
	defer run.session.mu.RUnlock()
	return agentschema.CloneAny(run.tools).(map[string]agentengine.PersistedTool)
}
