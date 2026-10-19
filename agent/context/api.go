package context

import (
	agenthistory "github.com/alfredxw/denova/agent/context/history"
	"github.com/alfredxw/denova/agent/schema"
)

type ClearState = agenthistory.ClearState

// IsContextStateMessage identifies a canonical context-state record.
func IsContextStateMessage(message *schema.Message) bool {
	return agenthistory.IsContextStateMessage(message)
}

type ElisionPolicy = agenthistory.ElisionPolicy
type ElisionMetrics = agenthistory.ElisionMetrics
type CleanupReplacement = agenthistory.CleanupReplacement
type CleanupMetrics = agenthistory.CleanupMetrics
type CleanupState = agenthistory.CleanupState
