// Package goal provides the standard revisioned Goal capability.
package goal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

const MaxObjectiveBytes = 64 * 1024

var (
	ErrNotFound         = errors.New("Goal does not exist")
	ErrRevisionConflict = errors.New("Goal revision conflict")
	ErrNotActive        = errors.New("Goal is not active")
)

type Clock func() time.Time

type Option func(*standardManager)

func WithClock(clock Clock) Option {
	return func(manager *standardManager) {
		if clock != nil {
			manager.clock = clock
		}
	}
}

// Standard returns the built-in objective state machine and completion
// evaluator.
func Standard(options ...Option) GoalManager {
	manager := &standardManager{clock: func() time.Time { return time.Now().UTC() }}
	for _, option := range options {
		if option != nil {
			option(manager)
		}
	}
	return manager
}

type standardManager struct{ clock Clock }

func (*standardManager) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "goal.standard", Version: 3}
}

func (manager *standardManager) Apply(_ context.Context, request GoalApplyRequest) (GoalState, error) {
	current := request.Current
	mutation := request.Mutation
	if len(mutation.Data) != 0 {
		return GoalState{}, errors.New("standard Goal does not accept custom mutation data")
	}
	if mutation.MutationID != "" && current.LastMutationID == mutation.MutationID {
		return current, nil
	}
	now := manager.clock().UTC()
	var next GoalState
	var err error
	switch mutation.Kind {
	case agentschema.GoalSet:
		next, err = set(current, request.Present, mutation, now)
	case agentschema.GoalPause:
		next, err = transition(current, mutation, now, GoalActive, GoalPaused)
	case agentschema.GoalResume:
		if current.Status != GoalPaused && current.Status != GoalBlocked {
			err = fmt.Errorf("Goal cannot resume from status %q", current.Status)
			break
		}
		next, err = transition(current, mutation, now, current.Status, GoalActive)
	case agentschema.GoalComplete:
		next, err = finish(current, mutation, now, GoalCompleted)
	case agentschema.GoalBlock:
		next, err = finish(current, mutation, now, GoalBlocked)
	case agentschema.GoalClear:
		next, err = transition(current, mutation, now, current.Status, GoalCleared)
		if err == nil {
			next.Objective, next.Report = "", ""
		}
	default:
		err = fmt.Errorf("unsupported Goal mutation %q", mutation.Kind)
	}
	if err != nil {
		return GoalState{}, err
	}
	next.LastMutationID = mutation.MutationID
	return next, nil
}

func (*standardManager) Prepare(_ context.Context, request GoalPrepareRequest) (GoalPreparation, error) {
	if !request.Present || !request.State.Active() {
		return GoalPreparation{}, nil
	}
	content := fmt.Sprintf(
		"<active_goal id=\"%s\" revision=\"%d\">\n<objective>%s</objective>\n</active_goal>\n\n"+
			"Goal execution protocol:\n"+
			"- Keep working toward the entire objective; an intermediate milestone is never completion.\n"+
			"- Verify the result when the objective defines or implies verification.\n"+
			"- If meaningful progress genuinely requires user input or an external state change, explain the exact blocker in the final response.\n"+
			"- Goal status is evaluated by the runtime after each completed turn.",
		html.EscapeString(request.State.ID), request.State.Revision, html.EscapeString(request.State.Objective),
	)
	return GoalPreparation{Context: []agentschema.ContextFragment{{
		Source: "goal.standard", Purpose: "active objective", Resource: "session-goal",
		Revision: fmt.Sprintf("%d", request.State.Revision), Stability: agentschema.ContextTurn, Placement: agentschema.ContextFinalUserPrefix,
		Content: content, HardLimit: 128 << 10,
	}}, ReservedTokens: maxGoalEvaluationOutputTokens + agentmodel.EstimateTextTokens(goalEvaluationPrompt)}, nil
}

func set(current GoalState, present bool, mutation agentschema.GoalMutation, now time.Time) (GoalState, error) {
	objective := strings.TrimSpace(mutation.Objective)
	if objective == "" || len(objective) > MaxObjectiveBytes {
		return GoalState{}, fmt.Errorf("Goal objective must contain 1..%d bytes", MaxObjectiveBytes)
	}
	if !present || !current.Visible() {
		if mutation.ExpectedRevision != 0 {
			return GoalState{}, revisionError(current.Revision, mutation.ExpectedRevision)
		}
		revision := uint64(1)
		if current.Revision != 0 {
			revision = current.Revision + 1
		}
		return GoalState{
			ID: newGoalID(), Objective: objective, Status: GoalActive, Revision: revision,
			CreatedAt: now, UpdatedAt: now, ActiveSince: &now,
		}, nil
	}
	if err := validateRevision(current, mutation); err != nil {
		return GoalState{}, err
	}
	next := stopClock(current, now)
	next.Objective, next.Status, next.Report = objective, GoalActive, ""
	next.Revision++
	next.UpdatedAt, next.ActiveSince = now, &now
	return next, nil
}

func transition(current GoalState, mutation agentschema.GoalMutation, now time.Time, from, to GoalStatus) (GoalState, error) {
	if err := validateRevision(current, mutation); err != nil {
		return GoalState{}, err
	}
	if current.Status != from {
		return GoalState{}, fmt.Errorf("Goal cannot transition from %q to %q", current.Status, to)
	}
	next := stopClock(current, now)
	next.Status, next.UpdatedAt = to, now
	next.Revision++
	if to == GoalActive {
		next.Report, next.ActiveSince = "", &now
	}
	return next, nil
}

func finish(current GoalState, mutation agentschema.GoalMutation, now time.Time, status GoalStatus) (GoalState, error) {
	if current.Status != GoalActive {
		return GoalState{}, ErrNotActive
	}
	if err := validateRevision(current, mutation); err != nil {
		return GoalState{}, err
	}
	next := stopClock(current, now)
	next.Status, next.Report, next.UpdatedAt = status, strings.TrimSpace(mutation.Report), now
	next.Revision++
	return next, nil
}

func validateRevision(current GoalState, mutation agentschema.GoalMutation) error {
	if !current.Visible() {
		return ErrNotFound
	}
	if mutation.ExpectedID != "" && mutation.ExpectedID != current.ID {
		return fmt.Errorf("%w: Goal identity changed", ErrRevisionConflict)
	}
	if mutation.ExpectedRevision == 0 || mutation.ExpectedRevision != current.Revision {
		return revisionError(current.Revision, mutation.ExpectedRevision)
	}
	return nil
}

func revisionError(have, want uint64) error {
	return fmt.Errorf("%w: have=%d want=%d", ErrRevisionConflict, have, want)
}

func stopClock(current GoalState, now time.Time) GoalState {
	next := current
	if current.Status == GoalActive && current.ActiveSince != nil {
		next.ActiveDurationMillis += max(0, now.Sub(current.ActiveSince.UTC()).Milliseconds())
	}
	next.ActiveSince = nil
	return next
}

func newGoalID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return "goal-" + hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("goal-fallback-%x", time.Now().UTC().UnixNano())
}

var _ GoalManager = (*standardManager)(nil)
