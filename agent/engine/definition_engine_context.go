package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentgoal "github.com/alfredxw/denova/agent/engine/goal"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agenttrace "github.com/alfredxw/denova/agent/lifecycle/trace"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentpermission "github.com/alfredxw/denova/agent/tool/permission"
)

func (engine *Engine) applyGoalPreparation(
	ctx context.Context,
	request Request,
	prepared *preparedDefinition,
) error {
	if prepared == nil || prepared.definition.Goal == nil {
		return nil
	}
	prepared.goalReservedTokens = 0
	raw, present := request.Snapshot.Capabilities[agentgoal.GoalCapability]
	var state agentgoal.GoalState
	var err error
	if present {
		state, err = agentgoal.DecodeGoalState(raw)
		if err != nil {
			return err
		}
	}
	return applyPreparedGoal(
		ctx,
		prepared,
		agentschema.SessionView{Key: engine.key, Revision: uint64(request.Snapshot.ContextCursor)},
		runViewForTurn(request.Snapshot),
		state,
		present,
	)
}

// applyPreparedGoal is the single model-facing Goal assembly seam shared by
// real runs and read-only Session inspection. Mutation and CAS remain
// in the lifecycle; this helper only materializes tools and context from an
// already selected Goal state.
func applyPreparedGoal(
	ctx context.Context,
	prepared *preparedDefinition,
	session agentschema.SessionView,
	run agentschema.RunView,
	state agentgoal.GoalState,
	present bool,
) error {
	if prepared == nil || prepared.definition.Goal == nil {
		return nil
	}
	goalPreparation, err := prepared.definition.Goal.Prepare(ctx, agentgoal.GoalPrepareRequest{
		Session: session, Run: run, State: state, Present: present,
	})
	if err != nil {
		return fmt.Errorf("prepare Goal capability: %w", err)
	}
	if goalPreparation.ReservedTokens < 0 {
		return errors.New("prepare Goal capability: reserved tokens cannot be negative")
	}
	definitions := append([]agenttool.ToolDefinition(nil), prepared.tools...)
	definitions = append(definitions, goalPreparation.Tools...)
	registry, err := agenttool.NewRegistry(ctx, definitions...)
	if err != nil {
		return fmt.Errorf("prepare Goal tools: %w", err)
	}
	prepared.tools = registry.Definitions()
	prepared.toolSnapshots = registry.Snapshots()
	prepared.goalFragments = append([]agentschema.ContextFragment(nil), goalPreparation.Context...)
	prepared.goalReservedTokens = goalPreparation.ReservedTokens
	prepared.fragments = append(prepared.fragments, prepared.goalFragments...)
	if err := validateContextFragments(prepared.fragments); err != nil {
		return err
	}
	return updatePreparedPrefixFingerprint(prepared)
}

func sessionKeyFromBinding(binding BindingRef) (agentsession.Key, error) {
	return agentsession.NormalizeKey(agentsession.Key{
		Namespace: binding.Kind, ID: binding.Key, Attributes: agentschema.CloneStringMap(binding.Labels),
	})
}

func runViewForTurn(snapshot TurnSnapshot) agentschema.RunView {
	return agentschema.RunView{
		ID: string(snapshot.OperationID), CommandID: string(snapshot.CommandID), Cycle: snapshot.Cycle,
		StartedAt: snapshot.StartedAt,
		Delivery:  publicTurnDelivery(snapshot.Delivery), Autonomous: snapshot.Autonomous,
	}
}

func publicTurnDelivery(delivery DeliveryKind) agentschema.TurnDelivery {
	switch delivery {
	case DeliveryStart:
		return agentschema.TurnDeliveryStart
	case DeliverySteer:
		return agentschema.TurnDeliverySteer
	case DeliveryFollowUp:
		return agentschema.TurnDeliveryFollowUp
	case DeliveryNextTurn:
		return agentschema.TurnDeliveryNextTurn
	default:
		return ""
	}
}

func turnReasonForSnapshot(snapshot TurnSnapshot) (TurnReason, error) {
	switch snapshot.Delivery {
	case DeliveryStart:
		return TurnReasonStart, nil
	case DeliverySteer:
		return TurnReasonSteer, nil
	case DeliveryFollowUp:
		return TurnReasonFollowUp, nil
	case DeliveryNextTurn:
		return TurnReasonNextTurn, nil
	default:
		return "", fmt.Errorf("unsupported Agent turn delivery %q", snapshot.Delivery)
	}
}

func runViewForStructural(snapshot StructuralOperationSnapshot) agentschema.RunView {
	return agentschema.RunView{
		ID: string(snapshot.OperationID), CommandID: string(snapshot.CommandID), Cycle: snapshot.Cycle,
	}
}

func assembleCycleMessages(
	transcript []*agentschema.Message,
	userText string,
	attachments []agentschema.Attachment,
	fragments []agentschema.ContextFragment,
	attachmentRoot string,
) ([]*agentschema.Message, *agentschema.Message, error) {
	messages := make([]*agentschema.Message, 0, len(transcript)+len(fragments)+1)
	messages = append(messages, leadingContextMessages(fragments)...)
	var prefixes []string
	var finalUserMessage string
	hasFinalUserMessage := false
	for _, fragment := range fragments {
		rendered := agenthistory.RenderContextFragment(fragment)
		switch fragment.Placement {
		case agentschema.ContextLeadingMessage:
		case agentschema.ContextFinalUserPrefix:
			prefixes = append(prefixes, rendered)
		case agentschema.ContextFinalUserMessage:
			finalUserMessage = rendered
			hasFinalUserMessage = true
		case agentschema.ContextAuditOnly:
		}
	}
	messages = append(messages, agentschema.CloneMessages(transcript)...)
	modelUserText := strings.TrimSpace(userText)
	if hasFinalUserMessage {
		modelUserText = finalUserMessage
	} else if len(prefixes) > 0 {
		modelUserText = strings.Join(prefixes, "\n\n---\n\n") + "\n\n---\n\n# User request\n\n" + modelUserText
	}
	user := agentschema.UserMessageWithAttachments(modelUserText, attachments)
	messages = append(messages, user)
	resolved, err := agentschema.ResolveMessageAttachmentPaths(attachmentRoot, messages)
	if err != nil {
		return nil, nil, err
	}
	return resolved, agentschema.CloneMessage(resolved[len(resolved)-1]), nil
}

// prepareHistoryModelCall is shared by Elision and Compaction. Rebuild the
// active input and middleware exactly as an ordinary model step, without
// mutating canonical messages or publishing maintenance state.
func prepareHistoryModelCall(prepared preparedDefinition, raw []*agentschema.Message, compaction agenthistory.CompactionRecord, present bool, input agentschema.Input, activeUserIndex int, modelContext *modelStepContext) (*preparedModelCall, *agentschema.Message, error) {
	summaryLimit := 0
	if prepared.definition.Compaction != nil {
		summaryLimit = prepared.definition.Compaction.SummaryLimitBytes()
	}
	effective, err := prepared.archive.EffectiveHistoryMessages(raw, prepared.elision, compaction, present, summaryLimit)
	if err != nil {
		return nil, nil, err
	}
	userIndex := prepared.archive.CompactionMessageIndex(raw, compaction, present, activeUserIndex)
	if userIndex < 0 || userIndex >= len(effective) {
		return nil, nil, errors.New("context maintenance removed the active Agent input")
	}
	messages, modelUser, err := assembleCycleMessages(effective[:userIndex], input.Text, input.Attachments, prepared.fragments, prepared.definition.AttachmentRoot)
	if err != nil {
		return nil, nil, err
	}
	messages = append(messages, agentschema.CloneMessages(effective[userIndex+1:])...)
	if modelContext.prepareCompaction == nil {
		return nil, nil, errors.New("context maintenance requires the active model preparation seam")
	}
	candidate, err := modelContext.prepareCompaction(messages, stableContextPrefixMessages(prepared.fragments, compaction, present))
	return candidate, modelUser, err
}

// leadingContextMessages is the single assembly rule for lifecycle-owned
// stable fragments. Normal turns, retries, and structural Compaction snapshots
// must preserve the exact same role and bytes for provider cache identity.
func leadingContextMessages(fragments []agentschema.ContextFragment) []*agentschema.Message {
	messages := make([]*agentschema.Message, 0, len(fragments))
	for _, fragment := range fragments {
		if fragment.Placement != agentschema.ContextLeadingMessage {
			continue
		}
		rendered := agenthistory.RenderContextFragment(fragment)
		switch effectiveContextRole(fragment) {
		case agentschema.User:
			messages = append(messages, agentschema.UserMessage(rendered))
		default:
			messages = append(messages, agentschema.SystemMessage(rendered))
		}
	}
	return messages
}

func newPreparedDefinitionLoop(
	ctx context.Context,
	prepared preparedDefinition,
	middlewares []agentmiddleware.Middleware,
	permission *permissionMiddleware,
	gate modelCallGate,
) (*modelToolLoop, error) {
	return newModelToolLoop(ctx, loopConfig{
		Name: prepared.definition.Name, Description: prepared.definition.Description,
		Instruction: prepared.definition.Instructions, Model: prepared.definition.Model,
		ModelIdentity: prepared.definition.ModelIdentity,
		Tools:         prepared.tools, Middlewares: middlewares,
		ResultProcessor: prepared.definition.ResultProcessor, Artifacts: prepared.definition.Artifacts,
		Retry:            prepared.definition.Execution.Retry,
		ModelMaxAttempts: prepared.definition.Execution.ModelMaxAttempts,
		MaxIterations:    prepared.definition.Execution.MaxIterations,
		IdleTimeout:      prepared.definition.Execution.IdleTimeout,
		ToolParallelism:  prepared.definition.Execution.ToolParallelism,
		modelCallGate:    gate,
		permission:       permission,
	})
}

// prepareDefinitionModelRequest runs the exact provider-neutral assembly
// pipeline without invoking the provider. Structural operations and
// public read-only inspection share this seam so caller Middleware, tool
// schemas, cache routing, and stable-prefix authentication cannot drift.
func prepareDefinitionModelRequest(
	ctx context.Context,
	prepared preparedDefinition,
	session agentschema.SessionView,
	run agentschema.RunView,
	messages []*agentschema.Message,
	stablePrefixMessages int,
) (*agentmodel.ModelRequestSnapshot, error) {
	var err error
	messages, err = agentschema.ResolveMessageAttachmentPaths(prepared.definition.AttachmentRoot, messages)
	if err != nil {
		return nil, err
	}
	permission := agentpermission.EffectivePermissionPolicy(prepared.definition.Permission)
	permissionStage := &permissionMiddleware{
		BaseMiddleware: &agentmiddleware.BaseMiddleware{}, policy: permission, session: session, run: run,
		attachments: agentschema.AttachmentsFromMessages(messages),
	}
	loop, err := newPreparedDefinitionLoop(
		ctx,
		prepared,
		append([]agentmiddleware.Middleware(nil), prepared.definition.Middlewares...),
		permissionStage,
		nil,
	)
	if err != nil {
		return nil, err
	}
	return newLoopRunner(loopRunnerConfig{Agent: loop, EnableStreaming: true}).prepareModelRequest(
		ctx,
		messages,
		stablePrefixMessages,
	)
}

// stableContextPrefixMessages returns the Definition-owned contiguous prefix
// assembled before canonical conversation body messages. The checkpoint is
// stable only when it replaces from raw index zero; a custom interior
// replacement cannot extend the provider cache prefix across mutable history.
func stableContextPrefixMessages(
	fragments []agentschema.ContextFragment,
	compaction agenthistory.CompactionRecord,
	compactionPresent bool,
) int {
	count := 0
	for _, fragment := range fragments {
		if fragment.Placement == agentschema.ContextLeadingMessage {
			count++
		}
	}
	if compactionPresent && !compaction.Removed && compaction.ReplacementFrom == 0 {
		count++
	}
	return count
}

func consumeMessageVariant(variant *loopMessage, source EventSource, displayOnly bool, emit EventSink) (*agentschema.Message, error) {
	if variant == nil {
		return nil, nil
	}
	if variant.discarded {
		return agentschema.CloneMessage(variant.Message), nil
	}
	toolInputs := newToolInputProjector(variant, source)
	if !variant.IsStreaming {
		message := agentschema.CloneMessage(variant.Message)
		if message != nil && message.Role == agentschema.Assistant {
			if message.Content != "" {
				if err := emit(AssistantDelta{Source: source, Delta: message.Content, DisplayOnly: displayOnly, ResponseOrdinal: variant.ModelResponseOrdinal}); err != nil {
					return nil, err
				}
			}
			if message.ReasoningContent != "" {
				if err := emit(ThinkingDelta{Source: source, Delta: message.ReasoningContent, DisplayOnly: displayOnly, ResponseOrdinal: variant.ModelResponseOrdinal}); err != nil {
					return nil, err
				}
			}
		}
		if err := toolInputs.observe(message, emit); err != nil {
			return nil, err
		}
		if variant.previewOnly {
			return nil, nil
		}
		return message, nil
	}
	if variant.MessageStream == nil {
		return nil, errors.New("Agent modelToolLoop returned a nil Message stream")
	}
	defer variant.MessageStream.Close()
	assembler := agentschema.NewMessageAssembler()
	for {
		chunk, err := variant.MessageStream.Recv()
		if errors.Is(err, io.EOF) {
			return assembler.Message()
		}
		if err != nil {
			var rejected *modelResponseRejected
			if errors.As(err, &rejected) {
				return nil, nil
			}
			return nil, err
		}
		if chunk == nil {
			return nil, errors.New("Agent modelToolLoop streamed a nil Message chunk")
		}
		if chunk.Content != "" {
			if err := emit(AssistantDelta{Source: source, Delta: chunk.Content, DisplayOnly: displayOnly, ResponseOrdinal: variant.ModelResponseOrdinal}); err != nil {
				return nil, err
			}
		}
		if chunk.ReasoningContent != "" {
			if err := emit(ThinkingDelta{Source: source, Delta: chunk.ReasoningContent, DisplayOnly: displayOnly, ResponseOrdinal: variant.ModelResponseOrdinal}); err != nil {
				return nil, err
			}
		}
		if err := assembler.Append(chunk); err != nil {
			return nil, err
		}
		message, err := assembler.Message()
		if err != nil {
			return nil, err
		}
		if err := toolInputs.observe(message, emit); err != nil {
			return nil, err
		}
	}
}

func (engine *Engine) emitToolExecution(
	ctx context.Context,
	request Request,
	execution *toolExecutionEvent,
	source EventSource,
	effects agentcanonical.EffectApplier,
	started map[string]bool,
	emit EventSink,
) error {
	if execution == nil {
		return nil
	}
	callID := execution.ExecutionID
	if callID == "" {
		callID = execution.ProviderCallID
	}
	metadata, err := agenttool.EncodeExecutionMetadata(execution.Definition.Descriptor)
	if err != nil {
		return err
	}
	if execution.ParentCallID != "" && !started[callID] {
		if err := emit(ToolInputStarted{
			CallID: callID, ParentCallID: execution.ParentCallID, Name: execution.ToolName,
			Index: execution.Index, Metadata: metadata, Source: source,
		}); err != nil {
			return err
		}
	}
	if execution.Phase == toolExecutionStarted {
		if !started[callID] {
			agenttrace.EmitTrace(ctx, engine.trace, agenttrace.TraceEvent{
				Kind: agenttrace.TraceToolStarted, Session: engine.key, RunID: string(request.Snapshot.OperationID),
				Cycle: request.Snapshot.Cycle, ToolCallID: execution.ExecutionID, ToolName: execution.ToolName,
			})
			if err := emit(ToolStarted{
				CallID: callID, ProviderCallID: execution.ProviderCallID, Name: execution.ToolName, Index: execution.Index,
				Arguments: append(json.RawMessage(nil), execution.Arguments...), Metadata: metadata, Source: source,
				ExecutionAuthorized: true,
			}); err != nil {
				return err
			}
			started[callID] = true
		}
		return nil
	}
	switch execution.Phase {
	case toolExecutionProgress:
		return emit(ToolProgress{
			CallID: callID, ProviderCallID: execution.ProviderCallID, Name: execution.ToolName, Index: execution.Index,
			Delta: execution.Delta, Metadata: metadata, Source: source,
		})
	case toolExecutionFinished:
		// Policy denial and invalid preflight can finish before concrete Tool.Run.
		// Record a paired zero-side-effect start immediately before the result so
		// the tool lifecycle remains structurally complete.
		if !started[callID] {
			if err := emit(ToolStarted{
				CallID: callID, ProviderCallID: execution.ProviderCallID, Name: execution.ToolName, Index: execution.Index,
				Arguments: append(json.RawMessage(nil), execution.Arguments...), Metadata: metadata, Source: source,
			}); err != nil {
				return err
			}
			started[callID] = true
		}
		result := agentschema.ToolResult{}
		if execution.Result != nil {
			result = *execution.Result
		}
		projection := result
		projection.Effects = nil
		encodedProjection, err := json.Marshal(projection)
		if err != nil {
			return fmt.Errorf("encode bounded Tool result projection: %w", err)
		}
		if len(result.Effects) != 0 && effects == nil {
			return errors.New("Tool produced Effects but Definition has no Effect Applier")
		}
		if err := engine.applyCanonicalEffects(ctx, request, effects, callID, result.Effects); err != nil {
			return err
		}
		for index, artifact := range result.Artifacts {
			encoded, err := json.Marshal(artifact)
			if err != nil {
				return fmt.Errorf("encode Tool artifact %d: %w", index, err)
			}
			if err := emit(ArtifactProduced{CallID: callID, Artifact: encoded}); err != nil {
				return err
			}
		}
		agenttrace.EmitTrace(ctx, engine.trace, agenttrace.TraceEvent{
			Kind: agenttrace.TraceToolFinished, Session: engine.key, RunID: string(request.Snapshot.OperationID),
			Cycle: request.Snapshot.Cycle, ToolCallID: callID, ToolName: execution.ToolName,
		})
		return emit(ToolFinished{
			CallID: callID, ProviderCallID: execution.ProviderCallID, Name: execution.ToolName, Index: execution.Index,
			Result:   result.DisplayContent,
			IsError:  result.IsError(),
			Metadata: metadata, Source: source, Projection: encodedProjection,
		})
	default:
		return fmt.Errorf("unsupported Tool execution phase %q", execution.Phase)
	}
}

func (engine *Engine) applyCanonicalEffects(
	ctx context.Context,
	request Request,
	adapter agentcanonical.EffectApplier,
	callID string,
	effects []agentschema.Effect,
) error {
	if len(effects) == 0 {
		return nil
	}
	requests := make([]agentcanonical.EffectRequest, len(effects))
	for index, effect := range effects {
		digest, err := agentschema.HashCanonical(struct {
			Version int
			Session agentsession.Key
			RunID   string
			Cycle   int
			CallID  string
			Index   int
		}{1, engine.key, string(request.Snapshot.OperationID), request.Snapshot.Cycle, callID, index})
		if err != nil {
			return err
		}
		requests[index] = agentcanonical.EffectRequest{
			ID: "effect-" + digest,
			Identity: agentcanonical.CommitIdentity{
				Session: engine.key, CommandID: string(request.Snapshot.CommandID),
				RunID: string(request.Snapshot.OperationID), Cycle: request.Snapshot.Cycle, Stage: agentcanonical.CommitOutput,
			},
			CallID: callID, Index: index, Effect: effect,
		}
	}
	results, err := adapter.ApplyEffects(ctx, requests)
	if err != nil {
		return err
	}
	byID := make(map[string]agentcanonical.EffectResult, len(results))
	for _, result := range results {
		byID[result.ID] = result
	}
	for _, request := range requests {
		result, ok := byID[request.ID]
		if !ok {
			return fmt.Errorf("Effect Applier omitted Tool effect %q", request.ID)
		}
		if result.Error != "" {
			return fmt.Errorf("apply canonical Tool effect %q: %s", request.ID, result.Error)
		}
		if strings.TrimSpace(result.Revision) == "" {
			return fmt.Errorf("Effect Applier Tool effect %q has no revision", request.ID)
		}
	}
	return nil
}

func modelRequestedToolNames(calls []agentschema.ToolCall) []string {
	if len(calls) == 0 {
		return nil
	}
	result := make([]string, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func runtimeEventSource(event *loopEvent) EventSource {
	if event == nil {
		return EventSource{}
	}
	source := EventSource{
		Name: strings.TrimSpace(event.AgentName), InvocationID: strings.TrimSpace(event.InvocationID),
		InvocationType: strings.TrimSpace(event.InvocationType),
	}
	for _, step := range event.RunPath {
		if name := strings.TrimSpace(step.String()); name != "" {
			source.Path = append(source.Path, name)
		}
	}
	if len(source.Path) == 0 && source.Name != "" {
		source.Path = []string{source.Name}
	}
	return source
}

func rootAgentEvent(event *loopEvent, rootName string) bool {
	if event == nil {
		return false
	}
	name := strings.TrimSpace(event.AgentName)
	rootName = strings.TrimSpace(rootName)
	if name != "" && rootName != "" && name != rootName {
		return false
	}
	return len(event.RunPath) <= 1
}
