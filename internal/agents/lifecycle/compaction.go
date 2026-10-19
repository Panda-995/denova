package lifecycle

import (
	agentcompaction "github.com/alfredxw/denova/agent/context/compaction"
)

// AgentCompactionBinder lets a conversation project the current Agent-owned
// checkpoint onto host context assembly without creating a competing durable
// compaction record in the product store.
type AgentCompactionBinder interface {
	BindAgentCompaction(*agentcompaction.CompactionState) error
}

func bindAgentCompaction(conversation any, state *agentcompaction.CompactionState) error {
	binder, ok := conversation.(AgentCompactionBinder)
	if !ok || binder == nil {
		return nil
	}
	return binder.BindAgentCompaction(state)
}
