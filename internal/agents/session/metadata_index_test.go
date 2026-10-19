package session

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"denova/config"
	"denova/internal/agents/conversationconfig"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestMetadataIndexReopensRefreshesAndRebuildsWithoutChangingHistory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	seed := conversationconfig.Config{AgentKind: config.AgentKindGeneral, ProfileID: "default", ThinkingLevel: "medium", ApprovalMode: config.AgentApprovalWrite}
	sess, err := store.CreateWithRuntimeConfig("First title", seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(agentschema.UserMessage("Complete original content")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("Another conversation"); err != nil {
		t.Fatal(err)
	}
	want, err := store.List(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sess.ID+".jsonl")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(dir, metadataIndexName))
	if err != nil || strings.Contains(string(index), "Complete original content") || strings.Contains(string(index), dir) {
		t.Fatalf("metadata index contains content or host paths: error=%v", err)
	}
	store, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.metadata) != len(want) || len(store.cache) != 0 {
		t.Fatal("cold metadata index did not restore summaries independently of sessions")
	}
	got, err := store.List(sess.ID)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("cold summaries changed: got=%#v want=%#v error=%v", got, want, err)
	}
	// An independent handle changes the canonical file; the saved stamp must
	// invalidate only this summary, including its persisted runtime seed.
	other, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Rename(sess.ID, "Updated title"); err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	updated, err := store.List("")
	if err != nil || updated[0].Title != "Updated title" || updated[0].Active || updated[0].RuntimeConfig == nil || !reflect.DeepEqual(*updated[0].RuntimeConfig, seed) {
		t.Fatalf("changed metadata was not refreshed: %#v error=%v", updated, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, metadataIndexName), []byte(`{"version":1,"entries":`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	rebuilt, err := store.List("")
	if err != nil || !reflect.DeepEqual(rebuilt, updated) {
		t.Fatalf("corrupt metadata cache changed canonical summaries: %#v error=%v", rebuilt, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(after), string(original)) {
		t.Fatal("metadata reads rewrote canonical history")
	}
	// Moving the Store cannot invalidate persistent identities or cached paths.
	moved := filepath.Join(filepath.Dir(dir), "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(moved)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err = store.List("")
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("moving the Store changed summaries: %#v error=%v", got, err)
	}
}

func TestMetadataIndexDoesNotPersistStaleResidentSummary(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sess, err := store.Create("Original title")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(""); err != nil {
		t.Fatal(err)
	}
	other, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := other.Rename(sess.ID, "Updated by another handle"); err != nil {
		t.Fatal(err)
	}
	// The first Store still owns a resident Session from before the rename.
	// Listing must not checkpoint that old summary with the new file stamp.
	if _, err := store.List(""); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	metas, err := reopened.List("")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 || metas[0].Title != "Updated by another handle" {
		t.Fatalf("metadata index persisted a stale resident summary: %#v", metas)
	}
}
