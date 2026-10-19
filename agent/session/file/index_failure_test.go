package file_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alfredxw/denova/agent/lifecycle"
	"github.com/alfredxw/denova/agent/session"
	sessionfile "github.com/alfredxw/denova/agent/session/file"
)

func TestRecoveryCacheWriteFailurePreservesAcceptedCanonicalTail(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store, err := sessionfile.New(root)
	if err != nil {
		t.Fatal(err)
	}
	key := session.Named("cache-failure")
	log, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.(session.IndexedLog).Recover(ctx, &lifecycle.RecoveryIndex{}); err != nil {
		t.Fatal(err)
	}
	queued := func(command string, cursor int) session.Record {
		data, _ := json.Marshal(map[string]any{"receipt": map[string]any{"command_id": command, "cursor": cursor}, "kind": "queue", "hash": command, "input": map[string]any{"text": command}})
		return session.Record{Kind: "session.input", Version: 1, Data: data}
	}
	if _, err := log.Append(ctx, 0, queued("before", 1)); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	indexes, _ := filepath.Glob(filepath.Join(root, "*.idx.json"))
	if err := os.Remove(indexes[0]); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(indexes[0], 0700); err != nil {
		t.Fatal(err)
	}
	log, err = store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.(session.IndexedLog).Recover(ctx, &lifecycle.RecoveryIndex{}); err != nil {
		t.Fatalf("optional cache prevented canonical recovery: %v", err)
	}
	if revision, err := log.Append(ctx, 1, queued("after", 2)); err != nil || revision != 2 {
		t.Fatalf("cache failure rejected an accepted command: revision=%d error=%v", revision, err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := store.OpenReader(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var recovered lifecycle.RecoveryIndex
	if err := reader.(session.IndexedLog).Recover(ctx, &recovered); err != nil || recovered.Revision != 2 || len(recovered.ReplayRecords()) != 2 {
		t.Fatalf("stale observation cache lost accepted tail: %+v %v", recovered, err)
	}
}
