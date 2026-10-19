package openaichatcompletions

import (
	"encoding/json"
	"fmt"

	"github.com/alfredxw/denova/agent/model/providers"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// chatContinuation retains opaque extra_content, including Gemini thought
// signatures. Tool metadata is bound to call IDs so context normalization can
// remove calls or update their arguments without replaying the original calls.
type chatContinuation struct {
	ExtraContent json.RawMessage            `json:"extra_content,omitempty"`
	ToolCalls    map[string]json.RawMessage `json:"tool_calls,omitempty"`
}

type toolContinuation struct {
	id           string
	extraContent json.RawMessage
}

// continuationState binds streaming metadata to IDs even when the signature
// and ID arrive in different deltas. Only a completed stream emits continuation;
// incremental opaque payloads must not be concatenated as message text.
type continuationState struct {
	extraContent json.RawMessage
	toolCalls    map[int]toolContinuation
}

func (state *continuationState) add(raw string) error {
	var message struct {
		ExtraContent *json.RawMessage `json:"extra_content"`
		ToolCalls    []struct {
			Index        *int             `json:"index"`
			ID           string           `json:"id"`
			ExtraContent *json.RawMessage `json:"extra_content"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		return fmt.Errorf("openai chat completions: preserve continuation: %w", err)
	}
	if message.ExtraContent != nil {
		state.extraContent = *message.ExtraContent
	}
	for index, call := range message.ToolCalls {
		if call.Index != nil {
			index = *call.Index
		}
		if state.toolCalls == nil {
			state.toolCalls = make(map[int]toolContinuation)
		}
		stored := state.toolCalls[index]
		if call.ID != "" {
			stored.id = call.ID
		}
		if call.ExtraContent != nil {
			stored.extraContent = *call.ExtraContent
		}
		state.toolCalls[index] = stored
	}
	return nil
}

func (state *continuationState) message(config providers.ModelConfig) (*agentschema.Message, error) {
	payload := chatContinuation{ExtraContent: state.extraContent}
	for _, call := range state.toolCalls {
		if len(call.extraContent) == 0 {
			continue
		}
		if payload.ToolCalls == nil {
			payload.ToolCalls = make(map[string]json.RawMessage)
		}
		payload.ToolCalls[call.id] = call.extraContent
	}
	if len(payload.ExtraContent) == 0 && len(payload.ToolCalls) == 0 {
		return nil, nil
	}
	continuation, err := providers.NewContinuation(config, payload)
	if err != nil {
		return nil, err
	}
	return &agentschema.Message{
		Role:  agentschema.Assistant,
		Extra: map[string]any{providers.ExtraKeyContinuation: continuation},
	}, nil
}
