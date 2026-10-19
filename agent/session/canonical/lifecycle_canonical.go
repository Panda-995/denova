package canonical

import (
	"context"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// CommitProductAcceptance lets an embedded canonical host accept product
// progress together with the current continuation checkpoint. For a concrete
// tool, result must be its confirmed product receipt; outside a tool it is nil.
// The callback must append the supplied checkpoint in its product transaction
// and must not reenter Session methods. Ordinary tools need no such callback.
func CommitProductAcceptance(ctx context.Context, result *agentschema.ToolResult, commit func(CanonicalCheckpoint) error) error {
	host, _ := ctx.Value(productAcceptorContextKey{}).(ProductAcceptor)
	if host == nil {
		return commit(nil)
	}
	return host.CommitProductAcceptance(ctx, result, commit)
}
