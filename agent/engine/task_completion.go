package engine

import (
	"context"
	"errors"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

var ErrTaskCompletionWaitInterrupted = errors.New("task completion wait interrupted")

// CompletionReader is the model loop's read-only view of the parent's
// completion mailbox. Tracking, delivery receipts and locking stay in lifecycle.
type CompletionReader interface {
	PendingCompletions() []agentschema.TaskCompletion
	HasCompletions() bool
	WaitCompletions(context.Context, <-chan struct{}) (bool, error)
}

type taskCompletionSessionContextKey struct{}

// ContextWithTaskCompletionSession supplies the mailbox for one parent cycle.
// The lifecycle owner persists delivery receipts from TranscriptUpdated before
// acknowledging that event; the reader itself never mutates durable state.
func ContextWithTaskCompletionSession(ctx context.Context, reader CompletionReader) context.Context {
	return context.WithValue(ctx, taskCompletionSessionContextKey{}, reader)
}

func pendingTaskCompletionsFromContext(ctx context.Context) []agentschema.TaskCompletion {
	reader, _ := ctx.Value(taskCompletionSessionContextKey{}).(CompletionReader)
	if reader == nil {
		return nil
	}
	return reader.PendingCompletions()
}

func hasTrackedTaskCompletions(ctx context.Context) bool {
	reader, _ := ctx.Value(taskCompletionSessionContextKey{}).(CompletionReader)
	return reader != nil && reader.HasCompletions()
}

func waitForTrackedTaskCompletionsFromContext(ctx context.Context, interrupt <-chan struct{}) (bool, error) {
	reader, _ := ctx.Value(taskCompletionSessionContextKey{}).(CompletionReader)
	if reader == nil {
		return false, nil
	}
	return reader.WaitCompletions(ctx, interrupt)
}
