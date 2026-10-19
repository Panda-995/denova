package external

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentrun "denova/internal/agents/run"
	"denova/internal/agents/session"

	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
)

// PlanEvent is an observed provider snapshot, never a host tool invocation.
// The native public Todo vocabulary is shared only at the presentation boundary.
func PlanEvent(items []agentevent.TodoItem) agentrun.Event {
	if items == nil {
		items = []agentevent.TodoItem{}
	}
	return agentrun.Event{Type: "todo_updated", Data: map[string]any{
		"id": "plan-" + rand.Text(), "schema": "agent.todo.v1", "items": items, "runtime_managed": true,
	}}
}

func PlanItems(event agentrun.Event) ([]agentevent.TodoItem, error) {
	raw, err := json.Marshal(event.Data)
	if err != nil {
		return nil, err
	}
	var snapshot struct {
		Items []agentevent.TodoItem `json:"items"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	for _, item := range snapshot.Items {
		if strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Text) == "" {
			return nil, fmt.Errorf("runtime plan requires item identity and text")
		}
		switch item.Status {
		case agentevent.TodoPending, agentevent.TodoInProgress, agentevent.TodoCompleted:
		default:
			return nil, fmt.Errorf("unknown runtime plan status %q", item.Status)
		}
	}
	return snapshot.Items, nil
}

func PlanDisplay(event agentrun.Event) (session.DisplayEvent, error) {
	items, err := PlanItems(event)
	if err != nil {
		return session.DisplayEvent{}, err
	}
	content, err := json.Marshal(map[string]any{"schema": "agent.todo.v1", "items": items})
	return session.DisplayEvent{ID: event.DataString("id"), Role: "todo_updated", Content: string(content), CreatedAt: time.Now().UTC()}, err
}
