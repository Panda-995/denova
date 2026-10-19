package update

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestUpdaterWaitsForSlowShutdown(t *testing.T) {
	// Fake time covers shutdown beyond the former 60-second deadline instantly.
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		err := waitForProcessExit(context.Background(), 123, UpdaterOptions{
			ProcessAlive: func(int) bool { return time.Since(started) < 90*time.Second },
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil || time.Since(started) < 90*time.Second {
			t.Fatalf("helper abandoned shutdown: elapsed=%s err=%v", time.Since(started), err)
		}
	})
}

func TestCompletedUpdateSurvivesInstallationMove(t *testing.T) {
	root := t.TempDir()
	install := filepath.Join(root, "original")
	service := &Service{currentVersion: "0.1.0", executablePath: filepath.Join(install, "nova")}
	writeUpdateTestPackage(t, install, updateTestPackageContents{Executable: "old"})
	packageDir := filepath.Join(root, "package")
	writeUpdateTestPackage(t, packageDir, updateTestPackageContents{Executable: "new"})
	if _, err := service.stageUpdate(packageDir, "0.2.0"); err != nil {
		t.Fatal(err)
	}
	path, err := readPendingManifestRef(updateDataDir(install))
	if err != nil {
		t.Fatal(err)
	}
	m, err := readManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := setPhase(path, &m, PhaseSucceeded, nil); err != nil {
		t.Fatal(err)
	}
	moved := filepath.Join(root, "moved")
	if err := os.Rename(install, moved); err != nil {
		t.Fatal(err)
	}
	service.executablePath = filepath.Join(moved, "nova")
	status, err := service.Status()
	if err != nil || status.Phase != PhaseSucceeded || status.ID != m.ID {
		t.Fatalf("moved installation cannot read its completed update: %+v %v", status, err)
	}
	path, err = readPendingManifestRef(updateDataDir(moved))
	if err != nil {
		t.Fatal(err)
	}
	actual, err := readManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if actual.InstallDir != moved || actual.TargetExecutable != service.executablePath {
		t.Fatalf("paths still refer to the old installation: %+v", actual)
	}
	for _, file := range []string{path, filepath.Join(updateDataDir(moved), pendingManifestRefName)} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var record map[string]any
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		for key, value := range record {
			if s, ok := value.(string); ok && filepath.IsAbs(s) {
				t.Errorf("persisted host path %s=%s", key, s)
			}
		}
	}
}

func TestApplyHandoffFailureRemainsRetryable(t *testing.T) {
	install, source := t.TempDir(), t.TempDir()
	writeUpdateTestPackage(t, install, updateTestPackageContents{Executable: "old"})
	writeUpdateTestPackage(t, source, updateTestPackageContents{Executable: "new", Updater: "invalid executable"})
	service := &Service{currentVersion: "0.1.0", executablePath: filepath.Join(install, "nova")}
	if _, err := service.stageUpdate(source, "0.2.0"); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status()
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := service.Apply(context.Background(), func() { t.Error("closed after failed handoff") }, 0, status.ID); err == nil || strings.Contains(err.Error(), "no staged update") {
			t.Fatalf("handoff was not attempted: %v", err)
		}
		status, err := service.Status()
		if err != nil || status.Phase != PhaseStaged || status.Error == "" {
			t.Fatalf("failed handoff cannot be retried: %+v %v", status, err)
		}
	}
}

func TestRelease051ManifestIsUpgradedBeforeReplacement(t *testing.T) {
	install := t.TempDir()
	root := filepath.Join(updateDataDir(install), "pending-0.2.0")
	source := filepath.Join(root, releasePackageRootName)
	writeUpdateTestPackage(t, install, updateTestPackageContents{Executable: "old"})
	writeUpdateTestPackage(t, source, updateTestPackageContents{Executable: "new"})
	path := filepath.Join(root, manifestFileName)
	target := filepath.Join(install, "nova")
	// This is the v0.5.1 wire format, including its separate empty backup path.
	legacy := map[string]any{
		"source_dir": source, "install_dir": install,
		"backup_dir":        filepath.Join(updateDataDir(install), "backup-legacy"),
		"target_executable": target, "updater_executable": filepath.Join(source, updaterExecutableName()),
		"relaunch_args": []string{target, "--no-open"},
		"version":       "0.2.0", "log_path": filepath.Join(root, applyLogFileName),
	}
	if err := writeJSONFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(filepath.Join(updateDataDir(install), pendingManifestRefName), pendingManifestRef{ManifestPath: path}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RunUpdater(context.Background(), path, UpdaterOptions{
		ProcessAlive: func(int) bool { return false },
		StartProcess: func(executable string, args, env []string) error {
			if !slices.Contains(env, "DENOVA_UPDATE_FROM_VERSION=0.5.1") {
				t.Fatal("legacy launch lost its original default port context")
			}
			return acknowledgeTestLaunch(executable, args, env)
		},
	}); err != nil {
		t.Fatal(err)
	}
	assertFileContent(t, target, "new")
	assertFileContent(t, filepath.Join(root, "backup", "nova"), "old")
	m, err := readManifest(path)
	if err != nil || m.State != PhaseSucceeded || m.ID == "" || m.PreviousVersion != "0.5.1" || m.RelaunchArgs[0] != target {
		t.Fatalf("legacy handoff failed: %+v %v", m, err)
	}
	var ref pendingManifestRef
	if err := readJSONFile(filepath.Join(updateDataDir(install), pendingManifestRefName), &ref); err != nil {
		t.Fatal(err)
	}
	if ref.ManifestPath != "pending-0.2.0/manifest.json" {
		t.Fatalf("legacy reference was not upgraded: %+v", ref)
	}
}
