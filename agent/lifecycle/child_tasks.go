package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

const sessionChildTasksRecord = "session.child_tasks"

// ChildTask is a parent's durable completion obligation. Registration precedes
// child admission. Accepted receipts and delivery acknowledgments remain in
// their respective canonical journals.
// The child's own journal remains the authority for execution and results.
type ChildTask struct {
	Key agentsession.Key `json:"key"`
}

type childAdoptionCatalogKey struct{}

type childTasks struct {
	Initialized bool                 `json:"initialized"`
	Pending     map[string]ChildTask `json:"pending,omitempty"`
}

// RegisterChildTask writes the admission intent before opening/running a child.
// Repeating the same identity is safe, including after a lost child receipt.
func (session *Session) RegisterChildTask(ctx context.Context, child ChildTask) error {
	if _, err := session.PendingChildTasks(ctx); err != nil {
		return err
	}
	key, err := agentsession.NormalizeKey(child.Key)
	if err != nil {
		return err
	}
	parent, err := ParentSessionKey(key)
	if err != nil {
		return err
	}
	parentID, _ := agentsession.CanonicalKey(parent)
	selfID, _ := agentsession.CanonicalKey(session.key)
	if parentID != selfID {
		return fmt.Errorf("child task does not belong to this Session")
	}
	child.Key = key
	id, _ := agentsession.CanonicalKey(key)
	session.mu.Lock()
	defer session.mu.Unlock()
	_, found := session.recovery.Children.Pending[id]
	if found {
		return nil
	}
	next := session.recovery.Children.clone()
	next.Pending[id] = child
	return session.appendRecordLocked(ctx, sessionChildTasksRecord, next)
}

// PendingChildTasks lists current obligations without catalog discovery once
// adopted. Completed children stay until parent delivery and descendant work
// are both settled. Suspended children always remain recoverable.
func (session *Session) PendingChildTasks(ctx context.Context) ([]ChildTask, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	session.childTasksMu.Lock()
	defer session.childTasksMu.Unlock()
	session.mu.RLock()
	state := session.recovery.Children.clone()
	revision := session.revision
	session.mu.RUnlock()
	if state.Initialized {
		return state.values(), nil
	}
	state.Initialized = true
	// A new journal has no legacy children to discover. Released journals are
	// adopted once by appending a checkpoint; their existing bytes are untouched.
	if revision != 0 {
		catalog, inherited := ctx.Value(childAdoptionCatalogKey{}).(map[string][]agentsession.Key)
		if !inherited {
			// Released trees have no pending-edge checkpoints. Enumerate once
			// and group by exact parent; recursive adoption reuses this snapshot
			// instead of rereading every manifest for every historical child.
			keys, err := session.agent.ListSessions(ctx, agentsession.Selector{All: true})
			if err != nil {
				return nil, err
			}
			catalog = make(map[string][]agentsession.Key)
			for _, key := range keys {
				if key.Attributes[ParentSessionAttribute] == "" {
					continue
				}
				parent, err := ParentSessionKey(key)
				if err != nil {
					continue
				} // An invalid edge cannot name this parent.
				canonical, _ := agentsession.CanonicalKey(parent)
				catalog[canonical] = append(catalog[canonical], key)
			}
			ctx = context.WithValue(ctx, childAdoptionCatalogKey{}, catalog)
		}
		self, _ := agentsession.CanonicalKey(session.key)
		keys := catalog[self]
		for _, key := range keys {
			child, err := session.agent.Session(ctx, key)
			if err != nil {
				return nil, err
			}
			pending, err := child.PendingChildTasks(ctx)
			if err != nil {
				return nil, err
			}
			snapshot, err := child.Snapshot(ctx)
			if err != nil {
				return nil, err
			}
			obligation := ChildTask{Key: key}
			needed := snapshot.ActiveRunID != "" || len(snapshot.QueuedRuns) != 0 || len(snapshot.PendingInteractions) != 0 || len(pending) != 0
			results, err := session.PendingChildTaskResults(ctx, child)
			if err != nil {
				return nil, err
			}
			needed = needed || len(results) != 0
			if needed {
				id, _ := agentsession.CanonicalKey(key)
				state.Pending[id] = obligation
			} else {
				if err := child.Close(ctx); err != nil {
					return nil, err
				}
			}
		}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := session.appendRecordLocked(ctx, sessionChildTasksRecord, state); err != nil {
		return nil, err
	}
	return state.values(), nil
}

// releaseChildTask follows a checked settlement/rejection under the child's
// admission fence. Public callers cannot bypass that retirement proof.
func (session *Session) releaseChildTask(ctx context.Context, key agentsession.Key) error {
	id, err := agentsession.CanonicalKey(key)
	if err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if _, found := session.recovery.Children.Pending[id]; !found {
		return nil
	}
	next := session.recovery.Children.clone()
	delete(next.Pending, id)
	return session.appendRecordLocked(ctx, sessionChildTasksRecord, next)
}

// RetireChildTask checks delivery and all current work under the child's Native
// admission fence, then releases the parent obligation and closes the handle.
// It also supports a child hosted by a different Agent. A false result keeps
// the obligation intact, including the intent-before-admission crash gap.
func (session *Session) RetireChildTask(ctx context.Context, child *Session) (retired bool, err error) {
	if child == nil || child.agent == nil {
		return false, fmt.Errorf("child task Session is required")
	}
	child.agent.admissionMu.Lock()
	defer child.agent.admissionMu.Unlock()
	snapshot, err := child.Snapshot(ctx)
	if err != nil {
		return false, err
	}
	pending, err := child.PendingChildTasks(ctx)
	if err != nil {
		return false, err
	}
	results, err := session.PendingChildTaskResults(ctx, child)
	if err != nil {
		return false, err
	}
	if len(snapshot.RecentRuns) == 0 || snapshot.ActiveRunID != "" || len(snapshot.QueuedRuns) != 0 || len(snapshot.PendingInteractions) != 0 || len(pending) != 0 || len(results) != 0 {
		return false, nil
	}
	if err := session.releaseChildTask(ctx, child.key); err != nil {
		return false, err
	}
	return true, child.Close(ctx)
}

// DiscardRejectedChildTask removes only an authoritatively empty child after a
// rejected admission. Accepted inputs, including idle Queue inputs, prevent
// deletion. Inspection and deletion share the Native admission fence.
func (session *Session) DiscardRejectedChildTask(ctx context.Context, child *Session) (discarded bool, err error) {
	if child == nil || child.agent == nil {
		return false, fmt.Errorf("child task Session is required")
	}
	child.agent.admissionMu.Lock()
	defer child.agent.admissionMu.Unlock()
	if _, err := session.PendingChildTaskResults(ctx, child); err != nil {
		return false, err
	}
	pending, err := child.PendingChildTasks(ctx)
	if err != nil {
		return false, err
	}
	child.mu.RLock()
	empty := len(child.recovery.Inputs) == 0 && len(child.recovery.Runs) == 0 && child.storageErr == nil
	child.mu.RUnlock()
	if !empty || len(pending) != 0 {
		return false, nil
	}
	if err := session.releaseChildTask(ctx, child.key); err != nil {
		return false, err
	}
	return true, child.Delete(ctx)
}

// PendingChildTaskResults reads only unacknowledged terminal results. The
// bounded RecentRuns display window must never decide completion delivery.
// The caller opens the exact child through its owner; this also supports
// children hosted by another Agent/Store.
func (session *Session) PendingChildTaskResults(ctx context.Context, child *Session) ([]agentevent.RunSummary, error) {
	if child == nil {
		return nil, fmt.Errorf("child task Session is required")
	}
	parent, err := ParentSessionKey(child.key)
	if err != nil {
		return nil, err
	}
	parentID, _ := agentsession.CanonicalKey(parent)
	selfID, _ := agentsession.CanonicalKey(session.key)
	if parentID != selfID {
		return nil, fmt.Errorf("child task does not belong to this Session")
	}
	child.mu.RLock()
	runs := make([]RecoveryRun, 0, len(child.recovery.Runs))
	for _, run := range child.recovery.Runs {
		if run.Settlement != 0 && run.Status != agentschema.ResultSuspended {
			runs = append(runs, run)
		}
	}
	child.mu.RUnlock()
	sort.Slice(runs, func(i, j int) bool { return runs[i].Settlement < runs[j].Settlement })
	var results []agentevent.RunSummary
	for _, run := range runs {
		id := agentschema.TaskCompletionID(child.key.Attributes["agent"], child.key.ID, run.Receipt.RunID)
		session.mu.RLock()
		_, delivered := session.taskCompletions.delivered[id]
		session.mu.RUnlock()
		if delivered {
			continue
		}
		record, err := child.readRecord(ctx, run.Settlement)
		if err != nil {
			return nil, err
		}
		var turn persistedTurn
		if err := json.Unmarshal(record.Data, &turn); err != nil {
			return nil, err
		}
		results = append(results, agentevent.RunSummary{ID: turn.RunID, CommandID: turn.CommandID, ReceiptCursor: run.Receipt.Cursor, Status: turn.Status, Reason: turn.Reason, Output: turn.Output})
	}
	return results, nil
}

func (state childTasks) clone() childTasks {
	result := childTasks{Initialized: state.Initialized, Pending: make(map[string]ChildTask, len(state.Pending))}
	for id, child := range state.Pending {
		child.Key.Attributes = agentschema.CloneStringMap(child.Key.Attributes)
		result.Pending[id] = child
	}
	return result
}

func (state childTasks) values() []ChildTask {
	result := make([]ChildTask, 0, len(state.Pending))
	for _, child := range state.Pending {
		child.Key.Attributes = agentschema.CloneStringMap(child.Key.Attributes)
		result = append(result, child)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key.ID < result[j].Key.ID })
	return result
}

func decodeChildTasks(data json.RawMessage) (childTasks, error) {
	var state childTasks
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if !state.Initialized {
		return state, fmt.Errorf("child task checkpoint is not initialized")
	}
	for id, child := range state.Pending {
		canonical, err := agentsession.CanonicalKey(child.Key)
		if err != nil || canonical != id {
			return state, fmt.Errorf("invalid child task checkpoint identity")
		}
	}
	return state, nil
}
