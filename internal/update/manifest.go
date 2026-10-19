package update

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"denova/internal/localfs"
)

const (
	manifestFileName       = "manifest.json"
	pendingManifestRefName = "pending-manifest.json"
	applyLogFileName       = "apply.log"
)

// ApplyManifest is the updater contract written by Denova and consumed by
// denova-updater. Only transaction data is persisted; runtime paths are derived
// from the manifest's location, so the whole installation remains portable.
type ApplyManifest struct {
	// State is the single durable update transaction. Empty state is the v0.5.1
	// staged format; it is upgraded before applying, without touching user data.
	ID              string   `json:"id,omitempty"`
	State           Phase    `json:"state,omitempty"`
	Error           string   `json:"error,omitempty"`
	PreviousVersion string   `json:"previous_version,omitempty"`
	OriginalEntries []string `json:"original_entries,omitempty"`

	ExecutableName    string   `json:"executable_name"`
	SourceDir         string   `json:"-"`
	InstallDir        string   `json:"-"`
	BackupDir         string   `json:"-"`
	CurrentPID        int      `json:"current_pid"`
	TargetExecutable  string   `json:"-"`
	UpdaterExecutable string   `json:"-"`
	RelaunchArgs      []string `json:"relaunch_args"`
	Version           string   `json:"version"`
	LogPath           string   `json:"-"`
}

type pendingManifestRef struct {
	ManifestPath string `json:"manifest_path"`
}

func writeManifest(path string, manifest ApplyManifest) error {
	manifest.ExecutableName = filepath.Base(manifest.TargetExecutable)
	if len(manifest.RelaunchArgs) > 0 {
		manifest.RelaunchArgs = append([]string{manifest.ExecutableName}, manifest.RelaunchArgs[1:]...)
	}
	if err := writeJSONFile(path, manifest, 0o644); err != nil {
		return fmt.Errorf("write update manifest: %w", err)
	}
	return nil
}

func readManifest(path string) (ApplyManifest, error) {
	var manifest ApplyManifest
	if err := readJSONFile(path, &manifest); err != nil {
		return ApplyManifest{}, fmt.Errorf("read update manifest: %w", err)
	}
	if manifest.State == "" {
		// v0.5.1 invokes the new staged helper with its old absolute-path record.
		// Its package is staged but no live files or backups have been changed.
		if manifest.ExecutableName == "" {
			var legacy struct {
				TargetExecutable string `json:"target_executable"`
			}
			if err := readJSONFile(path, &legacy); err != nil {
				return ApplyManifest{}, err
			}
			manifest.ExecutableName = filepath.Base(legacy.TargetExecutable)
		}
		manifest.State = PhaseStaged
		manifest.PreviousVersion = "0.5.1"
	}
	if manifest.ID == "" {
		manifest.ID = rand.Text()
	}
	switch manifest.State {
	case PhaseStaged, PhaseWaiting, PhaseBackingUp, PhaseApplying, PhaseStarting, PhaseRollingBack, PhaseSucceeded, PhaseFailed:
	default:
		return ApplyManifest{}, fmt.Errorf("unsupported update state %q", manifest.State)
	}
	if !filepath.IsLocal(manifest.ExecutableName) || filepath.Base(manifest.ExecutableName) != manifest.ExecutableName || manifest.ExecutableName == "." {
		return ApplyManifest{}, fmt.Errorf("invalid update executable name %q", manifest.ExecutableName)
	}
	root := filepath.Dir(path)
	if filepath.Base(filepath.Dir(root)) != updateDataDirName || !strings.HasPrefix(filepath.Base(root), "pending-") {
		return ApplyManifest{}, fmt.Errorf("manifest is outside an update transaction: %s", path)
	}
	manifest.InstallDir = filepath.Dir(filepath.Dir(root))
	manifest.SourceDir = filepath.Join(root, releasePackageRootName)
	manifest.BackupDir = filepath.Join(root, "backup")
	manifest.TargetExecutable = filepath.Join(manifest.InstallDir, manifest.ExecutableName)
	manifest.UpdaterExecutable = filepath.Join(manifest.SourceDir, updaterExecutableName())
	manifest.LogPath = filepath.Join(root, applyLogFileName)
	if len(manifest.RelaunchArgs) > 0 {
		manifest.RelaunchArgs[0] = manifest.TargetExecutable
	}
	return manifest, nil
}

func writePendingManifestRef(updateDir, manifestPath string) error {
	path := filepath.Join(updateDir, pendingManifestRefName)
	relative, err := filepath.Rel(updateDir, manifestPath)
	if err != nil || !filepath.IsLocal(relative) {
		return fmt.Errorf("manifest is outside update directory: %s", manifestPath)
	}
	if err := writeJSONFile(path, pendingManifestRef{ManifestPath: filepath.ToSlash(relative)}, 0o644); err != nil {
		return fmt.Errorf("record pending update: %w", err)
	}
	return nil
}

func readPendingManifestRef(updateDir string) (string, error) {
	path := filepath.Join(updateDir, pendingManifestRefName)
	var ref pendingManifestRef
	if err := readJSONFile(path, &ref); err != nil {
		return "", fmt.Errorf("read pending update: %w", err)
	}
	if filepath.IsAbs(ref.ManifestPath) {
		// The last release stored an absolute reference. Retain only its fixed
		// transaction directory, including when the installation has been moved.
		ref.ManifestPath = filepath.Join(filepath.Base(filepath.Dir(ref.ManifestPath)), manifestFileName)
	}
	if !filepath.IsLocal(ref.ManifestPath) || filepath.Base(ref.ManifestPath) != manifestFileName || !strings.HasPrefix(filepath.Dir(ref.ManifestPath), "pending-") || filepath.Base(filepath.Dir(ref.ManifestPath)) != filepath.Dir(ref.ManifestPath) {
		return "", fmt.Errorf("invalid pending update reference %q", ref.ManifestPath)
	}
	return filepath.Join(updateDir, ref.ManifestPath), nil
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	file, err := os.CreateTemp(filepath.Dir(path), ".update-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(mode); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := renameUpdatePath(file.Name(), path); err != nil {
		return err
	}
	return localfs.SyncDirectory(filepath.Dir(path))
}

func readJSONFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}
