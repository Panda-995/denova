package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentcompaction "github.com/alfredxw/denova/agent/context/compaction"
	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentengine "github.com/alfredxw/denova/agent/engine"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func (session *Session) Compact(ctx context.Context, request agentcompaction.CompactionRequest) (agentcompaction.CompactionResult, error) {
	commandID := strings.TrimSpace(request.IdempotencyKey)
	if commandID == "" {
		commandID = newPublicID("compact")
	}
	preparation, release, err := session.prepareStructuralDefinition(ctx, agentengine.CommandID(commandID), agentengine.StructuralCompactContext)
	if err != nil {
		return agentcompaction.CompactionResult{}, err
	}
	defer release()
	encoded, err := preparation.maintenance.CompactCommand(ctx, request)
	if err != nil {
		return agentcompaction.CompactionResult{}, err
	}
	operationID := agentengine.OperationID(newPublicID("compaction"))
	ref, err := session.compactionRef(agentengine.Cursor(preparation.cursor), encoded, "create bounded conversation checkpoint", "")
	if err != nil {
		return agentcompaction.CompactionResult{}, err
	}
	session.publishSessionEvent(agentevent.CompactionStarted{ID: string(operationID)})
	if err := session.executeStructural(ctx, preparation, agentengine.StructuralOperationSnapshot{
		Binding: session.binding.Clone(), CommandID: agentengine.CommandID(commandID), OperationID: operationID,
		Cycle: 1, Kind: agentengine.StructuralCompactContext, Ref: ref, ContextCursor: agentengine.Cursor(preparation.cursor),
	}); err != nil {
		return agentcompaction.CompactionResult{}, err
	}
	updated, exists, err := session.compactionState(ctx)
	if err != nil {
		return agentcompaction.CompactionResult{}, err
	}
	result := agentcompaction.CompactionResult{Changed: exists && updated.Revision != preparation.maintenance.CompactionRevision}
	if view := agenthistory.CompactionStatePointer(updated, exists); view != nil {
		result.State = *view
	}
	return result, nil
}

func (session *Session) RemoveCompaction(ctx context.Context, request agentcompaction.CompactionRemoveRequest) (bool, error) {
	commandID := strings.TrimSpace(request.IdempotencyKey)
	if commandID == "" {
		commandID = newPublicID("remove-compaction")
	}
	preparation, release, err := session.prepareStructuralDefinition(ctx, agentengine.CommandID(commandID), agentengine.StructuralRemoveCompaction)
	if err != nil {
		return false, err
	}
	defer release()
	encoded, err := preparation.maintenance.RemoveCommand(request)
	if err != nil {
		return false, err
	}
	if len(encoded) == 0 {
		return false, nil
	}
	operationID := agentengine.OperationID(newPublicID("compaction"))
	ref, err := session.compactionRef(agentengine.Cursor(preparation.cursor), encoded, "restore raw conversation history", preparation.maintenance.CompactionID)
	if err != nil {
		return false, err
	}
	session.publishSessionEvent(agentevent.CompactionStarted{ID: preparation.maintenance.CompactionID, Remove: true})
	if err := session.executeStructural(ctx, preparation, agentengine.StructuralOperationSnapshot{
		Binding: session.binding.Clone(), CommandID: agentengine.CommandID(commandID), OperationID: operationID,
		Cycle: 1, Kind: agentengine.StructuralRemoveCompaction, Ref: ref, ContextCursor: agentengine.Cursor(preparation.cursor),
	}); err != nil {
		return false, err
	}
	updated, exists, err := session.compactionState(ctx)
	return exists && updated.Removed && updated.Revision > preparation.maintenance.CompactionRevision, err
}

type structuralDefinitionPreparation struct {
	maintenance *agentengine.Maintenance
	cursor      uint64
}

func (session *Session) prepareStructuralDefinition(ctx context.Context, commandID agentengine.CommandID, kind agentengine.StructuralOperationKind) (structuralDefinitionPreparation, func(), error) {
	if err := session.usable(); err != nil {
		return structuralDefinitionPreparation{}, nil, err
	}
	session.mu.Lock()
	if session.active != nil || session.maintenance {
		session.mu.Unlock()
		return structuralDefinitionPreparation{}, nil, agentschema.ErrSessionBusy
	}
	session.maintenance = true
	state := append(json.RawMessage(nil), session.engineState...)
	capabilities := agentschema.CloneRawStateMap(session.capabilities)
	cursor := uint64(session.revision)
	history := session.canonicalSource
	session.mu.Unlock()
	release := func() {
		session.mu.Lock()
		session.maintenance = false
		session.mu.Unlock()
	}
	maintenance, err := agentengine.PrepareMaintenance(ctx, session.agent.source, session.agent.cacheKeys, agentengine.MaintenanceRequest{
		Session: agentschema.SessionView{Key: session.key, Revision: cursor}, CommandID: commandID, Kind: kind,
		State: state, Capabilities: capabilities, History: history,
	})
	if err != nil {
		release()
		return structuralDefinitionPreparation{}, nil, err
	}
	return structuralDefinitionPreparation{maintenance: maintenance, cursor: cursor}, release, nil
}

func (session *Session) executeStructural(ctx context.Context, preparation structuralDefinitionPreparation, snapshot agentengine.StructuralOperationSnapshot) error {
	engine, ok := session.engine.(agentengine.StructuralRunner)
	if !ok {
		return agentschema.ErrCapabilityUnsupported
	}
	result, err := engine.RunStructural(ctx, agentengine.StructuralRequest{
		Binding: session.binding.Clone(), Snapshot: snapshot, State: preparation.maintenance.State,
		Capabilities: preparation.maintenance.Capabilities, Controls: make(chan agentengine.Control),
	}, func(event agentengine.Event) error {
		update, ok := event.(agentengine.CapabilityState)
		if !ok || update.Delete || update.Capability != agenthistory.CompactionCapability {
			return fmt.Errorf("unsupported structural Agent event %T", event)
		}
		session.mu.Lock()
		_, persistErr := session.commitContextCheckpointLocked(context.Background(), agentengine.TranscriptUpdated{
			State: preparation.maintenance.State, CapabilityStates: map[string]json.RawMessage{update.Capability: update.State},
		})
		session.mu.Unlock()
		if persistErr != nil {
			return persistErr
		}
		if update.Capability == agenthistory.CompactionCapability && !update.Delete {
			state, decodeErr := agenthistory.DecodeCompactionState(update.State)
			if decodeErr != nil {
				return decodeErr
			}
			if state.Removed {
				session.publishSessionEvent(agentevent.CompactionRemoved{ID: state.ID, Revision: state.Revision})
			} else {
				session.publishSessionEvent(agentevent.CompactionCommitted{State: *agenthistory.CompactionStatePointer(state, true), Metrics: state.Metrics})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if result.Status != agentengine.Completed {
		return &agentevent.RunError{Result: agentschema.Result{Status: agentschema.ResultFailed, Reason: "Compaction did not complete"}}
	}
	return nil
}

func (session *Session) compactionRef(cursor agentengine.Cursor, descriptor json.RawMessage, purpose, id string) (agentengine.ContextCompactionRef, error) {
	resource, err := agentsessionCanonical(session.key)
	if err != nil {
		return agentengine.ContextCompactionRef{}, err
	}
	spec, err := agentschema.HashCanonical(struct {
		Version uint16
		Cursor  agentengine.Cursor
		Data    json.RawMessage
	}{1, cursor, descriptor})
	if err != nil {
		return agentengine.ContextCompactionRef{}, err
	}
	return agentengine.ContextCompactionRef{
		SpecRef: spec, Source: "agent.session.messages", Purpose: purpose, Resource: resource,
		ExpectedRevision: fmt.Sprintf("cursor:%d", cursor), CompactionID: id,
		Envelope: append(json.RawMessage(nil), descriptor...),
	}, nil
}

func (session *Session) compactionState(_ context.Context) (agenthistory.CompactionRecord, bool, error) {
	if err := session.usable(); err != nil {
		return agenthistory.CompactionRecord{}, false, err
	}
	session.mu.RLock()
	state, present, err := agenthistory.CompactionStateFrom(session.capabilities)
	clear, clearPresent, clearErr := agenthistory.ClearStateFrom(session.capabilities)
	session.mu.RUnlock()
	if err != nil {
		return agenthistory.CompactionRecord{}, false, err
	}
	if clearErr != nil {
		return agenthistory.CompactionRecord{}, false, clearErr
	}
	state, present = agenthistory.ClearCompaction(state, present, clear, clearPresent)
	return state, present, nil
}

func (session *Session) publishSessionEvent(payload agentevent.EventPayload) {
	session.mu.Lock()
	session.publishLocked(agentevent.Event{Payload: payload})
	session.mu.Unlock()
}
