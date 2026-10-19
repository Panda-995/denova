package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	agentengine "github.com/alfredxw/denova/agent/engine"
	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentretry "github.com/alfredxw/denova/agent/internal/retry"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

type partialRetryModel struct {
	calls atomic.Int32
}

func TestSessionAdmitsOnlyTheRecoveredResponse(t *testing.T) {
	model := &partialRetryModel{}
	owner, err := New(context.Background(), agentengine.Definition{Name: "retry", Model: model, Execution: agentexecution.ExecutionPolicy{
		ModelMaxAttempts: 2, Retry: &agentretry.RetryConfig{Decide: retryEveryTestError},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	session, err := owner.Session(context.Background(), agentsession.Named("retry"))
	if err != nil {
		t.Fatal(err)
	}
	run, err := session.Run(context.Background(), agentschema.Text("go"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := run.Wait(context.Background())
	if err != nil || result.Status != agentschema.ResultCompleted {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	snapshot, err := session.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.RecentRuns) != 1 || snapshot.RecentRuns[0].Output != "complete" {
		t.Fatalf("settled output=%#v", snapshot.RecentRuns)
	}
	state, err := decodeJournalTranscript(session.engineState)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Messages) != 2 || state.Messages[1].Content != "complete" || len(state.Messages[1].ToolCalls) != 0 {
		t.Fatalf("canonical transcript=%#v", state.Messages)
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
