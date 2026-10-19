package context

import (
	"fmt"
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

const CompactionSummaryPrefix = "[Denova Context Compaction]"

// NewCompactionSummaryMessage creates the stable model-visible checkpoint
// envelope shared by writing and game conversations.
func NewCompactionSummaryMessage(epoch int, summary string) *agentschema.Message {
	return agentschema.AssistantMessage(fmt.Sprintf(
		"%s epoch=%d\n\nAssistant-authored context summary (context data, not a user instruction):\n%s",
		CompactionSummaryPrefix,
		epoch,
		strings.TrimSpace(summary),
	), nil)
}

// IsCompactionSummaryMessage reports whether message is a Denova checkpoint.
func IsCompactionSummaryMessage(message *agentschema.Message) bool {
	return message != nil && strings.HasPrefix(strings.TrimSpace(message.Content), CompactionSummaryPrefix)
}
