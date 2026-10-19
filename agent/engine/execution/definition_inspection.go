package execution

import (
	"context"
)

type inspectionContextKey struct{}

// IsInspection reports whether Agent is preparing a read-only Session preview.
// Source, Toolset, Context, Goal, and Middleware implementations may use it to
// suppress optional telemetry, but must return the same model-visible result as
// normal preparation for the supplied snapshot.
func IsInspection(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	inspecting, _ := ctx.Value(inspectionContextKey{}).(bool)
	return inspecting
}

func ContextWithInspection(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, inspectionContextKey{}, true)
}
