package model

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentretry "github.com/alfredxw/denova/agent/internal/retry"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type partialRetryModel struct {
	calls atomic.Int32
}

func TestSideCallRetriesPartialStreamWithinItsOwnBudget(t *testing.T) {
	model := &partialRetryModel{}
	call := &ModelCall{Model: model, Messages: []*agentschema.Message{agentschema.UserMessage("summary")}, Streaming: true}
	response, err := call.Snapshot().Complete(context.Background(), 2, &agentretry.RetryConfig{Decide: retryEveryTestError})
	if err != nil || response.Content != "complete" || model.calls.Load() != 2 {
		t.Fatalf("response=%#v err=%v calls=%d", response, err, model.calls.Load())
	}
}

func TestSideCallCancellationDoesNotWaitForProvider(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "stream"}[streaming], func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			var model BaseChatModel = &blockingGenerateModel{started: started, release: release}
			if streaming {
				model = &blockingModelStart{started: started, release: release}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			call := &ModelCall{Model: model, Messages: []*agentschema.Message{agentschema.UserMessage("summary")}, Streaming: streaming}
			done := make(chan error, 1)
			agentasync.SafeGo(func() { _, err := call.Snapshot().Complete(ctx, 0, nil); done <- err }, func(err error) { done <- err })
			<-started
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("side call cancellation=%v", err)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatal("side call ignored cancellation")
			}
		})
	}
}

func retryEveryTestError(context.Context, agentretry.RetryContext) agentretry.RetryDecision {
	return agentretry.RetryDecision{Action: agentretry.RetryAgain, Reason: "test_transient"}
}

func (*partialRetryModel) Generate(context.Context, []*agentschema.Message, ...ModelOption) (*agentschema.Message, error) {
	return nil, errors.New("unexpected Generate")
}

func (model *partialRetryModel) Stream(context.Context, []*agentschema.Message, ...ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	if model.calls.Add(1) > 1 {
		return agentstream.StreamReaderFromArray([]*agentschema.Message{agentschema.AssistantMessage("complete", nil)}), nil
	}
	reader, writer := agentstream.Pipe[*agentschema.Message](-1)
	writer.Send(agentschema.AssistantMessage("partial", []agentschema.ToolCall{{ID: "unaccepted", Type: "function", Function: agentschema.FunctionCall{Name: "echo", Arguments: `{}`}}}), nil)
	writer.Send(nil, errors.New("connection lost"))
	writer.Close()
	return reader, nil
}
