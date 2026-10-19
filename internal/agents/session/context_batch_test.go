package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestContextBatchIsAtomicIdempotentAndRebuildsFromJournal(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("context-batch")
	if err != nil {
		t.Fatal(err)
	}
	identity := DomainCommitIdentity{CommandID: "command-1", OperationID: "operation-1", Cycle: 1}
	assistant := agentschema.AssistantMessage("checking", []agentschema.ToolCall{{
		ID: "call-1", Type: "function", Function: agentschema.FunctionCall{Name: "inspect", Arguments: `{"path":"chapter.md"}`},
	}})
	assistant.ReasoningContent = "private reasoning metadata"
	tool := agentschema.ToolMessage(agentschema.TextToolResult("complete evidence"), "call-1", agentschema.WithToolName("inspect"))
	messages := []*agentschema.Message{assistant, tool}
	before := sess.ContextCursor()

	if _, err := sess.CommitContextBatch(context.Background(), before, identity, 1, messages, nil); !errors.Is(err, ErrDomainCommitIdentityConflict) {
		t.Fatalf("non-contiguous sequence error=%v", err)
	}
	first, err := sess.CommitContextBatch(context.Background(), before, identity, 0, messages, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.ContextRevision != before.Revision+2 || sess.MessageCountTotal() != 2 {
		t.Fatalf("first receipt=%#v message_count=%d", first, sess.MessageCountTotal())
	}
	if first.Cursor != sess.ContextCursor() {
		t.Fatalf("first receipt cursor=%#v current=%#v", first.Cursor, sess.ContextCursor())
	}
	retry, err := sess.CommitContextBatch(context.Background(), before, identity, 0, messages, nil)
	if err != nil || retry != first || sess.MessageCountTotal() != 2 {
		t.Fatalf("retry=%#v count=%d err=%v", retry, sess.MessageCountTotal(), err)
	}
	conflicting := []*agentschema.Message{assistant.Clone(), agentschema.ToolMessage(agentschema.TextToolResult("different evidence"), "call-1", agentschema.WithToolName("inspect"))}
	if _, err := sess.CommitContextBatch(context.Background(), sess.ContextCursor(), identity, 0, conflicting, nil); !errors.Is(err, ErrDomainCommitIdentityConflict) {
		t.Fatalf("conflicting retry error=%v", err)
	}
	if err := sess.Append(agentschema.UserMessage("external append")); err != nil {
		t.Fatal(err)
	}
	retryAfterAppend, err := sess.CommitContextBatch(context.Background(), sess.ContextCursor(), identity, 0, messages, nil)
	if err != nil || retryAfterAppend.Cursor != first.Cursor {
		t.Fatalf("retry after external append=%#v err=%v", retryAfterAppend, err)
	}
	nextAssistant := agentschema.AssistantMessage("next", []agentschema.ToolCall{{
		ID: "call-2", Type: "function", Function: agentschema.FunctionCall{Name: "inspect", Arguments: `{}`},
	}})
	nextMessages := []*agentschema.Message{
		nextAssistant,
		agentschema.ToolMessage(agentschema.TextToolResult("next evidence"), "call-2", agentschema.WithToolName("inspect")),
	}
	if _, err := sess.CommitContextBatch(context.Background(), retryAfterAppend.Cursor, identity, 1, nextMessages, nil); !errors.Is(err, ErrContextRevisionConflict) {
		t.Fatalf("external append was not detected by the next batch: %v", err)
	}

	journal, err := os.ReadFile(sess.filePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(journal), `"type":"context_batch"`) != 1 || strings.Contains(string(journal), `"type":"context_message"`) {
		t.Fatalf("context batch was not one atomic journal record:\n%s", journal)
	}
	if strings.Contains(string(journal), `"kind"`) || strings.Contains(string(journal), `"batch_hash"`) || strings.Contains(string(journal), `"hash"`) {
		t.Fatalf("context batch persisted a redundant kind/hash field:\n%s", journal)
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	indexPath := strings.TrimSuffix(sess.filePath, filepath.Ext(sess.filePath)) + ".idx.json"
	if err := os.Remove(indexPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	reopened, err := loadSession(sess.filePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got := reopened.GetEffectiveMessages()
	want := append(append([]*agentschema.Message(nil), messages...), agentschema.UserMessage("external append"))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuilt messages:\nwant=%#v\ngot=%#v", want, got)
	}
	third, err := reopened.CommitContextBatch(context.Background(), before, identity, 0, messages, nil)
	if err != nil || third != first || reopened.MessageCountTotal() != 3 {
		t.Fatalf("reopened retry=%#v count=%d err=%v", third, reopened.MessageCountTotal(), err)
	}
}
