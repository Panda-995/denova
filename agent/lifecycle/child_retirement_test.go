package lifecycle

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

func settledChildForRetirement(t *testing.T) (*Session, *Session) {
	t.Helper()
	owner, err := New(t.Context(), agentengine.Definition{Model: &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("done", nil)}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	parent, err := owner.Session(t.Context(), agentsession.Named("retirement-parent"))
	if err != nil {
		t.Fatal(err)
	}
	attributes, _ := ChildSessionAttributes(parent.Key())
	attributes["agent"] = "child"
	child, err := owner.Session(t.Context(), agentsession.Key{Namespace: "task.child", ID: "retirement-child", Attributes: attributes})
	if err != nil {
		t.Fatal(err)
	}
	run, err := child.Run(t.Context(), agentschema.Text("work"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	id := agentschema.TaskCompletionID("child", child.Key().ID, run.ID())
	parent.mu.Lock()
	err = parent.appendRecordLocked(t.Context(), sessionTaskCompletionDeliveryRecord, persistedTaskCompletionDelivery{IDs: []string{id}})
	if err == nil {
		parent.taskCompletions.delivered[id] = struct{}{}
	}
	parent.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	return parent, child
}

func TestChildRetirementRechecksIdleQueueAfterSettlementInspection(t *testing.T) {
	parent, child := settledChildForRetirement(t)
	before, err := child.Snapshot(t.Context())
	if err != nil || len(before.QueuedRuns) != 0 || len(before.RecentRuns) != 1 {
		t.Fatalf("settled snapshot=%+v err=%v", before, err)
	}
	queued, err := child.Queue(t.Context(), agentschema.Input{Text: "next task evidence", IdempotencyKey: "idle-queue"})
	if err != nil {
		t.Fatal(err)
	}
	if retired, err := parent.RetireChildTask(t.Context(), child); err != nil || retired {
		t.Fatalf("stale settlement inspection retired accepted Queue: retired=%v err=%v", retired, err)
	}
	if discarded, err := parent.DiscardRejectedChildTask(t.Context(), child); err != nil || discarded {
		t.Fatalf("rejected admission cleanup deleted accepted Queue: discarded=%v err=%v", discarded, err)
	}
	children, err := parent.PendingChildTasks(t.Context())
	if err != nil || len(children) != 1 {
		t.Fatalf("accepted Queue lost its parent obligation: children=%+v err=%v", children, err)
	}
	if _, err := queued.Cancel(t.Context(), agentevent.QueueControlRequest{IdempotencyKey: "cancel-queue"}); err != nil {
		t.Fatal(err)
	}
	if retired, err := parent.RetireChildTask(t.Context(), child); err != nil || !retired {
		t.Fatalf("acknowledged idle child was not retired: retired=%v err=%v", retired, err)
	}
}

func TestChildRetirementSharesNativeAdmissionFence(t *testing.T) {
	parent, child := settledChildForRetirement(t)
	child.agent.admissionMu.Lock()
	var unlock sync.Once
	t.Cleanup(func() { unlock.Do(child.agent.admissionMu.Unlock) })
	done := make(chan error, 1)
	agentasync.SafeGo(func() {
		retired, err := parent.RetireChildTask(t.Context(), child)
		if err == nil && !retired {
			err = errors.New("settled child was not retired")
		}
		done <- err
	}, func(err error) { done <- err })
	select {
	case err := <-done:
		t.Fatalf("retirement crossed an in-progress Native admission: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	unlock.Do(child.agent.admissionMu.Unlock)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retirement did not finish after admission released")
	}
	if _, err := child.Queue(t.Context(), agentschema.Text("late input")); !errors.Is(err, agentschema.ErrSessionClosed) {
		t.Fatalf("retired handle accepted another input: %v", err)
	}
	children, err := parent.PendingChildTasks(t.Context())
	if err != nil || len(children) != 0 {
		t.Fatalf("closed-handle admission recreated a retired obligation: %+v %v", children, err)
	}
}
