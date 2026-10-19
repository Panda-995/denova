package engine

import (
	"context"
	"encoding/json"
	"fmt"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentgoal "github.com/alfredxw/denova/agent/engine/goal"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// InspectionRequest is an immutable preview snapshot with no admission or
// mutation authority. Lifecycle validates that it is still current afterward.
type InspectionRequest struct {
	Session      agentschema.SessionView
	Run          agentschema.RunView
	Input        agentschema.Input
	State        json.RawMessage
	Capabilities map[string]json.RawMessage
}

func Inspect(ctx context.Context, source Source, cacheKeys agentschema.CacheKeyGenerator, request InspectionRequest) (Inspection, error) {
	transcript, err := decodeEngineTranscript(request.State)
	if err != nil {
		return Inspection{}, err
	}
	clearState, clearPresent, err := applyClearToTranscript(&transcript, request.Capabilities)
	if err != nil {
		return Inspection{}, err
	}
	compaction, compactionPresent, err := agenthistory.CompactionStateFrom(request.Capabilities)
	if err != nil {
		return Inspection{}, err
	}
	compaction, compactionPresent = agenthistory.ClearCompaction(compaction, compactionPresent, clearState, clearPresent)
	prepareRequest := PrepareRequest{
		Session:    request.Session,
		Run:        request.Run,
		Input:      request.Input,
		Reason:     TurnReasonStart,
		HostData:   agentschema.CloneHostData(request.Input.HostData),
		Compaction: agenthistory.CompactionStatePointer(compaction, compactionPresent),
	}
	prepared, err := prepareDefinition(ctx, source, prepareRequest)
	if err != nil {
		return Inspection{}, err
	}
	var goal agentgoal.GoalState
	goalPresent := false
	if raw, present := request.Capabilities[agentgoal.GoalCapability]; present {
		goal, err = agentgoal.DecodeGoalState(raw)
		if err != nil {
			return Inspection{}, err
		}
		goalPresent = true
	}
	if err := applyPreparedGoal(ctx, &prepared, request.Session, request.Run, goal, goalPresent); err != nil {
		return Inspection{}, err
	}
	materialized, err := materializedDefinitionFingerprint(prepared)
	if err != nil {
		return Inspection{}, err
	}
	prepared.materializedFingerprint = materialized
	prepared.contextState = agenthistory.CloneContextStateSnapshot(transcript.ContextState)
	prepared.archive = transcript.Archive
	prepared.elision, err = agenthistory.ElisionStateFrom(request.Capabilities)
	if err != nil {
		return Inspection{}, err
	}
	stateMessages, inspectedContextState, err := prepared.archive.AdvanceContextState(
		transcript.Messages, prepared.fragments, prepared.contextState, compaction, compactionPresent,
	)
	if err != nil {
		return Inspection{}, err
	}
	prepared.contextState = inspectedContextState
	inspectionTranscript := append(agentschema.CloneMessages(transcript.Messages), agentschema.CloneMessages(stateMessages)...)

	summaryLimit := 0
	if prepared.definition.Compaction != nil {
		summaryLimit = prepared.definition.Compaction.SummaryLimitBytes()
	} else if compactionPresent && !compaction.Removed {
		return Inspection{}, fmt.Errorf("%w: active Compaction has no Manager in the selected Definition", agentschema.ErrDefinitionMismatch)
	}
	effective, err := prepared.archive.EffectiveHistoryMessages(inspectionTranscript, prepared.elision, compaction, compactionPresent, summaryLimit)
	if err != nil {
		return Inspection{}, err
	}
	messages, _, err := assembleCycleMessages(effective, request.Input.Text, request.Input.Attachments, prepared.fragments, prepared.definition.AttachmentRoot)
	if err != nil {
		return Inspection{}, err
	}
	ctx, err = contextWithProviderCacheKey(ctx, request.Session.Key, cacheKeys)
	if err != nil {
		return Inspection{}, err
	}
	modelRequest, err := prepareDefinitionModelRequest(
		ctx,
		prepared,
		request.Session,
		request.Run,
		messages,
		stableContextPrefixMessages(prepared.fragments, compaction, compactionPresent),
	)
	if err != nil {
		return Inspection{}, err
	}

	return Inspection{
		Session:       request.Session,
		Run:           request.Run,
		DefinitionKey: prepared.definitionKey, BehaviorKey: prepared.behaviorKey,
		MaterializedFingerprint: prepared.materializedFingerprint,
		PrefixFingerprint:       prepared.prefixFingerprint,
		ModelIdentity:           prepared.definition.ModelIdentity,
		Compaction:              agenthistory.CompactionStatePointer(compaction, compactionPresent),
		CompactionMetrics:       compaction.Metrics,
		ElisionMetrics:          agenthistory.ElisionForHistory(prepared.elision, compaction, compactionPresent).Metrics,
		ContextFragments:        append([]agentschema.ContextFragment(nil), prepared.fragments...),
		ModelRequest:            agentmodel.InspectModelRequest(modelRequest),
	}, nil
}
