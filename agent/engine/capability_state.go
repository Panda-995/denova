package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentgoal "github.com/alfredxw/denova/agent/engine/goal"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// LoadSessionState reads one Agent-owned durable capability slot from the
// currently executing tool. It is unavailable outside a concrete Tool.Run.
func (client *capabilityStateClient) LoadState(capability string, target any) (bool, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	raw, present := client.states[capability]
	if !present {
		return false, nil
	}
	if target == nil {
		return true, nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return false, fmt.Errorf("decode Session state %q: %w", capability, err)
	}
	return true, nil
}

// UpdateSessionState atomically derives and commits one Agent-owned durable
// capability slot from the currently executing tool. The callback executes
// under the cycle-local state lock and must not call Session-state functions.
func (client *capabilityStateClient) UpdateState(
	capability string,
	update func(current json.RawMessage, present bool) (next json.RawMessage, delete bool, err error),
) error {
	if client.emit == nil || update == nil {
		return agentschema.ErrCapabilityUnsupported
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	current, present := client.states[capability]
	next, remove, err := update(append(json.RawMessage(nil), current...), present)
	if err != nil {
		return err
	}
	if !remove && next == nil {
		return nil
	}
	if !remove && (len(next) == 0 || !json.Valid(next)) {
		return errors.New("Session state update requires valid non-empty JSON")
	}
	if !remove && present && bytes.Equal(current, next) {
		return nil
	}
	if err := client.emit(CapabilityState{
		Capability: capability, State: append(json.RawMessage(nil), next...), Delete: remove,
	}); err != nil {
		return err
	}
	if remove {
		delete(client.states, capability)
	} else {
		client.states[capability] = append(json.RawMessage(nil), next...)
	}
	return nil
}

type capabilityStateClient struct {
	mu     sync.Mutex
	states map[string]json.RawMessage
	emit   EventSink
}

func newCapabilityStateClient(states map[string]json.RawMessage, emit EventSink) *capabilityStateClient {
	return &capabilityStateClient{states: agentschema.CloneRawStateMap(states), emit: emit}
}

func (client *capabilityStateClient) updateGoal(
	ctx context.Context,
	manager agentgoal.GoalManager,
	session agentschema.SessionView,
	run agentschema.RunView,
	mutation agentschema.GoalMutation,
) (agentgoal.GoalState, error) {
	if client == nil || client.emit == nil || manager == nil {
		return agentgoal.GoalState{}, agentschema.ErrCapabilityUnsupported
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	currentRaw, present := client.states[agentgoal.GoalCapability]
	var current agentgoal.GoalState
	var err error
	if present {
		current, err = agentgoal.DecodeGoalState(currentRaw)
		if err != nil {
			return agentgoal.GoalState{}, err
		}
	}
	if mutation.MutationID == "" {
		mutation.MutationID = agentexecution.CurrentToolExecutionID(ctx)
	}
	next, err := agentgoal.ApplyGoalMutation(ctx, manager, agentgoal.GoalApplyRequest{
		Session: session, Run: run, Current: current, Present: present, Mutation: mutation,
	})
	if err != nil {
		return agentgoal.GoalState{}, err
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return agentgoal.GoalState{}, err
	}
	if err := client.emit(CapabilityState{
		Capability: agentgoal.GoalCapability, State: encoded,
		CompareCurrent: true, ExpectedPresent: present,
		ExpectedState: append(json.RawMessage(nil), currentRaw...),
	}); err != nil {
		return agentgoal.GoalState{}, err
	}
	client.states[agentgoal.GoalCapability] = append(json.RawMessage(nil), encoded...)
	return next, nil
}

func (client *capabilityStateClient) assertGoalCurrent() error {
	if client == nil || client.emit == nil {
		return agentschema.ErrCapabilityUnsupported
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	current, present := client.states[agentgoal.GoalCapability]
	return client.emit(CapabilityState{
		Capability: agentgoal.GoalCapability, CompareCurrent: true, ExpectedPresent: present,
		ExpectedState: append(json.RawMessage(nil), current...), CheckOnly: true,
	})
}

func (client *capabilityStateClient) goal() (agentgoal.GoalState, bool, error) {
	if client == nil {
		return agentgoal.GoalState{}, false, nil
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	raw, present := client.states[agentgoal.GoalCapability]
	if !present {
		return agentgoal.GoalState{}, false, nil
	}
	state, err := agentgoal.DecodeGoalState(raw)
	return state, present, err
}
