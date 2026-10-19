package canonical

import (
	"context"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// productAcceptor joins a host transaction to the current execution checkpoint.
// The canonical adapter never receives a Run or Session handle.
type ProductAcceptor interface {
	CommitProductAcceptance(context.Context, *agentschema.ToolResult, func(CanonicalCheckpoint) error) error
}

type productAcceptorContextKey struct{}

func ContextWithProductAcceptor(ctx context.Context, accept ProductAcceptor) context.Context {
	return context.WithValue(ctx, productAcceptorContextKey{}, accept)
}
