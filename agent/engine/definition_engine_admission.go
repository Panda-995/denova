package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentgoal "github.com/alfredxw/denova/agent/engine/goal"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

func (engine *Engine) canonicalInputRequest(
	request TurnSnapshot,
) (PrepareRequest, agentschema.Input, error) {
	input, err := DecodeInput(request.Input)
	if err != nil {
		return PrepareRequest{}, agentschema.Input{}, err
	}
	input.IdempotencyKey = string(request.CommandID)
	reason, err := turnReasonForSnapshot(request)
	if err != nil {
		return PrepareRequest{}, agentschema.Input{}, err
	}
	return PrepareRequest{
		Session:  agentschema.SessionView{Key: engine.key, Revision: uint64(request.ContextCursor)},
		Run:      runViewForTurn(request),
		Input:    input,
		Reason:   reason,
		HostData: agentschema.CloneHostData(input.HostData),
	}, input, nil
}

// PlanInputMaterialization resolves the product canonical-input adapter before
// the Definition is fully prepared.
func (engine *Engine) PlanInputMaterialization(
	ctx context.Context,
	request InputMaterializationRequest,
) (InputMaterializationPlan, error) {
	prepare, input, err := engine.canonicalInputRequest(request.Snapshot)
	if err != nil {
		return InputMaterializationPlan{}, err
	}
	adapter, err := engine.source.CanonicalInput(ctx, prepare)
	if err != nil {
		return InputMaterializationPlan{}, fmt.Errorf("resolve canonical Agent input: %w", err)
	}
	if adapter == nil {
		return InputMaterializationPlan{}, nil
	}
	hash, err := canonicalInputHash(input, adapter.Identity())
	if err != nil {
		return InputMaterializationPlan{}, err
	}
	return InputMaterializationPlan{Required: true, Hash: hash}, nil
}

func (engine *Engine) MaterializeInput(
	ctx context.Context,
	request InputMaterializationRequest,
	plan InputMaterializationPlan,
) (InputMaterializationReceipt, error) {
	prepare, input, err := engine.canonicalInputRequest(request.Snapshot)
	if err != nil {
		return InputMaterializationReceipt{}, err
	}
	adapter, err := engine.source.CanonicalInput(ctx, prepare)
	if err != nil {
		return InputMaterializationReceipt{}, fmt.Errorf("resolve canonical Agent input: %w", err)
	}
	if adapter == nil || !plan.Required {
		return InputMaterializationReceipt{}, errors.New("canonical Agent input materializer is unavailable")
	}
	want, err := canonicalInputHash(input, adapter.Identity())
	if err != nil {
		return InputMaterializationReceipt{}, err
	}
	if plan.Hash != want {
		return InputMaterializationReceipt{}, fmt.Errorf("%w: canonical Agent input changed after planning", ErrDomainCommitRejected)
	}
	identity := canonicalCommitIdentity(engine.key, request.Snapshot, agentcanonical.CommitInput)
	var receipt agentcanonical.CommitReceipt
	err = commitCheckpoint(ctx, request.Journal, CanonicalUpdate{Stage: agentcanonical.CommitInput, Snapshot: request.Snapshot, Hash: want}, func(checkpoint agentcanonical.CanonicalCheckpoint) error {
		var err error
		receipt, err = adapter.MaterializeInput(ctx, agentcanonical.InputCommitRequest{Identity: identity, Hash: want, Input: input, Checkpoint: checkpoint})
		return err
	})
	if err != nil {
		return InputMaterializationReceipt{}, fmt.Errorf("materialize canonical Agent input: %w", err)
	}
	revision := strings.TrimSpace(receipt.Revision)
	if revision == "" {
		return InputMaterializationReceipt{}, errors.New("materialize canonical Agent input returned an empty revision")
	}
	return InputMaterializationReceipt{Revision: revision}, nil
}

func canonicalInputHash(input agentschema.Input, adapter agentschema.CapabilityIdentity) (string, error) {
	if err := adapter.Validate("Canonical"); err != nil {
		return "", err
	}
	return agentschema.HashCanonical(struct {
		Version uint16
		Adapter agentschema.CapabilityIdentity
		Input   agentschema.Input
	}{Version: 2, Adapter: adapter, Input: input})
}

// verifyCanonicalInputCommit proves that admission and the fully prepared
// Definition describe the same canonical boundary.
func (engine *Engine) verifyCanonicalInputCommit(
	snapshot TurnSnapshot,
	input agentschema.Input,
	adapter agentcanonical.CanonicalAdapter,
) error {
	commit := snapshot.InputCommit
	if adapter == nil {
		if commit != nil {
			return fmt.Errorf("%w: canonical Agent input was committed but the prepared Definition has no Canonical Adapter", agentschema.ErrDefinitionMismatch)
		}
		return nil
	}
	if commit == nil {
		return fmt.Errorf("%w: prepared Definition requires a canonical Agent input receipt", ErrDomainCommitRejected)
	}
	wantIdentity := engineCommitIdentity(canonicalCommitIdentity(engine.key, snapshot, agentcanonical.CommitInput))
	if commit.Identity != wantIdentity {
		return fmt.Errorf("%w: canonical Agent input receipt identity does not match the active cycle", ErrDomainCommitRejected)
	}
	wantHash, err := canonicalInputHash(input, adapter.Identity())
	if err != nil {
		return err
	}
	if commit.Hash != wantHash || strings.TrimSpace(commit.Revision) == "" {
		return fmt.Errorf("%w: canonical Agent input receipt does not match the prepared Definition", agentschema.ErrDefinitionMismatch)
	}
	return nil
}

type committedOutput struct {
	output            *agentschema.Message
	canonicalMessages []*agentschema.Message
}

// resumeCommittedOutput uses the product's authoritative final projection.
// Only post-output Goal evaluation and normal settlement remain after this
// boundary; calling the model or CommitOutput again could create a new result.
func (engine *Engine) resumeCommittedOutput(ctx context.Context, request Request, input agentschema.Input, prepared preparedDefinition, state engineTranscript, emit EventSink) (Result, error) {
	commit := request.Snapshot.OutputCommit
	if commit.Identity != engineCommitIdentity(canonicalCommitIdentity(engine.key, request.Snapshot, agentcanonical.CommitOutput)) || commit.Revision == "" {
		return Result{}, errors.New("persisted output commit does not belong to this Agent cycle")
	}
	return engine.settleCommittedOutput(ctx, request, input, prepared, state, emit)
}

func (engine *Engine) settleCommittedOutput(ctx context.Context, request Request, input agentschema.Input, prepared preparedDefinition, state engineTranscript, emit EventSink) (Result, error) {
	if len(state.Messages) == 0 {
		return Result{}, errors.New("committed Agent output is missing its canonical messages")
	}
	final := state.Messages[len(state.Messages)-1]
	if final.Role != agentschema.Assistant || len(final.ToolCalls) != 0 {
		return Result{}, errors.New("committed Agent output is missing a final assistant message")
	}
	_, finish := agentmodel.ClassifyResponseFinishReason(final.ResponseMeta)
	var continuation *Continuation
	var err error
	if !finish.Incomplete() {
		continuation, err = engine.evaluateGoal(ctx, request, input, prepared, newCapabilityStateClient(request.Snapshot.Capabilities, emit), nil, final, emit)
		if err != nil {
			return Result{}, err
		}
	}
	encoded, err := encodeEngineTranscript(prepared, state.Messages)
	if err != nil {
		return Result{}, err
	}
	if err := emit(AssistantFinal{Content: final.Content, Thinking: final.ReasoningContent, State: encoded, Continuation: continuation}); err != nil {
		return Result{}, err
	}
	if finish.Incomplete() {
		return Result{Status: Incomplete, Reason: finish.TerminalReason()}, nil
	}
	return Result{Status: Completed}, nil
}

func (engine *Engine) commitCanonicalOutput(
	ctx context.Context,
	request Request,
	message *agentschema.Message,
	messages []*agentschema.Message,
	activeUserIndex int,
	adapter agentcanonical.CanonicalAdapter,
) (committedOutput, error) {
	if adapter == nil {
		return committedOutput{output: agentschema.CloneMessage(message)}, nil
	}
	identity := canonicalCommitIdentity(engine.key, request.Snapshot, agentcanonical.CommitOutput)
	hash, err := agentschema.HashCanonical(struct {
		Version uint16
		Message agentschema.Message
	}{Version: 1, Message: *agentschema.CloneMessage(message)})
	if err != nil {
		return committedOutput{}, err
	}
	var receipt agentcanonical.OutputCommitReceipt
	err = commitCheckpoint(ctx, request.Journal, CanonicalUpdate{Stage: agentcanonical.CommitOutput, Snapshot: request.Snapshot, Hash: hash}, func(checkpoint agentcanonical.CanonicalCheckpoint) error {
		var err error
		receipt, err = adapter.CommitOutput(ctx, agentcanonical.OutputCommitRequest{Identity: identity, Hash: hash, Message: *agentschema.CloneMessage(message), ContextMessages: agentschema.CloneMessages(messages), ActiveUserIndex: activeUserIndex, Checkpoint: checkpoint})
		return err
	})
	if err != nil {
		return committedOutput{}, fmt.Errorf("commit canonical Agent output: %w", err)
	}
	receipt.Revision = strings.TrimSpace(receipt.Revision)
	if receipt.Revision == "" {
		return committedOutput{}, errors.New("commit canonical Agent output returned an empty revision")
	}
	effective := agentschema.CloneMessage(message)
	if receipt.Transcript != nil {
		effective.Content = receipt.Transcript.Content
		effective.ReasoningContent = receipt.Transcript.Thinking
	}
	var canonicalMessages []*agentschema.Message
	if receipt.Transcript != nil && receipt.Transcript.ContextMessages != nil {
		canonicalMessages = receipt.Transcript.ContextMessages
		if len(canonicalMessages) != len(messages) {
			return committedOutput{}, errors.New("canonical output projection changed active history coordinates")
		}
		if err := agentcanonical.ValidateImportedTranscript(canonicalMessages); err != nil {
			return committedOutput{}, fmt.Errorf("invalid canonical output transcript: %w", err)
		}
		if len(canonicalMessages) == 0 || canonicalMessages[len(canonicalMessages)-1].Role != agentschema.Assistant {
			return committedOutput{}, errors.New("canonical output transcript requires a final assistant message")
		}
	}
	return committedOutput{output: effective, canonicalMessages: canonicalMessages}, nil
}

func (engine *Engine) commitCanonicalContext(
	ctx context.Context,
	request Request,
	adapter agentcanonical.CanonicalAdapter,
	sequence int,
	messages []*agentschema.Message,
	checkpointState TranscriptUpdated,
) error {
	contextAdapter, ok := adapter.(agentcanonical.CanonicalContextAdapter)
	if !ok || len(messages) == 0 {
		return nil
	}
	if err := agentcanonical.ValidateContextCommitMessages(messages); err != nil {
		return err
	}
	values := make([]agentschema.Message, len(messages))
	for index, message := range messages {
		if message == nil {
			return fmt.Errorf("canonical context message %d is nil", index)
		}
		values[index] = *message.Clone()
	}
	var receipt agentcanonical.CommitReceipt
	err := commitCheckpoint(ctx, request.Journal, CanonicalUpdate{Stage: agentcanonical.CommitContext, Snapshot: request.Snapshot, State: checkpointState.State, CapabilityStates: checkpointState.CapabilityStates}, func(checkpoint agentcanonical.CanonicalCheckpoint) error {
		var err error
		receipt, err = contextAdapter.CommitContext(ctx, agentcanonical.ContextCommitRequest{
			Identity: canonicalCommitIdentity(engine.key, request.Snapshot, agentcanonical.CommitContext), Sequence: sequence, Messages: values, Checkpoint: checkpoint,
		})
		return err
	})
	if err != nil {
		return fmt.Errorf("commit canonical Agent context: %w", err)
	}
	if strings.TrimSpace(receipt.Revision) == "" {
		return errors.New("commit canonical Agent context returned an empty revision")
	}
	return nil
}

func canonicalCommitIdentity(key agentsession.Key, snapshot TurnSnapshot, stage agentcanonical.CommitStage) agentcanonical.CommitIdentity {
	return agentcanonical.CommitIdentity{
		Session: key, CommandID: string(snapshot.CommandID), RunID: string(snapshot.OperationID),
		Cycle: snapshot.Cycle, Stage: stage,
	}
}

func engineCommitIdentity(identity agentcanonical.CommitIdentity) DomainCommitIdentity {
	stage := DomainCommitInput
	if identity.Stage == agentcanonical.CommitOutput {
		stage = DomainCommitOutput
	}
	return DomainCommitIdentity{
		CommandID: CommandID(identity.CommandID), OperationID: OperationID(identity.RunID),
		Cycle: identity.Cycle, Stage: stage,
	}
}

func (engine *Engine) ResolveInteraction(
	ctx context.Context,
	request InteractionResolveRequest,
) (json.RawMessage, error) {
	input, err := DecodeInput(request.Snapshot.Input)
	if err != nil {
		return nil, err
	}
	input.IdempotencyKey = string(request.Snapshot.CommandID)
	transcript, err := decodeEngineTranscript(request.Snapshot.State)
	if err != nil {
		return nil, err
	}
	currentCompaction, currentCompactionPresent, err := agenthistory.CompactionStateFrom(request.Snapshot.Capabilities)
	if err != nil {
		return nil, err
	}
	clearState, clearPresent, err := agenthistory.ClearStateFrom(request.Snapshot.Capabilities)
	if err != nil {
		return nil, err
	}
	currentCompaction, currentCompactionPresent = agenthistory.ClearCompaction(
		currentCompaction, currentCompactionPresent, clearState, clearPresent,
	)
	compaction := agenthistory.CompactionStatePointer(currentCompaction, currentCompactionPresent)
	prepared, err := prepareDefinitionBase(ctx, engine.source, PrepareRequest{
		Session: agentschema.SessionView{Key: engine.key, Revision: uint64(request.Snapshot.ContextCursor)},
		Run:     runViewForTurn(request.Snapshot),
		Input:   input, Reason: TurnReasonInteraction,
		DefinitionKey: transcript.DefinitionKey, BehaviorKey: transcript.BehaviorKey,
		HostData:   agentschema.CloneHostData(input.HostData),
		Compaction: compaction,
	})
	if err != nil {
		return nil, err
	}
	if transcript.DefinitionKey != "" && transcript.DefinitionKey != prepared.definitionKey {
		return nil, fmt.Errorf("%w: interaction Definition changed", agentschema.ErrDefinitionMismatch)
	}
	if transcript.BehaviorKey != "" && transcript.BehaviorKey != prepared.behaviorKey {
		return nil, fmt.Errorf("%w: interaction behavior identity changed", agentschema.ErrDefinitionMismatch)
	}
	var interactionHeader struct {
		ID         string                                   `json:"id"`
		Kind       agentinteraction.InteractionKind         `json:"kind"`
		Permission *agentinteraction.PermissionPresentation `json:"permission,omitempty"`
	}
	if err := json.Unmarshal(request.Interaction.Request, &interactionHeader); err != nil {
		return nil, fmt.Errorf("decode Interaction request header: %w", err)
	}
	if interactionHeader.ID != request.Interaction.ID {
		return nil, agentschema.ErrInteractionStale
	}
	var response agentinteraction.InteractionResponse
	if err := json.Unmarshal(request.Response, &response); err != nil {
		return nil, fmt.Errorf("decode Interaction response: %w", err)
	}
	policy := agentinteraction.EffectiveInteractionPolicy(prepared.definition.Interaction)
	if interactionHeader.Kind == agentinteraction.InteractionAsk && agentinteraction.IsStandardInteractionPolicy(policy) {
		resolution, resolveErr := agentinteraction.ResolvePersistedStandardAsk(request.Interaction.Request, response)
		if resolveErr != nil {
			return nil, resolveErr
		}
		encoded, encodeErr := json.Marshal(resolution)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode Interaction resolution: %w", encodeErr)
		}
		return encoded, nil
	}
	prepareRequest := PrepareRequest{
		Session: agentschema.SessionView{Key: engine.key, Revision: uint64(request.Snapshot.ContextCursor)},
		Run:     runViewForTurn(request.Snapshot), Input: input, Reason: TurnReasonInteraction,
		DefinitionKey: transcript.DefinitionKey, BehaviorKey: transcript.BehaviorKey,
		HostData:   agentschema.CloneHostData(input.HostData),
		Compaction: compaction,
	}
	// New approvals carry the exact tool contract in the owning journal. The
	// behavior fence above still checks policy and implementation identities;
	// mutable context is not part of the user's authorization. Old approvals
	// and custom Ask policies retain their full fence, restoring accepted context
	// when the checkpoint contains it and rematerializing legacy checkpoints.
	toolBound := interactionHeader.Kind == agentinteraction.InteractionPermission && interactionHeader.Permission != nil &&
		interactionHeader.Permission.ToolDefinitionHash != ""
	if toolBound {
		if err := materializeDefinitionTools(ctx, prepareRequest, &prepared); err != nil {
			return nil, err
		}
		if err := engine.applyGoalPreparation(ctx, Request{Snapshot: request.Snapshot}, &prepared); err != nil {
			return nil, err
		}
	} else if err := engine.materializeCycleCapabilities(ctx, prepareRequest, request.Snapshot, transcript.PreparedContext, &prepared); err != nil {
		return nil, err
	}
	if !toolBound {
		materialized, err := materializedDefinitionFingerprint(prepared)
		if err != nil {
			return nil, err
		}
		if transcript.PreparationStage == enginePreparationMaterialized &&
			transcript.MaterializedFingerprint != materialized {
			return nil, fmt.Errorf(
				"%w: interaction materialized Definition changed (previous=%s current=%s)",
				agentschema.ErrDefinitionMismatch, transcript.MaterializedFingerprint, materialized,
			)
		}
	}
	var interactionRequest agentinteraction.InteractionRequest
	if err := json.Unmarshal(request.Interaction.Request, &interactionRequest); err != nil {
		return nil, fmt.Errorf("decode Interaction request: %w", err)
	}
	if interactionRequest.ID != request.Interaction.ID {
		return nil, agentschema.ErrInteractionStale
	}
	var descriptor agenttool.ToolDescriptor
	if interactionRequest.Kind == agentinteraction.InteractionPermission {
		presentation := interactionRequest.Permission
		if presentation == nil {
			return nil, errors.New("Permission Interaction has no presentation")
		}
		found := false
		for _, tool := range prepared.toolSnapshots {
			if tool.Info.Name == presentation.Tool {
				if toolBound {
					hash, err := agentschema.HashCanonical(tool)
					if err != nil {
						return nil, err
					}
					if hash != presentation.ToolDefinitionHash {
						return nil, fmt.Errorf("%w: permission tool %q changed", agentschema.ErrDefinitionMismatch, presentation.Tool)
					}
				}
				descriptor, found = tool.Descriptor, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: permission tool %q is unavailable", agentschema.ErrDefinitionMismatch, presentation.Tool)
		}
	}
	resolution, err := policy.Resolve(ctx, interactionRequest, response)
	if err != nil {
		return nil, err
	}
	if interactionRequest.Kind == agentinteraction.InteractionPermission {
		presentation := interactionRequest.Permission
		if resolution.Cancelled {
			// Cancellation is never authorization and has no policy-owned work to
			// persist. In particular, do not call a custom policy with an empty
			// choice: a cancelled UI response must not accidentally enter its
			// remember path.
			resolution.Permission = agentinteraction.PermissionDeny
			encoded, encodeErr := json.Marshal(resolution)
			if encodeErr != nil {
				return nil, fmt.Errorf("encode cancelled Permission resolution: %w", encodeErr)
			}
			return encoded, nil
		}
		if resolution.Permission == agentinteraction.PermissionRemember && !presentation.CanRemember {
			return nil, errors.New("Permission Interaction cannot remember this request")
		}
		resolved, resolveErr := agentpermission.EffectivePermissionPolicy(prepared.definition.Permission).Resolve(ctx, agentpermission.PermissionResolveRequest{
			Request: agentpermission.PermissionRequest{
				Session: agentschema.SessionView{Key: engine.key, Revision: uint64(request.Snapshot.ContextCursor)},
				Run:     runViewForTurn(request.Snapshot),
				CallID:  presentation.CallID, Tool: presentation.Tool,
				Arguments: append(json.RawMessage(nil), presentation.Arguments...), Descriptor: descriptor,
			},
			Resolution: resolution,
		})
		if resolveErr != nil {
			return nil, resolveErr
		}
		switch resolution.Permission {
		case agentinteraction.PermissionAllowOnce:
			if !resolved.Allowed || resolved.Remembered {
				return nil, errors.New("Permission Policy returned an inconsistent allow-once decision")
			}
		case agentinteraction.PermissionRemember:
			if !resolved.Allowed || !resolved.Remembered {
				return nil, errors.New("Permission Policy returned success before the remembered rule was durable")
			}
		case agentinteraction.PermissionDeny:
			if resolved.Allowed || resolved.Remembered {
				return nil, errors.New("Permission Policy returned an inconsistent deny decision")
			}
		default:
			return nil, errors.New("Permission Policy received an invalid resolution")
		}
	}
	encoded, err := json.Marshal(resolution)
	if err != nil {
		return nil, fmt.Errorf("encode Interaction resolution: %w", err)
	}
	return encoded, nil
}

func (engine *Engine) PrepareAdmission(
	ctx context.Context,
	request TurnAdmissionRequest,
) ([]CapabilityState, error) {
	input, err := DecodeInput(request.Snapshot.Input)
	if err != nil || input.Goal == nil {
		return nil, err
	}
	input.IdempotencyKey = string(request.Snapshot.CommandID)
	transcript, err := decodeEngineTranscript(request.Snapshot.State)
	if err != nil {
		return nil, err
	}
	reason, err := turnReasonForSnapshot(request.Snapshot)
	if err != nil {
		return nil, err
	}
	prepared, err := prepareDefinitionBase(ctx, engine.source, PrepareRequest{
		Session: agentschema.SessionView{Key: engine.key, Revision: uint64(request.Snapshot.ContextCursor)},
		Run:     runViewForTurn(request.Snapshot),
		Input:   input, Reason: reason,
		DefinitionKey: transcript.DefinitionKey, BehaviorKey: transcript.BehaviorKey,
		HostData: agentschema.CloneHostData(input.HostData),
	})
	if err != nil {
		return nil, err
	}
	if prepared.definition.Goal == nil {
		return nil, agentschema.ErrCapabilityUnsupported
	}
	raw, present := request.Snapshot.Capabilities[agentgoal.GoalCapability]
	var current agentgoal.GoalState
	if present {
		current, err = agentgoal.DecodeGoalState(raw)
		if err != nil {
			return nil, err
		}
	}
	next, err := agentgoal.ApplyGoalMutation(ctx, prepared.definition.Goal, agentgoal.GoalApplyRequest{
		Session: agentschema.SessionView{Key: engine.key, Revision: uint64(request.Snapshot.ContextCursor)},
		Run:     runViewForTurn(request.Snapshot),
		Current: current, Present: present, Mutation: *input.Goal,
	})
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	if present && string(encoded) == string(raw) {
		return nil, nil
	}
	return []CapabilityState{{
		Capability: agentgoal.GoalCapability, State: encoded,
	}}, nil
}

type engineControlState struct {
	mu      sync.RWMutex
	control ControlKind
	failure error
}

func (state *engineControlState) set(kind ControlKind) {
	state.mu.Lock()
	if kind == ControlAbort || (kind == ControlSuspend && state.control != ControlAbort) || state.control == "" {
		state.control = kind
	}
	state.mu.Unlock()
}

func (state *engineControlState) kind() ControlKind {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.control
}

func (state *engineControlState) fail(err error) {
	state.mu.Lock()
	state.failure = err
	state.mu.Unlock()
}

func (state *engineControlState) err() error {
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.failure
}

var _ Runner = (*Engine)(nil)

var _ InteractionResolver = (*Engine)(nil)

var _ AdmissionPreparer = (*Engine)(nil)

var _ InputMaterializer = (*Engine)(nil)
