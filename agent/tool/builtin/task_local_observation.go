package builtin

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/alfredxw/denova/agent"
	agentevent "github.com/alfredxw/denova/agent/lifecycle/event"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func (tasks *LocalTasks) taskFromSessionSnapshot(
	ctx context.Context,
	session *agent.Session,
	ref TaskRef,
	snapshot agentevent.SessionSnapshot,
) (Task, error) {
	task, err := taskFromSnapshot(ref, snapshot)
	if errors.Is(err, ErrTaskNotFound) {
		exact, found, lookupErr := session.RunSnapshot(ctx, ref.Run)
		if lookupErr != nil {
			return Task{}, lookupErr
		}
		if found && exact.Result != nil {
			return Task{Ref: ref, Status: string(exact.Result.Status), Reason: exact.Result.Reason, Output: exact.Output, Receipt: &exact.Receipt}, nil
		}
	}
	if err != nil || task.Output != "" || !isTaskTerminal(task.Status) {
		return task, err
	}
	output, _, replayErr := replayTaskFinal(ctx, session, ref.Run, snapshot.RetentionStart)
	task.Output = output
	return task, replayErr
}

func taskFromSnapshot(ref TaskRef, snapshot agentevent.SessionSnapshot) (Task, error) {
	status := taskStatus(snapshot, ref.Run)
	switch status {
	case "unknown":
		return Task{}, ErrTaskNotFound
	case "running", "waiting_input", "aborting", "queued", string(agentschema.ResultSuspended),
		string(agentschema.ResultCompleted), string(agentschema.ResultFailed), string(agentschema.ResultIncomplete),
		string(agentschema.ResultBlocked), string(agentschema.ResultAborted):
		receipt := agentevent.CommandReceipt{RunID: ref.Run}
		if snapshot.ActiveRunID == ref.Run {
			receipt.CommandID, receipt.Cursor = snapshot.ActiveCommandID, snapshot.ActiveReceiptCursor
		}
		for _, queued := range snapshot.QueuedRuns {
			if queued.ID == ref.Run {
				receipt.CommandID, receipt.Cursor = queued.CommandID, queued.ReceiptCursor
			}
		}
		for _, recent := range snapshot.RecentRuns {
			if recent.ID == ref.Run {
				receipt.CommandID, receipt.Cursor = recent.CommandID, recent.ReceiptCursor
			}
		}
		return Task{
			Ref: ref, Status: status, Reason: taskSnapshotReason(snapshot, ref.Run),
			Output: taskSnapshotOutput(snapshot, ref.Run), Receipt: &receipt,
		}, nil
	default:
		return Task{}, fmt.Errorf("unsupported task status %q", status)
	}
}

func taskSnapshotReason(snapshot agentevent.SessionSnapshot, runID string) string {
	for _, recent := range snapshot.RecentRuns {
		if recent.ID == runID {
			return recent.Reason
		}
	}
	return ""
}

func isTaskTerminal(status string) bool {
	switch status {
	case string(agentschema.ResultCompleted), string(agentschema.ResultFailed), string(agentschema.ResultIncomplete),
		string(agentschema.ResultBlocked), string(agentschema.ResultAborted):
		return true
	case "running", "waiting_input", "aborting", "queued", string(agentschema.ResultSuspended):
		return false
	default:
		return false
	}
}

func collectTaskEvents(ctx context.Context, observation agentevent.Observation, runID string, after agentevent.Cursor) ([]TaskEvent, string, string, bool, error) {
	var events []TaskEvent
	var output string
	target := observation.Snapshot.Cursor
	cursor := after
	if observation.Snapshot.RetentionStart > target {
		return nil, strconv.FormatUint(uint64(target), 10), "", observation.Snapshot.MessagesTruncated, nil
	}
	if cursor >= target {
		return nil, strconv.FormatUint(uint64(target), 10), "", observation.Snapshot.MessagesTruncated, nil
	}
	for {
		select {
		case event, ok := <-observation.Events:
			if !ok {
				return events, strconv.FormatUint(uint64(cursor), 10), output, true, nil
			}
			cursor = event.Cursor
			if event.RunID != runID {
				if cursor >= target {
					return events, strconv.FormatUint(uint64(target), 10), output, observation.Snapshot.MessagesTruncated, nil
				}
				continue
			}
			projected := TaskEvent{
				Cursor: strconv.FormatUint(uint64(event.Cursor), 10), Type: taskEventType(event.Payload),
				Run: event.RunID, Event: event,
			}
			switch payload := event.Payload.(type) {
			case agentevent.AssistantDelta:
				projected.Type, projected.Text = "assistant_delta", payload.Delta
			case agentevent.ThinkingDelta:
				projected.Type, projected.Text = "thinking_delta", payload.Delta
			case agentevent.AssistantFinal:
				projected.Type, projected.Text, output = "assistant_final", payload.Content, payload.Content
			case agentevent.ToolStarted:
				projected.Type, projected.Tool = "tool_started", payload.Name
			case agentevent.ToolProgress:
				projected.Type, projected.Tool, projected.Text = "tool_progress", payload.Name, payload.Delta
			case agentevent.ToolFinished:
				projected.Type, projected.Tool, projected.Text = "tool_finished", payload.Name, payload.Result
			case agentevent.InteractionRequested:
				projected.Type = "interaction_requested"
			case agentevent.RunSettled:
				projected.Type, projected.Text = "run_settled", string(payload.Status)
			}
			events = append(events, projected)
			if cursor >= target {
				return events, strconv.FormatUint(uint64(target), 10), output, observation.Snapshot.MessagesTruncated, nil
			}
		case err, ok := <-observation.Errors:
			if ok && err != nil {
				return events, strconv.FormatUint(uint64(cursor), 10), output, true, err
			}
			observation.Errors = nil
		case <-ctx.Done():
			return events, strconv.FormatUint(uint64(cursor), 10), output, true, ctx.Err()
		}
	}
}

func taskEventType(payload agentevent.EventPayload) string {
	switch payload.(type) {
	case agentevent.RunAccepted:
		return "run_accepted"
	case agentevent.RunStarted:
		return "run_started"
	case agentevent.AssistantDelta:
		return "assistant_delta"
	case agentevent.ThinkingDelta:
		return "thinking_delta"
	case agentevent.ModelCompleted:
		return "model_completed"
	case agentevent.ModelRetry:
		return "model_retry"
	case agentevent.ContextNormalized:
		return "context_normalized"
	case agentevent.AssistantFinal:
		return "assistant_final"
	case agentevent.ToolInputStarted:
		return "tool_input_started"
	case agentevent.ToolInputDelta:
		return "tool_input_delta"
	case agentevent.ToolStarted:
		return "tool_started"
	case agentevent.ToolProgress:
		return "tool_progress"
	case agentevent.ToolFinished:
		return "tool_finished"
	case agentevent.ArtifactProduced:
		return "artifact_produced"
	case agentevent.EventStreamGap:
		return "event_stream_gap"
	case agentevent.GoalUpdated:
		return "goal_updated"
	case agentevent.GoalEvaluationFailed:
		return "goal_evaluation_failed"
	case agentevent.TodoUpdated:
		return "todo_updated"
	case agentevent.InteractionRequested:
		return "interaction_requested"
	case agentevent.InteractionResolved:
		return "interaction_resolved"
	case agentevent.CompactionStarted:
		return "compaction_started"
	case agentevent.CompactionCommitted:
		return "compaction_committed"
	case agentevent.CompactionRemoved:
		return "compaction_removed"
	case agentevent.CompactionFailed:
		return "compaction_failed"
	case agentevent.CompactionSkipped:
		return "compaction_skipped"
	case agentevent.CleanupStarted:
		return "cleanup_started"
	case agentevent.CleanupCompleted:
		return "cleanup_completed"
	case agentevent.CleanupFailed:
		return "cleanup_failed"
	case agentevent.CleanupSkipped:
		return "cleanup_skipped"
	case agentevent.CleanupCommitted:
		return "cleanup_committed"
	case agentevent.SessionCleared:
		return "session_cleared"
	case agentevent.ContextLimitReached:
		return "context_limit_reached"
	case agentevent.RunSettled:
		return "run_settled"
	case agentevent.NestedEvent:
		return "nested"
	default:
		return "unknown"
	}
}

func taskStatus(snapshot agentevent.SessionSnapshot, runID string) string {
	if snapshot.ActiveRunID == runID {
		if snapshot.ActiveStatus == agentschema.ResultSuspended {
			return string(agentschema.ResultSuspended)
		}
		if snapshot.ActiveAbortPending {
			return "aborting"
		}
		if len(snapshot.PendingInteractions) != 0 {
			return "waiting_input"
		}
		return "running"
	}
	for _, queued := range snapshot.QueuedRuns {
		if queued.ID == runID && queued.Delivery == agentevent.DeliveryNextTurn {
			return "queued"
		}
	}
	for _, recent := range snapshot.RecentRuns {
		if recent.ID == runID {
			return string(recent.Status)
		}
	}
	return "unknown"
}

func taskSnapshotOutput(snapshot agentevent.SessionSnapshot, runID string) string {
	if snapshot.ActiveRunID == runID {
		return snapshot.ActiveOutput.Content
	}
	for _, recent := range snapshot.RecentRuns {
		if recent.ID == runID {
			return recent.Output
		}
	}
	return ""
}

func parseTaskCursor(value string) (agentevent.Cursor, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid task cursor: %w", err)
	}
	return agentevent.Cursor(parsed), nil
}

func replayTaskFinal(
	ctx context.Context,
	session *agent.Session,
	runID string,
	retentionStart agentevent.Cursor,
) (string, bool, error) {
	after := agentevent.Cursor(0)
	if retentionStart > 0 {
		after = retentionStart - 1
	}
	observation, err := session.Observe(ctx, after)
	if err != nil {
		return "", true, err
	}
	_, _, output, incomplete, err := collectTaskEvents(ctx, observation, runID, after)
	return output, incomplete || output == "", err
}
