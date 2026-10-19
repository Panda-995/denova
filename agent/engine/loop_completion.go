package engine

import (
	"context"
	"errors"
	"fmt"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func (agent *modelToolLoop) continueAfterResponse(ctx context.Context, state *agentmiddleware.RunState, assistant *agentschema.Message, events *asyncGenerator[*loopEvent]) (context.Context, bool, error) {
	var feedback []*agentschema.Message
	for _, middleware := range agent.middlewares {
		nextCtx, fragments, err := middleware.BeforeAgentCompletion(ctx, state)
		if err != nil {
			return ctx, false, fmt.Errorf("before agent completion middleware: %w", err)
		}
		if nextCtx == nil {
			return ctx, false, errors.New("before agent completion middleware returned nil Go context")
		}
		ctx = nextCtx
		messages, err := agentmodel.ModelFeedbackMessages(fragments)
		if err != nil {
			return ctx, false, err
		}
		for _, message := range messages {
			if message.Role != agentschema.User {
				return ctx, false, errors.New("agent completion feedback must use user role")
			}
			message.Extra = map[string]any{agentschema.ExtraKeyAgentContinuation: true}
		}
		feedback = append(feedback, messages...)
	}
	if len(feedback) == 0 {
		return ctx, false, nil
	}
	state.Messages = append(state.Messages, feedback...)
	if !toolStartReceiptRequired(ctx) {
		return ctx, true, nil
	}
	boundary := &modelContinuationBoundary{
		messages: append([]*agentschema.Message{assistant.Clone()}, agentschema.CloneMessages(feedback)...),
		receipt:  make(chan error, 1),
	}
	events.Send(&loopEvent{AgentName: agent.name, Output: &loopOutput{Continuation: boundary}})
	select {
	case err := <-boundary.receipt:
		return ctx, true, err
	case <-ctx.Done():
		return ctx, false, agent.contextError(ctx, nil)
	}
}
