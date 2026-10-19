package update

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"denova/internal/hostruntime"
)

func (s *Service) Install(ctx context.Context) (InstallResult, error) {
	return s.InstallWithProgress(ctx, nil)
}

func (s *Service) InstallWithProgress(ctx context.Context, progress func(InstallProgress)) (result InstallResult, err error) {
	if err = s.beginOperation("downloading"); err != nil {
		return result, err
	}
	defer func() { s.endOperation(err) }()
	listener := progress
	progress = func(p InstallProgress) {
		s.mu.Lock()
		s.progress = &p
		s.mu.Unlock()
		reportInstallProgress(listener, p)
	}
	if !updateOperation.TryLock() {
		return InstallResult{}, ErrUpdateBusy
	}
	defer updateOperation.Unlock()
	releaseLock, err := s.lockOperation()
	if err != nil {
		return InstallResult{}, err
	}
	defer releaseLock()
	// A local upload may have staged a package after the automatic worker's
	// last observation. Preserve it under the same installation lock.
	status, err := s.Status()
	if err != nil {
		return InstallResult{}, err
	}
	if status.Phase == PhaseStaged {
		return InstallResult{}, ErrUpdateBusy
	}
	installCtx, cancel := context.WithTimeout(ctx, updateInstallTimeout)
	defer cancel()

	reportInstallProgress(progress, InstallProgress{Phase: "checking"})
	release, err := s.latestRelease(installCtx)
	if err != nil {
		return InstallResult{}, err
	}
	check := s.checkRelease(release)
	s.rememberCheck(check)
	if !check.UpdateAvailable {
		return InstallResult{}, errors.New("no update is available")
	}
	if check.Asset == nil {
		return InstallResult{}, errors.New("no release asset matches the current platform")
	}
	if s.executablePath == "" {
		return InstallResult{}, errors.New("cannot locate the current executable")
	}

	installDir := filepath.Dir(s.executablePath)
	updateDir := updateDataDir(installDir)
	downloadDir := filepath.Join(updateDir, "downloads")
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create update download directory: %w", err)
	}

	digest, err := s.releaseChecksum(installCtx, release, check.Asset.Name)
	if err != nil {
		return InstallResult{}, err
	}
	archivePath := filepath.Join(downloadDir, digest, check.Asset.Name)
	if err := s.downloadAsset(installCtx, updateAssetDownloadURL(check.Asset), archivePath, check.Asset.Size, digest, progress); err != nil {
		return InstallResult{}, err
	}
	reportInstallProgress(progress, InstallProgress{Phase: "verifying", AssetName: check.Asset.Name, ArchivePath: archivePath, Percent: 100})

	return s.stageArchive(installCtx, archivePath, check.LatestVersion, progress)
}

// stageArchive is shared by downloaded and uploaded packages. The caller holds
// updateOperation and owns the archive; extraction is always temporary.
func (s *Service) stageArchive(ctx context.Context, archivePath, version string, progress func(InstallProgress)) (InstallResult, error) {
	extractDir, err := os.MkdirTemp(filepath.Dir(archivePath), "extract-")
	if err != nil {
		return InstallResult{}, err
	}
	defer os.RemoveAll(extractDir)
	assetName := filepath.Base(archivePath)
	reportInstallProgress(progress, InstallProgress{Phase: "extracting", AssetName: assetName, ArchivePath: archivePath, Percent: 100})
	if err := extractArchive(archivePath, extractDir); err != nil {
		return InstallResult{}, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	if err := ctx.Err(); err != nil {
		return InstallResult{}, err
	}
	reportInstallProgress(progress, InstallProgress{Phase: "staging", AssetName: assetName, ArchivePath: archivePath, Percent: 100})
	result, err := s.stageUpdate(filepath.Join(extractDir, releasePackageRootName), version)
	if err == nil {
		reportInstallProgress(progress, InstallProgress{Phase: "staged", AssetName: assetName, ArchivePath: archivePath, Percent: 100})
	}
	return result, err
}

func (s *Service) stageUpdate(packageRoot, version string) (InstallResult, error) {
	if err := validateReleasePackage(packageRoot, filepath.Base(s.executablePath), updaterExecutableName()); err != nil {
		return InstallResult{}, fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	installDir := filepath.Dir(s.executablePath)
	updateDir := updateDataDir(installDir)
	if err := os.MkdirAll(updateDir, 0o755); err != nil {
		return InstallResult{}, err
	}
	stagedRoot, err := os.MkdirTemp(updateDir, "pending-"+safeUpdateName(version)+"-")
	if err != nil {
		return InstallResult{}, err
	}
	stagedDir := filepath.Join(stagedRoot, releasePackageRootName)
	backupDir := filepath.Join(stagedRoot, "backup")
	staged := false
	defer func() {
		if !staged {
			_ = os.RemoveAll(stagedRoot)
		}
	}()
	// Both directories belong to this installation's update directory. Consume
	// the temporary extraction directly instead of copying the entire package.
	if err := renameUpdatePath(packageRoot, stagedDir); err != nil {
		return InstallResult{}, fmt.Errorf("stage update package: %w", err)
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return InstallResult{}, err
	}

	manifestPath := filepath.Join(stagedRoot, manifestFileName)
	manifest := ApplyManifest{
		ID: rand.Text(), State: PhaseStaged, PreviousVersion: s.currentVersion,
		SourceDir:         stagedDir,
		InstallDir:        installDir,
		BackupDir:         backupDir,
		CurrentPID:        os.Getpid(),
		TargetExecutable:  s.executablePath,
		UpdaterExecutable: filepath.Join(stagedDir, updaterExecutableName()),
		RelaunchArgs:      relaunchArgs(os.Args, s.executablePath),
		Version:           version,
		LogPath:           filepath.Join(stagedRoot, applyLogFileName),
	}
	if err := writeManifest(manifestPath, manifest); err != nil {
		return InstallResult{}, err
	}
	if err := writePendingManifestRef(updateDir, manifestPath); err != nil {
		return InstallResult{}, err
	}
	staged = true
	slog.InfoContext(context.Background(), fmt.Sprintf("[update] Update staged old=%s new=%s staged=%s manifest=%s", s.currentVersion, version, stagedDir, manifestPath))
	return InstallResult{
		PreviousVersion:  s.currentVersion,
		InstalledVersion: version,
		Status:           "staged",
		Staged:           true,
		ApplyReady:       true,
		RestartRequired:  true,
		BackupPath:       backupDir,
		StagedPath:       stagedDir,
		ApplyLogPath:     manifest.LogPath,
	}, nil
}

func reportInstallProgress(progress func(InstallProgress), event InstallProgress) {
	if progress == nil {
		return
	}
	progress(event)
}

func safeUpdateName(version string) string {
	name := strings.TrimSpace(version)
	if name == "" {
		name = "unknown"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-", " ", "-")
	return replacer.Replace(name)
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func validateReleasePackage(packageRoot, exeName, updaterName string) error {
	requiredFiles := []string{exeName, updaterName}
	for _, name := range requiredFiles {
		path := filepath.Join(packageRoot, name)
		if fi, err := os.Stat(path); err != nil {
			return fmt.Errorf("update package is missing executable %s: %w", name, err)
		} else if fi.IsDir() {
			return fmt.Errorf("update executable is a directory: %s", name)
		}
	}
	for _, name := range []string{"skills"} {
		path := filepath.Join(packageRoot, name)
		if fi, err := os.Stat(path); err != nil {
			return fmt.Errorf("update package is missing directory %s: %w", name, err)
		} else if !fi.IsDir() {
			return fmt.Errorf("update package entry %s is not a directory", name)
		}
	}
	if runtimeExecutables := hostruntime.DiscoverForExecutable(filepath.Join(packageRoot, exeName)); runtimeExecutables.Ripgrep == "" {
		return fmt.Errorf("update package is missing executable bundled ripgrep")
	}
	for _, name := range []string{"LICENSE-MIT", "UNLICENSE"} {
		licensePath := filepath.Join(packageRoot, "licenses", "ripgrep", name)
		if info, err := os.Stat(licensePath); err != nil {
			return fmt.Errorf("update package is missing ripgrep license %s: %w", name, err)
		} else if !info.Mode().IsRegular() {
			return fmt.Errorf("invalid ripgrep license in update package: %s", name)
		}
	}
	return nil
}
