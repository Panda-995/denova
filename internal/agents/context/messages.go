package context

import (
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// CloneMessages returns a deep-enough model-message snapshot using the Agent
// library's canonical clone semantics.
func CloneMessages(messages []*agentschema.Message) []*agentschema.Message {
	if messages == nil {
		return nil
	}
	cloned := make([]*agentschema.Message, len(messages))
	for index, message := range messages {
		cloned[index] = agentschema.CloneMessage(message)
	}
	return cloned
}
