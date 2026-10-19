package lifecycle

import (
	"encoding/json"
	"fmt"

	agentsession "github.com/alfredxw/denova/agent/session"
)

// This version only invalidates a disposable file-store checkpoint, never the
// canonical journal. Change it whenever the recovery reduction changes.
const recoveryCheckpointVersion = 2

type recoveryCheckpoint struct {
	Version int `json:"version"`
	RecoveryIndex
}

func (*RecoveryIndex) New() agentsession.CheckpointProjection { return &RecoveryIndex{} }

func (index *RecoveryIndex) Reset() error { *index = RecoveryIndex{}; return nil }
func (index *RecoveryIndex) Restore(data json.RawMessage) error {
	var checkpoint recoveryCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return err
	}
	if checkpoint.Version != recoveryCheckpointVersion {
		return fmt.Errorf("unsupported Agent recovery checkpoint version %d", checkpoint.Version)
	}
	*index = checkpoint.RecoveryIndex
	return nil
}
func (index *RecoveryIndex) Checkpoint() (json.RawMessage, error) {
	return json.Marshal(recoveryCheckpoint{Version: recoveryCheckpointVersion, RecoveryIndex: *index})
}
func (index *RecoveryIndex) CurrentCheckpoint() (json.RawMessage, error) {
	return index.Current().Checkpoint()
}

func (index *RecoveryIndex) CurrentReadRevisions() []agentsession.Revision {
	result := make([]agentsession.Revision, 0, len(index.Recent))
	for _, run := range index.Recent {
		result = append(result, run.Settlement)
	}
	return result
}
