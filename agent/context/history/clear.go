package history

import (
	"encoding/json"
	"errors"
	"time"
)

const ClearCapability = "agent.clear"

type ClearState struct {
	Revision                  uint64    `json:"revision"`
	CompactionRevisionAtClear uint64    `json:"compaction_revision_at_clear,omitempty"`
	ClearedAt                 time.Time `json:"cleared_at"`
}

func DecodeClearState(raw json.RawMessage) (ClearState, error) {
	var state ClearState
	if err := json.Unmarshal(raw, &state); err != nil {
		return ClearState{}, err
	}
	if state.Revision == 0 || state.ClearedAt.IsZero() {
		return ClearState{}, errors.New("durable Clear state is invalid")
	}
	return state, nil
}

func ClearStateFrom(states map[string]json.RawMessage) (ClearState, bool, error) {
	raw, present := states[ClearCapability]
	if !present {
		return ClearState{}, false, nil
	}
	state, err := DecodeClearState(raw)
	return state, true, err
}

func ClearCompaction(current CompactionRecord, present bool, clearState ClearState, clearPresent bool) (CompactionRecord, bool) {
	if clearPresent && present && current.Revision <= clearState.CompactionRevisionAtClear {
		return CompactionRecord{}, false
	}
	return current, present
}
