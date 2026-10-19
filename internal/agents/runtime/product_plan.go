package agentruntime

import (
	"context"
	"encoding/json"

	agentrun "denova/internal/agents/run"
	"denova/internal/agents/runtime/external"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
)

// Plan is a recovery projection of the selected runtime's plan. Only provider
// observations replace it; Denova does not execute a second Todo state machine.
func (store ProductState) Plan(ctx context.Context) ([]agentevent.TodoItem, error) {
	raw, present, err := store.Read(ctx, agentexecution.TodoCapability)
	var state agentevent.TodoState
	if err == nil && present {
		err = json.Unmarshal(raw, &state)
	}
	return state.Items, err
}

func (store ProductState) ObservePlan(ctx context.Context, event agentrun.Event) error {
	if event.Type != "todo_updated" {
		return nil
	}
	items, err := external.PlanItems(event)
	if err != nil {
		return err
	}
	return store.Update(ctx, agentexecution.TodoCapability, func(raw json.RawMessage, present bool) (json.RawMessage, error) {
		var state agentevent.TodoState
		if present {
			if err := json.Unmarshal(raw, &state); err != nil {
				return nil, err
			}
		}
		state.Revision++
		state.Items = items
		return json.Marshal(state)
	})
}
