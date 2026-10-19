package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	sessionfile "github.com/alfredxw/denova/agent/session/file"
)

type inputAdmissionGate struct {
	agentengine.Runner
	entered chan struct{}
	release chan struct{}
}

func (gate *inputAdmissionGate) PrepareAdmission(ctx context.Context, _ agentengine.TurnAdmissionRequest) ([]agentengine.CapabilityState, error) {
	close(gate.entered)
	select {
	case <-gate.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestRunInputBeforeCycleCheckpointPreservesAcceptedInput(t *testing.T) {
	ctx := t.Context()
	owner, err := New(ctx, agentengine.Definition{Model: &lifecycleModel{responses: []*agentschema.Message{agentschema.AssistantMessage("done", nil)}}})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	sess, err := owner.Session(ctx, agentsession.Named("input-admission"))
	if err != nil {
		t.Fatal(err)
	}
	gate := &inputAdmissionGate{Runner: sess.engine, entered: make(chan struct{}), release: make(chan struct{})}
	sess.engine = gate
	defer close(gate.release)
	input := agentschema.Input{Text: "accepted work", IdempotencyKey: "start"}
	run, err := sess.Run(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("Run did not reach input admission")
	}
	got, found, err := sess.RunInput(ctx, run.ID())
	if err != nil || !found || !reflect.DeepEqual(got, input) {
		t.Fatalf("accepted input during admission: %+v, found=%v, err=%v", got, found, err)
	}
}

func TestInspectSessionDoesNotMaterializeRecoveryInteractions(t *testing.T) {
	ctx := t.Context()
	store := agentsession.Memory()
	key := agentsession.Named("unfinished-write")
	log, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	input := persistedInput{Receipt: agentevent.CommandReceipt{CommandID: "start", RunID: "run", Cursor: 1}, Kind: inputRun, Hash: "accepted-input"}
	_, input.Input, err = agentengine.EncodeInput(agentschema.Text("write"))
	if err != nil {
		t.Fatal(err)
	}
	var records []agentsession.Record
	for _, fact := range []struct {
		kind  string
		value any
	}{
		{sessionInputRecord, input},
		{turnStartedRecord, persistedTurn{RunID: "run", CommandID: "start", At: time.Now()}},
		{turnToolRecord, agentengine.PersistedTool{RunID: "run", CallID: "write", Name: "write", Started: true, Arguments: json.RawMessage(`{"path":"chapter.md"}`)}},
	} {
		record, err := sessionRecord(fact.kind, fact.value)
		if err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	if _, err := log.Append(ctx, 0, records...); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	model := &lifecycleModel{}
	owner, err := New(ctx, agentengine.Definition{Name: "inspect", Model: model}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	snapshot, err := owner.InspectSession(ctx, key)
	if err != nil || snapshot.ActiveRunID != "run" || snapshot.ActiveStatus != agentschema.ResultSuspended || len(snapshot.PendingInteractions) != 0 {
		t.Fatalf("inspection=%+v err=%v", snapshot, err)
	}
	if len(model.calls()) != 0 || len(owner.sessions) != 0 {
		t.Fatal("inspection registered or executed a Session")
	}
	reader, err := store.(agentsession.ReaderStore).OpenReader(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	if _, err := reader.Replay(ctx, func(agentsession.Record) error { count++; return nil }); err != nil {
		t.Fatal(err)
	}
	if count != len(records) {
		t.Fatalf("inspection appended recovery facts: %d", count)
	}
	// Opening the same journal for execution still restores its unknown effect.
	session, err := owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = session.Snapshot(ctx)
	if err != nil || len(snapshot.PendingInteractions) != 1 || snapshot.PendingInteractions[0].Verification == nil {
		t.Fatalf("execution recovery=%+v err=%v", snapshot, err)
	}
}

func TestCommandRunSurvivesRecentHistoryEvictionAndColdReopen(t *testing.T) {
	for name, factory := range map[string]func(*testing.T) agentsession.Store{
		"memory": func(t *testing.T) agentsession.Store { return newObservingSessionStore() },
		"file": func(t *testing.T) agentsession.Store {
			store, err := sessionfile.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			return store
		},
	} {
		t.Run(name, func(t *testing.T) {

			ctx := t.Context()
			store := factory(t)
			model := &lifecycleModel{}
			for i := range 40 {
				model.responses = append(model.responses, agentschema.AssistantMessage(fmt.Sprintf("answer-%d", i), nil))
			}
			owner, err := New(ctx, agentengine.Definition{Name: "inspect", Model: model}, WithSessionStore(store))
			if err != nil {
				t.Fatal(err)
			}
			sess, err := owner.Session(ctx, agentsession.Named("exact-command"))
			if err != nil {
				t.Fatal(err)
			}
			var first agentevent.CommandReceipt
			for i := range 40 {
				run, err := sess.Run(ctx, agentschema.Input{Text: fmt.Sprintf("message-%d", i), IdempotencyKey: fmt.Sprintf("command-%d", i)})
				if err != nil {
					t.Fatal(err)
				}
				if result, err := run.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
					t.Fatalf("run %d: %+v %v", i, result, err)
				}
				if i == 0 {
					first = run.Receipt()
				}
			}
			recent, err := sess.Snapshot(ctx)
			if err != nil || len(recent.RecentRuns) != 32 {
				t.Fatalf("recent: %+v %v", recent, err)
			}
			assertExact := func(sess *Session) {
				t.Helper()
				if len(sess.runs) != 0 {
					t.Fatalf("settled executions remain resident: %d", len(sess.runs))
				}
				for _, input := range sess.inputs {
					if input.status != inputPending && (input.input.Text != "" || input.Input.Text != "") {
						t.Fatal("settled input body remains resident")
					}
				}
				run, found, err := sess.CommandRun(ctx, "command-0")
				if err != nil || !found {
					t.Fatalf("old command lost: %v %v", found, err)
				}
				snapshot := run.Snapshot()
				if snapshot.Receipt != first || snapshot.Result == nil || snapshot.Result.Status != agentschema.ResultCompleted || snapshot.Output != "answer-0" {
					t.Fatalf("exact snapshot: %+v", snapshot)
				}
				if _, found, err := sess.CommandRun(ctx, "missing"); found || err != nil {
					t.Fatalf("missing lookup: %v %v", found, err)
				}
				retried, err := sess.Run(ctx, agentschema.Input{Text: "message-0", IdempotencyKey: "command-0"})
				if err != nil || retried.Receipt() != first {
					t.Fatalf("old command retry: %v %v", retried, err)
				}
				if _, err := sess.Run(ctx, agentschema.Input{Text: "changed", IdempotencyKey: "command-0"}); !errors.Is(err, ErrIdempotencyConflict) {
					t.Fatalf("conflicting old command: %v", err)
				}
				if len(sess.runs) != 0 {
					t.Fatal("historical lookup repopulated live Run registry")
				}
			}
			assertExact(sess)
			if err := owner.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			owner, err = New(ctx, agentengine.Definition{Name: "inspect", Model: model}, WithSessionStore(store))
			if err != nil {
				t.Fatal(err)
			}
			defer owner.Close(context.Background())
			sess, err = owner.Session(ctx, agentsession.Named("exact-command"))
			if err != nil {
				t.Fatal(err)
			}
			assertExact(sess)
			if len(model.calls()) != 40 {
				t.Fatalf("lookup executed model: %d", len(model.calls()))
			}
		})
	}
}

type blockedOpenStore struct {
	agentsession.Store
	entered chan struct{}
	release chan struct{}
}

func (store *blockedOpenStore) Open(ctx context.Context, key agentsession.Key) (agentsession.Log, error) {
	if key.ID == "slow" {
		close(store.entered)
		select {
		case <-store.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return store.Store.Open(ctx, key)
}

func TestSlowSessionOpenDoesNotBlockOtherSessions(t *testing.T) {
	store := &blockedOpenStore{Store: agentsession.Memory(), entered: make(chan struct{}), release: make(chan struct{})}
	owner, err := New(t.Context(), agentengine.Definition{Name: "inspect", Model: &lifecycleModel{}}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(context.Background())
	done := make(chan error, 1)
	agentasync.SafeGo(func() { _, err := owner.Session(t.Context(), agentsession.Named("slow")); done <- err }, func(err error) { done <- err })
	<-store.entered
	defer close(store.release)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	fast := make(chan error, 1)
	agentasync.SafeGo(func() { _, err := owner.Session(ctx, agentsession.Named("fast")); fast <- err }, func(err error) { fast <- err })
	select {
	case err := <-fast:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("unrelated Session waited for slow open")
	}
}

func TestColdInspectionDropsCancelledQueueControlHistoryAndKeepsCursor(t *testing.T) {
	ctx := t.Context()
	store, err := sessionfile.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	definition := agentengine.Definition{Model: &lifecycleModel{}}
	owner, err := New(ctx, definition, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	key := agentsession.Named("cancelled-queue-inspection")
	session, err := owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	var firstInput, firstControl agentevent.CommandReceipt
	for i := range 40 {
		queued, err := session.Queue(ctx, agentschema.Input{Text: "queued evidence", IdempotencyKey: fmt.Sprintf("queue-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		control, err := queued.Cancel(ctx, agentevent.QueueControlRequest{IdempotencyKey: fmt.Sprintf("cancel-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			firstInput, firstControl = queued.Receipt(), control
		}
	}
	before, err := session.Snapshot(ctx)
	if err != nil {
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
	after, err := owner.InspectSession(ctx, key)
	if err != nil || after.Cursor != before.Cursor || len(after.QueuedRuns) != 0 {
		t.Fatalf("cancelled queue broke cold inspection: cursor=%d want=%d queues=%+v err=%v", after.Cursor, before.Cursor, after.QueuedRuns, err)
	}
	session, err = owner.Session(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := session.Queue(ctx, agentschema.Input{Text: "queued evidence", IdempotencyKey: "queue-0"})
	if err != nil || queued.Receipt() != firstInput {
		t.Fatalf("observation changed historical input receipt: %v", err)
	}
	control, err := queued.Cancel(ctx, agentevent.QueueControlRequest{IdempotencyKey: "cancel-0"})
	if err != nil || control != firstControl {
		t.Fatalf("observation changed historical control receipt: got=%+v want=%+v err=%v", control, firstControl, err)
	}
}
