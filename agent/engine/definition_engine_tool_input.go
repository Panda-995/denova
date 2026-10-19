package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type projectedToolInput struct {
	name      string
	arguments string
	started   bool
}

// toolInputProjector observes the same fully merged tool-call view consumed by
// the native loop, so live display deltas and eventual execution share one
// execution identity and one append-only argument stream.
type toolInputProjector struct {
	variant *loopMessage
	source  EventSource
	calls   map[int]projectedToolInput
}

func newToolInputProjector(variant *loopMessage, source EventSource) *toolInputProjector {
	return &toolInputProjector{
		variant: variant, source: source, calls: make(map[int]projectedToolInput),
	}
}

func (projector *toolInputProjector) observe(message *agentschema.Message, emit EventSink) error {
	if projector == nil || message == nil || (message.Role != agentschema.Assistant && projector.variant.Role != agentschema.Assistant) {
		return nil
	}
	for ordinal, call := range message.ToolCalls {
		name := call.Function.Name
		if strings.TrimSpace(name) == "" {
			continue
		}
		state := projector.calls[ordinal]
		if state.name != "" && state.name != name {
			return fmt.Errorf("streamed tool input at ordinal %d changed name from %q to %q", ordinal, state.name, name)
		}
		if !strings.HasPrefix(call.Function.Arguments, state.arguments) {
			return fmt.Errorf("streamed tool input for %q at ordinal %d is not append-only", name, ordinal)
		}
		callID := projector.variant.ToolExecutionID(ordinal)
		if strings.TrimSpace(callID) == "" {
			return fmt.Errorf("assistant tool %q at ordinal %d is missing an execution ID", name, ordinal)
		}
		if !state.started {
			metadata, err := projector.toolMetadata(name)
			if err != nil {
				return err
			}
			if err := emit(ToolInputStarted{
				CallID: callID, ProviderCallID: call.ID, Name: name, Index: ordinal,
				Metadata: metadata, Source: projector.source,
			}); err != nil {
				return err
			}
			state.started = true
			state.name = name
		}
		if delta := strings.TrimPrefix(call.Function.Arguments, state.arguments); delta != "" {
			if err := emit(ToolInputDelta{
				CallID: callID, ProviderCallID: call.ID, Name: name, Delta: delta, Source: projector.source,
			}); err != nil {
				return err
			}
		}
		state.arguments = call.Function.Arguments
		projector.calls[ordinal] = state
	}
	return nil
}

func (projector *toolInputProjector) toolMetadata(name string) (json.RawMessage, error) {
	if projector == nil || projector.variant == nil {
		return nil, nil
	}
	for _, definition := range projector.variant.ToolDefinitions {
		if definition.Info == nil || definition.Info.Name != name {
			continue
		}
		metadata, err := agenttool.EncodeExecutionMetadata(definition.Descriptor)
		if err != nil {
			return nil, fmt.Errorf("encode tool %q live metadata: %w", name, err)
		}
		return metadata, nil
	}
	return nil, nil
}
