package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const defaultWaitInterval = 500 * time.Millisecond

type UpdaterOptions struct {
	ProcessAlive func(int) bool
	StartProcess func(string, []string, []string) error
	LogOutput    io.Writer
	ReadyTimeout time.Duration
}

type replaceEntry struct {
	Source   string
	Target   string
	Backup   string
	Dir      bool
	Optional bool
}

// RunUpdater applies a staged update and relaunches Denova. It is the entrypoint
// used by cmd/denova-updater.
func RunUpdater(ctx context.Context, manifestPath string, options UpdaterOptions) (resultErr error) {
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return err
	}
	logger, closeLog, err := updaterLogger(manifest.LogPath, options.LogOutput)
	if err != nil {
		return err
	}
	defer closeLog()
	defer func() {
		if resultErr != nil {
			logger.ErrorContext(ctx, "updater_failed", "error", resultErr)
		}
	}()
	unlock, err := lockInstallation(manifest.InstallDir)
	if err != nil {
		return err
	}
	defer unlock()
	manifest, err = readManifest(manifestPath)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "updater_started", "manifest", manifestPath, "version", manifest.Version)
	switch manifest.State {
	case PhaseStaged, PhaseWaiting:
		if err := validateApplyManifest(manifest); err != nil {
			_ = setPhase(manifestPath, &manifest, PhaseFailed, err)
			return err
		}
		if err := setPhase(manifestPath, &manifest, PhaseWaiting, nil); err != nil {
			return err
		}
	case PhaseApplying, PhaseRollingBack:
		// A complete snapshot was committed before any installation file changed.
	default:
		return fmt.Errorf("cannot run updater in state %q", manifest.State)
	}
	if err := writePendingManifestRef(updateDataDir(manifest.InstallDir), manifestPath); err != nil {
		return err
	}
	if err := writeJSONFile(manifestPath+".accepted", manifest.ID, 0o600); err != nil {
		return err
	}
	if err := waitForProcessExit(ctx, manifest.CurrentPID, options, logger); err != nil {
		if manifest.State == PhaseWaiting {
			_ = setPhase(manifestPath, &manifest, PhaseFailed, err)
		}
		return err
	}
	restore := func(cause error) error {
		if err := setPhase(manifestPath, &manifest, PhaseRollingBack, cause); err != nil {
			return errors.Join(cause, err)
		}
		if err := rollbackUpdate(ctx, manifest, logger); err != nil {
			return errors.Join(cause, err)
		}
		if err := setPhase(manifestPath, &manifest, PhaseFailed, cause); err != nil {
			return errors.Join(cause, err)
		}
		return errors.Join(cause, startRelaunch(ctx, manifestPath, manifest, options, logger))
	}
	if manifest.State == PhaseApplying || manifest.State == PhaseRollingBack {
		return restore(fmt.Errorf("recovered an interrupted update; previous program files restored"))
	}
	if err := setPhase(manifestPath, &manifest, PhaseBackingUp, nil); err != nil {
		return err
	}
	if err := backupUpdate(&manifest); err != nil {
		_ = setPhase(manifestPath, &manifest, PhaseFailed, err)
		return errors.Join(err, startRelaunch(ctx, manifestPath, manifest, options, logger))
	}
	if err := setPhase(manifestPath, &manifest, PhaseApplying, nil); err != nil {
		return err
	}
	if err := applyStagedUpdate(ctx, manifest, logger); err != nil {
		return restore(err)
	}
	if err := setPhase(manifestPath, &manifest, PhaseStarting, nil); err != nil {
		return restore(err)
	}
	if err := startRelaunch(ctx, manifestPath, manifest, options, logger); err != nil {
		return restore(err)
	}
	// The new application may already have migrated user data. A readiness failure
	// must keep the new program and diagnostics, rather than silently downgrading it.
	if err := waitForReady(ctx, manifestPath, manifest, options); err != nil {
		_ = setPhase(manifestPath, &manifest, PhaseFailed, err)
		return err
	}
	if err := setPhase(manifestPath, &manifest, PhaseSucceeded, nil); err != nil {
		return err
	}
	pruneCompletedUpdates(manifest, logger)
	return nil
}

func updaterLogger(logPath string, extra io.Writer) (*slog.Logger, func(), error) {
	writers := []io.Writer{}
	var closeLog func()
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
			return nil, func() {}, err
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, func() {}, err
		}
		writers = append(writers, f)
		closeLog = func() { _ = f.Close() }
	} else {
		closeLog = func() {}
	}
	if extra != nil {
		writers = append(writers, extra)
	}
	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}
	handler := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{AddSource: true, Level: slog.LevelInfo})
	return slog.New(handler).With("component", "denova-updater"), closeLog, nil
}

func waitForProcessExit(ctx context.Context, pid int, options UpdaterOptions, logger *slog.Logger) error {
	alive := options.ProcessAlive
	if alive == nil {
		alive = processRunning
	}
	// After acknowledging handoff we own the restart. Application shutdown may
	// drain long-running work; abandoning it on a timer can strand a stopped app.
	logger.InfoContext(ctx, "updater_waiting_for_process_exit", "pid", pid)
	for alive(pid) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(defaultWaitInterval):
		}
	}
	return nil
}

func applyStagedUpdate(ctx context.Context, manifest ApplyManifest, logger *slog.Logger) error {
	if err := validateApplyManifest(manifest); err != nil {
		return err
	}
	entries := updateEntries(manifest)
	for _, entry := range entries {
		if entry.Optional {
			if _, err := os.Stat(entry.Source); os.IsNotExist(err) {
				continue
			}
		}
		logger.InfoContext(ctx, "updater_replacing_entry", "target", entry.Target, "source", entry.Source)
		if err := copyEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func validateApplyManifest(manifest ApplyManifest) error {
	if manifest.SourceDir == "" || manifest.InstallDir == "" || manifest.BackupDir == "" || manifest.TargetExecutable == "" || manifest.UpdaterExecutable == "" {
		return fmt.Errorf("update manifest fields are incomplete")
	}
	if err := validateReleasePackage(manifest.SourceDir, filepath.Base(manifest.TargetExecutable), filepath.Base(manifest.UpdaterExecutable)); err != nil {
		return err
	}
	return nil
}

func updateEntries(manifest ApplyManifest) []replaceEntry {
	installDir := manifest.InstallDir
	return []replaceEntry{
		fileEntry(manifest.SourceDir, manifest.TargetExecutable, manifest.BackupDir, filepath.Base(manifest.TargetExecutable), false),
		fileEntry(manifest.SourceDir, installUpdaterTarget(installDir, manifest.UpdaterExecutable), manifest.BackupDir, filepath.Base(manifest.UpdaterExecutable), false),
		dirEntry(manifest.SourceDir, installDir, manifest.BackupDir, "skills", false),
		dirEntry(manifest.SourceDir, installDir, manifest.BackupDir, "tools", false),
		dirEntry(manifest.SourceDir, installDir, manifest.BackupDir, "licenses", false),
		fileEntry(manifest.SourceDir, filepath.Join(installDir, "README.md"), manifest.BackupDir, "README.md", true),
		fileEntry(manifest.SourceDir, filepath.Join(installDir, "README.en.md"), manifest.BackupDir, "README.en.md", true),
		fileEntry(manifest.SourceDir, filepath.Join(installDir, "CHANGELOG.md"), manifest.BackupDir, "CHANGELOG.md", true),
		fileEntry(manifest.SourceDir, filepath.Join(installDir, "LICENSE"), manifest.BackupDir, "LICENSE", true),
	}
}

func fileEntry(sourceDir, target, backupDir, name string, optional bool) replaceEntry {
	return replaceEntry{
		Source:   filepath.Join(sourceDir, name),
		Target:   target,
		Backup:   filepath.Join(backupDir, name),
		Optional: optional,
	}
}

func dirEntry(sourceDir, installDir, backupDir, name string, optional bool) replaceEntry {
	return replaceEntry{
		Source:   filepath.Join(sourceDir, name),
		Target:   filepath.Join(installDir, name),
		Backup:   filepath.Join(backupDir, name),
		Dir:      true,
		Optional: optional,
	}
}

// backupUpdate never modifies the live installation. OriginalEntries is persisted
// with PhaseApplying only after every snapshot file has been copied and synced.
func backupUpdate(manifest *ApplyManifest) error {
	manifest.OriginalEntries = nil
	for _, entry := range updateEntries(*manifest) {
		info, err := os.Stat(entry.Target)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		backup := replaceEntry{Source: entry.Target, Target: entry.Backup, Dir: info.IsDir()}
		if err := copyEntry(backup); err != nil {
			return err
		}
		manifest.OriginalEntries = append(manifest.OriginalEntries, filepath.Base(entry.Target))
	}
	return nil
}

func copyEntry(entry replaceEntry) error {
	if entry.Dir {
		if err := os.RemoveAll(entry.Target); err != nil {
			return err
		}
		if err := copyDir(entry.Source, entry.Target); err != nil {
			return fmt.Errorf("replace update directory target=%s err=%w", entry.Target, err)
		}
		return nil
	}
	info, err := os.Stat(entry.Source)
	if err != nil {
		if entry.Optional && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("update package is missing file %s: %w", filepath.Base(entry.Source), err)
	}
	if info.IsDir() {
		return fmt.Errorf("update package file is a directory: %s", entry.Source)
	}
	if err := copyFile(entry.Source, entry.Target, info.Mode().Perm()); err != nil {
		return fmt.Errorf("replace update file target=%s err=%w", entry.Target, err)
	}
	return nil
}

func rollbackUpdate(ctx context.Context, manifest ApplyManifest, logger *slog.Logger) error {
	// Restore the executable last: until everything else is restored the new
	// executable's startup gate prevents opening a partially restored installation.
	entries := updateEntries(manifest)
	entries = append(entries[1:], entries[0])
	for _, entry := range entries {
		logger.InfoContext(ctx, "updater_rolling_back_entry", "target", entry.Target)
		if !slices.Contains(manifest.OriginalEntries, filepath.Base(entry.Target)) {
			if err := os.RemoveAll(entry.Target); err != nil {
				return err
			}
			continue
		}
		entry.Source = entry.Backup
		entry.Optional = false
		if err := copyEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func startRelaunch(ctx context.Context, manifestPath string, manifest ApplyManifest, options UpdaterOptions, logger *slog.Logger) error {
	start := options.StartProcess
	if start == nil {
		start = startProcess
	}
	args := manifest.RelaunchArgs
	if len(args) == 0 {
		args = []string{manifest.TargetExecutable, "--no-open"}
	}
	logger.InfoContext(ctx, "updater_starting_denova", "executable", manifest.TargetExecutable, "args", len(args))
	env := slices.DeleteFunc(os.Environ(), func(item string) bool {
		key, _, _ := strings.Cut(item, "=")
		return strings.EqualFold(key, "DENOVA_UPDATE_MANIFEST") || strings.EqualFold(key, "DENOVA_UPDATE_ID") || strings.EqualFold(key, "DENOVA_UPDATE_FROM_VERSION")
	})
	if manifest.State == PhaseStarting {
		// Origin is a launch-only compatibility hint, not a user port override.
		// v0.5.1 did not record its effective port; the new app must resolve its
		// settings with that release's default before reserving a listener.
		env = append(env, "DENOVA_UPDATE_MANIFEST="+manifestPath, "DENOVA_UPDATE_ID="+manifest.ID, "DENOVA_UPDATE_FROM_VERSION="+manifest.PreviousVersion)
	}
	return start(manifest.TargetExecutable, args, env)
}

func startProcess(executable string, args []string, env []string) error {
	cmdArgs := []string(nil)
	if len(args) > 1 {
		cmdArgs = args[1:]
	}
	cmd := exec.Command(executable, cmdArgs...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Start()
}
