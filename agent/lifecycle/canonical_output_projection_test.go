package lifecycle

import (
	"context"
	"reflect"
	"testing"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

func TestCanonicalOutputProjectionPreservesCurrentTurnFinishReason(t *testing.T) {
	for _, finish := range []string{"stop", "length"} {
		t.Run(finish, func(t *testing.T) {
			ctx := context.Background()
			response := agentschema.AssistantMessage("approved answer", nil)
			response.ReasoningContent = "provider thinking"
			response.ResponseMeta = &agentschema.ResponseMeta{FinishReason: finish, Usage: &agentschema.TokenUsage{TotalTokens: 37}}
			canonical := []*agentschema.Message{agentschema.UserMessage("question"), agentschema.AssistantMessage("approved answer", nil)}
			var committed agentschema.Message
			adapter := agentcanonical.CanonicalAdapterFuncs{
				CapabilityIdentity: agentschema.CapabilityIdentity{Kind: "canonical.test.projected-output", Version: 1},
				MaterializeInputFn: func(context.Context, agentcanonical.InputCommitRequest) (agentcanonical.CommitReceipt, error) {
					return agentcanonical.CommitReceipt{Revision: "input-1"}, nil
				},
				CommitOutputFn: func(_ context.Context, request agentcanonical.OutputCommitRequest) (agentcanonical.OutputCommitReceipt, error) {
					committed = request.Message
					return agentcanonical.OutputCommitReceipt{
						Revision: "output-1",
						Transcript: &agentcanonical.OutputProjection{
							Content: "approved answer", Thinking: "approved thinking", ContextMessages: canonical,
						},
					}, nil
				},
			}
			owner, err := New(ctx, agentengine.Definition{
				Name: "projected output", Model: &lifecycleModel{responses: []*agentschema.Message{response}}, Canonical: adapter,
			}, WithSessionStore(agentsession.Memory()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = owner.Close(ctx) })
			session, err := owner.Session(ctx, agentsession.Named("projected-output"))
			if err != nil {
				t.Fatal(err)
			}
			run, err := session.Run(ctx, agentschema.Text("question"))
			if err != nil {
				t.Fatal(err)
			}
			result, waitErr := run.Wait(ctx)
			if finish == "length" {
				if result.Status != agentschema.ResultIncomplete || result.Reason != agentschema.ModelOutputTruncatedReason || waitErr == nil {
					t.Fatalf("projection lost incomplete output classification: %+v, %v", result, waitErr)
				}
			} else if result.Status != agentschema.ResultCompleted || waitErr != nil {
				t.Fatalf("projected output did not complete: %+v, %v", result, waitErr)
			}
			if !reflect.DeepEqual(committed.ResponseMeta, response.ResponseMeta) || committed.ReasoningContent != response.ReasoningContent {
				t.Fatalf("canonical commit lost provider metadata: %+v", committed)
			}
			snapshot, err := session.Snapshot(ctx)
			if err != nil || len(snapshot.RecentRuns) != 1 || snapshot.RecentRuns[0].Output != "approved answer" {
				t.Fatalf("current turn lost the approved output: %+v, %v", snapshot.RecentRuns, err)
			}
			session.mu.RLock()
			transcript, err := decodeJournalTranscript(session.engineState)
			session.mu.RUnlock()
			if err != nil || !reflect.DeepEqual(transcript.Messages, canonical) {
				t.Fatalf("retained transcript differs from the host projection: %+v, %v", transcript.Messages, err)
			}
		})
	}
}
