package agentchat

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	chatagent "denova/internal/agents/chat"
	agentcompaction "denova/internal/agents/context/compaction"
	agentstructural "denova/internal/agents/context/structural"
	agentrun "denova/internal/agents/run"
	compactionapp "denova/internal/app/compaction"
	conversationapp "denova/internal/app/conversation"
	apptask "denova/internal/app/task"
)

// CompactContext admits maintenance for exactly the selected Project/Session.
// It shares executor preparation with turns, without creating a user input.
func (service *Service) CompactContext(ctx context.Context, binding Binding, commandID string) (agentcompaction.Result, error) {
	active, err := service.admitCompaction(ctx, binding, commandID)
	if err != nil {
		return agentcompaction.Result{}, err
	}
	// Keep the synchronous API lifetime, while allowing shutdown to cancel the
	// registered Task without waiting for the admission lock or provider.
	ctx, release := apptask.AcceptanceContext(ctx, active.task)
	defer release()
	var result agentcompaction.Result
	var compactErr error
	if err := active.task.Start(func(_ context.Context, _ *apptask.Task, emit func(agentrun.Event)) {
		defer service.releaseActiveRun(active)
		result, compactErr = service.compactConversation(ctx, active)
		switch {
		case compactErr == nil:
			emit(agentrun.Event{Type: "done"})
		case errors.Is(compactErr, context.Canceled):
			emit(agentrun.Event{Type: "aborted"})
		default:
			emit(agentrun.Event{Type: "error", Data: map[string]any{
				"message": compactErr.Error(), "error_key": "agentRuntime.operationFailed",
			}})
		}
	}); err != nil {
		active.task.RejectStart(err)
		service.releaseActiveRun(active)
		return agentcompaction.Result{}, err
	}
	<-active.task.Done()
	if compactErr == nil && active.task.Status() == apptask.Failed {
		compactErr = fmt.Errorf("AgentChat context compaction panicked")
	}
	level := slog.LevelInfo
	if compactErr != nil && !errors.Is(compactErr, context.Canceled) {
		level = slog.LevelError
	}
	slog.Log(ctx, level, "AgentChat context compaction finished",
		"project_id", active.binding.ProjectID, "session_id", active.binding.SessionID,
		"command_id", active.commandID, "task_id", active.task.ID(), "triggered", result.Triggered, "error", compactErr)
	return result, compactErr
}

// Admission reserves only the selected conversation before slow preparation
// or model work. The existing active registry also fences clear/delete/config.
func (service *Service) admitCompaction(ctx context.Context, binding Binding, commandID string) (*run, error) {
	service.admission.Lock()
	defer service.admission.Unlock()
	binding, err := service.ResolveBinding(binding)
	if err != nil {
		return nil, err
	}
	if err := service.requireIdle(binding); err != nil {
		return nil, err
	}
	project, err := service.projectRuntime(ctx, binding.ProjectID)
	if err != nil {
		return nil, err
	}
	sess, err := project.store.Get(binding.SessionID)
	if err != nil {
		return nil, err
	}
	commandID, err = compactionapp.ResolveCommandID(commandID, agentstructural.CommandID("agent-chat-compact", binding.ProjectID, binding.SessionID, fmt.Sprint(sess.ContextCursor().Revision)))
	if err != nil {
		return nil, err
	}
	active := &run{kind: compactionRun, binding: binding, commandID: commandID, runtime: project.conversation(sess)}
	_, err = apptask.NewDeferredWithContext(ctx, func(task *apptask.Task) error {
		active.task = task
		return service.installActiveRun(active)
	})
	return active, err
}

func (service *Service) compactConversation(ctx context.Context, active *run) (agentcompaction.Result, error) {
	runtime, _, err := conversationapp.Prepare(ctx, active.runtime, chatagent.ChatRequest{})
	if err != nil {
		return agentcompaction.Result{}, err
	}
	host, err := service.host.ProjectAgentHostCapabilities(ctx, runtime.ProjectType, &runtime.Config, runtime.AgentKind)
	if err != nil {
		return agentcompaction.Result{}, err
	}
	execution, err := conversationapp.BuildExecution(ctx, runtime, host, service.host.AgentEngines(), "")
	if err != nil {
		return agentcompaction.Result{}, err
	}
	return execution.Compact(ctx, active.commandID, runtimeOptions(active.binding, active.task.ID()))
}
