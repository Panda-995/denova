package chat

import (
	"context"

	agentrun "denova/internal/agents/run"
	agenttoolruntime "denova/internal/agents/toolruntime"
	producttools "denova/internal/agents/tools"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
)

// PublicHostMiddleware installs Denova-only trace, review scope, and plan-mode
// context around the public Agent lifecycle. Interaction and artifact storage
// are bound through their dedicated Definition capabilities; the Agent package
// remains independent from Denova session types.
type PublicHostMiddleware struct {
	*agentmiddleware.BaseMiddleware
	request ChatRequest
	options agentrun.Options
	trace   PublicRunTraceBinder
}

// PublicRunTraceBinder attaches the Denova run ledger to the generic Agent
// context after the public lifecycle has assigned its durable Run ID.
type PublicRunTraceBinder interface {
	BindPublicRunTrace(context.Context, string) context.Context
}

func NewPublicHostMiddleware(
	request ChatRequest,
	options agentrun.Options,
	trace PublicRunTraceBinder,
) *PublicHostMiddleware {
	return &PublicHostMiddleware{
		BaseMiddleware: &agentmiddleware.BaseMiddleware{}, request: request, options: options, trace: trace,
	}
}

func (middleware *PublicHostMiddleware) BeforeAgent(
	ctx context.Context,
	run *agentmiddleware.RunContext,
) (context.Context, *agentmiddleware.RunContext, error) {
	runID := ""
	if scope, ok := agentexecution.InvocationScopeFromContext(ctx); ok {
		runID = scope.OperationID
	}
	if runID == "" {
		if identity, ok := agentexecution.InvocationIdentityFromContext(ctx); ok {
			runID = identity.OperationID
			if runID == "" {
				runID = identity.RunID
			}
		}
	}
	if middleware.trace != nil {
		ctx = middleware.trace.BindPublicRunTrace(ctx, runID)
	}
	ctx = producttools.ContextWithWorkspaceChangeScope(ctx, producttools.WorkspaceChangeScope{
		RunID: runID, SessionID: middleware.options.SessionID,
		ReviewThreadID: middleware.options.ReviewThreadID,
	})
	if middleware.request.PlanMode {
		ctx = agenttoolruntime.ContextWithToolAccessMode(ctx, agenttoolruntime.ToolAccessModePlanReadOnly)
	}
	return ctx, run, nil
}

var _ agentmiddleware.Middleware = (*PublicHostMiddleware)(nil)
