package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const readyTimeout = 90 * time.Second

// PrepareStartup runs before configuration or user data is opened. It hands an
// interrupted file replacement back to the staged updater and asks main to exit.
func PrepareStartup() (bool, error) {
	executable, err := os.Executable()
	if err != nil {
		return false, err
	}
	path, err := readPendingManifestRef(updateDataDir(filepath.Dir(executable)))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	m, err := readManifest(path)
	if err != nil {
		return false, err
	}
	if m.State == PhaseStarting && os.Getenv("DENOVA_UPDATE_ID") == m.ID && os.Getenv("DENOVA_UPDATE_MANIFEST") == path {
		return false, nil
	}
	if m.State == PhaseStaged || m.State == PhaseSucceeded || m.State == PhaseFailed {
		return false, nil
	}
	unlock, err := lockInstallation(m.InstallDir)
	if err != nil {
		return false, err
	}
	m, err = readManifest(path)
	if err != nil {
		unlock()
		return false, err
	}
	switch m.State {
	case PhaseStaged, PhaseSucceeded, PhaseFailed:
		unlock()
		return false, nil
	case PhaseStarting:
		var readyID string
		if err := readJSONFile(path+".ready", &readyID); err == nil && readyID == m.ID {
			err = setPhase(path, &m, PhaseSucceeded, nil)
		} else {
			err = setPhase(path, &m, PhaseFailed, fmt.Errorf("update startup was interrupted; inspect %s", m.LogPath))
		}
		unlock()
		return false, err
	case PhaseWaiting, PhaseBackingUp:
		// These phases never change live files, so the original app can start.
		err = setPhase(path, &m, PhaseFailed, fmt.Errorf("update interrupted in phase %s; inspect %s", m.State, m.LogPath))
		unlock()
		return false, err
	case PhaseApplying, PhaseRollingBack:
		m.CurrentPID = os.Getpid()
		err = writeManifest(path, m)
		unlock()
		if err != nil {
			return false, err
		}
		err = startApplyProcess(ApplyInvocation{Executable: m.UpdaterExecutable, Args: []string{m.UpdaterExecutable, "--manifest", path}, Env: os.Environ()})
		return err == nil, err
	default:
		unlock()
		return false, fmt.Errorf("unsupported update phase %q", m.State)
	}
}

// ConfirmReady acknowledges only an HTTP response from this update's version and
// transaction, after application initialization and listener startup both worked.
// It does nothing on ordinary launches. The caller owns the goroutine boundary.
func ConfirmReady(ctx context.Context, baseURL, version string) error {
	path, id := os.Getenv("DENOVA_UPDATE_MANIFEST"), os.Getenv("DENOVA_UPDATE_ID")
	if path == "" || id == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	defer client.CloseIdleConnections()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/update/status", nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			var status Status
			decodeErr := json.NewDecoder(resp.Body).Decode(&status)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK && decodeErr == nil && status.ID == id && status.Phase == PhaseStarting && sameVersion(status.CurrentVersion, version) && sameVersion(status.Version, version) {
				return writeJSONFile(path+".ready", id, 0o600)
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("confirm update readiness: %w", ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func sameVersion(a, b string) bool { return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v") }

func waitForReady(ctx context.Context, path string, m ApplyManifest, options UpdaterOptions) error {
	timeout := options.ReadyTimeout
	if timeout <= 0 {
		timeout = readyTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		var id string
		if err := readJSONFile(path+".ready", &id); err == nil && id == m.ID {
			return nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		select {
		case <-ctx.Done():
			return errors.New("new version did not confirm readiness; program and backup retained; see " + m.LogPath)
		case <-time.After(100 * time.Millisecond):
		}
	}
}
