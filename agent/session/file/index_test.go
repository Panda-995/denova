package file_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alfredxw/denova/agent/lifecycle"
	"github.com/alfredxw/denova/agent/session"
	sessionfile "github.com/alfredxw/denova/agent/session/file"
)

type countedRecovery struct {
	*lifecycle.RecoveryIndex
	applied *int
}

func (p *countedRecovery) New() session.CheckpointProjection {
	return &countedRecovery{RecoveryIndex: &lifecycle.RecoveryIndex{}, applied: p.applied}
}
func (p *countedRecovery) Apply(record session.Record) error {
	*p.applied++
	return p.RecoveryIndex.Apply(record)
}

func TestFileRecoveryUsesCheckpointTailAndRebuildsDisposableCaches(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store, err := sessionfile.New(root)
	if err != nil {
		t.Fatal(err)
	}
	key := session.Named("native-child")
	log, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	var records []session.Record
	for i := range 80 {
		for _, fact := range []struct {
			kind string
			data any
		}{
			{"session.input", map[string]any{"receipt": map[string]any{"command_id": fmt.Sprintf("command-%d", i), "run_id": fmt.Sprintf("run-%d", i), "cursor": i + 1}, "kind": "run", "hash": fmt.Sprintf("hash-%d", i), "input": map[string]any{"text": strings.Repeat("historical input", 300)}}},
			{"turn.finished", map[string]any{"run_id": fmt.Sprintf("run-%d", i), "command_id": fmt.Sprintf("command-%d", i), "status": "completed", "output": strings.Repeat("historical output", 300)}},
		} {
			data, _ := json.Marshal(fact.data)
			records = append(records, session.Record{Kind: fact.kind, Version: 1, Data: data})
		}
	}
	// Real Native Sessions retain model history in their latest transcript.
	// Execution needs it; read-only status must not copy it into its sidecar.
	transcript, _ := json.Marshal(map[string]any{"engine_state": map[string]any{"messages": []any{map[string]any{"role": "user", "content": strings.Repeat("archived context", 10000)}}}})
	records = append(records, session.Record{Kind: "session.transcript", Version: 1, Data: transcript})
	committed := session.Revision(len(records))
	if _, err := log.Append(ctx, 0, records...); err != nil {
		t.Fatal(err)
	}
	applied := 0
	index := &countedRecovery{RecoveryIndex: &lifecycle.RecoveryIndex{}, applied: &applied}
	if err := log.(session.IndexedLog).Recover(ctx, index); err != nil || applied != len(records) {
		t.Fatalf("initial rebuild: applied=%d err=%v", applied, err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	paths, _ := filepath.Glob(filepath.Join(root, "*.jsonl"))
	canonical, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	currentPaths, _ := filepath.Glob(filepath.Join(root, "*.idx.json.current"))
	current, err := os.ReadFile(currentPaths[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(current, []byte("historical output")) || bytes.Contains(current, []byte("historical input")) || bytes.Contains(current, []byte("archived context")) || len(current) >= 40000 {
		t.Fatalf("observation cache retains history bodies or unbounded metadata: %d bytes", len(current))
	}
	for _, readOnly := range []bool{false, true} {
		if readOnly {
			log, err = store.OpenReader(ctx, key)
		} else {
			log, err = store.Open(ctx, key)
		}
		if err != nil {
			t.Fatal(err)
		}
		applied = 0
		if err := log.(session.IndexedLog).Recover(ctx, index); err != nil || applied != 0 {
			t.Fatalf("cache hit replays history: readonly=%v applied=%d err=%v", readOnly, applied, err)
		}
		if !readOnly && len(index.Inputs) != 80 {
			t.Fatal("execution lost historical receipts")
		}
		if !readOnly && (len(index.ReplayRecords()) != 1 || !bytes.Contains(index.ReplayRecords()[0].Data, []byte("archived context"))) {
			t.Fatal("execution lost its canonical model transcript")
		}
		if readOnly && (len(index.Inputs) != 0 || len(index.Runs) != 32) {
			t.Fatalf("observation retained historical receipts: inputs=%d runs=%d", len(index.Inputs), len(index.Runs))
		}
		reader := log.(interface {
			ReadRecord(context.Context, session.Revision) (session.Record, error)
		})
		if got, err := reader.ReadRecord(ctx, 2); err != nil || !bytes.Contains(got.Data, []byte("historical output")) {
			t.Fatalf("exact old settlement lost: %+v %v", got, err)
		}
		if readOnly {
			if len(index.ReplayRecords()) != 0 {
				t.Fatal("observation retained model history")
			}
			if _, err := log.Append(ctx, committed, records[0]); err == nil {
				t.Fatal("reader appended")
			}
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// Inspection must leave a crash's incomplete final transaction untouched.
	file, err := os.OpenFile(paths[0], os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"incomplete":`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	torn, _ := os.ReadFile(paths[0])
	log, err = store.OpenReader(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	applied = 0
	if err := log.(session.IndexedLog).Recover(ctx, index); err != nil || applied != 0 {
		t.Fatalf("torn cached tail: applied=%d err=%v", applied, err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	inspected, _ := os.ReadFile(paths[0])
	if !bytes.Equal(inspected, torn) {
		t.Fatal("reader repaired the canonical tail")
	}
	// A process that committed after the checkpoint leaves an ordinary tail.
	log, err = store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	queued := session.Record{Kind: "session.input", Version: 1, Data: json.RawMessage(`{"receipt":{"command_id":"queued","cursor":81},"kind":"queue","hash":"queued-hash","input":{"text":"pending work"}}`)}
	if _, err := log.Append(ctx, committed, queued); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	log, err = store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	applied = 0
	if err := log.(session.IndexedLog).Recover(ctx, index); err != nil || applied != 1 || len(index.ReplayRecords()) != 2 {
		t.Fatalf("tail recovery: applied=%d records=%d err=%v", applied, len(index.ReplayRecords()), err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	for _, damage := range []string{"missing", "corrupt"} {
		if damage == "missing" {
			err = os.Remove(currentPaths[0])
		} else {
			err = os.WriteFile(currentPaths[0], []byte(`{"broken":true}`), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		log, err = store.OpenReader(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		applied = 0
		if err := log.(session.IndexedLog).Recover(ctx, index); err != nil || applied != len(records)+1 || len(index.ReplayRecords()) != 1 {
			t.Fatalf("%s cache rebuild: applied=%d err=%v", damage, applied, err)
		}
		if err := log.Close(); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, canonical) {
		t.Fatal("recovery rewrote canonical history")
	}
}
