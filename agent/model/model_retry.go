package model

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentretry "github.com/alfredxw/denova/agent/internal/retry"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ModelOutput is a detached response and request snapshot for business review.
// Attempt is one-based within the current logical model response budget.
type ModelOutput struct {
	Attempt int
	Message *agentschema.Message
	Request *ModelRequestSnapshot
}

// ModelOutputReview permits only acceptance or bounded, attributed feedback.
// Feedback replaces this middleware's previous feedback for the same response;
// it never replaces accepted history, options, or the model adapter.
type ModelOutputReview struct {
	Action   agentretry.ModelOutputAction
	Feedback []agentschema.ContextFragment
	Reason   string
}

const modelRepairFeedbackMaxBytes = 64 * 1024

func ModelRepairMessages(review ModelOutputReview) ([]*agentschema.Message, error) {
	var messages []*agentschema.Message
	total := 0
	for _, fragment := range review.Feedback {
		if fragment.Source == "" || fragment.Purpose == "" || fragment.HardLimit <= 0 {
			return nil, errors.New("model output feedback requires source, purpose, and a positive hard limit")
		}
		total += len(fragment.Content)
		if len(fragment.Content) > fragment.HardLimit || total > modelRepairFeedbackMaxBytes {
			return nil, errors.New("model output feedback exceeds its byte limit")
		}
		role := fragment.Role
		if role == "" {
			role = agentschema.User
		}
		if role != agentschema.User && role != agentschema.Assistant {
			return nil, errors.New("model output feedback must use user or assistant role")
		}
		messages = append(messages, &agentschema.Message{Role: role, Content: fmt.Sprintf("[source=%s; purpose=%s]\n%s", fragment.Source, fragment.Purpose, fragment.Content)})
	}
	return messages, nil
}

// Complete returns one accepted response from a fixed request, buffering any
// stream so partial failed attempts never become the next attempt's output.
// The attempt count and retry policy apply only to this side call, with
// its own budget, independent of the main response or total Agent run length.
func (snapshot *ModelRequestSnapshot) Complete(ctx context.Context, maximum int, retry *agentretry.RetryConfig) (*agentschema.Message, error) {
	if snapshot == nil || snapshot.model == nil {
		return nil, errors.New("model request snapshot is unavailable")
	}
	return agentretry.ExecuteModelAttempts(ctx, maximum, retry, func(int) (agentretry.ModelAttemptResult, error) {
		result := agentretry.ModelAttemptResult{OutputState: agentretry.ModelOutputNone, Review: agentretry.ModelOutputAccept}
		if !snapshot.Streaming() {
			result.Message, result.Failure = agentasync.AwaitContextCall(ctx, func() (*agentschema.Message, error) { return snapshot.Generate(ctx) }, nil, nil)
		} else {
			stream, err := agentasync.AwaitContextCall(ctx, func() (*agentstream.StreamReader[*agentschema.Message], error) { return snapshot.Stream(ctx) }, nil, func(stream *agentstream.StreamReader[*agentschema.Message]) {
				if stream != nil {
					stream.Close()
				}
			})
			if err != nil {
				result.Failure = err
				return result, nil
			}
			if stream == nil {
				return result, errors.New("model side call returned a nil stream")
			}
			defer func() {
				if ctx.Err() == nil {
					stream.Close()
					return
				}
				agentasync.SafeGo(stream.Close, func(err error) { slog.Error("Close cancelled model side stream failed", "error", err) })
			}()
			assembler := agentschema.NewMessageAssembler()
			for {
				chunk, err := agentasync.AwaitContextCall(ctx, stream.Recv, stream.Close, nil)
				if errors.Is(err, io.EOF) {
					result.Message, result.Failure = assembler.Message()
					break
				}
				if err != nil {
					result.Failure = err
					break
				}
				result.OutputState = agentretry.ModelOutputPartial
				if err := assembler.Append(chunk); err != nil {
					return result, err
				}
			}
		}
		if result.Failure == nil {
			if result.Message == nil {
				return result, errors.New("model side call returned no response")
			}
			result.OutputState = agentretry.ModelOutputComplete
		}
		return result, nil
	}, nil)
}
