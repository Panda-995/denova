package engine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentretry "github.com/alfredxw/denova/agent/internal/retry"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

type partialRetryModel struct {
	calls atomic.Int32
}

func TestModelRetryAndOutputRepairShareBudget(t *testing.T) {
	model := &scriptedModel{responses: []scriptedModelResponse{{err: errors.New("transient")}, {message: agentschema.AssistantMessage("reject me", nil)}}}
	native, err := newModelToolLoop(context.Background(), loopConfig{
		Model: model, ModelMaxAttempts: 2, Retry: &agentretry.RetryConfig{Decide: retryEveryTestError},
		Middlewares: []agentmiddleware.Middleware{&retryNormalizationMiddleware{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator := newLoopRunner(loopRunnerConfig{Agent: native}).Query(context.Background(), "go")
	var terminal error
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			terminal = event.Err
		}
	}
	if terminal == nil || !strings.Contains(terminal.Error(), "after 2 attempts") || len(model.capturedInputs()) != 2 {
		t.Fatalf("terminal=%v provider calls=%d", terminal, len(model.capturedInputs()))
	}
}

func TestRetryBackoffCanBeCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	model := &partialRetryModel{}
	native, err := newModelToolLoop(ctx, loopConfig{Model: model, ModelMaxAttempts: 2, Retry: &agentretry.RetryConfig{Decide: func(context.Context, agentretry.RetryContext) agentretry.RetryDecision {
		return agentretry.RetryDecision{Action: agentretry.RetryAgain, Delay: time.Hour, Reason: "test_wait"}
	}}})
	if err != nil {
		t.Fatal(err)
	}
	iterator := newLoopRunner(loopRunnerConfig{Agent: native, EnableStreaming: true}).Query(ctx, "go")
	for {
		event, ok := iterator.Next()
		if !ok {
			t.Fatal("ended before backoff")
		}
		if event.Output != nil && event.Output.MessageOutput != nil {
			_, _ = event.Output.MessageOutput.GetMessage()
		}
		if event.Output != nil && event.Output.ModelRetry != nil {
			cancel()
			break
		}
	}
	finished := make(chan error, 1)
	agentasync.SafeGo(func() {
		for {
			event, ok := iterator.Next()
			if !ok {
				break
			}
			if event.Err != nil {
				finished <- event.Err
				return
			}
		}
		finished <- nil
	}, func(err error) { finished <- err })
	select {
	case err := <-finished:
		if err == nil || model.calls.Load() != 1 {
			t.Fatalf("err=%v calls=%d", err, model.calls.Load())
		}
	case <-time.After(time.Second):
		t.Fatal("retry did not stop during backoff")
	}
}

func retryEveryTestError(context.Context, agentretry.RetryContext) agentretry.RetryDecision {
	return agentretry.RetryDecision{Action: agentretry.RetryAgain, Reason: "test_transient"}
}

func (*partialRetryModel) Generate(context.Context, []*agentschema.Message, ...agentmodel.ModelOption) (*agentschema.Message, error) {
	return nil, errors.New("unexpected Generate")
}

func (model *partialRetryModel) Stream(context.Context, []*agentschema.Message, ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	if model.calls.Add(1) > 1 {
		return agentstream.StreamReaderFromArray([]*agentschema.Message{agentschema.AssistantMessage("complete", nil)}), nil
	}
	reader, writer := agentstream.Pipe[*agentschema.Message](-1)
	writer.Send(agentschema.AssistantMessage("partial", []agentschema.ToolCall{{ID: "unaccepted", Type: "function", Function: agentschema.FunctionCall{Name: "echo", Arguments: `{}`}}}), nil)
	writer.Send(nil, errors.New("connection lost"))
	writer.Close()
	return reader, nil
}

func TestPartialResponseRetriesWithoutExecutingUnacceptedTools(t *testing.T) {
	model := &partialRetryModel{}
	var toolCalls atomic.Int32
	native, err := newModelToolLoop(context.Background(), loopConfig{
		Name: "partial-retry", Model: model,
		Tools: []agenttool.ToolDefinition{testToolDefinition(&functionTool{name: "echo", run: func(context.Context, string) (string, error) {
			toolCalls.Add(1)
			return "unexpected", nil
		}})},
		ModelMaxAttempts: 2, Retry: &agentretry.RetryConfig{Decide: retryEveryTestError},
	})
	if err != nil {
		t.Fatal(err)
	}
	iterator := newLoopRunner(loopRunnerConfig{Agent: native, EnableStreaming: true}).Query(context.Background(), "go")
	var completed string
	var ordinals []int
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			t.Fatalf("retry terminated the run: %v", event.Err)
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		output := event.Output.MessageOutput
		ordinals = append(ordinals, output.ModelResponseOrdinal)
		message, streamErr := output.GetMessage()
		if streamErr == nil && message != nil {
			completed = message.Content
		}
	}
	if completed != "complete" || model.calls.Load() != 2 || toolCalls.Load() != 0 {
		t.Fatalf("completed=%q provider_calls=%d tool_calls=%d", completed, model.calls.Load(), toolCalls.Load())
	}
	if len(ordinals) != 2 || ordinals[1] <= ordinals[0] {
		t.Fatalf("response ordinals=%v, want distinct increasing responses", ordinals)
	}
}
