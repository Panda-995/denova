package async

import (
	"context"
)

type admissionContextKey struct{}

// ContextWithAdmission binds the task-tree fence to model and tool work. The
// callback checks lifecycle admission without exposing its Run or Session.
func ContextWithAdmission(ctx context.Context, admit func(context.Context) error) context.Context {
	return context.WithValue(ctx, admissionContextKey{}, admit)
}

// AdmitWork checks the lifecycle fence before a provider or tool attempt.
func AdmitWork(ctx context.Context) error {
	admit, _ := ctx.Value(admissionContextKey{}).(func(context.Context) error)
	if admit == nil {
		return nil
	}
	return admit(ctx)
}
