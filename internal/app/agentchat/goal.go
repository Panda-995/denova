package agentchat

import (
	"context"
	"errors"

	agentconversation "denova/internal/agents/conversation"
	agentruntime "denova/internal/agents/runtime"

	publicgoal "github.com/alfredxw/denova/agent/engine/goal"
)

func (service *Service) ConversationGoal(ctx context.Context, binding Binding) (publicgoal.GoalState, bool, error) {
	service.admission.Lock()
	defer service.admission.Unlock()
	resolved, project, runtimeCfg, err := service.conversationRuntime(ctx, binding)
	if err != nil {
		return publicgoal.GoalState{}, false, err
	}
	if !project.store.Exists(resolved.SessionID) {
		return publicgoal.GoalState{}, false, nil
	}
	selection, err := agentconversation.PreviewSession(project.store, resolved.SessionID, &runtimeCfg, resolved.agentKind)
	if err != nil {
		return publicgoal.GoalState{}, false, err
	}
	sess, err := project.store.Get(resolved.SessionID)
	if err != nil {
		return publicgoal.GoalState{}, false, err
	}
	bound, err := service.host.AgentEngines().ConversationSession(project.executionRuntime, runtimeOptions(resolved, ""), sess, selection.Engine())
	if err != nil {
		return publicgoal.GoalState{}, false, err
	}
	return bound.Goal(ctx)
}

func (service *Service) MutateConversationGoal(ctx context.Context, binding Binding, action string, objective string, expectedRevision uint64) (publicgoal.GoalState, error) {
	service.admission.Lock()
	defer service.admission.Unlock()
	resolved, project, runtimeCfg, err := service.conversationRuntime(ctx, binding)
	if err != nil {
		return publicgoal.GoalState{}, err
	}
	selection, err := agentconversation.PreviewSession(project.store, resolved.SessionID, &runtimeCfg, resolved.agentKind)
	if err != nil {
		return publicgoal.GoalState{}, err
	}
	sess, _, err := getOrCreateConversation(project, resolved)
	if err != nil {
		return publicgoal.GoalState{}, err
	}
	mutation, err := agentruntime.GoalMutation(action, objective, expectedRevision)
	if err != nil {
		return publicgoal.GoalState{}, err
	}
	bound, err := service.host.AgentEngines().ConversationSession(project.executionRuntime, runtimeOptions(resolved, ""), sess, selection.Engine())
	if err != nil {
		return publicgoal.GoalState{}, err
	}
	return bound.UpdateGoal(ctx, mutation)
}

func IsGoalRevisionConflict(err error) bool { return errors.Is(err, publicgoal.ErrRevisionConflict) }
