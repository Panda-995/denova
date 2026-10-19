package model

import (
	"context"

	agentretry "github.com/alfredxw/denova/agent/internal/retry"
)

type ModelOutputState = agentretry.ModelOutputState

const ModelOutputNone = agentretry.ModelOutputNone
const ModelOutputPartial = agentretry.ModelOutputPartial
const ModelOutputComplete = agentretry.ModelOutputComplete

type RetryAction = agentretry.RetryAction

const RetryStop = agentretry.RetryStop
const RetryAgain = agentretry.RetryAgain

type RetryContext = agentretry.RetryContext
type RetryDecision = agentretry.RetryDecision
type RetryConfig = agentretry.RetryConfig

// TransientRetry applies the standard bounded-delay policy for provider failures.
func TransientRetry(ctx context.Context, attempt RetryContext) RetryDecision {
	return agentretry.TransientRetry(ctx, attempt)
}

type ModelOutputAction = agentretry.ModelOutputAction

const ModelOutputAccept = agentretry.ModelOutputAccept
const ModelOutputRepair = agentretry.ModelOutputRepair
