package compaction

import (
	"strings"

	basecontext "github.com/alfredxw/denova/agent/context"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func PreserveLeadingMessage(messages []*agentschema.Message, content string) []*agentschema.Message {
	content = strings.TrimSpace(content)
	if content == "" {
		return messages
	}
	for _, message := range messages {
		if message != nil && strings.TrimSpace(message.Content) == content {
			return messages
		}
	}
	boundary := 0
	for boundary < len(messages) {
		message := messages[boundary]
		if message == nil {
			break
		}
		role := strings.TrimSpace(string(message.Role))
		if role != string(agentschema.System) && role != "developer" {
			break
		}
		boundary++
	}
	leading := agentschema.UserMessage(content)
	leading.Extra = map[string]any{basecontext.MessageExtraPlacement: string(basecontext.PlacementLeadingMessage)}
	result := make([]*agentschema.Message, 0, len(messages)+1)
	result = append(result, messages[:boundary]...)
	result = append(result, leading)
	result = append(result, messages[boundary:]...)
	return result
}
