package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	sessionfile "github.com/alfredxw/denova/agent/session/file"
)

type interruptedTreeDiscovery struct {
	agentsession.Store
	probe            atomic.Bool
	entered, release chan struct{}
}

func (store *interruptedTreeDiscovery) Open(ctx context.Context, key agentsession.Key) (agentsession.Log, error) {
	log, err := store.Store.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	return &interruptedTreeLog{Log: log, store: store, key: key}, nil
}

type interruptedTreeLog struct {
	agentsession.Log
	store *interruptedTreeDiscovery
	key   agentsession.Key
}

func (log *interruptedTreeLog) Append(ctx context.Context, revision agentsession.Revision, records ...agentsession.Record) (agentsession.Revision, error) {
	if log.key.ID == "partial-child" {
		for _, record := range records {
			if record.Kind == sessionControlRecord && log.store.probe.CompareAndSwap(true, false) {
				close(log.store.entered)
				<-log.store.release
				return revision, context.Canceled
			}
		}
	}
	return log.Log.Append(ctx, revision, records...)
}

func TestPartialTreePauseKeepsAdmissionClosedAfterCommitFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	store := &interruptedTreeDiscovery{Store: agentsession.Memory(), entered: make(chan struct{}), release: make(chan struct{})}
	rootKey, childKey := agentsession.Named("partial-root"), agentsession.Named("partial-child")
	var err error
	childKey.Attributes, err = ChildSessionAttributes(rootKey)
	if err != nil {
		t.Fatal(err)
	}
	rootModel := &gatedLifecycleModel{started: make(chan struct{}), release: make(chan struct{})}
	owner, err := New(ctx, agentengine.Definition{Name: "test", Model: rootModel}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	root, err := owner.Session(ctx, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	run, err := root.Run(ctx, agentschema.Text("root task"))
	if err != nil {
		t.Fatal(err)
	}
	<-rootModel.started
	child, err := owner.Session(ctx, childKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.RegisterChildTask(ctx, ChildTask{Key: childKey}); err != nil {
		t.Fatal(err)
	}
	store.probe.Store(true)
	stopped := make(chan error, 1)
	agentasync.SafeGo(func() {
		_, err := owner.SuspendTree(ctx, rootKey, SuspendRequest{RunID: run.ID(), IdempotencyKey: "stop-tree"})
		stopped <- err
	}, func(err error) { stopped <- err })
	<-store.entered
	close(store.release)
	if err := <-stopped; err == nil {
		t.Fatal("partial tree pause reported success")
	}
	queued, err := child.FollowUp(ctx, agentschema.Input{Text: "received while stopping", IdempotencyKey: "pending-child"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := child.Run(ctx, agentschema.Text("start after partial failure")); !errors.Is(err, agentschema.ErrSessionBusy) {
		t.Fatalf("partial failure released admission: %v", err)
	}
	if _, err := owner.SuspendTree(ctx, rootKey, SuspendRequest{RunID: run.ID(), IdempotencyKey: "stop-tree"}); err != nil {
		t.Fatal(err)
	}
	if result, err := run.Wait(ctx); err != nil || result.Status != agentschema.ResultSuspended {
		t.Fatalf("root=%#v error=%v", result, err)
	}
	child, err = owner.Session(ctx, childKey)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := child.Snapshot(ctx)
	if err != nil || len(snapshot.QueuedRuns) != 1 || snapshot.QueuedRuns[0].ID != queued.RunID || rootModel.callCount() != 1 {
		t.Fatalf("stopped pending work=%#v error=%v", snapshot.QueuedRuns, err)
	}
	close(rootModel.release)
	resumed, err := owner.ResumeTree(ctx, rootKey, ResumeRequest{RunID: run.ID(), IdempotencyKey: "resume-tree"})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := resumed.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("resumed root=%#v error=%v", result, err)
	}
	attached, found, err := child.AttachRun(ctx, queued.RunID)
	if err != nil || !found {
		t.Fatalf("pending receipt disappeared: %v", err)
	}
	if result, err := attached.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("resumed pending=%#v error=%v", result, err)
	}
	if rootModel.callCount() != 3 {
		t.Fatal("pending work executed more than once")
	}
}

func TestTaskTreePauseReopenResumesParticipantsAndPendingWork(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := sessionfile.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootKey := agentsession.Named("tree")
	attributes, err := ChildSessionAttributes(rootKey)
	if err != nil {
		t.Fatal(err)
	}
	childKey, independentKey := agentsession.Named("child"), agentsession.Named("independent")
	childKey.Attributes, independentKey.Attributes = attributes, attributes
	models := map[string]*gatedLifecycleModel{}
	for _, key := range []agentsession.Key{rootKey, childKey, independentKey} {
		models[key.ID] = &gatedLifecycleModel{started: make(chan struct{}), release: make(chan struct{})}
	}
	owner, err := New(ctx, agentengine.SourceFunc(func(_ context.Context, request agentengine.PrepareRequest) (agentengine.Definition, error) {
		return agentengine.Definition{Name: "test", Model: models[request.Session.Key.ID]}, nil
	}), WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	runs := map[string]*Run{}
	for _, key := range []agentsession.Key{rootKey, childKey, independentKey} {
		session, err := owner.Session(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		runs[key.ID], err = session.Run(ctx, agentschema.Input{Text: key.ID, IdempotencyKey: "start"})
		if err != nil {
			t.Fatal(err)
		}
		<-models[key.ID].started
	}
	independent, _ := owner.Session(ctx, independentKey)
	if _, err := independent.SuspendAndClose(ctx, SuspendRequest{IdempotencyKey: "own-pause"}); err != nil {
		t.Fatal(err)
	}
	child, _ := owner.Session(ctx, childKey)
	pending, err := child.FollowUp(ctx, agentschema.Input{Text: "next child task", IdempotencyKey: "next"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SuspendTree(ctx, rootKey, SuspendRequest{RunID: runs[rootKey.ID].ID(), IdempotencyKey: "pause-tree"}); err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		if result, err := run.Wait(ctx); err != nil || result.Status != agentschema.ResultSuspended {
			t.Fatalf("paused result=%#v err=%v", result, err)
		}
	}
	if err := owner.Close(ctx); err != nil {
		t.Fatal(err)
	}
	for _, model := range models {
		close(model.release)
	}
	resumedModels := map[string]*lifecycleModel{}
	for _, key := range []agentsession.Key{rootKey, childKey, independentKey} {
		resumedModels[key.ID] = &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("first", nil), agentschema.AssistantMessage("second", nil)}}
	}
	owner, err = New(ctx, agentengine.SourceFunc(func(_ context.Context, request agentengine.PrepareRequest) (agentengine.Definition, error) {
		return agentengine.Definition{Name: "test", Model: resumedModels[request.Session.Key.ID]}, nil
	}), WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	child, err = owner.Session(ctx, childKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := child.ResumeRun(ctx, ResumeRequest{RunID: runs[childKey.ID].ID(), IdempotencyKey: "bypass"}); !errors.Is(err, agentschema.ErrSessionBusy) {
		t.Fatalf("child bypass error=%v", err)
	}
	root, err := owner.ResumeTree(ctx, rootKey, ResumeRequest{RunID: runs[rootKey.ID].ID(), IdempotencyKey: "resume-tree"})
	if err != nil {
		t.Fatal(err)
	}
	if root.ID() != runs[rootKey.ID].ID() {
		t.Fatal("root Run identity changed")
	}
	if result, err := root.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("root=%#v err=%v", result, err)
	}
	child, err = owner.Session(ctx, childKey)
	if err != nil {
		t.Fatal(err)
	}
	next, found, err := child.AttachRun(ctx, pending.RunID)
	if err != nil || !found {
		t.Fatalf("pending child missing: %v", err)
	}
	if result, err := next.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("pending=%#v err=%v", result, err)
	}
	if len(resumedModels[childKey.ID].calls()) != 2 || len(resumedModels[independentKey.ID].calls()) != 0 {
		t.Fatal("tree resumed the wrong tasks")
	}
}

func TestAbortTreeReleasesAdmissionForLaterTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	model := &gatedLifecycleModel{started: make(chan struct{}), release: make(chan struct{})}
	owner, err := New(ctx, agentengine.Definition{Name: "test", Model: model})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	key := agentsession.Named("abort-tree")
	session, err := owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	run, err := session.Run(ctx, agentschema.Text("first"))
	if err != nil {
		t.Fatal(err)
	}
	<-model.started
	if _, err := owner.AbortTree(ctx, key, agentevent.AbortRequest{IdempotencyKey: "abort-tree"}); err != nil {
		t.Fatal(err)
	}
	if result, err := run.Wait(ctx); err != nil || result.Status != agentschema.ResultAborted {
		t.Fatalf("aborted=%#v err=%v", result, err)
	}
	close(model.release)
	session, err = owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	next, err := session.Run(ctx, agentschema.Text("new task"))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := next.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("next=%#v err=%v", result, err)
	}
}

type suspensionAtCloseEngine struct {
	agentengine.Runner
	started chan struct{}
}

func (engine *suspensionAtCloseEngine) Run(ctx context.Context, request agentengine.Request, _ agentengine.EventSink) (agentengine.Result, error) {
	close(engine.started)
	select {
	case <-request.Controls:
		// A tree admission fence can suspend execution after Close has already
		// observed a running task and begun waiting for executionDone.
		return agentengine.Result{Status: agentengine.Suspended}, nil
	case <-ctx.Done():
		return agentengine.Result{}, ctx.Err()
	}
}

func TestAbortTreeSettlesRunThatSuspendsWhileClosing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	owner, err := New(ctx, agentengine.Definition{Name: "test", Model: &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("done", nil)}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	key := agentsession.Named("abort-during-suspension")
	session, err := owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	engine := &suspensionAtCloseEngine{Runner: session.engine, started: make(chan struct{})}
	session.engine = engine
	run, err := session.Run(ctx, agentschema.Text("first"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-engine.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := owner.AbortTree(ctx, key, agentevent.AbortRequest{IdempotencyKey: "abort-tree"}); err != nil {
		t.Fatal(err)
	}
	if result, err := run.Wait(ctx); err != nil || result.Status != agentschema.ResultAborted {
		t.Fatalf("aborted=%#v err=%v", result, err)
	}
	for range run.Events() {
	}
	reopened, err := owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	restored, found, err := reopened.AttachRun(ctx, run.ID())
	if err != nil || !found {
		t.Fatalf("aborted run missing after reopening: found=%v err=%v", found, err)
	}
	if result, err := restored.Wait(ctx); err != nil || result.Status != agentschema.ResultAborted {
		t.Fatalf("restored=%#v err=%v", result, err)
	}
}
