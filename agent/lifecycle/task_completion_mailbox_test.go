package lifecycle

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestTaskCompletionMailboxInjectsAfterToolBoundaryWithoutWait(t *testing.T) {
	ctx := context.Background()
	store := newObservingSessionStore()
	model := &scriptedModel{responses: []scriptedModelResponse{
		{message: agentschema.AssistantMessage("", []agentschema.ToolCall{{
			ID: "inspect", Type: "function", Function: agentschema.FunctionCall{Name: "inspect", Arguments: `{}`},
		}})},
		{message: agentschema.AssistantMessage("parent final", nil)},
	}}
	var parent *Session
	tools, err := agenttool.StaticTools(testToolDefinition(&functionTool{name: "inspect", run: func(context.Context, string) (string, error) {
		_, enqueueErr := parent.EnqueueTaskCompletion(ctx, testTaskCompletion("completion-after-tool", "child answer"))
		return "inspection complete", enqueueErr
	}}))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := New(ctx, agentengine.Definition{Name: "parent", Model: model, Tools: tools}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	parent, err = owner.Session(ctx, agentsession.Named("mailbox-after-tool"))
	if err != nil {
		t.Fatal(err)
	}
	run, err := parent.Run(ctx, agentschema.Text("use the tool"))
	if err != nil {
		t.Fatal(err)
	}
	if result, waitErr := run.Wait(ctx); waitErr != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("result=%#v err=%v", result, waitErr)
	}

	inputs := model.capturedInputs()
	if len(inputs) != 2 {
		t.Fatalf("model calls = %d, want 2", len(inputs))
	}
	completionIndex, toolIndex := -1, -1
	for index, message := range inputs[1] {
		if message.Role == agentschema.ToolRole {
			toolIndex = index
		}
		if message.TaskCompletion != nil {
			completionIndex = index
			if message.Content == "" || message.TaskCompletion.CompletionID != "completion-after-tool" {
				t.Fatalf("completion message = %#v", message)
			}
		}
	}
	if toolIndex < 0 || completionIndex <= toolIndex {
		t.Fatalf("second model input did not order tool before completion: %#v", inputs[1])
	}
	watch, err := parent.WatchTaskCompletions(ctx, []string{"completion-after-tool"})
	if err != nil || len(watch.PendingIDs) != 0 {
		t.Fatalf("delivered completion remained pending: %#v err=%v", watch.PendingIDs, err)
	}
	if count := store.count(sessionTaskCompletionDeliveryRecord); count != 1 {
		t.Fatalf("delivery receipt records = %d, want 1", count)
	}
}

func TestTrackedTaskCompletionsAllowParentWorkBeforeFinal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	model := &finalBoundaryModel{
		started: make(chan struct{}), release: make(chan struct{}),
	}
	owner, err := New(ctx, agentengine.Definition{Name: "parent", Model: model}, WithSessionStore(agentsession.Memory()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	session, err := owner.Session(ctx, agentsession.Named("final-boundary"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first-completion", "second-completion"} {
		if tracked, trackErr := session.TrackTaskCompletion(ctx, id); trackErr != nil || !tracked {
			t.Fatalf("track %q accepted=%t err=%v", id, tracked, trackErr)
		}
	}
	first, err := session.Run(ctx, agentschema.Text("first turn"))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-model.started:
	case <-ctx.Done():
		t.Fatal("parent could not work while its children were running")
	}
	close(model.release)
	if accepted, enqueueErr := session.EnqueueTaskCompletion(ctx, testTaskCompletion("first-completion", "first answer")); enqueueErr != nil || !accepted {
		t.Fatalf("enqueue accepted=%t err=%v", accepted, enqueueErr)
	}
	time.Sleep(20 * time.Millisecond)
	snapshot, err := session.Snapshot(ctx)
	if err != nil || snapshot.ActiveRunID != first.ID() || snapshot.ActiveOutput.Content != "" {
		t.Fatalf("provisional final escaped: snapshot=%#v err=%v", snapshot, err)
	}
	if accepted, enqueueErr := session.EnqueueTaskCompletion(ctx, testTaskCompletion("second-completion", "second answer")); enqueueErr != nil || !accepted {
		t.Fatalf("enqueue accepted=%t err=%v", accepted, enqueueErr)
	}
	if result, waitErr := first.Wait(ctx); waitErr != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("first result=%#v err=%v", result, waitErr)
	}
	if calls := model.callCount(); calls != 2 {
		t.Fatalf("model calls = %d, want parallel work then final synthesis", calls)
	}
	watch, err := session.WatchTaskCompletions(ctx, []string{"first-completion", "second-completion"})
	if err != nil || len(watch.PendingIDs) != 0 {
		t.Fatalf("delivered completion pending=%#v err=%v", watch.PendingIDs, err)
	}
	inputs := model.capturedInputs()
	if len(inputs) != 2 || countTaskCompletionMessages(inputs[1]) != 2 {
		t.Fatalf("model inputs = %#v", inputs)
	}
}

func TestTaskCompletionDeliveryReceiptSurvivesSessionReopen(t *testing.T) {
	ctx := context.Background()
	store := agentsession.Memory()
	model := &lifecycleModel{responses: []*agentschema.Message{
		agentschema.AssistantMessage("first", nil), agentschema.AssistantMessage("second", nil),
	}}
	owner, err := New(ctx, agentengine.Definition{Name: "parent", Model: model}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	session, err := owner.Session(ctx, agentsession.Named("durable-mailbox-receipt"))
	if err != nil {
		t.Fatal(err)
	}
	if accepted, enqueueErr := session.EnqueueTaskCompletion(ctx, testTaskCompletion("durable-completion", "durable answer")); enqueueErr != nil || !accepted {
		t.Fatalf("enqueue accepted=%t err=%v", accepted, enqueueErr)
	}
	run, err := session.Run(ctx, agentschema.Text("first turn"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(ctx); err != nil {
		t.Fatal(err)
	}

	owner, err = New(ctx, agentengine.Definition{Name: "parent", Model: model}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	session, err = owner.Session(ctx, agentsession.Named("durable-mailbox-receipt"))
	if err != nil {
		t.Fatal(err)
	}
	if accepted, enqueueErr := session.EnqueueTaskCompletion(ctx, testTaskCompletion("durable-completion", "duplicate")); enqueueErr != nil || accepted {
		t.Fatalf("duplicate enqueue accepted=%t err=%v", accepted, enqueueErr)
	}
	second, err := session.Run(ctx, agentschema.Text("second turn"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := second.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	calls := model.calls()
	if len(calls) != 2 || countTaskCompletionMessages(calls[1]) != 1 {
		t.Fatalf("reopened transcript duplicated completion: %#v", calls)
	}
}

func TestTaskCompletionWatchCoversBeforeAndAfterSubscription(t *testing.T) {
	ctx := context.Background()
	owner, err := New(ctx, agentengine.Definition{Name: "parent", Model: &lifecycleModel{}}, WithSessionStore(agentsession.Memory()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	session, err := owner.Session(ctx, agentsession.Named("mailbox-watch"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.EnqueueTaskCompletion(ctx, testTaskCompletion("already-pending", "first")); err != nil {
		t.Fatal(err)
	}
	before, err := session.WatchTaskCompletions(ctx, []string{"already-pending"})
	if err != nil || len(before.PendingIDs) != 1 {
		t.Fatalf("pending-before-subscribe=%#v err=%v", before.PendingIDs, err)
	}

	after, err := session.WatchTaskCompletions(ctx, []string{"arrives-later"})
	if err != nil || len(after.PendingIDs) != 0 {
		t.Fatalf("initial after-subscribe watch=%#v err=%v", after.PendingIDs, err)
	}
	if _, err := session.EnqueueTaskCompletion(ctx, testTaskCompletion("arrives-later", "second")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-after.Activity:
	default:
		t.Fatal("mailbox activity did not wake the existing subscription")
	}
	rechecked, err := session.WatchTaskCompletions(ctx, []string{"arrives-later"})
	if err != nil || len(rechecked.PendingIDs) != 1 {
		t.Fatalf("pending-after-subscribe=%#v err=%v", rechecked.PendingIDs, err)
	}
}

func TestTaskCompletionCheckpointFailureKeepsPendingAndSkipsProvider(t *testing.T) {
	ctx := context.Background()
	store := &taskCompletionFailingStore{Store: agentsession.Memory()}
	model := &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("must not run", nil)}}
	owner, err := New(ctx, agentengine.Definition{Name: "parent", Model: model}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	session, err := owner.Session(ctx, agentsession.Named("mailbox-commit-failure"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.EnqueueTaskCompletion(ctx, testTaskCompletion("retryable-completion", "answer")); err != nil {
		t.Fatal(err)
	}
	run, err := session.Run(ctx, agentschema.Text("turn"))
	if err != nil {
		t.Fatal(err)
	}
	if _, waitErr := run.Wait(ctx); waitErr == nil {
		t.Fatal("Run succeeded after the atomic completion checkpoint was rejected")
	}
	if calls := model.calls(); len(calls) != 0 {
		t.Fatalf("provider was called %d times after checkpoint failure", len(calls))
	}
	session.mu.RLock()
	_, pending := session.taskCompletions.pending["retryable-completion"]
	_, delivered := session.taskCompletions.delivered["retryable-completion"]
	closed := session.closed
	session.mu.RUnlock()
	if !pending || delivered || !closed {
		t.Fatalf("failed checkpoint pending=%v delivered=%v writer_closed=%v", pending, delivered, closed)
	}
}

type finalBoundaryModel struct {
	mu            sync.Mutex
	started       chan struct{}
	release       chan struct{}
	secondStarted chan struct{}
	once          sync.Once
	secondOnce    sync.Once
	inputs        [][]*agentschema.Message
}

func (model *finalBoundaryModel) Generate(ctx context.Context, input []*agentschema.Message, _ ...agentmodel.ModelOption) (*agentschema.Message, error) {
	return model.next(ctx, input)
}

func (model *finalBoundaryModel) Stream(ctx context.Context, input []*agentschema.Message, _ ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	message, err := model.next(ctx, input)
	if err != nil {
		return nil, err
	}
	return agentstream.StreamReaderFromArray([]*agentschema.Message{message}), nil
}

func (model *finalBoundaryModel) next(ctx context.Context, input []*agentschema.Message) (*agentschema.Message, error) {
	model.mu.Lock()
	model.inputs = append(model.inputs, agentschema.CloneMessages(input))
	call := len(model.inputs)
	model.mu.Unlock()
	if call == 1 {
		model.once.Do(func() { close(model.started) })
		select {
		case <-model.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else if call == 2 && model.secondStarted != nil {
		model.secondOnce.Do(func() { close(model.secondStarted) })
	}
	if call > 2 {
		return nil, errors.New("final-boundary model exhausted")
	}
	return agentschema.AssistantMessage("done", nil), nil
}

func (model *finalBoundaryModel) callCount() int {
	model.mu.Lock()
	defer model.mu.Unlock()
	return len(model.inputs)
}

func (model *finalBoundaryModel) capturedInputs() [][]*agentschema.Message {
	model.mu.Lock()
	defer model.mu.Unlock()
	result := make([][]*agentschema.Message, len(model.inputs))
	for index := range model.inputs {
		result[index] = agentschema.CloneMessages(model.inputs[index])
	}
	return result
}

func testTaskCompletion(id, payload string) agentschema.TaskCompletion {
	message := agentschema.UserMessage(payload)
	message.TaskCompletion = &agentschema.TaskCompletionMessageMeta{
		CompletionID: id, Author: "researcher", Recipient: "parent",
	}
	return agentschema.TaskCompletion{ID: id, Message: message}
}

func countTaskCompletionMessages(messages []*agentschema.Message) int {
	count := 0
	for _, message := range messages {
		if message != nil && message.TaskCompletion != nil {
			count++
		}
	}
	return count
}

type taskCompletionFailingStore struct {
	agentsession.Store
	mu     sync.Mutex
	failed bool
}

func (store *taskCompletionFailingStore) Open(ctx context.Context, key agentsession.Key) (agentsession.Log, error) {
	log, err := store.Store.Open(ctx, key)
	if err != nil {
		return nil, err
	}
	return &taskCompletionFailingLog{Log: log, store: store}, nil
}

type taskCompletionFailingLog struct {
	agentsession.Log
	store *taskCompletionFailingStore
}

func (log *taskCompletionFailingLog) Append(
	ctx context.Context,
	expected agentsession.Revision,
	records ...agentsession.Record,
) (agentsession.Revision, error) {
	for _, record := range records {
		if record.Kind != sessionTaskCompletionDeliveryRecord {
			continue
		}
		log.store.mu.Lock()
		if !log.store.failed {
			log.store.failed = true
			log.store.mu.Unlock()
			return expected, errors.New("injected task completion checkpoint failure")
		}
		log.store.mu.Unlock()
	}
	return log.Log.Append(ctx, expected, records...)
}
