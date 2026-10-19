package engine

import (
	"context"
	"encoding/json"
	"strings"

	agentcompaction "github.com/alfredxw/denova/agent/context/compaction"
	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

// MaintenanceRequest is admitted and held stable by lifecycle for the entire
// structural operation. The engine owns preparation and archive reconstruction.
type MaintenanceRequest struct {
	Session      agentschema.SessionView
	CommandID    CommandID
	Kind         StructuralOperationKind
	State        json.RawMessage
	Capabilities map[string]json.RawMessage
	History      agentcanonical.CanonicalHistorySource
}

// Maintenance exposes only the checkpoint and capability snapshot needed for
// execution, plus the existing compaction identity needed to publish results.
type Maintenance struct {
	State              json.RawMessage
	Capabilities       map[string]json.RawMessage
	CompactionID       string
	CompactionRevision uint64
	prepared           preparedDefinition
	transcript         engineTranscript
	compaction         agenthistory.CompactionRecord
	compactionPresent  bool
	session            agentschema.SessionView
	commandID          CommandID
	cacheKeys          agentschema.CacheKeyGenerator
}

func PrepareMaintenance(ctx context.Context, source Source, cacheKeys agentschema.CacheKeyGenerator, request MaintenanceRequest) (*Maintenance, error) {
	state, capabilities := request.State, request.Capabilities
	transcript, err := decodeEngineTranscript(state)
	if err != nil {
		return nil, err
	}
	clearState, clearPresent, err := applyClearToTranscript(&transcript, capabilities)
	if err != nil {
		return nil, err
	}
	current, present, err := agenthistory.CompactionStateFrom(capabilities)
	if err != nil {
		return nil, err
	}
	current, present = agenthistory.ClearCompaction(current, present, clearState, clearPresent)
	prepared, err := prepareDefinition(ctx, source, PrepareRequest{
		Session: agentschema.SessionView{Key: request.Session.Key, Revision: request.Session.Revision}, Run: structuralDefinitionRun(request.CommandID),
		Reason: TurnReasonStructural, HostData: agentschema.CloneHostData(transcript.HostData),
		Compaction: agenthistory.CompactionStatePointer(current, present),
	})
	if err != nil {
		return nil, err
	}
	materialized, err := materializedDefinitionFingerprint(prepared)
	if err != nil {
		return nil, err
	}
	prepared.materializedFingerprint = materialized
	if transcript.Archive != nil && (request.Kind == StructuralRemoveCompaction || prepared.definition.Compaction != nil && len(current.Summary) > prepared.definition.Compaction.SummaryLimitBytes()) {
		transcript, err = expandCanonicalArchive(ctx, request.History, transcript)
		if err != nil {
			return nil, err
		}
		state, err = json.Marshal(transcript)
		if err != nil {
			return nil, err
		}
	}
	prepared.contextState = agenthistory.CloneContextStateSnapshot(transcript.ContextState)
	prepared.archive = transcript.Archive
	prepared.historyHead = transcript.HistoryHead
	prepared.elision, err = agenthistory.ElisionStateFrom(capabilities)
	if err != nil {
		return nil, err
	}
	return &Maintenance{
		prepared: prepared, transcript: transcript, State: state, Capabilities: capabilities,
		compaction: current, compactionPresent: present,
		CompactionID: current.ID, CompactionRevision: current.Revision,
		session: request.Session, commandID: request.CommandID, cacheKeys: cacheKeys,
	}, nil
}

func (maintenance *Maintenance) CompactCommand(ctx context.Context, request agentcompaction.CompactionRequest) (json.RawMessage, error) {
	if maintenance.prepared.definition.Compaction == nil {
		return nil, agentschema.ErrCapabilityUnsupported
	}
	current, present := maintenance.compaction, maintenance.compactionPresent
	if request.ExpectedID != "" && (!present || current.ID != request.ExpectedID) ||
		request.ExpectedRevision != 0 && (!present || current.Revision != request.ExpectedRevision) {
		return nil, agentschema.ErrDefinitionMismatch
	}
	forkCtx, err := contextWithProviderCacheKey(ctx, maintenance.session.Key, maintenance.cacheKeys)
	if err != nil {
		return nil, err
	}
	modelSnapshot, err := prepareStructuralCompactionSnapshot(
		forkCtx, maintenance.prepared,
		agentschema.SessionView{Key: maintenance.session.Key, Revision: maintenance.session.Revision},
		structuralDefinitionRun(maintenance.commandID),
		maintenance.transcript.Messages,
		current, present,
	)
	if err != nil {
		return nil, err
	}
	modelFingerprint, err := modelRequestSnapshotFingerprint(modelSnapshot)
	if err != nil {
		return nil, err
	}
	envelope := compactionCommandEnvelope{
		Version: compactionCommandVersion, DefinitionKey: maintenance.prepared.definitionKey,
		BehaviorKey:             maintenance.prepared.behaviorKey,
		MaterializedFingerprint: maintenance.prepared.materializedFingerprint,
		ModelRequestFingerprint: modelFingerprint,
		Manager:                 maintenance.prepared.definition.Compaction.Identity(), Compact: &request,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func (maintenance *Maintenance) RemoveCommand(request agentcompaction.CompactionRemoveRequest) (json.RawMessage, error) {
	if maintenance.prepared.definition.Compaction == nil {
		return nil, agentschema.ErrCapabilityUnsupported
	}
	current, present := maintenance.compaction, maintenance.compactionPresent
	if !present || current.Removed {
		return nil, nil
	}
	if strings.TrimSpace(request.ID) == "" {
		request.ID = current.ID
	}
	if request.ID != current.ID || request.ExpectedRevision != 0 && request.ExpectedRevision != current.Revision {
		return nil, agentschema.ErrDefinitionMismatch
	}
	envelope := compactionCommandEnvelope{
		Version: compactionCommandVersion, DefinitionKey: maintenance.prepared.definitionKey,
		BehaviorKey:             maintenance.prepared.behaviorKey,
		MaterializedFingerprint: maintenance.prepared.materializedFingerprint,
		Manager:                 maintenance.prepared.definition.Compaction.Identity(), Remove: &request,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}
