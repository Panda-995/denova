package schema

// TaskCompletionID is stable across process recovery and parent adoption. The
// field order and omission rules preserve receipts from released journals.
func TaskCompletionID(author, session, run string) string {
	id, _ := HashCanonical(struct {
		Agent   string `json:"agent,omitempty"`
		Session string `json:"session,omitempty"`
		Run     string `json:"run,omitempty"`
	}{author, session, run})
	return id
}
