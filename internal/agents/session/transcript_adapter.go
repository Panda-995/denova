package session

import (
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func (s *Session) effectiveTranscriptMessagesLocked() []*agentschema.Message {
	start := s.clearAfterIndex - s.messageBaseIndex
	if start < 0 {
		start = 0
	}
	if start > len(s.messages) {
		start = len(s.messages)
	}
	result := make([]*agentschema.Message, len(s.messages)-start)
	for index, message := range s.messages[start:] {
		result[index] = agentschema.CloneMessage(message)
	}
	return result
}
