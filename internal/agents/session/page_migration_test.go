package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestPageConversationMigrationPreservesHistoryAndCanContinue(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := "platform-existing-page"
	old, err := store.GetOrCreate(id)
	if err != nil {
		t.Fatal(err)
	}
	config := json.RawMessage(`{"caller":{"kind":"plugin","id":"example.art"},"definition":"page:studio"}`)
	if _, err := old.SetPlatformRecord(t.Context(), "configuration", 0, config); err != nil {
		t.Fatal(err)
	}
	for _, message := range []*agentschema.Message{agentschema.UserMessage("Draw a page"), agentschema.AssistantMessage("A saved candidate is ready", nil)} {
		if err := old.Append(message); err != nil {
			t.Fatal(err)
		}
	}
	if err := old.AppendContextMessage(agentschema.UserMessage("Selected image: saved-image")); err != nil {
		t.Fatal(err)
	}
	before, err := old.ReadCanonicalMessages(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	history, err := old.ReadHistoryPage(t.Context(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	npc, err := store.GetOrCreate("platform-npc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := npc.SetPlatformRecord(t.Context(), "configuration", 0, json.RawMessage(`{"caller":{"kind":"game"},"definition":"local:npc"}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	adopted, err := store.Get("s-page-existing-page")
	if err != nil {
		t.Fatal(err)
	}
	after, err := adopted.ReadCanonicalMessages(t.Context())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("canonical history changed: %v", err)
	}
	gotHistory, err := adopted.ReadHistoryPage(t.Context(), 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotHistory.Entries) != len(history.Entries) {
		t.Fatal("visible history lost")
	}
	for i, item := range history.Entries {
		if gotHistory.Entries[i].Content != item.Content || !gotHistory.Entries[i].CreatedAt.Equal(item.CreatedAt) {
			t.Fatal("history content or timestamp changed")
		}
	}
	backup, err := os.ReadFile(path + ".pre-project-agent.bak")
	if err != nil || !bytes.Equal(original, backup) {
		t.Fatal("original journal backup changed")
	}
	if store.Exists(id) || !store.Exists("platform-npc") {
		t.Fatal("retired page or unrelated NPC identity changed")
	}
	if err := adopted.Append(agentschema.UserMessage("Change the dialogue")); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	continued, err := reopened.Get(adopted.ID)
	if err != nil || continued.MessageCountTotal() != adopted.MessageCountTotal() {
		t.Fatal("migration repeated or continuation lost")
	}
}

func TestPageConversationMigrationRejectsInvalidBackupAndRetries(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	old, err := store.GetOrCreate("platform-retry")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.SetPlatformRecord(t.Context(), "configuration", 0, json.RawMessage(`{"caller":{"kind":"plugin"},"definition":"page:studio"}`)); err != nil {
		t.Fatal(err)
	}
	if err := old.Append(agentschema.UserMessage("Keep this message")); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "platform-retry.jsonl")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup := path + ".pre-project-agent.bak"
	if err := os.WriteFile(backup, []byte("incomplete backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(dir); err == nil {
		t.Fatal("invalid backup allowed source removal")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed migration changed source")
	}
	if err := os.WriteFile(backup, before, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if !store.Exists("s-page-retry") || store.Exists("platform-retry") {
		t.Fatal("retry did not finish migration")
	}
}
