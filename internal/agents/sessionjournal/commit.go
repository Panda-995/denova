package sessionjournal

import (
	"encoding/json"
	"fmt"

	agentsession "github.com/alfredxw/denova/agent/session"
	agentcanonical "github.com/alfredxw/denova/agent/session/canonical"
)

// CheckpointRecords prepares the embedded Agent half of a product transaction.
// The caller holds its domain mutation lease and appends the returned records
// with the product change in one canonical journal transaction.
func CheckpointRecords(projection *Projection, checkpoint agentcanonical.CanonicalCheckpoint, revision string) ([]any, error) {
	if checkpoint == nil {
		return nil, nil
	}
	value, err := checkpoint(agentcanonical.CommitReceipt{Revision: revision})
	if err != nil {
		return nil, err
	}
	current, err := projection.Revision(value.Session)
	if err != nil {
		return nil, err
	}
	if current != value.ExpectedRevision {
		return nil, &agentsession.RevisionConflictError{Expected: value.ExpectedRevision, Actual: current}
	}
	if len(value.Records) == 0 {
		return nil, fmt.Errorf("canonical Agent checkpoint is empty")
	}
	records := make([]any, len(value.Records))
	for index, record := range value.Records {
		if err := agentsession.ValidateRecord(record); err != nil {
			return nil, err
		}
		if err := validateEmbeddedRecord(record); err != nil {
			return nil, err
		}
		records[index] = Envelope{Type: RecordType, Key: value.Session, Revision: current + agentsession.Revision(index) + 1,
			Kind: record.Kind, Version: record.Version, Data: append(json.RawMessage(nil), record.Data...)}
	}
	return records, nil
}
