package lifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	agenthistory "github.com/alfredxw/denova/agent/context/history"
	agentengine "github.com/alfredxw/denova/agent/engine"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

func TestInvalidCanonicalImportPreservesLoadedSession(t *testing.T) {
	ctx := context.Background()
	owner, err := New(ctx, agentengine.Definition{Name: "history-validation", Model: &lifecycleModel{}})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	session, err := owner.Session(ctx, agentsession.Named("history-validation"))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.LoadCanonicalMessages(ctx, []*agentschema.Message{agentschema.UserMessage("valid history")}); err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(session.engineState)
	call := agentschema.AssistantMessage("", []agentschema.ToolCall{{ID: "call", Function: agentschema.FunctionCall{Name: "read", Arguments: `{}`}}})
	for name, messages := range map[string][]*agentschema.Message{
		"split batch":     {call, agentschema.UserMessage("continue")},
		"unfinished tail": {call},
		"orphan result":   {agentschema.ToolMessage(agentschema.TextToolResult("result"), "orphan")},
		"nil message":     {nil},
	} {
		t.Run(name, func(t *testing.T) {
			if err := session.LoadCanonicalMessages(ctx, messages); !errors.Is(err, agentcanonical.ErrInvalidCanonicalMessages) {
				t.Fatalf("invalid history error = %v", err)
			}
			if !bytes.Equal(before, session.engineState) || session.active != nil {
				t.Fatal("invalid import changed the previously loaded Session")
			}
		})
	}
}

func TestCanonicalImportCommitsInvalidationAndCheckpointTogether(t *testing.T) {
	ctx := t.Context()
	store := canonicalMessageTestStore{Store: agentsession.Memory()}
	owner, err := New(ctx, agentengine.Definition{Name: "atomic-import", Model: &lifecycleModel{}}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	session, err := owner.Session(ctx, agentsession.Named("atomic-import"))
	if err != nil {
		t.Fatal(err)
	}
	if err := session.LoadCanonicalMessages(ctx, []*agentschema.Message{agentschema.UserMessage("original")}); err != nil {
		t.Fatal(err)
	}
	compact := json.RawMessage(`{"id":"old-projection","revision":1}`)
	session.capabilities[agenthistory.CompactionCapability] = compact
	if err := session.persistCapabilitiesLocked(ctx); err != nil {
		t.Fatal(err)
	}
	before, revision := bytes.Clone(session.engineState), session.revision
	log := &contextCheckpointLog{Log: session.log, reject: true}
	session.log = log
	imported := []*agentschema.Message{agentschema.UserMessage("edited")}
	if err := session.LoadCanonicalMessages(ctx, imported); !errors.Is(err, context.Canceled) {
		t.Fatalf("rejected import: %v", err)
	}
	if !bytes.Equal(before, session.engineState) || session.revision != revision ||
		!bytes.Equal(session.capabilities[agenthistory.CompactionCapability], compact) ||
		!bytes.Equal(session.durableCapabilities[agenthistory.CompactionCapability], compact) {
		t.Fatal("rejected import changed the checkpoint or its capability generation")
	}
	log.reject = false
	if err := session.LoadCanonicalMessages(ctx, imported); err != nil {
		t.Fatal(err)
	}
	if log.commits != 1 {
		t.Fatalf("invalidation and checkpoint used %d commits", log.commits)
	}
	if _, present := session.capabilities[agenthistory.CompactionCapability]; present {
		t.Fatal("edited history kept the old projection")
	}
	if err := owner.Close(ctx); err != nil {
		t.Fatal(err)
	}
	owner, err = New(ctx, agentengine.Definition{Name: "atomic-import", Model: &lifecycleModel{}}, WithSessionStore(store))
	if err != nil {
		t.Fatal(err)
	}
	session, err = owner.Session(ctx, agentsession.Named("atomic-import"))
	if err != nil {
		t.Fatal(err)
	}
	if _, present := session.capabilities[agenthistory.CompactionCapability]; present {
		t.Fatal("reopen resurrected an invalidated projection")
	}
	if err := session.LoadCanonicalMessages(ctx, imported); err != nil {
		t.Fatal(err)
	}
	restored, err := decodeJournalTranscript(session.engineState)
	if err != nil || len(restored.Messages) != 1 || restored.Messages[0].Content != "edited" {
		t.Fatalf("restored import: %#v, %v", restored, err)
	}
}
