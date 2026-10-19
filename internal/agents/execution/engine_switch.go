package execution

import (
	"context"

	agentrun "denova/internal/agents/run"

	agentgoal "github.com/alfredxw/denova/agent/engine/goal"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agentsession "github.com/alfredxw/denova/agent/session"
)

// ReleaseIdleForEngineSwitch checks detached children and unfinished Goals,
// then evicts the idle actors so the next execution reloads the canonical journal.
// The caller excludes new product admissions until the selection is committed.
func (runtime *Runtime) ReleaseIdleForEngineSwitch(ctx context.Context, options agentrun.Options) error {
	if runtime == nil || runtime.public == nil {
		return ErrRuntimeProjectionUnavailable
	}
	root, _, err := runtime.public.openSession(ctx, options)
	if err != nil {
		return err
	}
	family, err := runtime.public.taskSessions(ctx, root)
	if err != nil {
		return err
	}
	for _, current := range family {
		snapshot, err := current.Snapshot(ctx)
		if err != nil {
			return err
		}
		if snapshot.ActiveRunID != "" || len(snapshot.QueuedRuns) > 0 || len(snapshot.OpenTools) > 0 || len(snapshot.PendingInteractions) > 0 {
			return agentschema.ErrSessionBusy
		}
		goal, found, err := current.Goal(ctx)
		if err != nil {
			return err
		}
		if found && goal.Status != agentgoal.GoalCompleted && goal.Status != agentgoal.GoalCleared {
			return agentschema.ErrSessionBusy
		}
	}
	key := root.Key()
	return runtime.public.closeSessions(ctx, agentsession.Selector{Namespace: key.Namespace, ID: key.ID})
}
