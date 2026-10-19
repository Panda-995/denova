package schema

// TaskCompletion is one child-task result waiting for delivery to the parent
// model loop. The child Session terminal record remains the authoritative
// result; this value is only an in-process notification and bounded projection.
type TaskCompletion struct {
	ID      string
	Message *Message
}

// TaskCompletionWatch is a level-triggered snapshot plus an activity edge.
// Callers must inspect PendingIDs before waiting on Activity so a completion
// arriving immediately before subscription cannot be missed.
type TaskCompletionWatch struct {
	PendingIDs []string
	Activity   <-chan struct{}
}
