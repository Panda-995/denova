package platform

import (
	"context"
	"slices"

	"github.com/google/uuid"
)

// Consumers share a Project activation, but own their requests and asynchronous
// work. These references exist only in memory and never become user data.
type runtimeConsumer struct {
	ctx    context.Context
	cancel context.CancelFunc
	token  string
}

type consumerContextKey struct{}
type consumerRequest struct {
	id  string
	ctx context.Context
}

func (r *Runtime) retainConsumer(id, viewID string) (RuntimeSnapshot, error) {
	if err := validateConsumer(r.owner.release.Manifest, id, viewID); err != nil {
		return RuntimeSnapshot{}, err
	}
	r.mu.Lock()
	if id != "" {
		if r.consumers == nil {
			r.consumers = map[string]runtimeConsumer{}
		}
		if _, exists := r.consumers[id]; !exists {
			ctx, cancel := context.WithCancel(r.ctx)
			r.consumers[id] = runtimeConsumer{ctx: ctx, cancel: cancel, token: randomToken()}
		}
	}
	consumer := r.consumers[id]
	r.mu.Unlock()
	result := r.snapshot()
	result.Connection.ConsumerID = id
	if id != "" {
		result.Connection.Token = consumer.token
	}
	if viewID != "" {
		result.ViewURL = r.baseURL + "/views/" + viewID + "/"
	}
	return result, nil
}

func validateConsumer(manifest Manifest, id, viewID string) error {
	if id != "" {
		if _, err := uuid.Parse(id); err != nil {
			return failure("INVALID_ARGUMENT", "Consumer ID must be a UUID")
		}
	}
	if viewID != "" && !slices.ContainsFunc(manifest.Views, func(view View) bool { return view.ID == viewID }) {
		return failure("NOT_FOUND", "View %s is not declared", viewID)
	}
	return nil
}

// ReleaseConsumer cancels only this page/command. Other consumers retain their
// activation; the last release revokes the runtime and shuts down its processes.
func (m *Manager) ReleaseConsumer(ctx context.Context, runtimeID, consumerID string) error {
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	runtime := m.runtimes[runtimeID]
	if runtime == nil {
		return nil
	}
	runtime.mu.Lock()
	consumer, exists := runtime.consumers[consumerID]
	if exists {
		delete(runtime.consumers, consumerID)
		consumer.cancel()
	}
	empty := len(runtime.consumers) == 0
	runtime.mu.Unlock()
	if exists && empty {
		return m.stopLocked(ctx, runtimeID)
	}
	return nil
}

// executionContext outlives the initiating HTTP response, but not its owning
// page/command. Non-UI consumers retain the existing whole-runtime lifetime.
func (r *Runtime) executionContext(request context.Context) context.Context {
	if owner, ok := request.Value(consumerContextKey{}).(consumerRequest); ok {
		return owner.ctx
	}
	return r.ctx
}
