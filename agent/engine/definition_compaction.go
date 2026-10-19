package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcompaction "github.com/alfredxw/denova/agent/context/compaction"
	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

type compactionCommandEnvelope struct {
	Version                 uint16                                   `json:"version"`
	DefinitionKey           string                                   `json:"definition_key"`
	BehaviorKey             string                                   `json:"behavior_key"`
	MaterializedFingerprint string                                   `json:"materialized_fingerprint"`
	ModelRequestFingerprint string                                   `json:"model_request_fingerprint,omitempty"`
	Manager                 agentschema.CapabilityIdentity           `json:"manager"`
	Compact                 *agentcompaction.CompactionRequest       `json:"compact,omitempty"`
	Remove                  *agentcompaction.CompactionRemoveRequest `json:"remove,omitempty"`
}

const compactionCommandVersion = 2

func (engine *Engine) RunStructural(
	ctx context.Context,
	request StructuralRequest,
	emit EventSink,
) (Result, error) {
	if engine == nil || engine.source == nil || emit == nil {
		return Result{}, agentschema.ErrDefinitionUnavailable
	}
	transcript, err := decodeEngineTranscript(request.State)
	if err != nil {
		return Result{}, err
	}
	clearState, clearPresent, err := applyClearToTranscript(&transcript, request.Capabilities)
	if err != nil {
		return Result{}, err
	}
	storage, storagePresent, err := agenthistory.CompactionStateFrom(request.Capabilities)
	if err != nil {
		return Result{}, err
	}
	current, present := storage, storagePresent
	current, present = agenthistory.ClearCompaction(current, present, clearState, clearPresent)
	var envelope compactionCommandEnvelope
	if err := json.Unmarshal(request.Snapshot.Ref.Envelope, &envelope); err != nil {
		return Result{}, fmt.Errorf("decode Compaction command: %w", err)
	}
	if envelope.Version != compactionCommandVersion || strings.TrimSpace(envelope.DefinitionKey) == "" ||
		strings.TrimSpace(envelope.BehaviorKey) == "" || strings.TrimSpace(envelope.MaterializedFingerprint) == "" {
		return Result{}, errors.New("Compaction command envelope is incomplete")
	}
	prepared, err := prepareDefinition(ctx, engine.source, PrepareRequest{
		Session: agentschema.SessionView{Key: engine.key, Revision: uint64(request.Snapshot.ContextCursor)},
		Run:     structuralDefinitionRun(request.Snapshot.CommandID),
		Reason:  TurnReasonStructural, DefinitionKey: envelope.DefinitionKey, BehaviorKey: envelope.BehaviorKey,
		HostData:   agentschema.CloneHostData(transcript.HostData),
		Compaction: agenthistory.CompactionStatePointer(current, present),
	})
	if err != nil {
		return Result{}, err
	}
	if prepared.definition.Compaction == nil {
		return Result{}, agentschema.ErrCapabilityUnsupported
	}
	prepared.contextState = agenthistory.CloneContextStateSnapshot(transcript.ContextState)
	prepared.archive = transcript.Archive
	prepared.elision, err = agenthistory.ElisionStateFrom(request.Capabilities)
	if err != nil {
		return Result{}, err
	}
	materialized, materializedErr := materializedDefinitionFingerprint(prepared)
	if materializedErr != nil {
		return Result{}, materializedErr
	}
	if prepared.definitionKey != envelope.DefinitionKey || prepared.behaviorKey != envelope.BehaviorKey ||
		materialized != envelope.MaterializedFingerprint {
		return Result{}, agentschema.ErrDefinitionMismatch
	}
	if envelope.Manager != prepared.definition.Compaction.Identity() {
		return Result{}, fmt.Errorf("%w: Compaction Manager changed", agentschema.ErrDefinitionMismatch)
	}
	session := agentschema.SessionView{Key: engine.key, Revision: uint64(request.Snapshot.ContextCursor)}
	run := runViewForStructural(request.Snapshot)
	switch request.Snapshot.Kind {
	case StructuralCompactContext:
		if envelope.Compact == nil || envelope.Remove != nil {
			return Result{}, errors.New("Compaction command envelope does not match compact operation")
		}
		if current.ID == compactionID(request.Snapshot.OperationID) && !current.Removed {
			return Result{Status: Completed}, nil
		}
		forkCtx, cacheErr := contextWithProviderCacheKey(ctx, engine.key, engine.cacheKeys)
		if cacheErr != nil {
			return Result{}, cacheErr
		}
		modelSnapshot, snapshotErr := prepareStructuralCompactionSnapshot(
			forkCtx, prepared, session, structuralDefinitionRun(request.Snapshot.CommandID),
			transcript.Messages, current, present,
		)
		if snapshotErr != nil {
			return Result{}, snapshotErr
		}
		fingerprint, fingerprintErr := modelRequestSnapshotFingerprint(modelSnapshot)
		if fingerprintErr != nil {
			return Result{}, fingerprintErr
		}
		if envelope.ModelRequestFingerprint == "" || fingerprint != envelope.ModelRequestFingerprint {
			return Result{}, fmt.Errorf("%w: structural model request changed", agentschema.ErrDefinitionMismatch)
		}
		buildAfter := func(next agenthistory.CompactionRecord) (*agentmodel.ModelRequestSnapshot, error) {
			nextPrepared := prepared
			prepare := PrepareRequest{
				Session: session, Run: structuralDefinitionRun(request.Snapshot.CommandID), Reason: TurnReasonStructural,
				DefinitionKey: envelope.DefinitionKey, BehaviorKey: envelope.BehaviorKey,
				HostData: agentschema.CloneHostData(transcript.HostData), Compaction: agenthistory.CompactionStatePointer(next, true),
			}
			if err := rematerializeDefinitionContext(ctx, prepare, &nextPrepared); err != nil {
				return nil, err
			}
			return prepareStructuralCompactionSnapshot(
				forkCtx, nextPrepared, session, structuralDefinitionRun(request.Snapshot.CommandID),
				transcript.Messages, next, true,
			)
		}
		contextMessages, contextErr := agenthistory.ElisionForHistory(prepared.elision, current, present).ProjectArchive(transcript.Messages, prepared.archive)
		if contextErr != nil {
			return Result{}, contextErr
		}
		contextMessages, contextErr = projectToolArtifactPaths(forkCtx, prepared.definition.Artifacts, contextMessages)
		if contextErr != nil {
			return Result{}, contextErr
		}
		next, changed, _, compactErr := executeCompaction(
			ctx, prepared, session, run, transcript.Messages, contextMessages, "", current, present, storage.Revision,
			*envelope.Compact, compactionID(request.Snapshot.OperationID), modelSnapshot, buildAfter, nil, nil,
		)
		if compactErr != nil {
			return Result{}, compactErr
		}
		if changed {
			encoded, encodeErr := json.Marshal(next)
			if encodeErr != nil {
				return Result{}, encodeErr
			}
			if err := emit(CapabilityState{
				Capability: agenthistory.CompactionCapability, State: encoded,
			}); err != nil {
				return Result{}, err
			}
		}
	case StructuralRemoveCompaction:
		if envelope.Remove == nil || envelope.Compact != nil {
			return Result{}, errors.New("Compaction command envelope does not match remove operation")
		}
		remove := *envelope.Remove
		if !present || current.Removed {
			return Result{Status: Completed}, nil
		}
		if current.ID != remove.ID || remove.ExpectedRevision != 0 && current.Revision != remove.ExpectedRevision {
			return Result{}, agentschema.ErrDefinitionMismatch
		}
		current.Revision++
		current.Removed = true
		encoded, encodeErr := json.Marshal(current)
		if encodeErr != nil {
			return Result{}, encodeErr
		}
		if err := emit(CapabilityState{
			Capability: agenthistory.CompactionCapability, State: encoded,
		}); err != nil {
			return Result{}, err
		}
	default:
		return Result{}, fmt.Errorf("unsupported structural operation %q", request.Snapshot.Kind)
	}
	return Result{Status: Completed}, nil
}

func structuralDefinitionRun(commandID CommandID) agentschema.RunView {
	return agentschema.RunView{ID: string(commandID), CommandID: string(commandID), Cycle: 1}
}

func contextWithProviderCacheKey(ctx context.Context, key agentsession.Key, generate agentschema.CacheKeyGenerator) (context.Context, error) {
	if generate == nil {
		generate = defaultCacheKey
	}
	cacheKey, err := generate(key)
	if err != nil {
		return nil, fmt.Errorf("derive Agent provider Cache Key: %w", err)
	}
	cacheKey = strings.TrimSpace(cacheKey)
	if cacheKey == "" || len(cacheKey) > 256 {
		return nil, errors.New("Agent provider Cache Key is empty or exceeds 256 bytes")
	}
	return agentmodel.ContextWithSessionKey(ctx, cacheKey), nil
}

func prepareStructuralCompactionSnapshot(
	ctx context.Context,
	prepared preparedDefinition,
	session agentschema.SessionView,
	run agentschema.RunView,
	raw []*agentschema.Message,
	compaction agenthistory.CompactionRecord,
	compactionPresent bool,
) (*agentmodel.ModelRequestSnapshot, error) {
	stateMessages, nextContextState, err := prepared.archive.AdvanceContextState(
		raw, prepared.fragments, prepared.contextState, compaction, compactionPresent,
	)
	if err != nil {
		return nil, err
	}
	prepared.contextState = nextContextState
	raw = append(agentschema.CloneMessages(raw), agentschema.CloneMessages(stateMessages)...)
	effective, err := prepared.archive.EffectiveHistoryMessages(
		raw, prepared.elision, compaction, compactionPresent, prepared.definition.Compaction.SummaryLimitBytes(),
	)
	checkpointVisible := err == nil
	if err != nil {
		if !errors.Is(err, agentschema.ErrContextLimit) {
			return nil, err
		}
		effective, err = agenthistory.ElisionForHistory(prepared.elision, compaction, compactionPresent).ProjectArchive(raw, prepared.archive)
		if err != nil {
			return nil, err
		}
	}
	messages := make([]*agentschema.Message, 0, len(effective)+len(prepared.fragments))
	messages = append(messages, leadingContextMessages(prepared.fragments)...)
	messages = append(messages, effective...)
	stablePrefixMessages := stableContextPrefixMessages(prepared.fragments, compaction, compactionPresent)
	if !checkpointVisible && compactionPresent && !compaction.Removed && compaction.ReplacementFrom == 0 {
		stablePrefixMessages--
	}
	return prepareDefinitionModelRequest(ctx, prepared, session, run, messages, stablePrefixMessages)
}

func modelRequestSnapshotFingerprint(snapshot *agentmodel.ModelRequestSnapshot) (string, error) {
	if snapshot == nil {
		return "", errors.New("structural Compaction model request snapshot is unavailable")
	}
	return agentschema.HashCanonical(struct {
		Messages             []*agentschema.Message
		Options              *agentmodel.Options
		Streaming            bool
		StablePrefixMessages int
	}{snapshot.Messages(), snapshot.ResolvedOptions(), snapshot.Streaming(), snapshot.StablePrefixMessages()})
}

func executeCompaction(
	ctx context.Context,
	prepared preparedDefinition,
	session agentschema.SessionView,
	run agentschema.RunView,
	messages []*agentschema.Message,
	contextMessages []*agentschema.Message,
	currentInput string,
	current agenthistory.CompactionRecord,
	present bool,
	revisionBase uint64,
	request agentcompaction.CompactionRequest,
	checkpointID string,
	modelSnapshot *agentmodel.ModelRequestSnapshot,
	buildAfter func(agenthistory.CompactionRecord) (*agentmodel.ModelRequestSnapshot, error),
	onSkip func(string, agenthistory.CompactionMetrics) error,
	onCreate func(agenthistory.CompactionMetrics) error,
) (agenthistory.CompactionRecord, bool, agenthistory.CompactionMetrics, error) {
	if present && current.ID == checkpointID && !current.Removed {
		return current, false, current.Metrics, nil
	}
	if request.ExpectedID != "" && (!present || current.ID != request.ExpectedID) ||
		request.ExpectedRevision != 0 && (!present || current.Revision != request.ExpectedRevision) {
		return agenthistory.CompactionRecord{}, false, agenthistory.CompactionMetrics{}, agentschema.ErrDefinitionMismatch
	}
	summaryLimit := prepared.definition.Compaction.SummaryLimitBytes()
	if len(contextMessages) != len(messages) || present && current.ReplacementTo > prepared.archive.Count(messages) {
		return agenthistory.CompactionRecord{}, false, agenthistory.CompactionMetrics{}, errors.New("Compaction runtime source does not match journal coverage")
	}
	contextMessages, err := agentschema.ResolveMessageAttachmentPaths(prepared.definition.AttachmentRoot, contextMessages)
	if err != nil {
		return agenthistory.CompactionRecord{}, false, agenthistory.CompactionMetrics{}, err
	}
	groups, ends, retainedBytes := prepared.archive.CompactionGroups(messages, contextMessages, current, present)
	base := agenthistory.CompactionRecord{
		Version: 2, ID: checkpointID, Revision: max(uint64(1), revisionBase+1), CreatedAt: time.Now().UTC(),
	}
	if present {
		base.Revision = max(base.Revision, current.Revision+1)
		if !current.Removed {
			base.ReplacementFrom = current.ReplacementFrom
		}
	}
	recordThrough := func(end int) agenthistory.CompactionRecord {
		next := base
		next.ReplacementTo = end
		if prepared.activeModelUser != nil && prepared.activeUserIndex >= next.ReplacementFrom && prepared.activeUserIndex < end {
			index := prepared.activeUserIndex
			next.RetainedUserFrom = &index
		}
		return next
	}
	proposal, err := prepared.definition.Compaction.Plan(ctx, agentcompaction.CompactionPlanRequest{
		Session: session, Run: run, Groups: groups, RetainedBytes: retainedBytes,
		EstimateAfter: func(count int) (agentmodel.InputSize, error) {
			if count <= 0 || count > len(ends) || buildAfter == nil {
				return agentmodel.InputSize{}, errors.New("Compaction estimate requires an eligible group prefix and request projection")
			}
			next := recordThrough(ends[count-1])
			next.Summary = agenthistory.MergeProtectedReceiptContext("", current.Summary, compactionReceiptMessages(contextMessages[prepared.archive.Local(next.ReplacementFrom):prepared.archive.Local(next.ReplacementTo)], modelSnapshot), summaryLimit)
			after, err := buildAfter(next)
			if err != nil {
				return agentmodel.InputSize{}, err
			}
			return after.EstimateInput()
		},
		ModelSnapshot: modelSnapshot, LifecycleReservedTokens: prepared.goalReservedTokens,
		Force:   request.Force || present && len(current.Summary) > summaryLimit,
		Current: agenthistory.CompactionStatePointer(current, present),
	})
	plan := agenthistory.CompactionExecutionPlan{CompactionPlan: proposal}
	if present && !current.Removed {
		plan.SourceFrom = current.ReplacementFrom
	}
	if proposal.GroupCount > 0 && proposal.GroupCount <= len(ends) {
		plan.SourceTo = ends[proposal.GroupCount-1]
	}
	if err != nil {
		return agenthistory.CompactionRecord{}, false, agenthistory.CompactionMetrics{}, err
	}
	if plan.Action == agenthistory.CompactionNone {
		if onSkip != nil && strings.TrimSpace(plan.SkippedReason) != "" {
			if err := onSkip(plan.SkippedReason, plan.Metrics); err != nil {
				return agenthistory.CompactionRecord{}, false, plan.Metrics, err
			}
		}
		return current, false, plan.Metrics, nil
	}
	if plan.Action != agenthistory.CompactionCreate || plan.SourceFrom < 0 || plan.SourceTo <= plan.SourceFrom || plan.SourceTo > prepared.archive.Count(messages) {
		return agenthistory.CompactionRecord{}, false, plan.Metrics, errors.New("Compaction Manager returned an invalid source range")
	}
	wantHash, err := agentschema.HashCanonical(messages[prepared.archive.Local(plan.SourceFrom):prepared.archive.Local(plan.SourceTo)])
	if err != nil {
		return agenthistory.CompactionRecord{}, false, plan.Metrics, err
	}
	if onCreate != nil {
		if err := onCreate(plan.Metrics); err != nil {
			return agenthistory.CompactionRecord{}, false, plan.Metrics, err
		}
	}
	checkpoint, err := prepared.definition.Compaction.Compact(ctx, agentcompaction.CompactionCompactRequest{
		Session: session, Run: run,
		Messages:      prepared.archive.CompactionIncrementalSource(contextMessages, plan, current, present, summaryLimit),
		ModelSnapshot: modelSnapshot, Current: agenthistory.CompactionStatePointer(current, present),
	})
	if err != nil {
		return agenthistory.CompactionRecord{}, false, plan.Metrics, err
	}
	if err := ctx.Err(); err != nil {
		return agenthistory.CompactionRecord{}, false, plan.Metrics, err
	}
	checkpoint.Summary = agenthistory.MergeProtectedReceiptContext(checkpoint.Summary, current.Summary, compactionReceiptMessages(contextMessages[prepared.archive.Local(plan.SourceFrom):prepared.archive.Local(plan.SourceTo)], modelSnapshot), summaryLimit)
	checkpoint.Summary = strings.TrimSpace(checkpoint.Summary)
	if checkpoint.Summary == "" {
		return agenthistory.CompactionRecord{}, false, plan.Metrics, errors.New("Compaction Manager returned an invalid checkpoint")
	}
	if len(checkpoint.Summary) > summaryLimit {
		return agenthistory.CompactionRecord{}, false, plan.Metrics, fmt.Errorf("%w: Compaction checkpoint is %d bytes and exceeds the target Agent summary limit %d", agentschema.ErrContextLimit, len(checkpoint.Summary), summaryLimit)
	}
	if err := agenthistory.ValidateCompactionContextData(checkpoint.ContextData); err != nil {
		return agenthistory.CompactionRecord{}, false, plan.Metrics, err
	}
	next := recordThrough(plan.SourceTo)
	next.SourceHash = wantHash
	next.Summary, next.SummaryTokenEstimate = checkpoint.Summary, agentmodel.EstimateTextTokens(checkpoint.Summary)
	next.ContextData = agentschema.CloneHostData(checkpoint.ContextData)
	if modelSnapshot == nil || buildAfter == nil {
		return agenthistory.CompactionRecord{}, false, plan.Metrics, errors.New("Compaction requires exact before and after model request snapshots")
	}
	after, err := buildAfter(next)
	if err != nil {
		return agenthistory.CompactionRecord{}, false, plan.Metrics, fmt.Errorf("rebuild post-Compaction model request: %w", err)
	}
	metrics, err := validateCompactionProjection(modelSnapshot, after, plan)
	if err != nil {
		return agenthistory.CompactionRecord{}, false, metrics, err
	}
	if err := ctx.Err(); err != nil {
		return agenthistory.CompactionRecord{}, false, metrics, err
	}
	next.Metrics = metrics
	next.TokenEstimate = metrics.ProjectedTokensAfter
	return next, true, metrics, nil
}

func validateCompactionProjection(before, after *agentmodel.ModelRequestSnapshot, plan agenthistory.CompactionExecutionPlan) (agenthistory.CompactionMetrics, error) {
	metrics := plan.Metrics
	if before == nil || after == nil {
		return metrics, errors.New("Compaction validation requires exact before and after model request snapshots")
	}
	policy := plan.Validation
	if policy.ReservedTokens < 0 || policy.ContextWindowTokens < 0 || policy.HardLimitBytes < 0 {
		return metrics, errors.New("Compaction validation policy contains negative limits")
	}
	beforeMessages, afterMessages := before.Messages(), after.Messages()
	beforeSize, err := before.EstimateInput()
	if err != nil {
		return metrics, err
	}
	afterSize, err := after.EstimateInput()
	if err != nil {
		return metrics, err
	}
	beforeTokens, afterTokens := beforeSize.Tokens, afterSize.Tokens
	metrics.EstimatedTokensBefore = beforeTokens
	metrics.EstimatedTokensAfter = afterTokens
	metrics.ReservedTokens = policy.ReservedTokens
	metrics.ProjectedTokensBefore = metrics.CalibratedTokens(beforeTokens) + policy.ReservedTokens
	metrics.ProjectedTokensAfter = metrics.CalibratedTokens(afterTokens) + policy.ReservedTokens
	metrics.ContextWindowTokens = policy.ContextWindowTokens
	metrics.Threshold = policy.Threshold
	metrics.RecoveryBand = policy.RecoveryBand
	metrics.MessageCountBefore = len(beforeMessages)
	metrics.MessageCountAfter = len(afterMessages)
	metrics.SourceMessageCount = plan.SourceTo - plan.SourceFrom
	metrics.StablePrefixTokens, err = stableSnapshotTokens(after)
	if err != nil {
		return metrics, err
	}
	metrics.CacheExpectedPrefixTokens, err = stableSnapshotTokens(before)
	if err != nil {
		return metrics, err
	}
	metrics.CandidateFingerprint, metrics.CandidateGeneration = compactionCandidateIdentity(afterMessages)
	if policy.HardLimitBytes > 0 && afterSize.Bytes > policy.HardLimitBytes {
		return metrics, fmt.Errorf("%w: post-Compaction request exceeds the %d-byte provider input limit", agentschema.ErrContextLimit, policy.HardLimitBytes)
	}
	progress := metrics.ProjectedTokensBefore - metrics.ProjectedTokensAfter
	if progress <= 0 {
		return metrics, fmt.Errorf("Compaction made no progress: before=%d after=%d", metrics.ProjectedTokensBefore, metrics.ProjectedTokensAfter)
	}
	if policy.MinimumChangeTokens > 0 && progress < policy.MinimumChangeTokens {
		return metrics, fmt.Errorf("Compaction progress %d tokens is below the required minimum %d", progress, policy.MinimumChangeTokens)
	}
	if policy.ContextWindowTokens == 0 {
		return metrics, nil
	}
	if policy.Threshold <= 0 || policy.Threshold >= 1 || policy.RecoveryBand <= 0 || policy.RecoveryBand > 1 {
		return metrics, errors.New("Compaction validation requires threshold and recovery band within (0,1)")
	}
	publishLimit := int(float64(policy.ContextWindowTokens) * policy.Threshold)
	metrics.RecoveryTargetTokens = int(float64(publishLimit) * policy.RecoveryBand)
	metrics.RecoveryBandMet = metrics.ProjectedTokensAfter <= metrics.RecoveryTargetTokens
	metrics.Degraded = !metrics.RecoveryBandMet && metrics.ProjectedTokensAfter < publishLimit
	if metrics.ProjectedTokensAfter >= publishLimit {
		return metrics, fmt.Errorf("%w: post-Compaction request remains above hard publish band: after=%d limit=%d", agentschema.ErrContextLimit, metrics.ProjectedTokensAfter, publishLimit)
	}
	return metrics, nil
}

func stableSnapshotTokens(snapshot *agentmodel.ModelRequestSnapshot) (int, error) {
	if snapshot == nil {
		return 0, nil
	}
	messages := snapshot.Messages()
	boundary := min(snapshot.StablePrefixMessages(), len(messages))
	size, err := snapshot.WithMessages(messages[:boundary]).EstimateInput()
	return size.Tokens, err
}

func compactionCandidateIdentity(messages []*agentschema.Message) (string, uint64) {
	type candidate struct {
		Index int
		Call  string
		Tool  string
		Bytes int
	}
	values := make([]candidate, 0)
	for index, message := range messages {
		if message != nil && message.Role == agentschema.ToolRole {
			values = append(values, candidate{index, message.ToolCallID, message.ToolName, len(message.Content)})
		}
	}
	fingerprint, _ := agentschema.HashCanonical(values)
	return fingerprint, uint64(len(values))
}

func runtimeCompactionMetrics(metrics agenthistory.CompactionMetrics) CompactionMetrics {
	return CompactionMetrics{
		EstimatedTokensBefore:     metrics.EstimatedTokensBefore,
		ObservedPromptTokens:      metrics.ObservedPromptTokens,
		ObservedEstimateTokens:    metrics.ObservedEstimateTokens,
		EstimatedTokensAfter:      metrics.EstimatedTokensAfter,
		ProjectedTokensBefore:     metrics.ProjectedTokensBefore,
		ProjectedTokensAfter:      metrics.ProjectedTokensAfter,
		ReservedTokens:            metrics.ReservedTokens,
		ContextWindowTokens:       metrics.ContextWindowTokens,
		Threshold:                 metrics.Threshold,
		RecoveryBand:              metrics.RecoveryBand,
		RecoveryTargetTokens:      metrics.RecoveryTargetTokens,
		RecoveryBandMet:           metrics.RecoveryBandMet,
		Degraded:                  metrics.Degraded,
		StablePrefixTokens:        metrics.StablePrefixTokens,
		SourceMessageCount:        metrics.SourceMessageCount,
		MessageCountBefore:        metrics.MessageCountBefore,
		MessageCountAfter:         metrics.MessageCountAfter,
		CacheExpectedPrefixTokens: metrics.CacheExpectedPrefixTokens,
		CacheReadTokens:           metrics.CacheReadTokens,
		CandidateFingerprint:      metrics.CandidateFingerprint,
		CandidateGeneration:       metrics.CandidateGeneration,
	}
}

func compactionID(operationID OperationID) string {
	return "compaction-" + string(operationID)
}

func (engine *Engine) prepareAutomaticCompaction(
	ctx context.Context,
	request Request,
	prepared preparedDefinition,
	messages []*agentschema.Message,
	contextMessages []*agentschema.Message,
	modelSnapshot *agentmodel.ModelRequestSnapshot,
	current agenthistory.CompactionRecord,
	present bool,
	storage agenthistory.CompactionRecord,
	buildAfter func(agenthistory.CompactionRecord) (*agentmodel.ModelRequestSnapshot, error),
	emit EventSink,
) (agenthistory.CompactionRecord, bool, bool, agenthistory.CompactionMetrics, error) {
	if prepared.definition.Compaction == nil {
		return current, present, false, agenthistory.CompactionMetrics{}, nil
	}
	checkpointID := fmt.Sprintf("compaction-%s-%d-%d", request.Snapshot.OperationID, request.Snapshot.Cycle, max(current.Revision, storage.Revision)+1)
	next, changed, metrics, err := executeCompaction(
		ctx, prepared,
		agentschema.SessionView{Key: engine.key, Revision: uint64(request.Snapshot.ContextCursor)},
		runViewForTurn(request.Snapshot),
		messages, contextMessages, request.Snapshot.Input.Text, current, present, storage.Revision,
		agentcompaction.CompactionRequest{},
		checkpointID, modelSnapshot, buildAfter, func(reason string, metrics agenthistory.CompactionMetrics) error {
			if reason != "degraded_no_progress_latch" {
				return nil
			}
			return emit(CompactionSkipped{
				ID: checkpointID, Reason: reason, Automatic: true, Metrics: runtimeCompactionMetrics(metrics),
			})
		}, func(metrics agenthistory.CompactionMetrics) error {
			return emit(CompactionStarted{
				ID: checkpointID, Automatic: true, Metrics: runtimeCompactionMetrics(metrics),
			})
		},
	)
	if err != nil {
		return agenthistory.CompactionRecord{}, false, false, metrics, err
	}
	if !changed {
		return current, present, false, metrics, nil
	}
	return next, true, true, metrics, nil
}

func automaticCompactionFingerprint(
	prepared preparedDefinition,
	current agenthistory.CompactionRecord,
	present bool,
	snapshot *agentmodel.ModelRequestSnapshot,
) (string, error) {
	if snapshot == nil {
		return "", errors.New("automatic Compaction requires a final model request snapshot")
	}
	candidateFingerprint, candidateGeneration := compactionCandidateIdentity(snapshot.Messages())
	return agentschema.HashCanonical(struct {
		Model                agentschema.CapabilityIdentity
		Manager              agentschema.CapabilityIdentity
		PrefixFingerprint    string
		Options              *agentmodel.Options
		Compaction           *agenthistory.CompactionState
		CandidateFingerprint string
		CandidateGeneration  uint64
		ClearRevision        uint64
	}{
		Model: prepared.definition.ModelIdentity, Manager: prepared.definition.Compaction.Identity(),
		PrefixFingerprint: prepared.prefixFingerprint, Options: snapshot.ResolvedOptions(),
		Compaction:           agenthistory.CompactionStatePointer(current, present),
		CandidateFingerprint: candidateFingerprint, CandidateGeneration: candidateGeneration,
		ClearRevision: prepared.clearRevision,
	})
}

func compactionHealthStateFrom(states map[string]json.RawMessage) (compactionHealthState, bool, error) {
	raw, present := states[agenthistory.CompactionHealthCapability]
	if !present {
		return compactionHealthState{}, false, nil
	}
	var health compactionHealthState
	if err := json.Unmarshal(raw, &health); err != nil {
		return compactionHealthState{}, false, fmt.Errorf("decode Compaction health: %w", err)
	}
	if strings.TrimSpace(health.Fingerprint) == "" || health.ConsecutiveFailures <= 0 {
		return compactionHealthState{}, false, errors.New("durable Compaction health state is invalid")
	}
	return health, true, nil
}

func nextCompactionHealth(previous compactionHealthState, present bool, fingerprint string, failure error) compactionHealthState {
	consecutive := 1
	if present && previous.Fingerprint == fingerprint {
		consecutive = previous.ConsecutiveFailures + 1
	}
	reason := strings.TrimSpace(failure.Error())
	if len(reason) > 512 {
		reason = reason[:512]
	}
	return compactionHealthState{
		Fingerprint: fingerprint, ConsecutiveFailures: consecutive, FailureCode: reason,
	}
}

func emitCompactionHealth(
	emit EventSink,
	health compactionHealthState,
) error {
	encoded, err := json.Marshal(health)
	if err != nil {
		return err
	}
	return emit(CapabilityState{
		Capability: agenthistory.CompactionHealthCapability, State: encoded,
	})
}

func clearCompactionHealth(emit EventSink, present bool) error {
	if !present {
		return nil
	}
	return emit(CapabilityState{
		Capability: agenthistory.CompactionHealthCapability, Delete: true,
	})
}

var _ StructuralRunner = (*Engine)(nil)

// Protect only receipts present in the final provider projection. A host may
// hide tool exchanges through middleware; those must not reappear in a summary.
func compactionReceiptMessages(source []*agentschema.Message, snapshot *agentmodel.ModelRequestSnapshot) []*agentschema.Message {
	visible := make(map[string]*agentschema.Message)
	for _, message := range snapshot.Messages() {
		if message != nil && message.Role == agentschema.ToolRole {
			visible[message.ToolCallID] = message
		}
	}
	var result []*agentschema.Message
	for _, message := range source {
		if message != nil && message.Role == agentschema.ToolRole {
			if projected := visible[message.ToolCallID]; projected != nil {
				result = append(result, projected)
			}
		}
	}
	return result
}

func defaultCacheKey(key agentsession.Key) (string, error) {
	canonical, err := agentsession.CanonicalKey(key)
	if err != nil {
		return "", err
	}
	digest, err := agentschema.HashCanonical(struct {
		Version uint16
		Session string
	}{1, canonical})
	if err != nil {
		return "", err
	}
	return "agent-" + digest[:32], nil
}
