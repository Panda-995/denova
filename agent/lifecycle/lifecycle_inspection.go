package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

// Inspect prepares the exact provider-neutral request for one prospective
// start turn without admitting a command, mutating transcript/capabilities, or
// invoking a model or tool. It succeeds only when the Session is idle for the
// complete optimistic read. Preparation capabilities and Middleware run under
// an inspection-marked context and must remain read-only.
//
// Goal mutations are deliberately rejected: they require durable admission
// and revision fencing before they can affect model context. Inspect the
// current Goal, or commit the mutation through Run/UpdateGoal first.
func (session *Session) Inspect(ctx context.Context, input agentschema.Input) (agentengine.Inspection, error) {
	if err := session.usable(); err != nil {
		return agentengine.Inspection{}, err
	}
	if input.Goal != nil {
		return agentengine.Inspection{}, errors.New("Agent Session inspection cannot preview an uncommitted Goal mutation")
	}
	commandID := strings.TrimSpace(input.IdempotencyKey)
	if commandID == "" {
		fingerprint, err := agentschema.HashCanonical(struct {
			Session agentsession.Key
			Input   agentschema.Input
		}{Session: session.key, Input: input})
		if err != nil {
			return agentengine.Inspection{}, fmt.Errorf("fingerprint Agent Session inspection: %w", err)
		}
		commandID = "inspection-" + fingerprint[:32]
		input.IdempotencyKey = commandID
	}
	if _, _, err := agentengine.EncodeInput(input); err != nil {
		return agentengine.Inspection{}, err
	}
	ctx = agentexecution.ContextWithInspection(ctx)
	session.mu.RLock()
	if session.active != nil {
		session.mu.RUnlock()
		return agentengine.Inspection{}, agentschema.ErrSessionBusy
	}
	revision := session.revision
	checkpointState := append([]byte(nil), session.engineState...)
	capabilities := agentschema.CloneRawStateMap(session.capabilities)
	session.mu.RUnlock()
	inspection, err := agentengine.Inspect(ctx, session.agent.source, session.agent.cacheKeys, agentengine.InspectionRequest{
		Session: agentschema.SessionView{Key: session.key, Revision: uint64(revision)},
		// This preview identity grants no lifecycle control authority.
		Run:   agentschema.RunView{ID: commandID, CommandID: commandID, Cycle: 1, StartedAt: time.Now().UTC(), Delivery: agentschema.TurnDeliveryStart},
		Input: input, State: checkpointState, Capabilities: capabilities,
	})
	if err != nil {
		return agentengine.Inspection{}, err
	}

	session.mu.RLock()
	unchanged := session.active == nil && session.revision == revision && string(session.engineState) == string(checkpointState)
	session.mu.RUnlock()
	if !unchanged {
		return agentengine.Inspection{}, fmt.Errorf("%w: Agent Session changed during inspection", agentschema.ErrSessionBusy)
	}
	return inspection, nil
}
