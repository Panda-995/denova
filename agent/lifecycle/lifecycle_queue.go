package lifecycle

import (
	"context"
	"strings"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// QueuedInput locates a durable supplemental input by Session and command ID.
// The handle owns no separate queue or execution state.
type QueuedInput struct {
	session *Session
	id      string
}

func (queued *QueuedInput) ID() string {
	if queued == nil {
		return ""
	}
	return queued.id
}

func (queued *QueuedInput) Receipt() agentevent.CommandReceipt {
	if queued == nil || queued.session == nil {
		return agentevent.CommandReceipt{}
	}
	queued.session.mu.RLock()
	defer queued.session.mu.RUnlock()
	if item := queued.session.inputs[queued.id]; item != nil {
		return item.Receipt
	}
	return agentevent.CommandReceipt{}
}

func (queued *QueuedInput) Cancel(ctx context.Context, request agentevent.QueueControlRequest) (agentevent.CommandReceipt, error) {
	return queued.control(ctx, request, inputCancelled)
}

// Interrupt promotes the input to the next safe steering boundary. It never
// starts an idle Session or resumes a suspended Run.
func (queued *QueuedInput) Interrupt(ctx context.Context, request agentevent.QueueControlRequest) (agentevent.CommandReceipt, error) {
	return queued.control(ctx, request, inputPending)
}

func (queued *QueuedInput) control(ctx context.Context, request agentevent.QueueControlRequest, status inputStatus) (agentevent.CommandReceipt, error) {
	ctx, err := commandContext(ctx)
	if err != nil {
		return agentevent.CommandReceipt{}, err
	}
	if queued == nil || queued.session == nil {
		return agentevent.CommandReceipt{}, agentschema.ErrRunSettled
	}
	session := queued.session
	if err := session.usable(); err != nil {
		return agentevent.CommandReceipt{}, err
	}
	id := strings.TrimSpace(request.IdempotencyKey)
	if id == "" {
		id = newPublicID("command")
	}
	if err := agentexecution.ValidateIdempotencyKey(id); err != nil {
		return agentevent.CommandReceipt{}, err
	}
	reason := strings.TrimSpace(request.Reason)
	if status == inputPending {
		reason = ""
	}
	hash, err := agentschema.HashCanonical(struct {
		InputID string
		Status  inputStatus
		Reason  string
	}{queued.id, status, reason})
	if err != nil {
		return agentevent.CommandReceipt{}, err
	}
	session.mu.Lock()
	if session.closed || session.closing {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, agentschema.ErrSessionClosed
	}
	if previous, found := session.controlReceipts[id]; found {
		session.mu.Unlock()
		if previous.Hash != hash {
			return agentevent.CommandReceipt{}, ErrIdempotencyConflict
		}
		return previous.Receipt, nil
	}
	if session.inputs[id] != nil {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, ErrIdempotencyConflict
	}
	item := session.inputs[queued.id]
	if item == nil {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, agentschema.ErrRunSettled
	}
	if item.status == inputConsumed {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, ErrInputConsumed
	}
	if item.status == inputCancelled {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, ErrInputCancelled
	}
	// An earlier Run's input cannot steer a later Run. Check after replaying
	// accepted receipts so retries keep their original result after settlement.
	if status == inputPending && item.targetRunID != "" && (session.active == nil || session.active.id != item.targetRunID) {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, agentschema.ErrRunSettled
	}
	control := persistedControlReceipt{Receipt: agentevent.CommandReceipt{CommandID: id, RunID: item.targetRunID, Cursor: session.cursor + 1}, Hash: hash}
	delivery := item.delivery
	if status == inputPending {
		delivery = agentengine.DeliverySteer
	}
	update := persistedInputUpdate{CommandID: queued.id, RunID: item.targetRunID, Status: status, Delivery: delivery, Control: &control}
	if err := session.appendRecordLocked(ctx, sessionInputUpdateRecord, update); err != nil {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, err
	}
	item.status, item.delivery = status, delivery
	if status == inputCancelled {
		item.input = agentschema.Input{}
		item.Input = agentengine.UserInput{}
		item.bytes = 0
	}
	session.controlReceipts[id] = control
	session.cursor = control.Receipt.Cursor
	if status == inputPending {
		session.promoteInputLocked(item)
	}
	active := session.active
	session.mu.Unlock()
	if status == inputPending && active != nil && active.id == item.targetRunID && !active.isSuspended() {
		active.requestPreemption()
	}
	return control.Receipt, nil
}

func (session *Session) promoteInputLocked(item *acceptedInput) {
	for index, candidate := range session.inputOrder {
		if candidate == item {
			copy(session.inputOrder[1:index+1], session.inputOrder[:index])
			session.inputOrder[0] = item
			return
		}
	}
}

func (run *Run) isSuspended() bool {
	if run == nil {
		return false
	}
	run.mu.RLock()
	defer run.mu.RUnlock()
	return run.result.Status == agentschema.ResultSuspended
}
