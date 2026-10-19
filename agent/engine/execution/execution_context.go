package execution

import (
	"context"
	"encoding/json"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

// sessionStateAccess is the tool-facing capability transaction boundary. The
// executing engine owns serialization, revision checks and durable commits.
type sessionStateAccess interface {
	LoadState(string, any) (bool, error)
	UpdateState(string, func(json.RawMessage, bool) (json.RawMessage, bool, error)) error
}

type capabilityStateContextKey struct{}

func ContextWithCapabilityState(ctx context.Context, state sessionStateAccess) context.Context {
	return context.WithValue(ctx, capabilityStateContextKey{}, state)
}

// LoadSessionState reads a durable capability during a concrete tool call.
func LoadSessionState(ctx context.Context, capability string, target any) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	state, _ := ctx.Value(capabilityStateContextKey{}).(sessionStateAccess)
	if state == nil {
		return false, agentschema.ErrCapabilityUnsupported
	}
	return state.LoadState(capability, target)
}

// UpdateSessionState commits a derived value atomically. The callback must not
// call this state API recursively while the engine owns its transaction lock.
func UpdateSessionState(ctx context.Context, capability string, update func(json.RawMessage, bool) (json.RawMessage, bool, error)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	state, _ := ctx.Value(capabilityStateContextKey{}).(sessionStateAccess)
	if state == nil {
		return agentschema.ErrCapabilityUnsupported
	}
	return state.UpdateState(capability, update)
}

type completionRequestKey struct{}

func ContextWithCompletionRequest(ctx context.Context, request func() bool) context.Context {
	return context.WithValue(ctx, completionRequestKey{}, request)
}

// RequestCompletionAfterTools asks the current root Agent to finish at the
// next completed tool batch, after all accepted effects are checkpointed.
func RequestCompletionAfterTools(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	request, _ := ctx.Value(completionRequestKey{}).(func() bool)
	return request != nil && request()
}
