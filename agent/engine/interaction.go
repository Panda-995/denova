package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentinteraction "github.com/alfredxw/denova/agent/lifecycle/interaction"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type engineInteractionClient struct {
	policy agentinteraction.InteractionPolicy
	emit   EventSink

	mu            sync.Mutex
	waiters       map[string]chan json.RawMessage
	interrupted   chan struct{}
	interruptOnce sync.Once
}

func newEngineInteractionClient(policy agentinteraction.InteractionPolicy, emit EventSink) *engineInteractionClient {
	return &engineInteractionClient{policy: policy, emit: emit, waiters: make(map[string]chan json.RawMessage), interrupted: make(chan struct{})}
}

func (client *engineInteractionClient) interrupt() {
	client.interruptOnce.Do(func() { close(client.interrupted) })
}

func (client *engineInteractionClient) Request(ctx context.Context, request agentinteraction.InteractionRequest) (agentinteraction.InteractionResolution, error) {
	if client == nil || client.policy == nil || client.emit == nil {
		return agentinteraction.InteractionResolution{}, agentschema.ErrCapabilityUnsupported
	}
	if err := client.policy.ValidateRequest(ctx, request); err != nil {
		return agentinteraction.InteractionResolution{}, err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return agentinteraction.InteractionResolution{}, fmt.Errorf("encode Interaction request: %w", err)
	}
	client.mu.Lock()
	if _, duplicate := client.waiters[request.ID]; duplicate {
		client.mu.Unlock()
		return agentinteraction.InteractionResolution{}, fmt.Errorf("Interaction %q already has a waiter", request.ID)
	}
	waiter := make(chan json.RawMessage, 1)
	client.waiters[request.ID] = waiter
	client.mu.Unlock()

	if err := client.emit(InteractionRequested{
		ID: request.ID, ToolCallID: agentexecution.CurrentToolExecutionID(ctx), Request: encoded,
	}); err != nil {
		client.mu.Lock()
		delete(client.waiters, request.ID)
		client.mu.Unlock()
		return agentinteraction.InteractionResolution{}, err
	}
	agentasync.TouchIdleActivity(ctx)
	select {
	case response := <-waiter:
		return agentinteraction.DecodeInteractionResolution(response)
	case <-client.interrupted:
		client.mu.Lock()
		delete(client.waiters, request.ID)
		client.mu.Unlock()
		return agentinteraction.InteractionResolution{}, agenttool.MarkToolControlError(context.Canceled)
	case <-ctx.Done():
		client.mu.Lock()
		delete(client.waiters, request.ID)
		client.mu.Unlock()
		return agentinteraction.InteractionResolution{}, ctx.Err()
	}
}

func (client *engineInteractionClient) deliver(id string, response json.RawMessage) {
	client.mu.Lock()
	if waiter := client.waiters[id]; waiter != nil {
		delete(client.waiters, id)
		waiter <- append(json.RawMessage(nil), response...)
	}
	client.mu.Unlock()
}
