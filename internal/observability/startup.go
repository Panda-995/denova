package observability

import (
	"context"
	"log/slog"
	"time"
)

// StartStartupStage records a stage's start and returns its completion logger.
// The caller must complete it exactly once, passing the operation's error.
func StartStartupStage(ctx context.Context, stage string) func(error) {
	started := time.Now()
	slog.InfoContext(ctx, "startup_stage_started", "stage", stage)
	return func(err error) {
		durationMS := float64(time.Since(started)) / float64(time.Millisecond)
		if err != nil {
			slog.ErrorContext(ctx, "startup_stage_failed", "stage", stage, "duration_ms", durationMS, "error", err)
			return
		}
		slog.InfoContext(ctx, "startup_stage_completed", "stage", stage, "duration_ms", durationMS)
	}
}
