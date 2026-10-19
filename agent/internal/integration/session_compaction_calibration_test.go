package integration_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alfredxw/denova/agent"
	"github.com/alfredxw/denova/agent/context/compaction"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
	sessionfile "github.com/alfredxw/denova/agent/session/file"
)

// Report exact local usage so a different calibration ratio can only come
// from pairing a response with a different request, not a tokenizer mismatch.
type calibrationModel struct {
	mu        sync.Mutex
	summaries int
	inputs    [][]*agentschema.Message
	pause     chan struct{}
	paused    bool
}

func (model *calibrationModel) Generate(ctx context.Context, messages []*agentschema.Message, options ...agentmodel.ModelOption) (*agentschema.Message, error) {
	model.mu.Lock()
	defer model.mu.Unlock()
	if strings.HasPrefix(messages[len(messages)-1].Content, "[Runtime context compaction request]") {
		model.summaries++
		return agentschema.AssistantMessage(strings.Repeat("checkpoint ", 360), nil), nil
	}
	if model.pause != nil && model.summaries > 0 && !model.paused {
		model.paused = true
		close(model.pause)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	model.inputs = append(model.inputs, messages)
	answer := agentschema.AssistantMessage(strings.Repeat("a", 24_000), nil)
	answer.ResponseMeta = &agentschema.ResponseMeta{Usage: &agentschema.TokenUsage{
		PromptTokens: agentmodel.EstimateRequestTextTokens(messages, agentmodel.GetCommonOptions(nil, options...).Tools),
	}}
	return answer, nil
}

func (model *calibrationModel) Stream(ctx context.Context, messages []*agentschema.Message, options ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	answer, err := model.Generate(ctx, messages, options...)
	return agentstream.StreamReaderFromArray([]*agentschema.Message{answer}), err
}

func TestManualCompactionDoesNotRecalibrateRetainedUsageFromShorterHistory(t *testing.T) {
	for _, scenario := range []string{"manual", "manual_reopen", "automatic_pause_reopen"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := t.Context()
			model := &calibrationModel{}
			if scenario == "automatic_pause_reopen" {
				model.pause = make(chan struct{})
			}
			definition := agent.Definition{
				Name: "calibration", Model: model,
				ModelIdentity: agentschema.CapabilityIdentity{Kind: "test.calibration", Version: 1},
				Instructions:  strings.Repeat("s", 80_000),
				Compaction: compaction.Standard(compaction.StandardConfig{
					ContextWindowTokens: 100_000, ReservedTokens: 12_048,
					TriggerBytes: 340_000, KeepRecentBytes: 68_000,
				}),
			}
			store, err := sessionfile.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			owner, err := agent.New(ctx, definition, agent.WithSessionStore(store))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = owner.Close(context.Background()) }()
			conversation, err := owner.Session(ctx, agentsession.Named("calibration"))
			if err != nil {
				t.Fatal(err)
			}
			if err := conversation.LoadCanonicalMessages(ctx, []*agentschema.Message{
				agentschema.UserMessage(strings.Repeat("o", 160_000)), agentschema.AssistantMessage("Old answer", nil),
				agentschema.UserMessage(strings.Repeat("r", 24_000)), agentschema.AssistantMessage("Recent answer", nil),
			}); err != nil {
				t.Fatal(err)
			}
			run, err := conversation.Run(ctx, agent.Text(strings.Repeat("u", 16_000)))
			if err != nil {
				t.Fatal(err)
			}
			if result, err := run.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
				t.Fatalf("initial run: %+v %v", result, err)
			}
			runID := ""
			if scenario == "automatic_pause_reopen" {
				run, err = conversation.Run(ctx, agent.Text(strings.Repeat("n", 2000)))
				if err != nil {
					t.Fatal(err)
				}
				select {
				case <-model.pause:
				case <-time.After(3 * time.Second):
					t.Fatal("automatic checkpoint did not reach the next provider call")
				}
				runID = run.ID()
				if _, err := conversation.SuspendAndClose(ctx, agent.SuspendRequest{RunID: runID, IdempotencyKey: "pause-after-checkpoint"}); err != nil {
					t.Fatal(err)
				}
			} else {
				compacted, err := conversation.Compact(ctx, compaction.CompactionRequest{Force: true})
				if err != nil || !compacted.Changed {
					t.Fatalf("manual compaction: %+v %v", compacted, err)
				}
			}
			if scenario != "manual" {
				if err := owner.Close(ctx); err != nil {
					t.Fatal(err)
				}
				owner, err = agent.New(ctx, definition, agent.WithSessionStore(store))
				if err != nil {
					t.Fatal(err)
				}
				conversation, err = owner.Session(ctx, agentsession.Named("calibration"))
				if err != nil {
					t.Fatal(err)
				}
			}
			if runID != "" {
				run, err = conversation.ResumeRun(ctx, agent.ResumeRequest{RunID: runID, IdempotencyKey: "resume-after-checkpoint"})
			} else {
				run, err = conversation.Run(ctx, agent.Text(strings.Repeat("n", 16_000)))
			}
			if err != nil {
				t.Fatal(err)
			}
			if result, err := run.Wait(ctx); err != nil || result.Status != agentschema.ResultCompleted {
				t.Fatalf("next run: %+v %v", result, err)
			}
			model.mu.Lock()
			defer model.mu.Unlock()
			if len(model.inputs) != 2 || model.summaries != 1 {
				t.Fatalf("after compaction: primary calls=%d summaries=%d; want 2 completed calls and only one summary", len(model.inputs), model.summaries)
			}
			var retained *agentschema.ResponseMeta
			for _, message := range model.inputs[1] {
				if message.Role == agentschema.Assistant && message.ResponseMeta != nil && message.ResponseMeta.Usage != nil {
					retained = message.ResponseMeta
				}
			}
			if retained == nil || retained.InputEstimate == nil || retained.InputEstimate.Version != agentmodel.InputEstimateVersion || retained.InputEstimate.Tokens != retained.Usage.PromptTokens || retained.InputEstimate.Model != definition.ModelIdentity {
				t.Fatalf("retained response lost its original request estimate: %+v", retained)
			}
		})
	}
}
