package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agenttoolresult "github.com/alfredxw/denova/agent/tool/result"
)

const toolProgressTruncatedMarker = "\n[tool progress truncated]"

type toolStartReceiptContextKey struct{}

func contextWithToolStartReceipt(ctx context.Context) context.Context {
	return context.WithValue(ctx, toolStartReceiptContextKey{}, struct{}{})
}

func toolStartReceiptRequired(ctx context.Context) bool {
	_, required := ctx.Value(toolStartReceiptContextKey{}).(struct{})
	return required
}

type toolExecutionResult struct {
	message        *agentschema.Message
	result         agentschema.ToolResult
	executionID    string
	providerCallID string
	err            error
}

type preparedToolCall struct {
	index        int
	batchSize    int
	call         agentschema.ToolCall
	executionID  string
	parentCallID string
	registry     *agenttool.Registry
	definition   agenttool.ToolDefinition
	snapshot     agenttool.ToolDefinitionSnapshot
	precomputed  *toolExecutionResult
}

type indexedToolExecutionResult struct {
	index  int
	result toolExecutionResult
}

const fallbackToolResultMaxBytes = 128 * 1024

var fallbackToolResultDescriptor = agenttool.ToolDescriptor{
	Source: agenttool.ToolSourceOther, Execution: agenttool.ToolExecutionParallelRead,
	MutationScope: agenttool.ToolMutationNone, PostCheck: agenttool.ToolPostCheckNone,
	Recovery: agenttool.ToolRecoveryReadOnly, ResultProjection: agentschema.ToolResultBoundedModelContext,
	ResultRetention: agentschema.ToolResultProtected,
	Steering:        agenttool.SteeringFinishCurrent, MaxResultBytes: fallbackToolResultMaxBytes,
}

// executePreparedToolBatch schedules calls in descriptor-defined stages.
// Parallel reads share one bounded stage; exclusive and child calls are
// source-order barriers.
func (agent *modelToolLoop) executePreparedToolBatch(
	ctx context.Context,
	prepared []preparedToolCall,
	events *asyncGenerator[*loopEvent],
	cancel *cancelControl,
) ([]toolExecutionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	results := make([]toolExecutionResult, len(prepared))
	for index := range prepared {
		prepared[index].batchSize = len(prepared)
	}

	for index := 0; index < len(prepared); {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if cancel.pending(cancelAfterTools | cancelAfterModel) {
			agent.fillSteeringSkipped(prepared[index:], results, events)
			return results, nil
		}
		current := prepared[index]
		if current.precomputed != nil {
			results[index] = *current.precomputed
			agent.emitToolFinished(events, current, current.precomputed.result)
			index++
			continue
		}

		if current.snapshot.Descriptor.Execution != agenttool.ToolExecutionParallelRead {
			completion, err := agent.runOneToolCall(ctx, current, events, cancel)
			results[index] = completion
			if err != nil {
				agent.fillPolicySkipped(prepared[index+1:], results, events, "a lifecycle or control failure stopped later tool stages")
				return results, err
			}
			index++
			continue
		}

		end := index
		for end < len(prepared) && prepared[end].precomputed == nil &&
			prepared[end].snapshot.Descriptor.Execution == agenttool.ToolExecutionParallelRead {
			end++
		}
		stageResults, started, err := agent.runParallelToolStage(ctx, prepared[index:end], events, cancel)
		for offset, completion := range stageResults {
			results[index+offset] = completion
		}
		if err != nil {
			if started < end-index {
				agent.fillPolicySkipped(prepared[index+started:end], results, events, "a lifecycle or control failure stopped the parallel stage")
			}
			agent.fillPolicySkipped(prepared[end:], results, events, "a lifecycle or control failure stopped later tool stages")
			return results, err
		}
		if started < end-index {
			// Steering stops the whole remaining batch, not just the tail of the
			// current parallel stage. Every assistant call must still receive a
			// paired result before the safe point ends this run.
			agent.fillSteeringSkipped(prepared[index+started:], results, events)
			return results, nil
		}
		index = end
	}
	return results, nil
}

func (agent *modelToolLoop) prepareToolCalls(ctx context.Context, registry *agenttool.Registry, calls []agentschema.ToolCall, modelResponseOrdinal int) []preparedToolCall {
	prepared := make([]preparedToolCall, len(calls))
	for index, call := range calls {
		prepared[index] = prepareToolCall(
			registry, call, index, agentexecution.ToolExecutionIDForOrdinal(ctx, modelResponseOrdinal, index), "",
		)
	}
	return prepared
}

func prepareToolCall(
	registry *agenttool.Registry,
	call agentschema.ToolCall,
	index int,
	executionID string,
	parentCallID string,
) preparedToolCall {
	call = agentschema.CloneToolCalls([]agentschema.ToolCall{call})[0]
	originalCall := agentschema.CloneToolCalls([]agentschema.ToolCall{call})[0]
	canonicalArguments, argumentErr := agenttool.CanonicalizeToolArgumentsJSON(call.Function.Arguments)
	if argumentErr != nil {
		call.Function.Arguments = `{}`
	} else {
		call.Function.Arguments = canonicalArguments
	}
	item := preparedToolCall{
		index: index, call: call, executionID: executionID, parentCallID: parentCallID, registry: registry,
	}
	name := call.Function.Name
	switch {
	case call.Type != "" && call.Type != "function":
		result := syntheticCallResult(call, agentschema.ToolResultError, agentschema.ToolSyntheticInvalidCall,
			fmt.Sprintf("tool %q has unsupported call type %q", name, call.Type))
		item.precomputed = &result
	case strings.TrimSpace(name) == "":
		result := syntheticCallResult(call, agentschema.ToolResultError, agentschema.ToolSyntheticInvalidCall, "tool call has no name")
		item.precomputed = &result
	default:
		definition, exists := registry.Lookup(name)
		if !exists {
			result := syntheticCallResult(call, agentschema.ToolResultError, agentschema.ToolSyntheticUnknownTool,
				fmt.Sprintf("unknown tool %q", name))
			item.precomputed = &result
			break
		}
		snapshot, _ := registry.Snapshot(name)
		item.definition = definition
		item.snapshot = snapshot
		if argumentErr != nil {
			result := invalidToolArgumentsResult(originalCall, argumentErr)
			item.precomputed = &result
		} else if normalized, err := agenttool.NormalizeToolArguments(snapshot.Info, call.Function.Arguments); err != nil {
			result := invalidToolArgumentsResult(originalCall, err)
			item.precomputed = &result
		} else {
			item.call.Function.Arguments = normalized
		}
	}
	if item.precomputed != nil {
		bindToolExecutionIdentity(item.precomputed, item)
		descriptor := fallbackToolResultDescriptor
		if item.snapshot.Info != nil {
			descriptor = item.snapshot.Descriptor
		}
		normalizeToolExecutionResult(item.precomputed, descriptor)
	}
	return item
}

func (agent *modelToolLoop) runOneToolCall(
	ctx context.Context,
	call preparedToolCall,
	events *asyncGenerator[*loopEvent],
	cancel *cancelControl,
) (toolExecutionResult, error) {
	completed := make(chan toolExecutionResult, 1)
	agent.launchToolCall(ctx, call, events, cancel, completed)
	// Cancellation requests stopping; it does not prove Tool.Run has stopped
	// writing. Keep the execution owner until the endpoint actually returns.
	result := <-completed
	return result, result.err
}

func (agent *modelToolLoop) runParallelToolStage(
	ctx context.Context,
	calls []preparedToolCall,
	events *asyncGenerator[*loopEvent],
	cancel *cancelControl,
) ([]toolExecutionResult, int, error) {
	results := make([]toolExecutionResult, len(calls))
	completed := make(chan indexedToolExecutionResult, len(calls))
	started := 0
	running := 0
	var terminalErr error

	launch := func(index int) {
		call := calls[index]
		output := make(chan toolExecutionResult, 1)
		agent.launchToolCall(ctx, call, events, cancel, output)
		agentasync.SafeGo(func() {
			result := <-output
			completed <- indexedToolExecutionResult{index: index, result: result}
		}, func(err error) {
			result := toolFailureResult(call.call, err)
			bindToolExecutionIdentity(&result, call)
			completed <- indexedToolExecutionResult{index: index, result: result}
		})
		started++
		running++
	}

	for running < agent.toolParallelism && started < len(calls) && !cancel.pending(cancelAfterTools|cancelAfterModel) {
		launch(started)
	}
	for running > 0 {
		select {
		case completion := <-completed:
			running--
			results[completion.index] = completion.result
			if terminalErr == nil && completion.result.err != nil {
				terminalErr = completion.result.err
			}
			for terminalErr == nil && running < agent.toolParallelism && started < len(calls) &&
				!cancel.pending(cancelAfterTools|cancelAfterModel) {
				launch(started)
			}
		case <-ctx.Done():
			for running > 0 {
				completion := <-completed
				running--
				results[completion.index] = completion.result
			}
			return results, started, ctx.Err()
		}
	}
	return results, started, terminalErr
}

func (agent *modelToolLoop) launchToolCall(
	ctx context.Context,
	call preparedToolCall,
	events *asyncGenerator[*loopEvent],
	cancel *cancelControl,
	completed chan<- toolExecutionResult,
) {
	agentasync.SafeGo(func() {
		completed <- agent.executePreparedTool(ctx, call, events, cancel)
	}, func(err error) {
		result := toolFailureResultForDescriptor(call.call, call.snapshot.Descriptor, err)
		bindToolExecutionIdentity(&result, call)
		result.err = errors.Join(result.err, agent.confirmToolFinished(ctx, events, call, result.result))
		completed <- result
	})
}

func (agent *modelToolLoop) executePreparedTool(
	ctx context.Context,
	prepared preparedToolCall,
	events *asyncGenerator[*loopEvent],
	cancel *cancelControl,
) toolExecutionResult {
	authorizedArguments := prepared.call.Function.Arguments
	callCtx := agentexecution.ContextWithToolExecution(
		ctx, prepared.executionID, prepared.call.ID, prepared.call.Function.Name,
	)
	callCtx = agentevent.ContextWithNestedEventForwarder(callCtx, func(event agentevent.NestedEvent) error {
		cloned := agentevent.CloneNestedEvent(event)
		if cloned.ParentCallID == "" {
			cloned.ParentCallID = prepared.executionID
		}
		events.Send(&loopEvent{
			AgentName: agent.name, RunPath: []loopRunStep{newLoopRunStep(agent.name)},
			Output: &loopOutput{NestedEvent: &cloned},
		})
		return nil
	})
	dispatcher := nestedToolDispatcher{
		loop: agent, registry: prepared.registry, events: events, cancel: cancel, parentCallID: prepared.executionID,
	}
	callCtx = agenttool.ContextWithNestedToolInvoker(callCtx, dispatcher.call)
	callCtx = agenttool.ContextWithToolSteering(callCtx, cancel.requestedSignal(), func() bool { return cancel.pending(cancelAfterTools | cancelAfterModel) })

	var progressMu sync.Mutex
	var progress strings.Builder
	progressTruncated := false
	limit := prepared.snapshot.Descriptor.MaxResultBytes
	callCtx = agenttool.ContextWithToolProgress(callCtx, func(delta string) {
		delta = strings.ToValidUTF8(delta, "\uFFFD")
		progressMu.Lock()
		if progressTruncated {
			progressMu.Unlock()
			return
		}
		remaining := max(0, limit-progress.Len())
		emitted := delta
		if len(emitted) > remaining {
			emitted = emitted[:remaining]
			for len(emitted) > 0 && !utf8.ValidString(emitted) {
				emitted = emitted[:len(emitted)-1]
			}
			emitted += toolProgressTruncatedMarker
			progressTruncated = true
		}
		content := emitted
		if progressTruncated {
			content = strings.TrimSuffix(emitted, toolProgressTruncatedMarker)
		}
		progress.WriteString(content)
		progressMu.Unlock()
		if emitted != "" {
			events.Send(agent.toolExecutionEvent(prepared, toolExecutionProgress, emitted, nil))
		}
	})

	if prepared.snapshot.Descriptor.Steering == agenttool.SteeringInterruptibleWait {
		interruptibleCtx, stop := context.WithCancel(callCtx)
		callCtx = interruptibleCtx
		defer stop()
		if signal := cancel.requestedSignal(); signal != nil {
			agentasync.SafeGo(func() {
				select {
				case <-signal:
					if cancel.pending(cancelAfterTools | cancelAfterModel) {
						stop()
					}
				case <-interruptibleCtx.Done():
				}
			}, func(error) {})
		}
	}

	var started sync.Once
	var startErr error
	endpoint := agentmiddleware.ToolCallEndpoint(func(runCtx context.Context, arguments string, options ...agenttool.ToolOption) (agentschema.ToolResult, error) {
		// Tool-call middleware may derive a context but cannot replace the fixed
		// Definition artifact authority. Rebind at the concrete execution seam so
		// both permission and arbitrary wrappers are outside this invariant.
		runCtx = agenttool.ContextWithDefinitionToolArtifactStorage(runCtx, agent.artifacts)
		if err := runCtx.Err(); err != nil {
			return agentschema.ToolResult{}, err
		}
		normalizedArguments, err := agenttool.NormalizeToolArguments(prepared.snapshot.Info, arguments)
		if err != nil {
			return invalidToolArgumentsToolResult(prepared.call.Function.Name, arguments, err), nil
		}
		if agent.permission != nil && normalizedArguments != authorizedArguments {
			return agentschema.ToolResult{}, fmt.Errorf("%w: tool %q", agentschema.ErrPermissionArgumentsChanged, prepared.call.Function.Name)
		}
		// The fixed permission fence and caller middleware have both completed at
		// this concrete seam. A pending/denied approval is therefore never
		// presented as an executing process.
		started.Do(func() {
			event := agent.toolExecutionEvent(prepared, toolExecutionStarted, "", nil)
			event.Output.ToolExecution.Arguments = json.RawMessage(normalizedArguments)
			if toolStartReceiptRequired(runCtx) {
				event.Output.ToolExecution.startReceipt = make(chan error, 1)
			}
			events.Send(event)
			if receipt := event.Output.ToolExecution.startReceipt; receipt != nil {
				select {
				case startErr = <-receipt:
				case <-runCtx.Done():
					startErr = context.Cause(runCtx)
				}
			}
		})
		if startErr != nil {
			return agentschema.ToolResult{}, startErr
		}
		result, err := runToolSafely(prepared.definition.Tool, runCtx, normalizedArguments, options...)
		if err == nil && result.ModelContent == "" && result.DisplayContent == "" {
			progressMu.Lock()
			progressContent := progress.String()
			if progressTruncated {
				marker := toolProgressTruncatedMarker
				if len(marker) >= limit {
					progressContent = marker[:limit]
				} else {
					end := limit - len(marker)
					if len(progressContent) > end {
						progressContent = progressContent[:end]
						for len(progressContent) > 0 && !utf8.ValidString(progressContent) {
							progressContent = progressContent[:len(progressContent)-1]
						}
					}
					progressContent += marker
				}
			}
			progressMu.Unlock()
			if progressContent != "" {
				result.ModelContent = progressContent
				result.DisplayContent = progressContent
			}
		}
		return result, err
	})
	toolContext := &agentmiddleware.ToolContext{
		Index: prepared.index, Name: prepared.call.Function.Name,
		ExecutionID: prepared.executionID, ProviderCallID: prepared.call.ID, ParentCallID: prepared.parentCallID,
		Definition: prepared.snapshot,
	}
	for index := len(agent.middlewares) - 1; index >= 0; index-- {
		wrapped, err := agent.middlewares[index].WrapToolCall(callCtx, endpoint, toolContext)
		if err != nil {
			result := toolFailureResult(prepared.call, fmt.Errorf("wrap tool %q: %w", prepared.call.Function.Name, err))
			bindToolExecutionIdentity(&result, prepared)
			agent.emitToolFinished(events, prepared, result.result)
			return result
		}
		if wrapped == nil {
			result := toolFailureResult(prepared.call, fmt.Errorf("wrap tool %q returned nil endpoint", prepared.call.Function.Name))
			bindToolExecutionIdentity(&result, prepared)
			agent.emitToolFinished(events, prepared, result.result)
			return result
		}
		endpoint = wrapped
	}
	if agent.permission != nil {
		wrapped, err := agent.permission.WrapToolCall(callCtx, endpoint, toolContext)
		if err != nil {
			result := toolFailureResult(prepared.call, fmt.Errorf("build permission fence for tool %q: %w", prepared.call.Function.Name, err))
			bindToolExecutionIdentity(&result, prepared)
			agent.emitToolFinished(events, prepared, result.result)
			return result
		}
		if wrapped == nil {
			result := toolFailureResult(prepared.call, fmt.Errorf("permission fence for tool %q returned nil endpoint", prepared.call.Function.Name))
			bindToolExecutionIdentity(&result, prepared)
			agent.emitToolFinished(events, prepared, result.result)
			return result
		}
		endpoint = wrapped
	}

	result, err := endpoint(callCtx, prepared.call.Function.Arguments)
	var terminalErr error
	if err != nil {
		if prepared.snapshot.Descriptor.Steering == agenttool.SteeringInterruptibleWait &&
			cancel.pending(cancelAfterTools|cancelAfterModel) && errors.Is(callCtx.Err(), context.Canceled) {
			if cancel.pending(cancelModel) {
				return toolExecutionResult{executionID: prepared.executionID, providerCallID: prepared.call.ID,
					err: &cancelError{Info: &cancelInfo{Mode: cancelAfterTools | cancelModel}}}
			}
			result = agenttool.SyntheticToolResult(agentschema.ToolResultSkipped, agentschema.ToolSyntheticSteeringInterrupted,
				fmt.Sprintf("tool %q was interrupted to apply pending user steering", prepared.call.Function.Name))
			err = nil
		} else if agenttool.IsToolControlError(err) {
			terminalErr = err
			if result.Status == "" {
				result = agenttool.ToolErrorResult(toolErrorContent(prepared.call, err), err.Error())
			}
			err = nil
		} else if ctx.Err() != nil {
			return toolExecutionResult{executionID: prepared.executionID, providerCallID: prepared.call.ID, err: err}
		} else {
			result = agenttool.ToolErrorResult(toolErrorContent(prepared.call, err), err.Error())
			err = nil
		}
	}
	if result.Status == "" {
		result.Status = agentschema.ToolResultSuccess
	}
	if agent.resultProcessor != nil {
		processed, processErr := agent.resultProcessor.Process(callCtx, agenttoolresult.ToolResultProcessRequest{
			ToolName: prepared.call.Function.Name, Arguments: prepared.call.Function.Arguments,
			ExecutionID: prepared.executionID, ProviderCallID: prepared.call.ID,
			BatchSize:  prepared.batchSize,
			Definition: prepared.snapshot, Result: result,
		})
		result = processed
		if processErr != nil {
			if agenttool.IsToolControlError(processErr) {
				terminalErr = processErr
			}
			result = retainToolResultProcessorFailure(prepared.call, result, processErr)
		}
	}
	normalized, normalizeErr := agenttool.NormalizeToolResult(result, prepared.snapshot.Descriptor)
	if normalizeErr != nil {
		normalized, _ = agenttool.NormalizeToolResult(
			agenttool.ToolErrorResult(toolErrorContent(prepared.call, normalizeErr), normalizeErr.Error()),
			prepared.snapshot.Descriptor,
		)
	}
	completion := toolExecutionResult{
		result: normalized, message: agentschema.ToolMessage(normalized, prepared.call.ID, agentschema.WithToolName(prepared.call.Function.Name)),
		executionID: prepared.executionID, providerCallID: prepared.call.ID, err: terminalErr,
	}
	completion.err = errors.Join(completion.err, agent.confirmToolFinished(ctx, events, prepared, normalized))
	return completion
}

// confirmToolFinished keeps a completed effect ahead of subsequent work. The
// standalone loop has no journal consumer and retains asynchronous delivery.
func (agent *modelToolLoop) confirmToolFinished(ctx context.Context, events *asyncGenerator[*loopEvent], prepared preparedToolCall, result agentschema.ToolResult) error {
	event := agent.toolExecutionEvent(prepared, toolExecutionFinished, "", &result)
	if toolStartReceiptRequired(ctx) {
		event.Output.ToolExecution.finishReceipt = make(chan error, 1)
	}
	events.Send(event)
	if receipt := event.Output.ToolExecution.finishReceipt; receipt != nil {
		select {
		case err := <-receipt:
			return err
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	}
	return nil
}

// retainToolResultProcessorFailure preserves every valid partial projection
// while pairing it with a bounded, model-visible diagnostic. Processor errors
// must never erase already-published artifacts, receipts, structured details,
// or display output merely to change the outcome status.
func retainToolResultProcessorFailure(call agentschema.ToolCall, result agentschema.ToolResult, err error) agentschema.ToolResult {
	diagnostic := toolErrorContent(call, err)
	if !agenttool.IsToolControlError(err) {
		result.Status = agentschema.ToolResultError
		result.SyntheticReason = ""
	}
	if strings.TrimSpace(result.ModelContent) == "" {
		result.ModelContent = diagnostic
	} else {
		result.ModelContent += "\n\n[Tool result processing failed]\n" + diagnostic
	}
	if strings.TrimSpace(result.DisplayContent) == "" {
		result.DisplayContent = diagnostic
	} else {
		result.DisplayContent += "\n\nTool result processing failed: " + err.Error()
	}
	return result
}

// runToolSafely converts a concrete extension panic before it can jump across
// lifecycle middleware. That lets the outer durable wrapper record a bounded
// error completion for a tool that already has a start receipt.
func runToolSafely(tool agenttool.Tool, ctx context.Context, arguments string, options ...agenttool.ToolOption) (
	result agentschema.ToolResult,
	err error,
) {
	defer func() {
		if value := recover(); value != nil {
			result = agentschema.ToolResult{}
			err = agentasync.RecoveredPanic(value)
		}
	}()
	return tool.Run(ctx, arguments, options...)
}

func (agent *modelToolLoop) fillSteeringSkipped(
	calls []preparedToolCall,
	results []toolExecutionResult,
	events *asyncGenerator[*loopEvent],
) {
	for _, prepared := range calls {
		if prepared.precomputed != nil {
			results[prepared.index] = *prepared.precomputed
			agent.emitToolFinished(events, prepared, prepared.precomputed.result)
			continue
		}
		result := syntheticCallResult(prepared.call, agentschema.ToolResultSkipped, agentschema.ToolSyntheticSteeringBeforeStart,
			fmt.Sprintf("tool %q was not started because user steering is pending", prepared.call.Function.Name))
		if normalized, err := agenttool.NormalizeToolResult(result.result, prepared.snapshot.Descriptor); err == nil {
			result.result = normalized
			result.message = agentschema.ToolMessage(normalized, prepared.call.ID, agentschema.WithToolName(prepared.call.Function.Name))
		}
		bindToolExecutionIdentity(&result, prepared)
		results[prepared.index] = result
		agent.emitToolFinished(events, prepared, result.result)
	}
}

func (agent *modelToolLoop) fillPolicySkipped(
	calls []preparedToolCall,
	results []toolExecutionResult,
	events *asyncGenerator[*loopEvent],
	reason string,
) {
	for _, prepared := range calls {
		if prepared.precomputed != nil {
			results[prepared.index] = *prepared.precomputed
			agent.emitToolFinished(events, prepared, prepared.precomputed.result)
			continue
		}
		result := syntheticCallResult(prepared.call, agentschema.ToolResultSkipped, agentschema.ToolSyntheticPolicyBlocked,
			fmt.Sprintf("tool %q was not started because %s", prepared.call.Function.Name, reason))
		if normalized, err := agenttool.NormalizeToolResult(result.result, prepared.snapshot.Descriptor); err == nil {
			result.result = normalized
			result.message = agentschema.ToolMessage(normalized, prepared.call.ID, agentschema.WithToolName(prepared.call.Function.Name))
		}
		bindToolExecutionIdentity(&result, prepared)
		results[prepared.index] = result
		agent.emitToolFinished(events, prepared, result.result)
	}
}

func (agent *modelToolLoop) emitToolFinished(
	events *asyncGenerator[*loopEvent],
	prepared preparedToolCall,
	result agentschema.ToolResult,
) {
	events.Send(agent.toolExecutionEvent(prepared, toolExecutionFinished, "", &result))
}

func (agent *modelToolLoop) toolExecutionEvent(
	prepared preparedToolCall,
	phase toolExecutionPhase,
	delta string,
	result *agentschema.ToolResult,
) *loopEvent {
	var cloned *agentschema.ToolResult
	if result != nil {
		value := *result
		value.Details = append(json.RawMessage(nil), result.Details...)
		value.Artifacts = append([]agentschema.ToolArtifactRef(nil), result.Artifacts...)
		value.Effects = cloneEffects(result.Effects)
		cloned = &value
	}
	return &loopEvent{
		AgentName: agent.name,
		RunPath:   []loopRunStep{newLoopRunStep(agent.name)},
		Output: &loopOutput{ToolExecution: &toolExecutionEvent{
			Phase: phase, Index: prepared.index, ExecutionID: prepared.executionID,
			ProviderCallID: prepared.call.ID,
			ParentCallID:   prepared.parentCallID,
			ToolName:       prepared.call.Function.Name, Arguments: json.RawMessage(prepared.call.Function.Arguments), Definition: prepared.snapshot,
			Delta: delta, Result: cloned,
		}},
	}
}

func cloneEffects(effects []agentschema.Effect) []agentschema.Effect {
	cloned := make([]agentschema.Effect, len(effects))
	for index, effect := range effects {
		cloned[index] = effect
		cloned[index].Data = append(json.RawMessage(nil), effect.Data...)
	}
	return cloned
}

func syntheticCallResult(call agentschema.ToolCall, status agentschema.ToolResultStatus, reason agentschema.ToolSyntheticReason, message string) toolExecutionResult {
	result := agenttool.SyntheticToolResult(status, reason, toolErrorContent(call, errors.New(message)))
	return toolExecutionResult{
		result:  result,
		message: agentschema.ToolMessage(result, call.ID, agentschema.WithToolName(call.Function.Name)),
	}
}

func invalidToolArgumentsResult(call agentschema.ToolCall, err error) toolExecutionResult {
	result := invalidToolArgumentsToolResult(call.Function.Name, call.Function.Arguments, err)
	return toolExecutionResult{
		result:  result,
		message: agentschema.ToolMessage(result, call.ID, agentschema.WithToolName(call.Function.Name)),
	}
}

func invalidToolArgumentsToolResult(toolName, receivedArguments string, err error) agentschema.ToolResult {
	issues := []agenttool.ToolArgumentIssue{{Code: "constraint_violation", Path: "$", Message: "invalid tool arguments"}}
	var argumentErr *agenttool.ToolArgumentsError
	if errors.As(err, &argumentErr) && argumentErr != nil && len(argumentErr.Issues) > 0 {
		issues = append([]agenttool.ToolArgumentIssue(nil), argumentErr.Issues...)
	} else if err != nil {
		issues[0].Message = err.Error()
	}
	payload := struct {
		Error struct {
			Code              string                        `json:"code"`
			Tool              string                        `json:"tool"`
			Message           string                        `json:"message"`
			Issues            []agenttool.ToolArgumentIssue `json:"issues"`
			ReceivedArguments *string                       `json:"received_arguments,omitempty"`
		} `json:"error"`
	}{}
	payload.Error.Code = string(agentschema.ToolSyntheticInvalidArguments)
	payload.Error.Tool = strings.TrimSpace(toolName)
	payload.Error.Message = "Tool arguments could not be normalized safely; correct the listed issues and retry."
	payload.Error.Issues = issues
	displayEncoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		displayEncoded = []byte(`{"error":{"code":"invalid_arguments","message":"invalid tool arguments"}}`)
	}
	payload.Error.ReceivedArguments = &receivedArguments
	modelEncoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		modelEncoded = displayEncoded
	}
	result := agenttool.SyntheticToolResult(agentschema.ToolResultError, agentschema.ToolSyntheticInvalidArguments, string(modelEncoded))
	result.DisplayContent = string(displayEncoded)
	result.Details = append(json.RawMessage(nil), displayEncoded...)
	return result
}

func toolFailureResult(call agentschema.ToolCall, err error) toolExecutionResult {
	result := agenttool.ToolErrorResult(toolErrorContent(call, err), err.Error())
	var terminalErr error
	if agenttool.IsToolControlError(err) || errors.Is(err, context.Canceled) {
		terminalErr = err
	}
	return toolExecutionResult{
		result:  result,
		message: agentschema.ToolMessage(result, call.ID, agentschema.WithToolName(call.Function.Name)),
		err:     terminalErr,
	}
}

func toolFailureResultForDescriptor(call agentschema.ToolCall, descriptor agenttool.ToolDescriptor, err error) toolExecutionResult {
	result := toolFailureResult(call, err)
	normalizeToolExecutionResult(&result, descriptor)
	return result
}

func normalizeToolExecutionResult(result *toolExecutionResult, descriptor agenttool.ToolDescriptor) {
	if result == nil {
		return
	}
	normalized, err := agenttool.NormalizeToolResult(result.result, descriptor)
	if err != nil {
		fallback := agenttool.ToolErrorResult(toolErrorContent(agentschema.ToolCall{}, err), err.Error())
		normalized, _ = agenttool.NormalizeToolResult(fallback, fallbackToolResultDescriptor)
	}
	result.result = normalized
	toolName := ""
	toolCallID := ""
	if result.message != nil {
		toolName = result.message.ToolName
		toolCallID = result.message.ToolCallID
	}
	result.message = agentschema.ToolMessage(normalized, toolCallID, agentschema.WithToolName(toolName))
}

func bindToolExecutionIdentity(result *toolExecutionResult, prepared preparedToolCall) {
	if result == nil {
		return
	}
	result.executionID = prepared.executionID
	result.providerCallID = prepared.call.ID
}

func toolErrorContent(call agentschema.ToolCall, err error) string {
	message := "tool execution failed"
	if err != nil {
		message = err.Error()
	}
	payload, marshalErr := json.Marshal(map[string]any{
		"error": map[string]string{"message": message, "tool": call.Function.Name},
	})
	if marshalErr != nil {
		return fmt.Sprintf(`{"error":{"message":%q}}`, message)
	}
	return string(payload)
}

// modelFinishReasonBlocksToolExecution keeps the side-effect boundary explicit
// at the call site while sharing the same provider-neutral completion rule.
func modelFinishReasonBlocksToolExecution(meta *agentschema.ResponseMeta) (string, bool) {
	reason, class := agentmodel.ClassifyResponseFinishReason(meta)
	return reason, class.Incomplete()
}

func (agent *modelToolLoop) incompleteModelToolResults(
	preparedCalls []preparedToolCall,
	finishReason string,
	events *asyncGenerator[*loopEvent],
) []toolExecutionResult {
	results := make([]toolExecutionResult, len(preparedCalls))
	finishReason = strings.TrimSpace(finishReason)
	if finishReason == "" {
		finishReason = "incomplete"
	}
	for index, prepared := range preparedCalls {
		result := syntheticCallResult(prepared.call, agentschema.ToolResultSkipped, agentschema.ToolSyntheticModelIncomplete,
			fmt.Sprintf("tool call was not executed because the model response ended with finish_reason %q; arguments may be incomplete", finishReason))
		if prepared.snapshot.Info != nil {
			normalizeToolExecutionResult(&result, prepared.snapshot.Descriptor)
		} else {
			normalizeToolExecutionResult(&result, fallbackToolResultDescriptor)
		}
		bindToolExecutionIdentity(&result, prepared)
		results[index] = result
		agent.emitToolFinished(events, prepared, result.result)
	}
	return results
}
