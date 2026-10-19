package project

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"denova/internal/portablepath"
	workspacelayout "denova/internal/workspace"
)

// repairIncompleteBookRegistrations corrects only failed registrations caused
// by mistaking a user data root for Book-private state. Completed Book Stores
// and directories with actual Book markers retain their explicit type.
func (registry *Registry) repairIncompleteBookRegistrations(data *registryData, original []byte) (bool, error) {
	var repaired []string
	for index := range data.Projects {
		record := &data.Projects[index]
		if record.Type != TypeBook || isBookWorkspace(record.WorkspacePath) ||
			!workspacelayout.IsUserDataRoot(workspacelayout.Dir(record.WorkspacePath)) {
			continue
		}
		layout, err := registry.Layout(*record)
		if err != nil {
			return false, err
		}
		if err := portablepath.CheckPath(layout.StoreRoot, "migration.json"); err != nil {
			return false, err
		}
		if _, err := os.Lstat(filepath.Join(layout.StoreRoot, "migration.json")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("inspect incomplete Project Store migration: %w", err)
		}
		record.Type = TypeGeneral
		record.UpdatedAt = time.Now().UTC()
		if data.CurrentBookID == record.ID {
			data.CurrentBookID = ""
		}
		repaired = append(repaired, record.ID)
	}
	if len(repaired) == 0 {
		return false, nil
	}
	digest := sha256.Sum256(original)
	relative := fmt.Sprintf("backups/project-classification-v1/projects-%x.json", digest[:8])
	if err := portablepath.CheckPath(registry.denovaDir, relative); err != nil {
		return false, err
	}
	backup := filepath.Join(registry.denovaDir, filepath.FromSlash(relative))
	if existing, err := os.ReadFile(backup); errors.Is(err, os.ErrNotExist) {
		if err := writeFileAtomic(backup, original, 0o600); err != nil {
			return false, fmt.Errorf("back up Project Registry before classification repair: %w", err)
		}
	} else if err != nil {
		return false, err
	} else if !bytes.Equal(existing, original) {
		return false, fmt.Errorf("Project classification backup does not match original registry")
	}
	slog.Info("Repairing incomplete Book registrations as General Projects", "project_ids", repaired, "backup", relative)
	return true, nil
}
