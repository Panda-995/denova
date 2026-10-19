package engine

import (
	"errors"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

// canonicalToolBatchAssistant accepts the finalized Agent-owned response while
// retaining lifecycle metadata attached when its raw stream crossed the
// Definition Engine. Tool preparation and middleware rewrites are deliberately
// not reconstructed from lower-level execution events.
func canonicalToolBatchAssistant(current, candidate *agentschema.Message) (*agentschema.Message, error) {
	if current == nil || current.Role != agentschema.Assistant || len(current.ToolCalls) == 0 {
		return nil, errors.New("canonical tool batch has no pending assistant owner")
	}
	if _, err := agentcanonical.ValidateCanonicalToolCallMessage(candidate); err != nil {
		return nil, err
	}
	canonical := candidate.Clone()
	if canonical.AgentMeta == nil {
		canonical.AgentMeta = current.Clone().AgentMeta
	} else if current.AgentMeta != nil && canonical.AgentMeta.ModelResponseOrdinal != current.AgentMeta.ModelResponseOrdinal {
		return nil, errors.New("canonical tool batch changed its model response identity")
	}
	return canonical, nil
}

func completedCanonicalToolBatch(current *agentschema.Message, messages []*agentschema.Message) ([]*agentschema.Message, error) {
	if err := agentcanonical.ValidateContextCommitMessages(messages); err != nil {
		return nil, err
	}
	result := agentschema.CloneMessages(messages)
	assistant, err := canonicalToolBatchAssistant(current, result[0])
	if err != nil {
		return nil, err
	}
	result[0] = assistant
	return result, nil
}
