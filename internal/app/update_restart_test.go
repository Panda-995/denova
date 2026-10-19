package app

import (
	"context"
	"testing"

	"denova/config"
	apptask "denova/internal/app/task"
)

// Background scheduler leases must not prevent an idle restart. Project and
// workspace operations must: images and version writes do not always own Tasks.
func TestUpdateRestartWaitsForWorkAndFencesAdmission(t *testing.T) {
	ctx := context.Background()
	a := &App{cfg: &config.Config{NovaDir: t.TempDir()}, workspace: t.TempDir()}
	a.ensureServices()
	background, err := a.acquireRootOperation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer background.Release()
	image, err := a.acquireWorkspaceOperation(ctx, a.workspace, true)
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	ready, err := a.fenceIdleUpdateLocked(ctx)
	a.mu.Unlock()
	if err != nil || ready {
		t.Fatalf("restarted during image operation: %v %v", ready, err)
	}
	image.Release()
	task, err := apptask.NewDeferred(func(task *apptask.Task) error {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.registerWorkspaceTaskLocked(task, a.workspace, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	ready, err = a.fenceIdleUpdateLocked(ctx)
	a.mu.Unlock()
	if err != nil || ready {
		t.Fatalf("restarted during registered Task: %v %v", ready, err)
	}
	task.Finish()
	a.unregisterWorkspaceTask(task)
	a.mu.Lock()
	ready, err = a.fenceIdleUpdateLocked(ctx)
	a.mu.Unlock()
	if err != nil || !ready {
		t.Fatalf("idle work did not fence: %v %v", ready, err)
	}
	if operation, err := a.acquireWorkspaceOperation(ctx, a.workspace, true); err == nil {
		operation.Release()
		t.Fatal("image operation slipped past restart fence")
	}
	if _, err := apptask.NewDeferred(func(task *apptask.Task) error {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.registerWorkspaceTaskLocked(task, a.workspace, true)
	}); err == nil {
		t.Fatal("Task slipped past restart fence")
	}
	// Handoff failure reopens admission. No staged transaction exists here, so
	// the real updater refuses before starting any process.
	if _, err := a.applyUpdateNow(ctx, "missing"); err == nil {
		t.Fatal("applied without a transaction")
	}
	operation, err := a.acquireWorkspaceOperation(ctx, a.workspace, true)
	if err != nil {
		t.Fatalf("failed update stranded admission: %v", err)
	}
	operation.Release()
}
