package goal

import (
	"encoding/json"
	"errors"
	"fmt"
)

func DecodeGoalState(encoded json.RawMessage) (GoalState, error) {
	var state GoalState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return GoalState{}, fmt.Errorf("decode Goal state: %w", err)
	}
	if state.Revision == 0 || state.Status == "" {
		return GoalState{}, errors.New("Agent Goal state is invalid")
	}
	return state, nil
}
