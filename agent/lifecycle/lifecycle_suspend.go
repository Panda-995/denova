package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

const sessionControlRecord = "session.control"

// SuspendRequest stops one Session at tool-safe boundaries and releases its
// writer. Hosts coordinate descendants through their existing task service.
type SuspendRequest struct{ RunID, Reason, IdempotencyKey string }

type ResumeRequest struct{ RunID, IdempotencyKey string }

type Suspension struct {
	Session agentsession.Key
	RunID   string
	Status  agentschema.ResultStatus
	Receipt agentevent.CommandReceipt
}

type persistedSessionControl struct {
	persistedControlReceipt
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
}

func (session *Session) acceptControlLocked(ctx context.Context, kind, runID, id, reason string) (agentevent.CommandReceipt, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = newPublicID("command")
	}
	if err := agentexecution.ValidateIdempotencyKey(id); err != nil {
		return agentevent.CommandReceipt{}, err
	}
	hash, err := agentschema.HashCanonical(struct{ Kind, RunID, Reason string }{kind, runID, reason})
	if err != nil {
		return agentevent.CommandReceipt{}, err
	}
	if previous, found := session.controlReceipts[id]; found {
		if previous.Hash != hash {
			return agentevent.CommandReceipt{}, ErrIdempotencyConflict
		}
		return previous.Receipt, nil
	}
	if session.inputs[id] != nil {
		return agentevent.CommandReceipt{}, ErrIdempotencyConflict
	}
	control := persistedSessionControl{persistedControlReceipt: persistedControlReceipt{
		Receipt: agentevent.CommandReceipt{CommandID: id, RunID: runID, Cursor: session.cursor + 1}, Hash: hash}, Kind: kind, Reason: reason}
	if err := session.appendRecordLocked(ctx, sessionControlRecord, control); err != nil {
		return agentevent.CommandReceipt{}, err
	}
	session.controlReceipts[id], session.cursor = control.persistedControlReceipt, control.Receipt.Cursor
	session.lastControl = control
	return control.Receipt, nil
}

func (session *Session) replayControl(data json.RawMessage) error {
	var control persistedSessionControl
	if err := json.Unmarshal(data, &control); err != nil {
		return err
	}
	if control.Receipt.CommandID == "" || control.Hash == "" {
		return errors.New("invalid Agent control receipt")
	}
	switch control.Kind {
	case "suspend", "resume", "abort":
	case "suspend_tree", "resume_tree", "abort_tree", "release_tree":
		var tree persistedTreeControl
		if err := json.Unmarshal(data, &tree); err != nil {
			return err
		}
		if tree.TreeID == "" {
			return errors.New("Agent tree control has no command identity")
		}
		session.treeControl = tree
	default:
		return errors.New("unsupported persisted Agent control")
	}
	session.controlReceipts[control.Receipt.CommandID] = control.persistedControlReceipt
	session.lastControl = control
	session.cursor = max(session.cursor, control.Receipt.Cursor)
	return nil
}

func (session *Session) SuspendAndClose(ctx context.Context, request SuspendRequest) (Suspension, error) {
	return session.suspendAndClose(ctx, request, nil)
}

func (session *Session) suspendAndClose(ctx context.Context, request SuspendRequest, stopDescendants func() error) (Suspension, error) {
	ctx, err := commandContext(ctx)
	if err != nil {
		return Suspension{}, err
	}
	if session == nil {
		return Suspension{}, agentschema.ErrSessionClosed
	}
	session.mu.Lock()
	if previous, found := session.controlReceipts[strings.TrimSpace(request.IdempotencyKey)]; found && (session.lastControl.Receipt.Cursor > previous.Receipt.Cursor || session.active == nil || request.RunID != "" && session.active.id != request.RunID) {
		reason := strings.TrimSpace(request.Reason)
		if reason == "" {
			reason = "Agent Session suspended"
		}
		runID := request.RunID
		if runID == "" {
			runID = previous.Receipt.RunID
		}
		receipt, err := session.acceptControlLocked(ctx, "suspend", runID, request.IdempotencyKey, reason)
		session.mu.Unlock()
		return Suspension{Session: session.Key(), RunID: runID, Status: agentschema.ResultSuspended, Receipt: receipt}, err
	}
	run := session.active
	if request.RunID != "" && (run == nil || run.id != request.RunID) {
		session.mu.Unlock()
		return Suspension{}, agentschema.ErrNoActiveRun
	}
	runID := request.RunID
	if run != nil {
		runID = run.id
	}
	result := Suspension{Session: session.Key(), RunID: runID, Status: agentschema.ResultSuspended}
	result.Receipt = session.lastControl.Receipt
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "Agent Session suspended"
	}
	if session.closed || session.closing {
		if _, accepted := session.controlReceipts[request.IdempotencyKey]; !accepted {
			session.mu.Unlock()
			return Suspension{}, agentschema.ErrSessionClosed
		}
		receipt, err := session.acceptControlLocked(ctx, "suspend", runID, request.IdempotencyKey, reason)
		if err != nil {
			session.mu.Unlock()
			return Suspension{}, err
		}
		result.Receipt = receipt
	}
	if session.closed {
		err = session.closeErr
		session.mu.Unlock()
		return result, err
	}
	if !session.closing {
		receipt, err := session.acceptControlLocked(ctx, "suspend", runID, request.IdempotencyKey, reason)
		if err != nil {
			session.mu.Unlock()
			return Suspension{}, err
		}
		result.Receipt = receipt
		session.closing, session.closeDone = true, make(chan struct{})
		if run != nil {
			run.mu.Lock()
			run.suspendReason = reason
			run.mu.Unlock()
		}
		agentasync.SafeGo(func() { session.completeSuspension(run, stopDescendants) }, func(err error) {
			slog.Error("Agent Session suspension failed", "session_id", session.key.ID, "error", err)
			session.mu.Lock()
			session.closeErr = err
			close(session.closeDone)
			session.mu.Unlock()
		})
	}
	done := session.closeDone
	session.mu.Unlock()
	select {
	case <-done:
		session.mu.RLock()
		err := session.closeErr
		session.mu.RUnlock()
		return result, err
	case <-ctx.Done():
		return result, ctx.Err()
	}
}

func (session *Session) completeSuspension(run *Run, stopDescendants func() error) {
	if run != nil && !run.isSuspended() && !run.isSettled() {
		select {
		case run.controls <- agentengine.Control{Kind: agentengine.ControlSuspend}:
		case <-run.executionDone:
		}
	}
	var err error
	if stopDescendants != nil {
		err = stopDescendants()
	}
	if run != nil {
		<-run.executionDone
	}
	if run != nil {
		run.mu.RLock()
		err = errors.Join(err, run.err)
		run.mu.RUnlock()
	}
	if err != nil && stopDescendants != nil {
		// A partial tree stop keeps the root writer and admission fence. The
		// caller can retry the same command after addressing the failed child.
		session.mu.Lock()
		session.closeErr, session.closing = err, false
		close(session.closeDone)
		session.mu.Unlock()
		if run != nil {
			run.endHandle()
		}
		return
	}
	err = errors.Join(err, session.closeWriter())
	session.mu.Lock()
	session.closeErr = err
	pending := append([]*Run(nil), session.pending...)
	session.mu.Unlock()
	if run != nil {
		run.mu.Lock()
		run.err = err
		run.mu.Unlock()
		run.endHandle()
	}
	for _, queued := range pending {
		queued.mu.Lock()
		queued.result = agentschema.Result{Status: agentschema.ResultSuspended, Reason: "Agent Session suspended before task activation"}
		queued.mu.Unlock()
		queued.cancel()
		queued.endHandle()
	}
	session.mu.Lock()
	close(session.closeDone)
	session.mu.Unlock()
}

func (session *Session) closeWriter() error {
	err := session.log.Close()
	session.mu.Lock()
	session.closed = true
	pending := append([]*Run(nil), session.pending...)
	for id, observer := range session.observers {
		delete(session.observers, id)
		close(observer.events)
		close(observer.errors)
	}
	session.mu.Unlock()
	for _, run := range pending {
		if run.isSettled() {
			continue
		}
		run.mu.Lock()
		run.result = agentschema.Result{Status: agentschema.ResultSuspended, Reason: "Agent Session writer closed before task activation"}
		run.mu.Unlock()
		run.cancel()
		run.endHandle()
	}
	canonical, _ := agentsession.CanonicalKey(session.key)
	session.agent.mu.Lock()
	if session.agent.sessions[canonical] == session {
		delete(session.agent.sessions, canonical)
	}
	session.agent.mu.Unlock()
	return err
}

func (run *Run) suspensionRequested() bool {
	run.mu.RLock()
	defer run.mu.RUnlock()
	return run.suspendReason != ""
}

func (run *Run) suspendExecution() {
	err := run.persistEngineTranscript()
	run.mu.Lock()
	run.result, run.err = agentschema.Result{Status: agentschema.ResultSuspended, Reason: run.suspendReason}, err
	run.mu.Unlock()
	run.cancel()
}

// ResumeRun creates a fresh process handle for the same logical Run. Opening
// and attaching never execute work, and a running Resume is idempotent.
func (session *Session) ResumeRun(ctx context.Context, request ResumeRequest) (*Run, error) {
	return session.resumeRun(ctx, request, "")
}

// ResumeRunWithReceipt returns the resume command receipt, separate from the
// original Run input. Exact retries never resume work again after later controls.
func (session *Session) ResumeRunWithReceipt(ctx context.Context, request ResumeRequest) (*Run, agentevent.CommandReceipt, error) {
	return session.resumeRunCommand(ctx, request, "")
}

func (session *Session) resumeRun(ctx context.Context, request ResumeRequest, treeID string) (*Run, error) {
	run, _, err := session.resumeRunCommand(ctx, request, treeID)
	return run, err
}

func (session *Session) resumeRunCommand(ctx context.Context, request ResumeRequest, treeID string) (*Run, agentevent.CommandReceipt, error) {
	ctx, err := commandContext(ctx)
	if err != nil {
		return nil, agentevent.CommandReceipt{}, err
	}
	if err := session.usable(); err != nil {
		return nil, agentevent.CommandReceipt{}, err
	}
	session.agent.admissionMu.Lock()
	defer session.agent.admissionMu.Unlock()
	sealed, err := session.agent.treeSealed(ctx, session.key, treeID)
	if err != nil {
		return nil, agentevent.CommandReceipt{}, err
	}
	if sealed {
		return nil, agentevent.CommandReceipt{}, agentschema.ErrSessionBusy
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closing || session.closed {
		return nil, agentevent.CommandReceipt{}, agentschema.ErrSessionClosed
	}
	if _, found := session.controlReceipts[strings.TrimSpace(request.IdempotencyKey)]; found {
		receipt, err := session.acceptControlLocked(ctx, "resume", request.RunID, request.IdempotencyKey, "")
		return session.runs[request.RunID], receipt, err
	}
	previous := session.runs[strings.TrimSpace(request.RunID)]
	if previous == nil {
		return nil, agentevent.CommandReceipt{}, agentschema.ErrNoActiveRun
	}
	if previous.isSettled() {
		return nil, agentevent.CommandReceipt{}, agentschema.ErrRunSettled
	}
	if session.active != nil && session.active != previous {
		return nil, agentevent.CommandReceipt{}, agentschema.ErrSessionBusy
	}
	receipt, err := session.acceptControlLocked(ctx, "resume", previous.id, request.IdempotencyKey, "")
	if err != nil {
		return nil, agentevent.CommandReceipt{}, err
	}
	if session.lastControl.Receipt.Cursor > receipt.Cursor {
		return previous, receipt, nil
	}
	if !previous.isSuspended() && session.active == previous {
		return previous, receipt, nil
	}
	run := newPublicRun(session, previous.id, previous.commandID, previous.input, previous.delivery, runUsesSession)
	run.receipt, run.startedAt, run.cycle, run.snapshot = previous.receipt, previous.startedAt, previous.cycle, previous.snapshot
	run.resuming = run.cycle > 0
	run.treeResumeID = treeID
	run.tools, run.responses, run.interactions = previous.tools, previous.responses, previous.interactions
	session.runs[run.id], session.active = run, run
	session.removePendingLocked(previous)
	for index, pending := range session.pending {
		if !pending.isSuspended() {
			// Reopened queued Runs have fresh handles but still need the tree
			// permit while ancestors are being resumed, descendant first.
			pending.treeResumeID = treeID
			continue
		}
		fresh := newPublicRun(session, pending.id, pending.commandID, pending.input, pending.delivery, pending.ownership)
		fresh.receipt, fresh.treeResumeID = pending.receipt, treeID
		session.pending[index], session.runs[fresh.id] = fresh, fresh
	}
	agentasync.SafeGo(run.execute, func(err error) {
		run.finish(agentschema.Result{Status: agentschema.ResultFailed, Reason: err.Error()}, err)
	})
	return run, receipt, nil
}
