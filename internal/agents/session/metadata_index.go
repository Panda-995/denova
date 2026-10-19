package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"denova/internal/agents/conversationconfig"
	"denova/internal/revisionfile"
)

const metadataIndexName = ".metadata.idx.json"
const metadataIndexVersion = 2

// Persist only summary data. Identity comes from the map key; Active belongs
// to the current list request, not the cache.
type metadataIndexEntry struct {
	Stamp         sessionFileStamp           `json:"stamp"`
	Title         string                     `json:"title"`
	CreatedAt     time.Time                  `json:"created_at"`
	UpdatedAt     time.Time                  `json:"updated_at"`
	MessageCount  int                        `json:"message_count"`
	RuntimeConfig *conversationconfig.Config `json:"runtime_config,omitempty"`
}

type metadataIndex struct {
	Version int                           `json:"version"`
	Entries map[string]metadataIndexEntry `json:"entries"`
}

// This is the existing metadata cache checkpointed once per Session directory.
// Every entry is checked against its canonical file stamp by metadataLocked.
// Missing/corrupt indexes, new files and changed files replay canonical metadata;
// execution and history never consult this index. IDs are relative to the Store.
func (s *Store) restoreMetadataIndex() {
	path := filepath.Join(s.dir, metadataIndexName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	var index metadataIndex
	if err == nil {
		err = json.Unmarshal(data, &index)
	}
	if err == nil && index.Version != metadataIndexVersion {
		err = fmt.Errorf("metadata index version mismatch")
	}
	if err != nil {
		slog.Warn("session_metadata_index_rebuild", "path", path, "error", err)
		s.metadataDirty = true
		return
	}
	for id, entry := range index.Entries {
		if validateSessionID(id) != nil {
			slog.Warn("session_metadata_index_rebuild", "path", path, "error", "invalid session identity")
			s.metadata = make(map[string]cachedSessionMetadata)
			s.metadataDirty = true
			return
		}
		s.metadata[id] = cachedSessionMetadata{stamp: entry.Stamp, meta: SessionMeta{
			ID: id, Title: entry.Title, CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt,
			MessageCount: entry.MessageCount, RuntimeConfig: entry.RuntimeConfig,
		}}
	}
}

func (s *Store) checkpointMetadataLocked() {
	if !s.metadataDirty {
		return
	}
	entries := make(map[string]metadataIndexEntry, len(s.metadata))
	for id, cached := range s.metadata {
		entries[id] = metadataIndexEntry{
			Stamp: cached.stamp, Title: cached.meta.Title, CreatedAt: cached.meta.CreatedAt, UpdatedAt: cached.meta.UpdatedAt,
			MessageCount: cached.meta.MessageCount, RuntimeConfig: cached.meta.RuntimeConfig,
		}
	}
	encoded, err := json.Marshal(metadataIndex{Version: metadataIndexVersion, Entries: entries})
	path := filepath.Join(s.dir, metadataIndexName)
	if err == nil {
		_, err = revisionfile.ReplaceIfRevision(context.Background(), path, "", encoded, revisionfile.Options{FileMode: 0o600})
	}
	if err != nil {
		slog.Warn("session_metadata_index_checkpoint_failed", "path", path, "error", err)
		return
	}
	s.metadataDirty = false
}
