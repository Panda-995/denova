package resourceexchange

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"denova/internal/revisionfile"
	"github.com/google/uuid"
)

func TestRecoveryArchivesSettledRecordsWithoutReadingHistory(t *testing.T) {
	for _, state := range []string{"committed", "rolled_back"} {
		t.Run(state, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "fixture.json"), []byte("external edit"), 0o600); err != nil {
				t.Fatal(err)
			}
			txn := transaction{ID: uuid.NewString(), State: state, Changes: []fileChange{{Target: FileTarget{Path: "fixture.json"}, Before: []byte("before"), After: []byte("after"), Existed: true}}}
			if err := saveTransaction(context.Background(), root, txn); err != nil {
				t.Fatal(err)
			}
			if err := Recover(root, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(root, "resource-exchange", "transactions", txn.ID+".json")); !os.IsNotExist(err) {
				t.Fatalf("settled record remained pending: %v", err)
			}
			snapshot, err := readTransaction(context.Background(), root, txn.ID)
			if err != nil || !snapshot.Exists {
				t.Fatalf("archived response retry lost its record: %#v %v", snapshot, err)
			}
			if raw, err := os.ReadFile(filepath.Join(root, "fixture.json")); err != nil || string(raw) != "external edit" {
				t.Fatalf("settled record was replayed: %q %v", raw, err)
			}
			// A broken historical backup must not become a startup dependency.
			if err := os.WriteFile(filepath.Join(root, "resource-exchange", "backups", txn.ID+".json"), []byte("broken backup"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := Recover(root, nil); err != nil {
				t.Fatalf("history was read during recovery: %v", err)
			}
		})
	}
}

func TestArchiveFailureKeepsDurableCommitRetryable(t *testing.T) {
	root := t.TempDir()
	id := uuid.NewString()
	backup := filepath.Join(root, "resource-exchange", "backups", id+".json")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("preserved backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	changes := []fileChange{{Target: FileTarget{Path: "fixture.json"}, Expected: revisionfile.MissingRevision, After: []byte("committed")}}
	if err := commitFiles(context.Background(), root, nil, id, changes); err != nil {
		t.Fatal(err)
	}
	snapshot, err := readTransaction(context.Background(), root, id)
	if err != nil || !snapshot.Exists || !json.Valid(snapshot.Content) {
		t.Fatalf("durable commit was hidden by archive failure: %#v %v", snapshot, err)
	}
	var txn transaction
	if err := json.Unmarshal(snapshot.Content, &txn); err != nil || txn.State != "committed" {
		t.Fatalf("commit was not durably settled: %#v %v", txn, err)
	}
	if raw, err := os.ReadFile(filepath.Join(root, "fixture.json")); err != nil || string(raw) != "committed" {
		t.Fatalf("archive failure rolled back the commit: %q %v", raw, err)
	}
	if raw, err := os.ReadFile(backup); err != nil || string(raw) != "preserved backup" {
		t.Fatalf("existing backup was overwritten: %q %v", raw, err)
	}
}
