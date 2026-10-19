package toolresult

import (
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func validToolCall(call agentschema.ToolCall) bool {
	_, err := agenttool.NormalizeToolCallForModelContext(call, nil)
	return err == nil
}

func assistantHasIndependentContent(message *agentschema.Message) bool {
	if message == nil {
		return false
	}
	return message.Content != "" || message.Name != "" || message.ReasoningContent != "" ||
		len(message.MultiContent) > 0 || len(message.AssistantGenMultiContent) > 0
}
