package chat

import (
	"context"
	"fmt"
	"reflect"

	agentcontext "denova/internal/agents/context"
	"denova/internal/agents/toolresult"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentmodel "github.com/alfredxw/denova/agent/model"
)

type contextNormalizerMiddleware struct {
	*agentmiddleware.BaseMiddleware
}

// NewModelContextMiddlewares returns Denova's model-only history policy and
// presentation normalizer. Agent retains complete raw tool batches and
// provider reasoning so Cleanup, Compaction, recovery, and audit stay lossless;
// only the request projection drops history that the product never replays.
func NewModelContextMiddlewares(policy toolresult.ContextPolicy) []agentmiddleware.Middleware {
	return []agentmiddleware.Middleware{NewModelHistoryProjectionMiddleware(policy), NewContextNormalizerMiddleware()}
}

// NewContextNormalizerMiddleware creates the provider-neutral repair boundary
// that runs immediately before each model call.
func NewContextNormalizerMiddleware() agentmiddleware.Middleware {
	return &contextNormalizerMiddleware{BaseMiddleware: &agentmiddleware.BaseMiddleware{}}
}

func (m *contextNormalizerMiddleware) BeforeModelCall(
	ctx context.Context,
	call *agentmodel.ModelCall,
	modelContext *agentmiddleware.ModelContext,
) (context.Context, *agentmodel.ModelCall, error) {
	if call == nil {
		return ctx, call, nil
	}
	normalized, err := agentcontext.NormalizeModelContextMessages(call.Messages)
	if err != nil {
		return ctx, call, fmt.Errorf("normalize provider-neutral model context: %w", err)
	}
	if !reflect.DeepEqual(call.Messages, normalized) {
		modelContext.ReportContextNormalization(agentmiddleware.ContextNormalizationMetrics{
			RepairCount: 1, MessagesBefore: len(call.Messages), MessagesAfter: len(normalized),
		})
	}
	next := *call
	next.Messages = normalized
	return ctx, &next, nil
}
