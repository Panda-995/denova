package engine

import (
	"context"

	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

// CanonicalHost is the execution-scoped journal port. The lifecycle owns its
// implementation; the engine receives confirmed facts without Run/Session access.
// CommitCanonical must atomically persist the product change and checkpoint,
// publishing the new recovery state only after that commit succeeds. ToolFacts
// returns a detached snapshot of confirmed tool facts for recovery.
type CanonicalHost interface {
	CommitCanonical(context.Context, CanonicalUpdate, func(agentcanonical.CanonicalCheckpoint) error) error
	ToolFacts() map[string]PersistedTool
}

// commitCheckpoint keeps standalone execution valid without a product journal.
// Embedded execution supplies the per-cycle host explicitly on its request.
func commitCheckpoint(ctx context.Context, host CanonicalHost, update CanonicalUpdate, commit func(agentcanonical.CanonicalCheckpoint) error) error {
	if host == nil {
		return commit(nil)
	}
	return host.CommitCanonical(ctx, update, commit)
}
