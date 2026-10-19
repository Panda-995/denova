package canonical

import (
	"errors"
	"fmt"
	"strings"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ErrInvalidCanonicalMessages identifies invalid history in one Session. Hosts
// should reject that conversation's admission without disabling the Agent or
// retrying the request as an uncertain provider failure. Import never repairs
// raw messages implicitly: cleanup and compaction refer to their stable indices.
var ErrInvalidCanonicalMessages = errors.New("agent canonical history is invalid")

// canonicalContextStateOrder restores the semantic order of context-state
// updates. The host must append accepted input first as the durable admission
// fence, so state updates committed for that cycle physically follow it even
// though they are model-visible immediately before that user message.
func CanonicalContextStateOrder(messages []*agentschema.Message) []*agentschema.Message {
	input := agentschema.CloneMessages(messages)
	result := make([]*agentschema.Message, 0, len(input))
	for index := 0; index < len(input); {
		message := input[index]
		if message == nil || message.Role != agentschema.User || agenthistory.IsContextStateMessage(message) {
			result = append(result, message)
			index++
			continue
		}
		end := index + 1
		for end < len(input) && agenthistory.IsContextStateMessage(input[end]) {
			end++
		}
		result = append(result, input[index+1:end]...)
		result = append(result, message)
		index = end
	}
	return result
}

func ValidateImportedTranscript(messages []*agentschema.Message) error {
	pending := make(map[string]struct{})
	for index, message := range messages {
		if message == nil {
			return fmt.Errorf("Agent transcript message %d is nil", index)
		}
		if len(pending) > 0 && message.Role != agentschema.ToolRole {
			return fmt.Errorf("Agent transcript message %d splits an incomplete tool-result batch", index)
		}
		switch message.Role {
		case agentschema.User:
			if len(message.ToolCalls) != 0 || strings.TrimSpace(message.ToolCallID) != "" {
				return fmt.Errorf("Agent transcript user message %d contains tool protocol fields", index)
			}
		case agentschema.Assistant:
			if strings.TrimSpace(message.ToolCallID) != "" {
				return fmt.Errorf("Agent transcript assistant message %d contains a tool result ID", index)
			}
			for _, call := range message.ToolCalls {
				id := strings.TrimSpace(call.ID)
				if id == "" || strings.TrimSpace(call.Function.Name) == "" {
					return fmt.Errorf("Agent transcript assistant message %d has an invalid tool call", index)
				}
				if _, duplicate := pending[id]; duplicate {
					return fmt.Errorf("Agent transcript assistant message %d repeats tool call %q", index, id)
				}
				pending[id] = struct{}{}
			}
		case agentschema.ToolRole:
			if len(message.ToolCalls) != 0 {
				return fmt.Errorf("Agent transcript tool message %d contains nested tool calls", index)
			}
			id := strings.TrimSpace(message.ToolCallID)
			if _, ok := pending[id]; !ok || id == "" {
				return fmt.Errorf("Agent transcript tool message %d has no matching pending call", index)
			}
			delete(pending, id)
		case agentschema.System:
			return fmt.Errorf("Agent transcript message %d is system-owned and cannot be product-imported", index)
		default:
			return fmt.Errorf("Agent transcript message %d has unsupported role %q", index, message.Role)
		}
	}
	if len(pending) > 0 {
		return errors.New("Agent transcript ends with an incomplete tool-result batch")
	}
	return nil
}
