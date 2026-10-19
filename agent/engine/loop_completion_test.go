package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type completionFeedbackMiddleware struct {
	agentmiddleware.BaseMiddleware
}

func (*completionFeedbackMiddleware) BeforeAgentCompletion(ctx context.Context, state *agentmiddleware.RunState) (context.Context, []agentschema.ContextFragment, error) {
	if state.Messages[len(state.Messages)-1].Content != "accepted prose" {
		return ctx, nil, nil
	}
	return ctx, []agentschema.ContextFragment{{Source: "test", Purpose: "finish work", Content: "Continue the accepted response", HardLimit: 1024}}, nil
}

func TestAcceptedResponseContinuationWaitsForCommitAndDoesNotUseRetry(t *testing.T) {
	commitFailure := errors.New("context commit failed")
	for _, commitErr := range []error{nil, commitFailure} {
		name := "committed"
		if commitErr != nil {
			name = "commit rejected"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			model := &fullSeamRetryModel{responses: []*agentschema.Message{
				agentschema.AssistantMessage("accepted prose", nil), agentschema.AssistantMessage("done", nil),
			}}
			observer := &preparationStateObserver{}
			loop, err := newModelToolLoop(ctx, loopConfig{Model: model, ModelMaxAttempts: 1,
				Middlewares: []agentmiddleware.Middleware{&completionFeedbackMiddleware{}, observer}})
			if err != nil {
				t.Fatal(err)
			}
			iterator := loop.Run(contextWithToolStartReceipt(ctx), &loopInput{Messages: []*agentschema.Message{agentschema.UserMessage("work")}})
			var failure error
			commits := 0
			for {
				event, ok := iterator.Next()
				if !ok {
					break
				}
				if event.Err != nil {
					failure = event.Err
				}
				if event.Output == nil {
					continue
				}
				if attempt := event.Output.ModelAttempt; attempt != nil {
					attempt.Receipt <- nil
				}
				if boundary := event.Output.Continuation; boundary != nil {
					commits++
					model.mu.Lock()
					calls := len(model.inputs)
					model.mu.Unlock()
					if calls != 1 || len(boundary.messages) != 2 || boundary.messages[0].Content != "accepted prose" || !agentschema.IsAgentContinuationMessage(boundary.messages[1]) {
						t.Fatalf("continuation lost its accepted response or ran before commit: calls=%d messages=%#v", calls, boundary.messages)
					}
					boundary.receipt <- commitErr
				}
			}
			if commits != 1 || !errors.Is(failure, commitErr) {
				t.Fatalf("commits=%d failure=%v", commits, failure)
			}
			if commitErr != nil {
				if len(model.inputs) != 1 {
					t.Fatal("uncommitted continuation reached the provider")
				}
				return
			}
			if len(model.inputs) != 2 || len(model.inputs[1]) != 3 || model.inputs[1][1].Content != "accepted prose" || !agentschema.IsAgentContinuationMessage(model.inputs[1][2]) || len(observer.messages) != 4 {
				t.Fatal("accepted response and continuation were not appended in conversation order")
			}
		})
	}
}
