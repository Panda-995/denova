package interactiveapp

import (
	"context"
	"testing"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
)

// Output-boundary tests use a narrator-only model. Accept its fixture modules
// after real input materialization, with the runtime's selected cycle identity.
type gameSubmissionFixture struct {
	*agentmiddleware.BaseMiddleware
	t            *testing.T
	conversation *Conversation
	intent, goal string
}

func (fixture gameSubmissionFixture) BeforeAgent(ctx context.Context, run *agentmiddleware.RunContext) (context.Context, *agentmiddleware.RunContext, error) {
	if !agentexecution.IsInspection(ctx) {
		submitTestTurnResult(fixture.t, fixture.conversation, fixture.intent, fixture.goal)
	}
	return ctx, run, nil
}

func gameSubmissionForTest(t *testing.T, conversation *Conversation, intent, goal string) agentmiddleware.Middleware {
	return gameSubmissionFixture{BaseMiddleware: &agentmiddleware.BaseMiddleware{}, t: t, conversation: conversation, intent: intent, goal: goal}
}
