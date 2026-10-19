package platform

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"denova/internal/portablepath"
)

// This archive contains only one plugin's Project content. It never carries
// executable packages, configuration, credentials or scope-private journals.
type projectContentArchive struct {
	Format   int    `json:"format"`
	PluginID string `json:"pluginId"`
}

func (m *Manager) projectContentPath(projectID, pluginID string) (string, error) {
	if err := validateID(pluginID); err != nil {
		return "", err
	}
	_, layout, err := m.registry.Resolve(projectID, true)
	if err != nil {
		return "", err
	}
	relative := "extensions/" + pluginID + "/content"
	if err := portablepath.CheckNoCollision(layout.StoreRoot, relative); err != nil {
		return "", err
	}
	return filepath.Join(layout.StoreRoot, filepath.FromSlash(relative)), nil
}

func validateProjectContent(files map[string][]byte) error {
	for name, data := range files {
		folder, base, ok := strings.Cut(name, "/")
		if !ok || strings.Contains(base, "/") {
			return failure("INVALID_ARGUMENT", "Invalid plugin content path %s", name)
		}
		switch folder {
		case "documents":
			if !strings.HasSuffix(base, ".json") || len(data) > MaxDefinitionBytes/2 || !json.Valid(data) {
				return failure("INVALID_ARGUMENT", "Invalid plugin JSON document %s", name)
			}
		case "assets":
			_, extension, err := mediaType(data)
			if err != nil {
				return err
			}
			digest := sha256.Sum256(data)
			if base != hex.EncodeToString(digest[:])+extension {
				return failure("INVALID_ARGUMENT", "Asset content address does not match %s", name)
			}
		default:
			return failure("INVALID_ARGUMENT", "Unsupported plugin content folder %s", folder)
		}
	}
	return nil
}

func (m *Manager) ExportProjectPluginContent(projectID, pluginID string, writer io.Writer) error {
	directory, err := m.projectContentPath(projectID, pluginID)
	if err != nil {
		return err
	}
	if err := portablepath.PreflightTree(directory); err != nil {
		return failure("NOT_FOUND", "Plugin Project content is unavailable: %v", err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	files, err := readArchiveFiles(root.FS(), []string{"."}, MaxAssetBytes)
	if err != nil {
		return err
	}
	if err := validateProjectContent(files); err != nil {
		return err
	}
	// Refuse a changing snapshot, including media added while a board is saved.
	current, err := readArchiveFiles(root.FS(), []string{"."}, MaxAssetBytes)
	if err != nil {
		return err
	}
	if !maps.EqualFunc(files, current, bytes.Equal) {
		return failure("DOCUMENT_CONFLICT", "Plugin content changed during export; export again")
	}
	files["content.json"], _ = json.Marshal(projectContentArchive{Format: 1, PluginID: pluginID})
	names, err := packageFileNames(files)
	if err != nil {
		return err
	}
	if err := writePackageArchive(Candidate{Files: names, files: files}, writer); err != nil {
		return err
	}
	slog.Info("platform_project_content_exported", "plugin", pluginID, "project", projectID)
	return nil
}

// Import only into an empty destination. A temporary sibling is renamed into
// place after complete validation; existing author content is never overwritten.
func (m *Manager) ImportProjectPluginContent(projectID, pluginID string, raw []byte) error {
	m.runtimeMu.Lock()
	defer m.runtimeMu.Unlock()
	for _, runtime := range m.runtimes {
		if runtime.owner.context.Scope.ProjectID == projectID && runtime.uses(Plugin, pluginID) {
			return failure("DOCUMENT_CONFLICT", "Close this Project's plugin before importing its content")
		}
	}
	directory, err := m.projectContentPath(projectID, pluginID)
	if err != nil {
		return err
	}
	if len(raw) > MaxPackageBytes {
		return failure("LIMIT_EXCEEDED", "Content archive is too large")
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return failure("INVALID_ARGUMENT", "Invalid content ZIP")
	}
	if len(archive.File) > MaxPackageFiles {
		return failure("LIMIT_EXCEEDED", "Content archive has too many files")
	}
	files, err := readPackageEntries(archive.File, MaxAssetBytes)
	if err != nil {
		return err
	}
	var metadata projectContentArchive
	if err := decodeJSON(files["content.json"], &metadata); err != nil {
		return err
	}
	if metadata.Format != 1 || metadata.PluginID != pluginID {
		return failure("INVALID_ARGUMENT", "Content archive format or plugin identity does not match")
	}
	delete(files, "content.json")
	if err := validateProjectContent(files); err != nil {
		return err
	}
	if len(files) == 0 {
		return failure("INVALID_ARGUMENT", "Content archive is empty")
	}
	entries, err := os.ReadDir(directory)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(entries) > 0 {
		return failure("DOCUMENT_CONFLICT", "Project already contains this plugin's content; choose an empty Project")
	}
	parent := filepath.Dir(directory)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".content-import-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for name, data := range files {
		if err := writeBytes(filepath.Join(staging, filepath.FromSlash(name)), data); err != nil {
			return err
		}
	}
	// Removing an empty directory cannot delete user content; concurrent additions
	// make Remove fail. No operation below recursively removes the destination.
	if err := os.Remove(directory); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(staging, directory); err != nil {
		return err
	}
	slog.Info("platform_project_content_imported", "plugin", pluginID, "project", projectID)
	return nil
}
