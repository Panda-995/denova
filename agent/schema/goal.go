package schema

import (
	"encoding/json"
)

type GoalMutationKind string

const (
	GoalSet      GoalMutationKind = "set"
	GoalPause    GoalMutationKind = "pause"
	GoalResume   GoalMutationKind = "resume"
	GoalComplete GoalMutationKind = "complete"
	GoalBlock    GoalMutationKind = "block"
	GoalClear    GoalMutationKind = "clear"
)

type GoalMutation struct {
	Kind             GoalMutationKind `json:"kind"`
	Objective        string           `json:"objective,omitempty"`
	ExpectedID       string           `json:"expected_id,omitempty"`
	ExpectedRevision uint64           `json:"expected_revision,omitempty"`
	Report           string           `json:"report,omitempty"`
	MutationID       string           `json:"mutation_id,omitempty"`
	// Data is an opaque command payload interpreted only by the selected Goal
	// Manager. Custom mutation kinds therefore do not need fields added to the
	// Agent package.
	Data json.RawMessage `json:"data,omitempty"`
}
