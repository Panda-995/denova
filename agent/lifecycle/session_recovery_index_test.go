package lifecycle

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

func TestCurrentRecoveryKeepsDistinctRecentRunsAcrossSuspensions(t *testing.T) {
	var index RecoveryIndex
	settle := func(runID string, status agentschema.ResultStatus) {
		record, err := sessionRecord(turnInterruptedRecord, persistedTurn{RunID: runID, CommandID: runID, Status: status})
		if err != nil {
			t.Fatal(err)
		}
		record.Revision = index.Revision + 1
		if err := index.Apply(record); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 31 {
		settle(fmt.Sprintf("completed-%02d", i), agentschema.ResultCompleted)
	}
	for range 40 {
		settle("resumable", agentschema.ResultSuspended)
	}
	current := index.Current()
	if len(current.Runs) != 32 || len(current.Recent) != 32 || current.Recent[0].Receipt.RunID != "completed-00" {
		t.Fatalf("repeated suspension evicted distinct historical runs: recent=%d runs=%d", len(current.Recent), len(current.Runs))
	}
	settle("resumable", agentschema.ResultCompleted)
	settle("newest", agentschema.ResultCompleted)
	current = index.Current()
	if len(current.Recent) != 32 || current.Recent[0].Receipt.RunID != "completed-01" || current.Recent[31].Receipt.RunID != "newest" || current.Runs["resumable"].Status != agentschema.ResultCompleted {
		t.Fatalf("current projection changed the distinct-run window or latest settlement: %+v", current.Recent)
	}
}

func TestRecoveryIndexKeepsOnlyUnfinishedBodiesAndOriginalReceipts(t *testing.T) {
	var index RecoveryIndex
	apply := func(kind string, value any) {
		record, err := sessionRecord(kind, value)
		if err != nil {
			t.Fatal(err)
		}
		record.Revision = index.Revision + 1
		if err := index.Apply(record); err != nil {
			t.Fatal(err)
		}
	}
	input := persistedInput{Receipt: agentevent.CommandReceipt{CommandID: "pending", Cursor: 1}, Kind: inputQueue, Hash: "original-idempotency-hash"}
	_, input.Input, _ = agentengine.EncodeInput(agentschema.Input{Text: "queued body"})
	apply(sessionInputRecord, input)
	host := &agentschema.HostData{Type: "product", Version: 1, Data: json.RawMessage(`{"scope":"original"}`)}
	apply(sessionInputUpdateRecord, persistedInputUpdate{CommandID: "pending", RunID: "active", Status: inputPending, HostData: host})
	apply(sessionInputUpdateRecord, persistedInputUpdate{CommandID: "pending", RunID: "active", Status: inputConsumed})
	tool := agentengine.PersistedTool{RunID: "active", CallID: "write", Name: "write", Arguments: json.RawMessage(`{"path":"target"}`), Started: true}
	apply(turnToolRecord, tool)
	replay := index.ReplayRecords()
	var binding, unknown bool
	for _, record := range replay {
		binding = binding || strings.Contains(string(record.Data), `"scope":"original"`)
		unknown = unknown || record.Kind == turnToolRecord
	}
	if !binding || !unknown {
		t.Fatal("pending ownership or unknown effect was lost")
	}
	tool.Result = &agentschema.ToolResult{Status: agentschema.ToolResultSuccess, ModelContent: "large tool body", DisplayContent: "large tool body"}
	apply(turnToolRecord, tool)
	apply(turnFinishedRecord, persistedTurn{RunID: "active", CommandID: "root", Status: agentschema.ResultCompleted, Output: "completed output"})
	if len(index.ReplayRecords()) != 0 {
		t.Fatalf("terminal facts retained: %+v", index.ReplayRecords())
	}
	saved := index.Inputs["pending"]
	if saved.Hash != input.Hash || saved.Receipt != input.Receipt || saved.Status != inputConsumed || saved.RunID != "active" {
		t.Fatalf("receipt changed: %+v", saved)
	}
	encoded, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"queued body", "large tool body", "completed output", "original\"}"} {
		if strings.Contains(string(encoded), body) {
			t.Fatalf("historical body retained: %s", body)
		}
	}
}

func TestCancelledInputReleasesBodyButRemainsIdempotent(t *testing.T) {
	store := agentsession.Memory()
	owner, err := New(t.Context(), agentengine.Definition{Name: "queue", Model: &lifecycleModel{}}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(t.Context())
	sess, err := owner.Session(t.Context(), agentsession.Named("cancelled"))
	if err != nil {
		t.Fatal(err)
	}
	input := agentschema.Input{Text: strings.Repeat("large queued body", 100), IdempotencyKey: "queue"}
	queued, err := sess.Queue(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	control := agentevent.QueueControlRequest{IdempotencyKey: "cancel"}
	receipt, err := queued.Cancel(t.Context(), control)
	if err != nil {
		t.Fatal(err)
	}
	if sess.inputs["queue"].input.Text != "" || sess.inputs["queue"].Input.Text != "" {
		t.Fatal("cancelled body remains resident")
	}
	if err := owner.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	owner, err = New(t.Context(), agentengine.Definition{Name: "queue", Model: &lifecycleModel{}}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(t.Context())
	sess, err = owner.Session(t.Context(), agentsession.Named("cancelled"))
	if err != nil {
		t.Fatal(err)
	}
	retried, err := sess.Queue(t.Context(), input)
	if err != nil || retried.Receipt() != queued.Receipt() {
		t.Fatalf("input retry: %v", err)
	}
	same, err := retried.Cancel(t.Context(), control)
	if err != nil || same != receipt {
		t.Fatalf("control retry: %v", err)
	}
}
