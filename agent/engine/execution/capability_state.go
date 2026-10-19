package execution

import (
	"errors"
)

const TodoCapability = "agent.todo"

// ErrCapabilityStateConflict reports that a cycle-local capability decision
// was derived from a Session value that changed before the decision committed.
// Callers must discard the stale decision instead of overwriting newer state.
var ErrCapabilityStateConflict = errors.New("Agent Session capability state changed")
