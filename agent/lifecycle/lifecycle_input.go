package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

const (
	sessionInputRecord       = "session.input"
	sessionInputUpdateRecord = "session.input_update"
	// The inbox is bounded independently of retained conversation history.
	// Accepted messages are never truncated or silently evicted.
	maxPendingSessionInputs     = 1024
	maxPendingSessionInputBytes = 64 << 20
	maxSessionInputBytes        = 16 << 20
)

var (
	ErrIdempotencyConflict = errors.New("agent command ID was already used for different content")
	ErrInputConsumed       = errors.New("agent input was already consumed")
	ErrInputCancelled      = errors.New("agent input was cancelled")
	ErrInputQueueFull      = errors.New("agent input queue is full")
)

type sessionInputKind string

const (
	inputRun      sessionInputKind = "run"
	inputFollowUp sessionInputKind = "follow_up"
	inputQueue    sessionInputKind = "queue"
	inputSteer    sessionInputKind = "steer"
)

type inputStatus string

const (
	inputPending   inputStatus = "pending"
	inputConsumed  inputStatus = "consumed"
	inputCancelled inputStatus = "cancelled"
)

type persistedInput struct {
	Receipt agentevent.CommandReceipt `json:"receipt"`
	Kind    sessionInputKind          `json:"kind"`
	Hash    string                    `json:"hash"`
	Input   agentengine.UserInput     `json:"input"`
}

type persistedInputUpdate struct {
	CommandID string                   `json:"command_id"`
	RunID     string                   `json:"run_id,omitempty"`
	Status    inputStatus              `json:"status"`
	Delivery  agentengine.DeliveryKind `json:"delivery,omitempty"`
	Control   *persistedControlReceipt `json:"control,omitempty"`
	HostData  *agentschema.HostData    `json:"host_data,omitempty"`
}

type persistedControlReceipt struct {
	Receipt agentevent.CommandReceipt `json:"receipt"`
	Hash    string                    `json:"hash"`
}

type acceptedInput struct {
	persistedInput
	input       agentschema.Input
	targetRunID string
	status      inputStatus
	delivery    agentengine.DeliveryKind
	bytes       int
}

// FollowUp durably accepts a distinct task. An idle Session starts it; a busy
// or suspended Session keeps it queued. The receipt does not imply execution.
func (session *Session) FollowUp(ctx context.Context, input agentschema.Input) (agentevent.CommandReceipt, error) {
	receipt, _, err := session.receiveInput(ctx, input, inputFollowUp, "", runUsesSession)
	return receipt, err
}

// Queue stores a supplemental message without starting a Run. Messages
// received while idle bind to the next explicit Run/FollowUp activation.
func (session *Session) Queue(ctx context.Context, input agentschema.Input) (*QueuedInput, error) {
	receipt, _, err := session.receiveInput(ctx, input, inputQueue, "", runUsesSession)
	if err != nil {
		return nil, err
	}
	return &QueuedInput{session: session, id: receipt.CommandID}, nil
}

func (session *Session) Queued(ctx context.Context, id string) (*QueuedInput, bool, error) {
	if _, err := commandContext(ctx); err != nil {
		return nil, false, err
	}
	if err := session.usable(); err != nil {
		return nil, false, err
	}
	session.mu.RLock()
	input := session.inputs[strings.TrimSpace(id)]
	session.mu.RUnlock()
	if input == nil || (input.Kind != inputQueue && input.Kind != inputSteer) {
		return nil, false, nil
	}
	return &QueuedInput{session: session, id: input.Receipt.CommandID}, true, nil
}

func (session *Session) receiveInput(ctx context.Context, input agentschema.Input, kind sessionInputKind, target string, ownership runSessionOwnership) (agentevent.CommandReceipt, *Run, error) {
	ctx, err := commandContext(ctx)
	if err != nil {
		return agentevent.CommandReceipt{}, nil, err
	}
	if err := session.usable(); err != nil {
		return agentevent.CommandReceipt{}, nil, err
	}
	session.agent.admissionMu.Lock()
	defer session.agent.admissionMu.Unlock()
	if err := session.usable(); err != nil {
		return agentevent.CommandReceipt{}, nil, err
	}
	sealed, err := session.agent.treeSealed(ctx, session.key, resumeTreePermit(ctx))
	if err != nil {
		return agentevent.CommandReceipt{}, nil, err
	}
	if _, child := session.key.Attributes[ParentSessionAttribute]; child && ((kind == inputRun && !sealed) || kind == inputFollowUp || kind == inputQueue) {
		parentKey, err := ParentSessionKey(session.key)
		if err != nil {
			return agentevent.CommandReceipt{}, nil, err
		}
		exists, err := session.agent.SessionExists(ctx, parentKey)
		if err != nil {
			return agentevent.CommandReceipt{}, nil, err
		}
		// Independent child owners register through LocalTasks in the actual
		// parent store; admission must not create a second parent journal.
		if exists {
			parent, err := session.agent.Session(ctx, parentKey)
			if err != nil {
				return agentevent.CommandReceipt{}, nil, err
			}
			if err := parent.RegisterChildTask(ctx, ChildTask{Key: session.key}); err != nil {
				return agentevent.CommandReceipt{}, nil, err
			}
		}
	}
	input = cloneInput(input)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = newPublicID("command")
	}
	if err := agentexecution.ValidateIdempotencyKey(input.IdempotencyKey); err != nil {
		return agentevent.CommandReceipt{}, nil, err
	}
	if input.Goal != nil && input.Goal.MutationID == "" {
		key, err := agentschema.HashCanonical(input.IdempotencyKey)
		if err != nil {
			return agentevent.CommandReceipt{}, nil, err
		}
		input.Goal.MutationID = "goal-" + key[:32]
	}
	_, payload, err := agentengine.EncodeInput(input)
	if err != nil {
		return agentevent.CommandReceipt{}, nil, err
	}
	hash, err := agentschema.HashCanonical(struct {
		Kind   sessionInputKind
		Target string
		Input  agentschema.Input
	}{kind, target, input})
	if err != nil {
		return agentevent.CommandReceipt{}, nil, err
	}
	session.mu.Lock()
	if session.closed || session.closing {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, nil, agentschema.ErrSessionClosed
	}
	if previous := session.inputs[input.IdempotencyKey]; previous != nil {
		session.mu.Unlock()
		if previous.Hash != hash {
			return agentevent.CommandReceipt{}, nil, ErrIdempotencyConflict
		}
		run, _, err := session.AttachRun(ctx, previous.Receipt.RunID)
		return previous.Receipt, run, err
	}
	if _, found := session.controlReceipts[input.IdempotencyKey]; found {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, nil, ErrIdempotencyConflict
	}
	active := session.active
	if kind == inputRun && (sealed || active != nil || session.maintenance || len(session.pending) > 0) {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, nil, agentschema.ErrSessionBusy
	}
	if kind == inputSteer {
		if active == nil || active.id != target || active.isSettled() {
			session.mu.Unlock()
			return agentevent.CommandReceipt{}, nil, agentschema.ErrRunSettled
		}
	}
	receipt := agentevent.CommandReceipt{CommandID: input.IdempotencyKey, Cursor: session.cursor + 1}
	var run, activate *Run
	delivery := agentengine.DeliveryFollowUp
	switch kind {
	case inputRun, inputFollowUp:
		id, err := session.agent.nextRunID(session.key)
		if err != nil {
			session.mu.Unlock()
			return agentevent.CommandReceipt{}, nil, err
		}
		receipt.RunID = id
		delivery = agentengine.DeliveryStart
		if kind == inputFollowUp {
			delivery = agentengine.DeliveryNextTurn
		}
		run = newPublicRun(session, id, input.IdempotencyKey, input, delivery, ownership)
		if active == nil && !session.maintenance && !sealed {
			activate = run
			if len(session.pending) > 0 {
				activate = session.pending[0]
			}
		}
	case inputQueue, inputSteer:
		if active != nil && !active.isSettled() {
			receipt.RunID = active.id
			run = active
		}
		if kind == inputSteer {
			delivery = agentengine.DeliverySteer
		}
	default:
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, nil, fmt.Errorf("unsupported input action %q", kind)
	}
	if (kind == inputQueue || kind == inputSteer) && active != nil && input.HostData == nil {
		input.HostData = agentschema.CloneHostData(active.input.HostData)
		_, payload, err = agentengine.EncodeInput(input)
		if err != nil {
			session.mu.Unlock()
			return agentevent.CommandReceipt{}, nil, err
		}
	}
	stored := persistedInput{Receipt: receipt, Kind: kind, Hash: hash, Input: payload}
	encoded, err := json.Marshal(stored)
	if err != nil {
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, nil, err
	}
	count, bytes := 0, len(encoded)
	for _, item := range session.inputs {
		if item.status == inputPending {
			count++
			bytes += item.bytes
		}
	}
	if count >= maxPendingSessionInputs || bytes > maxPendingSessionInputBytes || len(encoded) > maxSessionInputBytes {
		if run != nil && (kind == inputRun || kind == inputFollowUp) {
			run.cancel()
		}
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, nil, ErrInputQueueFull
	}
	records := []agentsession.Record{{Kind: sessionInputRecord, Version: sessionRecordVersion, Data: encoded}}
	startedAt := time.Now().UTC()
	if activate != nil {
		started, err := sessionRecord(turnStartedRecord, persistedTurn{RunID: activate.id, CommandID: activate.commandID, At: startedAt})
		if err != nil {
			session.mu.Unlock()
			return agentevent.CommandReceipt{}, nil, err
		}
		records = append(records, started)
		for _, item := range session.inputOrder {
			if item.status == inputPending && item.Kind == inputQueue && item.targetRunID == "" {
				bound := persistedInputUpdate{CommandID: item.Receipt.CommandID, RunID: activate.id, Status: inputPending, Delivery: item.delivery}
				if item.input.HostData == nil {
					bound.HostData = agentschema.CloneHostData(activate.input.HostData)
				}
				update, err := sessionRecord(sessionInputUpdateRecord, bound)
				if err != nil {
					session.mu.Unlock()
					return agentevent.CommandReceipt{}, nil, err
				}
				records = append(records, update)
			}
		}
	}
	if err := session.appendRecordsLocked(ctx, records...); err != nil {
		if run != nil && (kind == inputRun || kind == inputFollowUp) {
			run.cancel()
		}
		session.mu.Unlock()
		return agentevent.CommandReceipt{}, nil, err
	}
	item := &acceptedInput{persistedInput: stored, input: input, targetRunID: receipt.RunID, status: inputPending, delivery: delivery, bytes: len(encoded)}
	session.inputs[receipt.CommandID] = item
	if kind == inputSteer {
		session.inputOrder = append([]*acceptedInput{item}, session.inputOrder...)
	} else {
		session.inputOrder = append(session.inputOrder, item)
	}
	session.cursor = receipt.Cursor
	if kind == inputRun || kind == inputFollowUp {
		run.receipt = receipt.Cursor
		session.runs[run.id] = run
		if activate != run {
			session.pending = append(session.pending, run)
		}
	}
	if activate != nil {
		if len(session.pending) > 0 && session.pending[0] == activate {
			session.pending = session.pending[1:]
		}
		activate.markStarted(startedAt)
		session.active = activate
		for _, record := range records {
			if record.Kind == sessionInputUpdateRecord {
				if err := session.replayInput(record); err != nil {
					session.mu.Unlock()
					return agentevent.CommandReceipt{}, nil, err
				}
			}
		}
	}
	session.mu.Unlock()
	if kind == inputRun || kind == inputFollowUp {
		run.publish(agentevent.RunAccepted{CommandID: receipt.CommandID})
	}
	if activate != nil {
		agentasync.SafeGo(activate.execute, func(err error) {
			activate.finish(agentschema.Result{Status: agentschema.ResultFailed, Reason: err.Error()}, err)
		})
	}
	if kind == inputSteer && active != nil && !active.isSuspended() {
		active.requestPreemption()
	}
	return receipt, run, nil
}

func sessionRecord(kind string, value any) (agentsession.Record, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return agentsession.Record{}, err
	}
	return agentsession.Record{Kind: kind, Version: sessionRecordVersion, Data: encoded}, nil
}

func (run *Run) requestPreemption() {
	select {
	case run.controls <- agentengine.Control{Kind: agentengine.ControlPreempt}:
	case <-run.done:
	}
}

func (session *Session) replayInput(record agentsession.Record) error {
	switch record.Kind {
	case sessionInputRecord:
		var stored persistedInput
		if err := json.Unmarshal(record.Data, &stored); err != nil {
			return err
		}
		if stored.Receipt.CommandID == "" || stored.Hash == "" {
			return errors.New("invalid persisted Agent input")
		}
		input, err := agentengine.DecodeInput(stored.Input)
		if err != nil {
			return err
		}
		input.IdempotencyKey = stored.Receipt.CommandID
		delivery := agentengine.DeliveryFollowUp
		switch stored.Kind {
		case inputRun:
			delivery = agentengine.DeliveryStart
		case inputFollowUp:
			delivery = agentengine.DeliveryNextTurn
		case inputQueue:
		case inputSteer:
			delivery = agentengine.DeliverySteer
		default:
			return fmt.Errorf("unsupported persisted input kind %q", stored.Kind)
		}
		item := &acceptedInput{persistedInput: stored, input: input, targetRunID: stored.Receipt.RunID, status: inputPending, delivery: delivery, bytes: len(record.Data)}
		session.inputs[stored.Receipt.CommandID] = item
		if stored.Kind == inputSteer {
			session.inputOrder = append([]*acceptedInput{item}, session.inputOrder...)
		} else {
			session.inputOrder = append(session.inputOrder, item)
		}
		session.cursor = max(session.cursor, stored.Receipt.Cursor)
		if stored.Kind == inputRun || stored.Kind == inputFollowUp {
			run := newPublicRun(session, stored.Receipt.RunID, stored.Receipt.CommandID, input, delivery, runUsesSession)
			run.receipt = stored.Receipt.Cursor
			session.runs[run.id] = run
			session.pending = append(session.pending, run)
		}
	case sessionInputUpdateRecord:
		var update persistedInputUpdate
		if err := json.Unmarshal(record.Data, &update); err != nil {
			return err
		}
		item := session.inputs[update.CommandID]
		if item == nil {
			return errors.New("persisted input update has no accepted input")
		}
		switch update.Status {
		case inputPending, inputConsumed, inputCancelled:
		default:
			return errors.New("invalid persisted input status")
		}
		// A retained control update may refer to an archived input. Its final
		// metadata was restored from the index; only the control receipt is needed.
		if item.input.Text == "" && len(item.input.Attachments) == 0 {
			if update.Control != nil {
				session.controlReceipts[update.Control.Receipt.CommandID] = *update.Control
				session.cursor = max(session.cursor, update.Control.Receipt.Cursor)
			}
			return nil
		}
		item.status, item.targetRunID = update.Status, update.RunID
		if update.HostData != nil {
			if item.input.HostData != nil {
				return errors.New("queued input host ownership is already bound")
			}
			item.input.HostData = agentschema.CloneHostData(update.HostData)
			_, payload, err := agentengine.EncodeInput(item.input)
			if err != nil {
				return err
			}
			item.Input = payload
		}
		if update.Delivery != "" {
			item.delivery = update.Delivery
		}
		if item.status == inputPending && item.delivery == agentengine.DeliverySteer {
			session.promoteInputLocked(item)
		}
		if update.Control != nil {
			session.controlReceipts[update.Control.Receipt.CommandID] = *update.Control
			session.cursor = max(session.cursor, update.Control.Receipt.Cursor)
		}
	}
	return nil
}
