package lore

import (
	"context"
	"denova/internal/revisionfile"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

func (s *Store) Ensure() error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()

	sourcePath, legacy := s.readableItemsPath()
	collection, err := s.loadOrCreate()
	if err != nil {
		return err
	}
	if data, err := os.ReadFile(s.itemsPath()); err == nil {
		var header struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(data, &header); err != nil {
			return err
		}
		if header.Version == loreItemsVersion {
			return nil
		}
		return s.save(collection)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := s.save(collection); err != nil {
		return err
	}
	if legacy {
		slog.InfoContext(context.Background(), fmt.Sprintf("[lore-store] migrated legacy Lore collection source=%s target=%s", sourcePath, s.itemsPath()))
	}
	return nil
}

func (s *Store) loadOrCreate() (Collection, error) {
	path, _ := s.readableItemsPath()
	snapshot, err := revisionfile.Read(context.Background(), path)
	if err == nil && snapshot.Exists {
		// Reads project old categories without mutation. Ensure or the next typed
		// write persists the new format after preserving the source snapshot.
		collection, decodeErr := decodeCollectionSnapshot(snapshot)
		if decodeErr != nil {
			return Collection{}, fmt.Errorf("解析 Lore items 失败 path=%s: %w", path, decodeErr)
		}
		return collection, nil
	}
	if err != nil {
		return Collection{}, err
	}
	return Collection{Version: loreItemsVersion, Categories: DefaultCategories(), Items: []Item{}}, nil
}

func (s *Store) save(collection Collection) error {
	collection.Version = loreItemsVersion
	if err := validateCategories(collection); err != nil {
		return err
	}
	normalized := make([]Item, 0, len(collection.Items))
	for _, item := range collection.Items {
		item.ResolvedMaterials = nil
		normalized = append(normalized, normalizeLoreItem(item))
	}
	collection.Items = normalized
	if err := validateLoreItemIdentities(collection.Items); err != nil {
		return fmt.Errorf("拒绝保存无效 Lore collection: %w", err)
	}
	if err := validateMaterials(collection); err != nil {
		return err
	}
	if err := validateIndexGuide(collection); err != nil {
		return err
	}
	pruneIndexOrder(&collection)
	path := s.itemsPath()
	if err := s.backupLegacyCategories(path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(collection, "", "  ")
	if err != nil {
		return err
	}
	_, err = revisionfile.ReplaceIfRevision(context.Background(), path, "", append(data, '\n'), revisionfile.Options{})
	return err
}

// Preserve each legacy snapshot before its first replacement, including a
// restored historical version. Backups are recovery copies, never dual writes.
func (s *Store) backupLegacyCategories(path string) error {
	snapshot, err := revisionfile.Read(context.Background(), path)
	if err != nil || !snapshot.Exists {
		return err
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(snapshot.Content, &header); err != nil {
		return err
	}
	if header.Version >= loreItemsVersion {
		return nil
	}
	return s.backupCategorySnapshot("migration", snapshot)
}

func (s *Store) backupCategorySnapshot(reason string, snapshot revisionfile.Snapshot) error {
	if !snapshot.Exists {
		return nil
	}
	backup := filepath.Join(s.workspace, "setting", "lore", "backups", "categories-"+reason+"-"+snapshot.Revision[7:]+".json")
	_, err := revisionfile.ReplaceIfRevision(context.Background(), backup, revisionfile.MissingRevision, snapshot.Content, revisionfile.Options{})
	if errors.Is(err, revisionfile.ErrRevisionConflict) {
		return nil
	}
	if err == nil {
		slog.Info("[lore] saved category backup", "path", backup, "reason", reason)
	}
	return err
}

func (s *Store) itemsPath() string {
	return ItemsPath(s.workspace)
}

func (s *Store) hasItem(items []Item, id string) bool {
	return loreItemIndex(items, id) >= 0
}

// WithMutationLock coordinates an application-level resource transaction with
// normal Lore mutations. The callback must not call this Store's mutators.
func (s *Store) WithMutationLock(operation func() error) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	return operation()
}
