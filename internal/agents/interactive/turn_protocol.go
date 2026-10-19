package interactive

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"

	producttools "denova/internal/agents/tools"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentstream "github.com/alfredxw/denova/agent/model/stream"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

const (
	interactiveTurnSubmissionToolName = producttools.SubmitInteractiveTurnToolName
	legacyActorStatePatchesToolName   = "submit_actor_state_patches"
	legacyInteractiveChoicesToolName  = "submit_choices"
	interactiveRetryFeedbackMaxBytes  = 1024
	interactiveRetryFeedbackPrefix    = "[Interactive turn protocol feedback; source=backend completion guard]"
)

const (
	TurnSubmissionToolName = interactiveTurnSubmissionToolName
)

type interactiveTurnProtocolStateKey struct{}

type interactiveTurnProtocolRunState struct {
	narrativeCandidateReady atomic.Bool
	legacyNarrative         string
}

func interactiveTurnProtocolState(ctx context.Context) *interactiveTurnProtocolRunState {
	state, _ := ctx.Value(interactiveTurnProtocolStateKey{}).(*interactiveTurnProtocolRunState)
	return state
}

func RequestTurnCompletion(ctx context.Context) bool {
	state := interactiveTurnProtocolState(ctx)
	if state == nil || !state.narrativeCandidateReady.Load() {
		return false
	}
	return agentexecution.RequestCompletionAfterTools(ctx)
}

// TurnProtocolMiddleware keeps the tool schema stable for prompt
// caching and provides a narrative-only fallback when a model submits before
// producing a prose candidate.
type TurnProtocolMiddleware struct {
	*agentmiddleware.BaseMiddleware
	ready         func() bool
	loadNarrative func(context.Context) (NarrativeCandidate, error)
}

func NewTurnProtocolMiddleware(config InteractiveStoryToolContext) *TurnProtocolMiddleware {
	return &TurnProtocolMiddleware{
		BaseMiddleware: &agentmiddleware.BaseMiddleware{},
		ready:          config.TurnResultReady, loadNarrative: config.LoadNarrativeCandidate,
	}
}

func (m *TurnProtocolMiddleware) BeforeAgent(ctx context.Context, runCtx *agentmiddleware.RunContext) (context.Context, *agentmiddleware.RunContext, error) {
	state := &interactiveTurnProtocolRunState{}
	if m.loadNarrative != nil {
		narrative, err := m.loadNarrative(ctx)
		if err != nil {
			return ctx, runCtx, err
		}
		state.narrativeCandidateReady.Store(narrative.Content != "")
		if !narrative.HistoryAccepted {
			state.legacyNarrative = narrative.Content
		}
	}
	return context.WithValue(ctx, interactiveTurnProtocolStateKey{}, state), runCtx, nil
}

// BeforeModelCall reads released drafts without rewriting their journals.
// New accepted responses already have canonical history and bypass this path.
func (m *TurnProtocolMiddleware) BeforeModelCall(ctx context.Context, call *agentmodel.ModelCall, _ *agentmiddleware.ModelContext) (context.Context, *agentmodel.ModelCall, error) {
	progress := interactiveTurnProtocolState(ctx)
	if progress == nil || progress.legacyNarrative == "" || (m.ready != nil && m.ready()) {
		return ctx, call, nil
	}
	for _, message := range call.Messages {
		if message.Role == agentschema.Assistant && message.Content == progress.legacyNarrative {
			return ctx, call, nil
		}
	}
	feedback, err := agentmodel.ModelFeedbackMessages(interactiveProtocolFeedback())
	if err != nil {
		return ctx, call, err
	}
	next := *call
	next.Messages = append(agentschema.CloneMessages(call.Messages), agentschema.AssistantMessage(progress.legacyNarrative, nil))
	next.Messages = append(next.Messages, feedback...)
	return ctx, &next, nil
}

// BeforeAgentCompletion checks product completeness after normal response
// acceptance. Feedback continues the same conversation without rejecting prose.
func (m *TurnProtocolMiddleware) BeforeAgentCompletion(ctx context.Context, _ *agentmiddleware.RunState) (context.Context, []agentschema.ContextFragment, error) {
	if m.ready == nil || m.ready() {
		return ctx, nil, nil
	}
	return ctx, interactiveProtocolFeedback(), nil
}

func (m *TurnProtocolMiddleware) WrapModel(_ context.Context, wrapped agentmodel.BaseChatModel, _ *agentmiddleware.ModelContext) (agentmodel.BaseChatModel, error) {
	if m == nil || m.ready == nil || !m.ready() {
		return wrapped, nil
	}
	return &interactiveNarrativeOnlyModel{BaseChatModel: wrapped}, nil
}

func (m *TurnProtocolMiddleware) AfterModelRewriteState(ctx context.Context, state *agentmiddleware.RunState, _ *agentmiddleware.ModelContext) (context.Context, *agentmiddleware.RunState, error) {
	last := state.Messages[len(state.Messages)-1]
	if OutputContainsNarrativeCandidate(last) {
		if progress := interactiveTurnProtocolState(ctx); progress != nil {
			progress.narrativeCandidateReady.Store(true)
		}
	}
	if m.ready != nil && m.ready() && last != nil && len(last.ToolCalls) > 0 {
		return ctx, state, errors.New("tools are forbidden after TurnResult acceptance")
	}
	return ctx, state, nil
}

type interactiveNarrativeOnlyModel struct {
	agentmodel.BaseChatModel
}

func (m *interactiveNarrativeOnlyModel) Generate(ctx context.Context, messages []*agentschema.Message, opts ...agentmodel.ModelOption) (*agentschema.Message, error) {
	narrativeOpts := append([]agentmodel.ModelOption(nil), opts...)
	narrativeOpts = append(narrativeOpts, agentmodel.WithToolChoice(agentmodel.ToolChoiceForbidden))
	return m.BaseChatModel.Generate(ctx, messages, narrativeOpts...)
}

func (m *interactiveNarrativeOnlyModel) Stream(ctx context.Context, messages []*agentschema.Message, opts ...agentmodel.ModelOption) (*agentstream.StreamReader[*agentschema.Message], error) {
	narrativeOpts := append([]agentmodel.ModelOption(nil), opts...)
	narrativeOpts = append(narrativeOpts, agentmodel.WithToolChoice(agentmodel.ToolChoiceForbidden))
	return m.BaseChatModel.Stream(ctx, messages, narrativeOpts...)
}

func interactiveProtocolFeedback() []agentschema.ContextFragment {
	feedback := strings.Join([]string{
		interactiveRetryFeedbackPrefix,
		"You attempted to finish the turn before both state_changes and choices were accepted.",
		"The first prose candidate is locked and already displayed. Call only submit_interactive_turn now, providing only fields named by retry_modules. Do not resubmit accepted modules, and do not repeat or rewrite prose after ready=true.",
		"Do not finish this turn before both submission modules are accepted.",
	}, "\n")
	return []agentschema.ContextFragment{{
		Source: "interactive_turn_protocol", Purpose: "missing_modules", Role: agentschema.User,
		Content: feedback, HardLimit: interactiveRetryFeedbackMaxBytes,
	}}
}

// OutputContainsNarrativeCandidate identifies prose accepted by the Game turn
// protocol. Other tool commentary is not the turn's locked narrative.
func OutputContainsNarrativeCandidate(message *agentschema.Message) bool {
	if message == nil || strings.TrimSpace(message.Content) == "" {
		return false
	}
	for _, call := range message.ToolCalls {
		if !IsInteractiveTurnSubmissionTool(call.Function.Name) {
			return false
		}
	}
	return true
}

// IsInteractiveTurnSubmissionTool reports whether the tool finalizes the
// current interactive turn. Submission tool calls always come after the
// narrative prose, so they anchor the narrative position in display events.
func IsInteractiveTurnSubmissionTool(name string) bool {
	switch strings.TrimSpace(name) {
	case interactiveTurnSubmissionToolName, legacyActorStatePatchesToolName, legacyInteractiveChoicesToolName:
		return true
	default:
		return false
	}
}
