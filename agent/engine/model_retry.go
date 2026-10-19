package engine

// modelResponseRejected ends a preview without admitting it to the transcript.
// The following retry event explains the next attempt to display consumers.
type modelResponseRejected struct{ reason string }

func (err *modelResponseRejected) Error() string {
	return "model response was not accepted: " + err.reason
}
