package session

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"

	"denova/config"
	externaljournal "denova/internal/agents/runtime/external/journal"
	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestCanonicalWindowReadsExternalToolStartAcrossSeekBoundary(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sess, err := store.GetOrCreate("external-window")
	if err != nil {
		t.Fatal(err)
	}
	native, err := sess.EnsureRuntimeConfig(testRuntimeConfig(config.AgentKindIDE))
	if err != nil {
		t.Fatal(err)
	}
	settings := native.Config
	settings.Runtime = &config.RuntimeSelection{Kind: config.RuntimeCodex, Codex: &config.CodexRuntimeSettings{Model: "fixture-model"}}
	external, err := sess.SetRuntimeConfig(settings, native.Revision)
	if err != nil {
		t.Fatal(err)
	}
	commit := func(kind externaljournal.Kind, value any, message *agentschema.Message, messageID string) {
		t.Helper()
		record, err := externaljournal.NewRecord(kind, "operation", external.Revision, value)
		if err != nil {
			t.Fatal(err)
		}
		if err := sess.UpdateExternal(t.Context(), external.Revision, func(ExternalState) (ExternalTransaction, error) {
			return ExternalTransaction{Records: []externaljournal.Record{record}, Message: message, Metadata: MessageMetadata{MessageID: messageID}}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	commit(externaljournal.OperationAccepted, externaljournal.Accepted{CommandID: "input", Fingerprint: "original", InputMessageID: "user", Runtime: external.Engine()}, agentschema.UserMessage("inspect"), "user")
	commit(externaljournal.ToolStarted, externaljournal.StartedTool{ExecutionID: "execution", Tool: "read", Arguments: []byte(`{"path":"chapter.md"}`), Recovery: externaljournal.ReadOnly}, nil, "")
	var messages []*agentschema.Message
	for index := range 260 {
		messages = append(messages, agentschema.AssistantMessage(fmt.Sprint(index), nil))
	}
	if err := sess.AppendContextMessages(messages...); err != nil {
		t.Fatal(err)
	}
	commit(externaljournal.ToolFinished, externaljournal.FinishedTool{ExecutionID: "execution", Success: true, Result: "Original tool result"}, nil, "")
	commit(externaljournal.OperationClosed, externaljournal.Closed{Status: externaljournal.Completed, MessageID: "answer"}, agentschema.AssistantMessage("done", nil), "answer")
	all, err := sess.ReadCanonicalMessages(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	window, err := sess.ReadCanonicalWindow(t.Context(), len(all), &agenthistory.HistoryArchive{From: 0, To: len(all) - 3})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(window, all[len(all)-3:]) {
		t.Fatal("seeking lost the original tool call/result pair")
	}
}

func TestColdCanonicalWindowPreservesSemanticCoordinates(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("indexed-window")
	if err != nil {
		t.Fatal(err)
	}
	var ordered []*agentschema.Message
	for turn := range 200 {
		user := agentschema.UserMessage(fmt.Sprintf("request-%d", turn))
		state := agenthistory.NewContextStateMessage(agentschema.ContextFragment{StateID: "workspace", Revision: fmt.Sprint(turn), Content: "Current workspace state"}, "fingerprint", "set", "")
		answer := agentschema.AssistantMessage(fmt.Sprintf("answer-%d", turn), nil)
		if err := sess.AppendContextMessages(user, state, answer); err != nil {
			t.Fatal(err)
		}
		ordered = append(ordered, state, user, answer)
	}
	feedback := agentschema.UserMessage("Continue accepted work")
	feedback.Extra = map[string]any{agentschema.ExtraKeyAgentContinuation: true}
	restoredState := agenthistory.NewContextStateMessage(agentschema.ContextFragment{StateID: "workspace", Revision: "restored", Content: "Restored workspace state"}, "fingerprint", "set", "")
	if err := sess.AppendContextMessages(feedback, restoredState); err != nil {
		t.Fatal(err)
	}
	ordered = append(ordered, feedback, restoredState)
	path := sess.filePath
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(store.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sess, err = store.Get("indexed-window")
	if err != nil {
		t.Fatal(err)
	}
	archive := &agenthistory.HistoryArchive{From: 2, To: len(ordered) - 6, Retained: []int{4, 257, 512}}
	var want []*agentschema.Message
	for index, message := range ordered {
		if archive.Contains(index) {
			want = append(want, message)
		}
	}
	got, err := sess.ReadCanonicalWindow(t.Context(), len(ordered), archive)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("retained window changed context-state order or archive coordinates")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes := sess.journal.ReplayStats().LastRangeBytesRead; bytes >= info.Size()/4 {
		t.Fatalf("tail window read=%d journal=%d", bytes, info.Size())
	}
	if _, err := sess.ReadCanonicalWindow(t.Context(), len(ordered)+1, archive); err == nil {
		t.Fatal("accepted a changed canonical count")
	}
	if _, err := sess.ReadCanonicalWindow(t.Context(), len(ordered), &agenthistory.HistoryArchive{From: 2, To: 10, Retained: []int{4, 4}}); err == nil {
		t.Fatal("accepted duplicate retained coordinates")
	}
}

func TestReadCanonicalMessagesRebuildsBeyondResidentWindow(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("canonical-window")
	if err != nil {
		t.Fatal(err)
	}

	total := sessionRecentTransactionLimit + 25
	batch := make([]*agentschema.Message, total)
	for index := 0; index < total; index++ {
		batch[index] = agentschema.UserMessage(fmt.Sprintf("message-%03d", index))
	}
	if err := sess.AppendContextMessages(batch...); err != nil {
		t.Fatal(err)
	}
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	sess, err = store.Get("canonical-window")
	if err != nil {
		t.Fatal(err)
	}
	resident, base, durable := sess.MessageWindow()
	if len(resident) != sessionRecentTransactionLimit || base != total-sessionRecentTransactionLimit || durable != total {
		t.Fatalf("resident window len=%d base=%d durable=%d", len(resident), base, durable)
	}

	messages, err := sess.ReadCanonicalMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != total || messages[0].Content != "message-000" || messages[total-1].Content != fmt.Sprintf("message-%03d", total-1) {
		t.Fatalf("canonical messages len=%d first=%q last=%q", len(messages), messages[0].Content, messages[len(messages)-1].Content)
	}
}

func TestReadCanonicalMessagesStartsAfterLatestClear(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sess, err := store.GetOrCreate("canonical-clear")
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(agentschema.UserMessage("before clear")); err != nil {
		t.Fatal(err)
	}
	if err := appendReleasedClearMarker(sess); err != nil {
		t.Fatal(err)
	}
	if err := sess.Append(agentschema.UserMessage("after clear")); err != nil {
		t.Fatal(err)
	}

	messages, err := sess.ReadCanonicalMessages(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Content != "after clear" {
		t.Fatalf("canonical messages after clear = %#v", messages)
	}
}
