package app

import (
	"context"
	"io"
	"log/slog"

	agentrun "denova/internal/agents/run"
	apptask "denova/internal/app/task"
	"denova/internal/i18n"
	"denova/internal/observability"
	"denova/internal/update"
)

func (a *App) CheckUpdate(ctx context.Context) (update.CheckResult, error) {
	return a.updateService().Check(ctx)
}

func (a *App) InstallUpdate(ctx context.Context) (update.InstallResult, error) {
	return a.installUpdate(ctx, nil)
}

// InstallLocalUpdate validates and stages an uploaded archive for explicit apply.
func (a *App) InstallLocalUpdate(ctx context.Context, name string, archive io.Reader) (update.InstallResult, error) {
	return a.updateService().InstallLocal(ctx, name, archive)
}

func (a *App) updateService() *update.Service {
	if a == nil {
		return update.NewService()
	}
	a.ensureServices()
	return a.updates
}

func (a *App) installUpdate(ctx context.Context, progress func(update.InstallProgress)) (update.InstallResult, error) {
	if a == nil {
		return a.updateService().InstallWithProgress(ctx, progress)
	}
	operation, err := a.acquireRootOperation(context.WithoutCancel(ctx))
	if err != nil {
		return update.InstallResult{}, err
	}
	defer operation.Release()
	return a.updateService().InstallWithProgress(operation.Context(), progress)
}

// StartInstallUpdateTask retains the initiating request's language throughout
// the detached download; error details share the task diagnostic envelope.
func (a *App) StartInstallUpdateTask(locale string) *apptask.Task {
	return apptask.New(func(ctx context.Context, task *apptask.Task, emit func(agentrun.Event)) {
		result, err := a.installUpdate(ctx, func(progress update.InstallProgress) {
			emit(agentrun.Event{Type: "update_progress", Data: progress})
		})
		if err != nil {
			slog.ErrorContext(ctx, "[app/update_app_service.go] update installation failed", "error", err)
			emit(agentrun.Event{Type: "error", Data: map[string]any{"message": i18n.New(locale).T("api.update.installFailed"), "code": "api.update.installFailed", "details": map[string]any{"operation": "update.install", "detail": observability.ErrorCause(err)}}})
			return
		}
		emit(agentrun.Event{Type: "update_result", Data: result})
	})
}
