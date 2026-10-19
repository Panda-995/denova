package interactive

import (
	"denova/internal/agents/toolresult"

	"github.com/alfredxw/denova/agent/model/providers"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// AgentMessagesFromModelContext is the common persisted Game message codec.
// Provider-owned continuation stays in the private model projection.
func AgentMessagesFromModelContext(messages []ModelContextMessage) []*agentschema.Message {
	result := make([]*agentschema.Message, 0, len(messages))
	for _, stored := range messages {
		message := AgentMessageFromModelContext(stored)
		continuation := providers.ContinuationExtra(stored.ProviderContinuation)
		if len(continuation) > 0 {
			if message.Extra == nil {
				message.Extra = make(map[string]any)
			}
			for key, value := range continuation {
				message.Extra[key] = value
			}
		}
		result = append(result, message)
	}
	return result
}

// SettledTurnToolMessages keeps the protocol while the canonical final
// narrative supplies assistant prose once per settled Turn.
func SettledTurnToolMessages(messages []ModelContextMessage) []*agentschema.Message {
	projected := AgentMessagesFromModelContext(messages)
	for _, message := range projected {
		if message.Role == agentschema.Assistant && len(message.ToolCalls) > 0 {
			message.Content = ""
		}
	}
	return projected
}

func ModelTurnMessages(turn StoryModelTurn, policy toolresult.ContextPolicy) []*agentschema.Message {
	messages := []*agentschema.Message{agentschema.UserMessageWithAttachments(turn.User, turn.Attachments)}
	messages = append(messages, SettledTurnToolMessages(turn.ModelContextMessages)...)
	assistant := agentschema.AssistantMessage(turn.Narrative, nil)
	assistant.Extra = providers.ContinuationExtra(turn.ProviderContinuation)
	messages = append(messages, assistant)
	return toolresult.ApplyContextPolicy(messages, policy)
}

func PlayerInputContextMessages(batches []ModelContextBatchEvent, user *agentschema.Message) []*agentschema.Message {
	messages := []*agentschema.Message{user}
	for _, batch := range batches {
		messages = append(messages, AgentMessagesFromModelContext(batch.Messages)...)
	}
	return messages
}
