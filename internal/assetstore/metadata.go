package assetstore

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"

	"denova/internal/portablepath"
	"denova/internal/revisionfile"
)

// Metadata stores only otherwise-unrecorded generation details, keyed by the
// immutable filename. It is never used to enumerate or resolve product assets.
type Metadata struct {
	Version int                        `json:"version"`
	Files   map[string]json.RawMessage `json:"files"`
}

func DecodeMetadata(data []byte) (Metadata, error) {
	metadata := Metadata{Version: 1, Files: map[string]json.RawMessage{}}
	if len(data) == 0 {
		return metadata, nil
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Metadata{}, err
	}
	if metadata.Version != 1 || metadata.Files == nil {
		return Metadata{}, fmt.Errorf("unsupported generation metadata")
	}
	seen := map[string]bool{}
	for name := range metadata.Files {
		if err := portablepath.ValidateComponent(name); err != nil {
			return Metadata{}, err
		}
		key := portablepath.FoldKey(name)
		if name == "meta.json" || seen[key] {
			return Metadata{}, fmt.Errorf("invalid generation filename: %s", name)
		}
		seen[key] = true
	}
	return metadata, nil
}

// MergeMetadata preserves provenance for retained newer files during restore.
// The target version owns existing keys; retained entries fill absent keys.
func MergeMetadata(target, retained []byte) ([]byte, error) {
	metadata, err := DecodeMetadata(target)
	if err != nil {
		return nil, err
	}
	previous, err := DecodeMetadata(retained)
	if err != nil {
		return nil, err
	}
	changed := len(target) == 0
	for name, detail := range previous.Files {
		if _, exists := metadata.Files[name]; !exists {
			metadata.Files[name] = detail
			changed = true
		}
	}
	if !changed {
		return target, nil
	}
	return json.MarshalIndent(metadata, "", "  ")
}

func recordGeneration(ctx context.Context, workspace, name string, detail json.RawMessage) error {
	if !IsManaged(name) {
		return fmt.Errorf("invalid generation path: %s", name)
	}
	_, err := revisionfile.Mutate(ctx, filepath.Join(workspace, filepath.FromSlash(MetaPath(name))), revisionfile.Options{}, func(snapshot revisionfile.Snapshot) ([]byte, error) {
		metadata, err := DecodeMetadata(snapshot.Content)
		if err != nil {
			return nil, err
		}
		if _, exists := metadata.Files[path.Base(name)]; exists {
			return snapshot.Content, nil
		}
		metadata.Files[path.Base(name)] = detail
		return json.MarshalIndent(metadata, "", "  ")
	})
	return err
}
