package agents

import (
	"strings"

	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

// Message is the product-facing alias of Agent's stable model message. App and
// transport layers depend on Agent composition, while provider/session code can
// use the same wire without exposing an implementation-specific framework.
type Message = agentschema.Message

// ToolArtifactStore is the stable facade used by application conversations
// without coupling them directly to the underlying Agent module package.
type ToolArtifactStore = agenttool.ToolArtifactStore
type ToolArtifactBackend = agenttool.ToolArtifactBackend

type Role = agentschema.RoleType
type ToolCall = agentschema.ToolCall
type FunctionCall = agentschema.FunctionCall
type ToolInfo = agentschema.ToolInfo
type StreamReader[T any] = agentstream.StreamReader[T]

const (
	RoleSystem    = agentschema.System
	RoleUser      = agentschema.User
	RoleAssistant = agentschema.Assistant
	RoleTool      = agentschema.ToolRole
)

var (
	SystemMessage    = agentschema.SystemMessage
	UserMessage      = agentschema.UserMessage
	AssistantMessage = agentschema.AssistantMessage
	ToolMessage      = agentschema.ToolMessage
	TextToolResult   = agentschema.TextToolResult
	WithToolName     = agentschema.WithToolName
)

func StreamReaderFromArray[T any](values []T) *StreamReader[T] {
	return agentstream.StreamReaderFromArray(values)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
