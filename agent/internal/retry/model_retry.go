package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"time"

	agentasync "github.com/alfredxw/denova/agent/internal/async"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// ModelOutputState distinguishes a failed preview from a complete response.
type ModelOutputState string

const (
	ModelOutputNone     ModelOutputState = "none"
	ModelOutputPartial  ModelOutputState = "partial"
	ModelOutputComplete ModelOutputState = "complete"
)

type RetryAction string

const (
	RetryStop  RetryAction = "stop"
	RetryAgain RetryAction = "retry"
)

// RetryContext describes a failed provider call. Attempt is one-based and
// includes this call; retry policy cannot replace the request or its model.
type RetryContext struct {
	Attempt     int
	Err         error
	OutputState ModelOutputState
}

type RetryDecision struct {
	Action RetryAction
	Delay  time.Duration
	Reason string
}

// RetryConfig decides only whether a failed call should be repeated. The
// ExecutionPolicy owns the shared network retry and output repair budget.
// A nil config disables automatic failure retries.
type RetryConfig struct {
	Decide func(context.Context, RetryContext) RetryDecision
}

// TransientRetry is the standard failure policy: exponential backoff with
// jitter, a 30-second local cap, and any longer provider Retry-After delay.
// Provider adapters may expose Retryable and RetryDelay methods on errors.
func TransientRetry(ctx context.Context, attempt RetryContext) RetryDecision {
	stop := RetryDecision{Action: RetryStop}
	if attempt.Err == nil || ctx.Err() != nil || errors.Is(attempt.Err, context.Canceled) {
		return stop
	}
	reason := "network_unavailable"
	var classified interface{ Retryable() bool }
	var network net.Error
	switch {
	case errors.As(attempt.Err, &classified):
		if !classified.Retryable() {
			return stop
		}
		reason = "provider_unavailable"
	case errors.Is(attempt.Err, context.DeadlineExceeded), errors.Is(attempt.Err, io.ErrUnexpectedEOF), errors.Is(attempt.Err, io.EOF):
	case errors.As(attempt.Err, &network):
	default:
		return stop
	}
	delay := min(30*time.Second, time.Second<<min(5, max(0, attempt.Attempt-1)))
	delay = delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
	var hint interface{ RetryDelay() time.Duration }
	if errors.As(attempt.Err, &hint) {
		delay = max(delay, hint.RetryDelay())
	}
	return RetryDecision{Action: RetryAgain, Delay: delay, Reason: reason}
}

type ModelOutputAction string

const (
	ModelOutputAccept ModelOutputAction = "accept"
	ModelOutputRepair ModelOutputAction = "repair"
)

func decideModelRetry(ctx context.Context, policy *RetryConfig, attempt RetryContext) (RetryDecision, error) {
	if policy == nil || policy.Decide == nil {
		return RetryDecision{Action: RetryStop}, nil
	}
	decision := policy.Decide(ctx, attempt)
	switch decision.Action {
	case RetryStop:
		return decision, nil
	case RetryAgain:
		if decision.Delay < 0 {
			return RetryDecision{}, errors.New("model retry delay cannot be negative")
		}
		return decision, nil
	default:
		return RetryDecision{}, fmt.Errorf("unsupported model retry action %q", decision.Action)
	}
}

type ModelAttemptResult struct {
	Message     *agentschema.Message
	Failure     error
	OutputState ModelOutputState
	Review      ModelOutputAction
	Reason      string
}

// executeModelAttempts is shared by the main loop and fixed-model side calls.
// Preparation/review errors stop immediately; only provider failures reach the
// failure policy. The callback cannot reset the budget after output repair.
func ExecuteModelAttempts(ctx context.Context, maximum int, retry *RetryConfig,
	call func(int) (ModelAttemptResult, error),
	repeating func(int, ModelAttemptResult, RetryDecision) error,
) (*agentschema.Message, error) {
	maximum = max(1, maximum)
	for attempt := 1; ; attempt++ {
		if err := agentasync.AdmitWork(ctx); err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := call(attempt)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		decision := RetryDecision{Action: RetryStop}
		if result.Failure != nil {
			decision, err = decideModelRetry(ctx, retry, RetryContext{Attempt: attempt, Err: result.Failure, OutputState: result.OutputState})
			if err != nil {
				return nil, err
			}
		} else if result.Review == ModelOutputRepair {
			decision = RetryDecision{Action: RetryAgain, Reason: result.Reason}
		}
		if decision.Action == RetryStop {
			return result.Message, result.Failure
		}
		if attempt >= maximum {
			if result.Failure != nil {
				return nil, result.Failure
			}
			return nil, fmt.Errorf("model output rejected after %d attempts: %s", maximum, result.Reason)
		}
		if repeating != nil {
			if err := repeating(attempt, result, decision); err != nil {
				return nil, err
			}
		}
		slog.InfoContext(ctx, "Retrying model request", "attempt", attempt, "max_attempts", maximum, "output_state", result.OutputState, "delay", decision.Delay, "reason", decision.Reason)
		if decision.Delay > 0 {
			if err := agentasync.WaitContext(ctx, decision.Delay); err != nil {
				return nil, err
			}
		}
	}
}
