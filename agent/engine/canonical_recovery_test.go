package engine

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/alfredxw/denova/agent/context/history"
	"github.com/alfredxw/denova/agent/schema"
	"github.com/alfredxw/denova/agent/session/canonical"
)

type recoveryHistory struct {
	head     canonical.CanonicalHistoryHead
	messages []*schema.Message
}

func TestContinuationKeepsContextStateOrderAcrossCanonicalRecovery(t *testing.T) {
	feedback := schema.UserMessage("Continue the accepted work")
	feedback.Extra = map[string]any{schema.ExtraKeyAgentContinuation: true}
	stateMessage := history.NewContextStateMessage(schema.ContextFragment{
		Source: "workspace", Purpose: "current workspace", Resource: "workspace", StateID: "workspace", Revision: "1",
		Stability: schema.ContextSessionState, Placement: schema.ContextStateMessage, Content: "restored context", HardLimit: 1024,
	}, strings.Repeat("a", 64), "initialize", "")
	source := recoveryHistory{head: canonical.CanonicalHistoryHead{Identity: "lane", Revision: "1"}, messages: []*schema.Message{
		schema.UserMessage("original input"), schema.AssistantMessage("accepted prose", nil), feedback, stateMessage,
	}}
	contextState, err := history.RebuildContextStateSnapshot(source.messages)
	if err != nil {
		t.Fatal(err)
	}
	state := engineTranscript{Version: 1, HistoryHead: source.head, Messages: source.messages, ContextState: contextState}
	checkpoint, err := canonicalMessageCheckpoint(state)
	if err != nil {
		t.Fatal(err)
	}
	if got := canonical.CanonicalContextStateOrder(source.messages); !reflect.DeepEqual(got, source.messages) {
		t.Error("continuation feedback was reordered as a newly admitted user input")
	}
	raw, matched, err := RestoreCanonicalCheckpoint(t.Context(), checkpoint, source.head, source)
	if err != nil || !matched {
		t.Fatalf("streaming recovery changed continuation order: matched=%t error=%v", matched, err)
	}
	restored, err := decodeEngineTranscript(raw)
	if err != nil || !reflect.DeepEqual(restored.Messages, source.messages) {
		t.Fatalf("streaming recovery changed accepted messages: %v", err)
	}
	imported, err := PrepareCanonicalImport(t.Context(), CanonicalImportRequest{Messages: source.messages, Head: source.head, Checkpoint: checkpoint, Active: true, Canonical: true})
	if err != nil {
		t.Fatalf("canonical import changed continuation order: %v", err)
	}
	importedState, err := decodeEngineTranscript(imported.State)
	if err != nil || !reflect.DeepEqual(importedState.Messages, source.messages) {
		t.Fatalf("canonical import changed accepted messages: %v", err)
	}
}

func (s recoveryHistory) CanonicalHistoryHead(context.Context) (canonical.CanonicalHistoryHead, error) {
	return s.head, nil
}
func (s recoveryHistory) CanonicalMessages(context.Context) ([]*schema.Message, error) {
	panic("recovery must use the streaming visitor")
}
func (s recoveryHistory) VisitCanonicalMessages(_ context.Context, visit func(*schema.Message) error) error {
	for _, message := range s.messages {
		if err := visit(message.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func TestRestoreCanonicalCheckpointAcceptsReleasedInlineMetadata(t *testing.T) {
	source := recoveryHistory{head: canonical.CanonicalHistoryHead{Identity: "lane", Revision: "1"}, messages: []*schema.Message{
		schema.UserMessage("archived user"), schema.AssistantMessage("archived answer", nil),
		schema.UserMessage("current user"), schema.AssistantMessage("current answer", nil),
	}}
	contextState := schema.UserMessage("current workspace context")
	contextState.Extra = map[string]any{"agent.context_state": "v1"}
	source.messages = append(source.messages[:3], contextState, source.messages[3])
	state := engineTranscript{Version: 2, HistoryHead: source.head, Archive: &history.HistoryArchive{From: 0, To: 2}, Messages: canonical.CanonicalContextStateOrder(source.messages)[2:]}
	checkpoint, err := canonicalMessageCheckpoint(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, released := range []bool{false, true} {
		t.Run(map[bool]string{false: "message-free", true: "released-inline"}[released], func(t *testing.T) {
			if released {
				checkpoint.Metadata, err = json.Marshal(state)
				if err != nil {
					t.Fatal(err)
				}
			}
			raw, matched, err := RestoreCanonicalCheckpoint(t.Context(), checkpoint, source.head, source)
			if err != nil || !matched {
				t.Fatalf("restore: matched=%t err=%v", matched, err)
			}
			restored, err := decodeEngineTranscript(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored, state) {
				t.Fatalf("restored transcript differs: %#v", restored)
			}
			fresh, err := CanonicalMessageCheckpoint(raw)
			if err != nil {
				t.Fatal(err)
			}
			var metadata engineTranscript
			if err := json.Unmarshal(fresh.Metadata, &metadata); err != nil {
				t.Fatal(err)
			}
			if len(metadata.Messages) != 0 {
				t.Fatal("new checkpoint repeated released message bodies")
			}
		})
	}
}
