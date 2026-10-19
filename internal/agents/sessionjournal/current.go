package sessionjournal

import (
	"context"
	"encoding/json"
	"fmt"

	"denova/internal/agents/conversationjournal"

	agentsession "github.com/alfredxw/denova/agent/session"
)

// CurrentProjection reduces only Agent lifecycle facts for read-only status.
// Its sidecar is disposable and validated against the same canonical journal.
// Historical query locators belong to the ordinary domain index.
const currentProjectionVersion = 2

type currentCheckpoint struct {
	Version int `json:"version"`
	Projection
}

type CurrentProjection struct{ Projection }

func (projection *CurrentProjection) Reset() error { projection.Projection.Reset(); return nil }
func (projection *CurrentProjection) Restore(data json.RawMessage) error {
	var checkpoint currentCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return err
	}
	if checkpoint.Version != currentProjectionVersion {
		return fmt.Errorf("unsupported current projection version %d", checkpoint.Version)
	}
	projection.Projection = checkpoint.Projection
	return projection.Normalize()
}
func (projection *CurrentProjection) Apply(record conversationjournal.Record) error {
	_, err := projection.Projection.Apply(record)
	return err
}
func (projection *CurrentProjection) Checkpoint() (json.RawMessage, error) {
	data, err := projection.CurrentCheckpoint()
	if err != nil {
		return nil, err
	}
	// Trim tail/rebuild metadata before exposing this read-only reader.
	if err := projection.Restore(data); err != nil {
		return nil, err
	}
	return data, nil
}

// OpenCurrent reads a small checkpoint plus tail without acquiring execution.
// The Journal rejects appends; observation can neither accept nor resume work.
func OpenCurrent(ctx context.Context, path string, identity conversationjournal.Identity, key agentsession.Key) (agentsession.Log, error) {
	projection := &CurrentProjection{}
	journal, err := conversationjournal.OpenCurrent(ctx, path, identity, projection)
	if err != nil {
		return nil, err
	}
	log, err := NewLog(journal, &projection.Projection, key, true, nil)
	if err != nil {
		_ = journal.Close()
		return nil, err
	}
	return log, nil
}

func (projection *CurrentProjection) CurrentReadCursors() []conversationjournal.Cursor {
	return projection.Projection.CurrentReadCursors()
}
