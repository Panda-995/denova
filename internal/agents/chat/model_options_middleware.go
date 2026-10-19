package chat

import (
	"context"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentmodel "github.com/alfredxw/denova/agent/model"
)

type defaultMaxTokensMiddleware struct {
	*agentmiddleware.BaseMiddleware
	maxOutputTokens int
}

// NewDefaultMaxTokensMiddleware projects the provider-profile default into the
// final provider-neutral call. Explicit per-call options keep precedence.
func NewDefaultMaxTokensMiddleware(maxOutputTokens int) agentmiddleware.Middleware {
	return &defaultMaxTokensMiddleware{
		BaseMiddleware:  &agentmiddleware.BaseMiddleware{},
		maxOutputTokens: maxOutputTokens,
	}
}

func (middleware *defaultMaxTokensMiddleware) BeforeModelCall(
	ctx context.Context,
	call *agentmodel.ModelCall,
	_ *agentmiddleware.ModelContext,
) (context.Context, *agentmodel.ModelCall, error) {
	if call == nil || middleware.maxOutputTokens <= 0 {
		return ctx, call, nil
	}
	if agentmodel.GetCommonOptions(&agentmodel.Options{}, call.Options...).MaxTokens != nil {
		return ctx, call, nil
	}
	next := *call
	next.Options = append(append([]agentmodel.ModelOption(nil), call.Options...), agentmodel.WithMaxTokens(middleware.maxOutputTokens))
	return ctx, &next, nil
}
