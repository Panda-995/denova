package interactive

import (
	"context"

	story "denova/internal/interactive"
)

// InteractiveStoryToolContext is the application-facing input used to build
// the Game Agent tool surface. Runtime-only collaborators stay here in
// the agents package; concrete tools receive a narrower projection so the
// tools package does not depend on agent orchestration internals.
type InteractiveStoryToolContext struct {
	Store                  *story.Store
	StoryID                string
	BranchID               string
	SubmitStateSchemaBatch func(
		context.Context,
		story.ActorStateSchemaBatch,
	) (story.ActorStateSchemaBatchResult, error)
	PrepareTurn            func(context.Context, story.TurnCheckRequest) (story.RuleResolution, error)
	SelectProtagonist      func(context.Context, string) (story.StoryProtagonist, error)
	SubmitTurnResult       func(context.Context, story.TurnSubmissionInput) (story.TurnSubmissionReceipt, error)
	TurnResultReady        func() bool
	LoadNarrativeCandidate func(context.Context) (NarrativeCandidate, error)
}

// NarrativeCandidate distinguishes accepted message history from released
// drafts that retained prose only as product state. The latter needs a
// request-local read projection until its turn completes.
type NarrativeCandidate struct {
	Content         string
	HistoryAccepted bool
}
