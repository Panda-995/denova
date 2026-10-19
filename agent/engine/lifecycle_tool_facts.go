package engine

import (
	"context"
	"errors"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// restorePendingToolBatch pairs confirmed individual results without executing
// tools. Unstarted/read-only interrupted calls become explicit skipped results;
// unknown writes must already have passed awaitRecoveryInput.
func (engine *Engine) restorePendingToolBatch(ctx context.Context, request Request, prepared *preparedDefinition, state *engineTranscript, emit EventSink) error {
	host := request.Journal
	if host == nil {
		return nil
	}
	checkpoint, err := CanonicalMessageCheckpoint(request.Snapshot.State)
	if err != nil || len(checkpoint.Pending) == 0 {
		return err
	}
	assistant := checkpoint.Pending[0]
	if assistant.Role != agentschema.Assistant || len(assistant.ToolCalls) == 0 {
		return errors.New("pending tool checkpoint has no assistant calls")
	}
	batch := []*agentschema.Message{assistant.Clone()}
	if assistant.AgentMeta == nil || assistant.AgentMeta.ModelResponseOrdinal < 1 {
		return errors.New("pending tool checkpoint has no model response identity")
	}
	scope, err := agentsession.CanonicalKey(engine.key)
	if err != nil {
		return err
	}
	namespace := agentexecution.RootToolNamespace(agentexecution.InvocationIdentity{Scope: scope, OperationID: string(request.Snapshot.OperationID), Cycle: request.Snapshot.Cycle}, prepared.definition.Name)
	facts := host.ToolFacts()
	for index, call := range assistant.ToolCalls {
		executionID := agentexecution.ExecutionIDForNamespace(namespace, assistant.AgentMeta.ModelResponseOrdinal, index)
		fact, found := facts[executionID]
		result := agenttool.SyntheticToolResult(agentschema.ToolResultSkipped, agentschema.ToolSyntheticSteeringBeforeStart, "The tool did not start before execution stopped. No effect was applied.")
		if found && fact.Result != nil {
			result = *fact.Result
		} else if found && fact.Started {
			if fact.Descriptor == nil || fact.Descriptor.MutationScope != agenttool.ToolMutationNone {
				return errors.New("unverified tool effect cannot enter model execution")
			}
			result = agenttool.SyntheticToolResult(agentschema.ToolResultSkipped, agentschema.ToolSyntheticSteeringInterrupted, "The read or wait stopped without a confirmed result. It may be requested again if still needed.")
		}
		batch = append(batch, agentschema.ToolMessage(result, call.ID, agentschema.WithToolName(call.Function.Name)))
	}
	state.Messages = append(agentschema.CloneMessages(state.Messages[:checkpoint.MessageCount]), batch...)
	sequence := prepared.contextSequence
	prepared.contextSequence++
	encoded, err := encodeEngineTranscriptState(*prepared, state.Messages, state.ActiveModelUser, state.ActiveUserIndex)
	if err != nil {
		return err
	}
	if err := engine.commitCanonicalContext(ctx, request, prepared.definition.Canonical, sequence, batch, TranscriptUpdated{State: encoded}); err != nil {
		return err
	}
	return emit(TranscriptUpdated{State: encoded})
}
