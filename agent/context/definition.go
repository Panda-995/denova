package context

import (
	"context"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ContextSource returns accountable model-visible fragments for one cycle.
// Accepted fragments are journaled and reused on same-cycle resume. Materialize
// runs for new cycles and explicit context refreshes such as compaction; it must
// not be required to restore executable tool or canonical commit state.
type ContextSource interface {
	Identity() agentschema.CapabilityIdentity
	Materialize(context.Context, ContextRequest) ([]agentschema.ContextFragment, error)
}

type ContextRequest struct {
	Session agentschema.SessionView
	Run     agentschema.RunView
	Input   agentschema.Input
	// Compaction is the current Agent-owned checkpoint. ContextSource may use
	// its opaque ContextData to project a bounded host-owned context.
	Compaction *agenthistory.CompactionState
}
