package lifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

type childCatalogProbe struct {
	agentsession.Store
	lists int
}

func (store *childCatalogProbe) List(ctx context.Context, selector agentsession.Selector) ([]agentsession.Key, error) {
	store.lists++
	return store.Store.List(ctx, selector)
}

func TestChildIntentSurvivesCrashBeforeAdmission(t *testing.T) {
	ctx := t.Context()
	store := &childCatalogProbe{Store: agentsession.Memory()}
	definition := agentengine.Definition{Model: &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("done", nil), agentschema.AssistantMessage("done again", nil)}}}
	owner, err := New(ctx, definition, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	rootKey := agentsession.Named("intent-parent")
	root, err := owner.Session(ctx, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := ChildSessionAttributes(rootKey)
	if err != nil {
		t.Fatal(err)
	}
	child := ChildTask{Key: agentsession.Key{Namespace: "task.researcher", ID: "accepted-intent", Attributes: attributes}}
	if err := root.RegisterChildTask(ctx, child); err != nil {
		t.Fatal(err)
	}
	snapshot, err := root.Snapshot(ctx)
	if err != nil || len(snapshot.ChildTasks) != 1 {
		t.Fatalf("child snapshot=%+v err=%v", snapshot.ChildTasks, err)
	}
	snapshot.ChildTasks[0].Key.Attributes[ParentSessionAttribute] = "caller mutation"
	if pending, err := root.PendingChildTasks(ctx); err != nil || !reflect.DeepEqual(pending, []ChildTask{child}) {
		t.Fatalf("snapshot mutated durable child identity: pending=%+v err=%v", pending, err)
	}
	// No child journal/receipt exists yet. Closing models the admission gap.
	if err := owner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	owner, err = New(ctx, definition, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	root, err = owner.Session(ctx, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	store.lists = 0
	pending, err := root.PendingChildTasks(ctx)
	if err != nil || !reflect.DeepEqual(pending, []ChildTask{child}) || store.lists != 0 {
		t.Fatalf("pending=%+v scans=%d err=%v", pending, store.lists, err)
	}
	if err := root.RegisterChildTask(ctx, child); err != nil {
		t.Fatal(err)
	}
	opened, err := owner.Session(ctx, child.Key)
	if err != nil {
		t.Fatal(err)
	}
	for cycle := range 2 {
		run, err := opened.Run(ctx, agentschema.Input{Text: "work", IdempotencyKey: fmt.Sprint("input-", cycle)})
		if err != nil {
			t.Fatal(err)
		}
		if result, err := run.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
	pending, err = root.PendingChildTasks(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("repeated admission duplicated the pending child: %+v %v", pending, err)
	}
	if err := root.releaseChildTask(ctx, child.Key); err != nil {
		t.Fatal(err)
	}
	if _, err := opened.Queue(ctx, agentschema.Input{Text: "keep for the next task", IdempotencyKey: "idle-queue"}); err != nil {
		t.Fatal(err)
	}
	pending, err = root.PendingChildTasks(ctx)
	if err != nil || len(pending) != 1 || pending[0].Key.ID != child.Key.ID {
		t.Fatalf("idle child Queue did not restore its obligation: %+v %v", pending, err)
	}
}

func TestSessionExistsUsesExactIdentityWithCatalogOnlyStore(t *testing.T) {
	ctx := t.Context()
	store := &childCatalogProbe{Store: agentsession.Memory()}
	key := agentsession.Key{ID: "same-id", Attributes: map[string]string{"agent": "child"}}
	other := agentsession.Key{Namespace: "other", ID: key.ID, Attributes: key.Attributes}
	extra := agentsession.Key{ID: key.ID, Attributes: map[string]string{"agent": "child", "extra": "different identity"}}
	for _, candidate := range []agentsession.Key{other, extra} {
		log, err := store.Open(ctx, candidate)
		if err != nil {
			t.Fatal(err)
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := New(ctx, agentengine.Definition{Model: &lifecycleModel{}}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	if found, err := owner.SessionExists(ctx, key); err != nil || found {
		t.Fatalf("subset selector was treated as exact identity: found=%v err=%v", found, err)
	}
	log, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if found, err := owner.SessionExists(ctx, key); err != nil || !found {
		t.Fatalf("normalized exact identity was not found: found=%v err=%v", found, err)
	}
}

func TestReleasedChildTreeIsAdoptedOnce(t *testing.T) {
	ctx := t.Context()
	store := &childCatalogProbe{Store: agentsession.Memory()}
	rootKey := agentsession.Named("legacy-parent")
	attributes, _ := ChildSessionAttributes(rootKey)
	childKey := agentsession.Key{Namespace: "task.child", ID: "legacy-child", Attributes: attributes}
	grandAttributes, _ := ChildSessionAttributes(childKey)
	grandKey := agentsession.Key{Namespace: "task.grandchild", ID: "legacy-grandchild", Attributes: grandAttributes}
	for _, key := range []agentsession.Key{rootKey, childKey, grandKey} {
		log, err := store.Open(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		var records []agentsession.Record
		for _, fact := range []struct {
			kind  string
			value any
		}{
			{sessionInputRecord, persistedInput{Kind: inputRun, Receipt: agentevent.CommandReceipt{RunID: key.ID, CommandID: key.ID, Cursor: 1}, Hash: "legacy-input", Input: agentengine.UserInput{}}},
			{turnStartedRecord, persistedTurn{RunID: key.ID, CommandID: key.ID, At: time.Now()}},
		} {
			data, _ := json.Marshal(fact.value)
			records = append(records, agentsession.Record{Kind: fact.kind, Version: 1, Data: data})
		}
		if _, err := log.Append(ctx, 0, records...); err != nil {
			t.Fatal(err)
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	for reopen := range 2 {
		owner, err := New(ctx, agentengine.Definition{Model: &lifecycleModel{}}, WithSessionStore(store))
		if err != nil {
			t.Fatal(err)
		}
		root, err := owner.Session(ctx, rootKey)
		if err != nil {
			t.Fatal(err)
		}
		store.lists = 0
		children, err := root.PendingChildTasks(ctx)
		if err != nil || len(children) != 1 || children[0].Key.ID != childKey.ID {
			t.Fatalf("adopted children=%+v err=%v", children, err)
		}
		wantScans := 1
		if reopen != 0 {
			wantScans = 0
		}
		if store.lists != wantScans {
			t.Fatalf("reopen=%d scans=%d want=%d", reopen, store.lists, wantScans)
		}
		if err := owner.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestChildResultsOutsideRecentWindowRemainPending(t *testing.T) {
	ctx := t.Context()
	store := agentsession.Memory()
	rootKey := agentsession.Named("old-result-parent")
	attributes, _ := ChildSessionAttributes(rootKey)
	attributes["agent"] = "researcher"
	childKey := agentsession.Key{Namespace: "task.researcher", ID: "many-results", Attributes: attributes}
	log, err := store.Open(ctx, childKey)
	if err != nil {
		t.Fatal(err)
	}
	var records []agentsession.Record
	var delivered []string
	for index := range 33 {
		id := fmt.Sprintf("run-%02d", index)
		input, _ := json.Marshal(persistedInput{Kind: inputRun, Receipt: agentevent.CommandReceipt{RunID: id, CommandID: id, Cursor: agentevent.Cursor(index + 1)}, Hash: id, Input: agentengine.UserInput{}})
		result, _ := json.Marshal(persistedTurn{RunID: id, CommandID: id, At: time.Now(), Status: agentschema.ResultCompleted, Output: id})
		records = append(records, agentsession.Record{Kind: sessionInputRecord, Version: 1, Data: input}, agentsession.Record{Kind: turnFinishedRecord, Version: 1, Data: result})
		if index != 0 {
			delivered = append(delivered, agentschema.TaskCompletionID("researcher", childKey.ID, id))
		}
	}
	if _, err := log.Append(ctx, 0, records...); err != nil {
		t.Fatal(err)
	}
	_ = log.Close()
	log, err = store.Open(ctx, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	delivery, _ := json.Marshal(persistedTaskCompletionDelivery{IDs: delivered})
	if _, err := log.Append(ctx, 0, agentsession.Record{Kind: sessionTaskCompletionDeliveryRecord, Version: 1, Data: delivery}); err != nil {
		t.Fatal(err)
	}
	_ = log.Close()
	owner, err := New(ctx, agentengine.Definition{Model: &lifecycleModel{}}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	root, err := owner.Session(ctx, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := root.PendingChildTasks(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("old unacknowledged result lost during adoption: pending=%+v err=%v", pending, err)
	}
	child, err := owner.Session(ctx, childKey)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := child.Snapshot(ctx)
	if err != nil || len(snapshot.RecentRuns) != 32 || snapshot.RecentRuns[0].ID == "run-00" {
		t.Fatalf("unexpected display window: %+v %v", snapshot.RecentRuns, err)
	}
	results, err := root.PendingChildTaskResults(ctx, child)
	if err != nil || len(results) != 1 || results[0].ID != "run-00" || results[0].Output != "run-00" {
		t.Fatalf("old pending results=%+v err=%v", results, err)
	}
}

func TestNewSessionPersistsEmptyChildSetWithoutCatalogAdoption(t *testing.T) {
	ctx := t.Context()
	store := &childCatalogProbe{Store: agentsession.Memory()}
	definition := agentengine.Definition{Model: &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("done", nil)}}}
	owner, err := New(ctx, definition, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	key := agentsession.Named("new-session-without-children")
	session, err := owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	run, err := session.Run(ctx, agentschema.Input{Text: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := run.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err = New(ctx, definition, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	session, err = owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	store.lists = 0
	children, err := session.PendingChildTasks(ctx)
	if err != nil || len(children) != 0 || store.lists != 0 {
		t.Fatalf("new Session required legacy adoption: children=%+v scans=%d err=%v", children, store.lists, err)
	}
}
