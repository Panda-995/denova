package lifecycle

import (
	"context"
	"fmt"
	"strings"

	agentchat "denova/internal/agents/chat"
	"denova/internal/agents/modelio"
	agentrun "denova/internal/agents/run"

	publicgoal "github.com/alfredxw/denova/agent/engine/goal"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// NewGoalManager returns Denova's adapter for the public revisioned Goal
// capability. Native owns evaluation and state transitions; this adapter only
// prepares Denova continuation HostData. External runtimes do not call it.
func NewGoalManager() publicgoal.GoalManager {
	return denovaGoalManager{delegate: publicgoal.Standard()}
}

type denovaGoalManager struct{ delegate publicgoal.GoalManager }

func (manager denovaGoalManager) Identity() agentschema.CapabilityIdentity {
	return agentschema.CapabilityIdentity{Kind: "denova.goal.standard", Version: 3}
}

func (manager denovaGoalManager) Apply(ctx context.Context, request publicgoal.GoalApplyRequest) (publicgoal.GoalState, error) {
	return manager.delegate.Apply(ctx, request)
}

func (manager denovaGoalManager) Prepare(ctx context.Context, request publicgoal.GoalPrepareRequest) (publicgoal.GoalPreparation, error) {
	return manager.delegate.Prepare(ctx, request)
}

func (manager denovaGoalManager) AfterRun(ctx context.Context, request publicgoal.GoalAfterRunRequest) (publicgoal.GoalAfterRunDecision, error) {
	decision, err := manager.delegate.AfterRun(modelio.WithTraceSource(ctx, "goal_evaluation"), request)
	if err != nil || decision.Verdict != publicgoal.GoalVerdictContinue {
		return decision, err
	}
	data, err := DecodeTurnHostData(request.Input)
	if err != nil {
		return decision, fmt.Errorf("prepare Denova Goal continuation: %w", err)
	}
	message := strings.TrimSpace(decision.Input.Text)
	if message == "" {
		return decision, fmt.Errorf("prepare Denova Goal continuation: message is empty")
	}
	// Preserve only locale and durable routing. References, selections, review
	// feedback, and explicit Skills belonged to the completed caller turn and
	// must not be replayed as a new autonomous instruction.
	next, err := TurnInput(TurnNext, agentchat.ChatRequest{
		Message: message, Locale: data.Caller.Locale,
		InputVisibility: agentrun.InputModelOnly,
	}, agentrun.Options{
		AutomationTaskID: data.AutomationID,
		TurnID:           data.TurnID,
		MaintenanceTask:  data.MaintenanceTask,
		Mode:             data.Mode,
		WriteMode:        data.WriteMode,
		WriteScope:       data.WriteScope,
		RestoreData:      data.RestoreData,
	})
	if err != nil {
		return decision, fmt.Errorf("prepare Denova Goal continuation input: %w", err)
	}
	// Agent assigns the durable command identity for automatic continuations.
	next.IdempotencyKey = ""
	decision.Input = next
	return decision, nil
}

var _ publicgoal.GoalManager = denovaGoalManager{}
