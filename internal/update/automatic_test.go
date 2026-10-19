package update

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"denova/internal/hostruntime"
)

// Exercise the real feed, download, checksum and staging path. Automatic
// preparation must neither need a browser nor replace an explicitly staged pack.
func TestAutomaticUpdatePreparesOnlyWithDownloadConsent(t *testing.T) {
	asset := "denova-v0.2.0-" + platformKey(runtime.GOOS, runtime.GOARCH) + ".tar.gz"
	archive := testReleaseArchive(t, "denova", map[string]string{
		"denova": "new", updaterExecutableName(): "updater", "skills/example/SKILL.md": "skill",
		"tools/" + hostruntime.RipgrepExecutableName(): "rg",
		"licenses/ripgrep/LICENSE-MIT":                 "MIT", "licenses/ripgrep/UNLICENSE": "Unlicense",
	})
	var checks, downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/releases/latest":
			checks.Add(1)
			_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v0.2.0", Assets: []githubAsset{
				{Name: asset, Size: int64(len(archive)), BrowserDownloadURL: serverURL(r, "/archive")},
				{Name: "checksums.txt", BrowserDownloadURL: serverURL(r, "/sums")},
			}})
		case "/sums":
			_, _ = fmt.Fprintf(w, "%x  %s\n", sha256.Sum256(archive), asset)
		case "/archive":
			downloads.Add(1)
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s := &Service{repository: "owner/repo", currentVersion: "0.1.0", httpClient: server.Client(), executablePath: filepath.Join(t.TempDir(), "denova"), githubAPIBase: server.URL + "/repos"}
	ctx := context.Background()
	if err := s.updateDue(ctx, Policy{Download: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if checks.Load() != 0 {
		t.Fatal("disabled checks accessed the feed")
	}
	if err := s.updateDue(ctx, Policy{Check: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if checks.Load() != 1 || downloads.Load() != 0 {
		t.Fatal("checking downloaded without consent")
	}
	for range 3 {
		if _, err := s.Status(); err != nil {
			t.Fatal(err)
		}
	}
	if checks.Load() != 1 {
		t.Fatal("status fetched the feed")
	}
	if err := s.updateDue(ctx, Policy{Check: true, Download: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	status, err := s.Status()
	if err != nil || status.Phase != PhaseStaged || status.Version != "0.2.0" || status.Operation != "" || downloads.Load() != 1 {
		t.Fatalf("automatic preparation: %+v, %v, downloads=%d", status, err, downloads.Load())
	}
	if err := s.updateDue(ctx, Policy{Check: true, Download: true}, time.Now().Add(2*automaticCheckInterval)); err != nil {
		t.Fatal(err)
	}
	latest, _ := s.Status()
	if latest.ID != status.ID || checks.Load() != 2 || downloads.Load() != 1 {
		t.Fatal("replaced or rechecked an already staged transaction")
	}
	// A manual upload can finish after the worker's status check. Download
	// admission must recheck under the installation lock before replacing it.
	if _, err := s.Install(ctx); !errors.Is(err, ErrUpdateBusy) {
		t.Fatalf("download replaced staged update: %v", err)
	}
	latest, _ = s.Status()
	if latest.ID != status.ID || downloads.Load() != 1 {
		t.Fatal("download modified the staged transaction")
	}
	// A new process can recover the package without in-memory download state.
	restarted := &Service{currentVersion: "0.1.0", executablePath: s.executablePath}
	recovered, err := restarted.Status()
	if err != nil || recovered.ID != status.ID || recovered.Phase != PhaseStaged {
		t.Fatalf("recover staged: %+v %v", recovered, err)
	}
	// Another upload between the UI observation and Apply cannot change the
	// user's selected transaction, even after the installation lock is acquired.
	if _, err := s.Apply(ctx, func() { t.Error("unexpected shutdown") }, 0, "stale-id"); err == nil {
		t.Fatal("applied an unselected transaction")
	}
}

func TestAutomaticUpdateRetriesAreBoundedAndShutdownCancels(t *testing.T) {
	var checks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	s := &Service{repository: "owner/repo", currentVersion: "0.1.0", httpClient: server.Client(), executablePath: filepath.Join(t.TempDir(), "denova"), githubAPIBase: server.URL}
	if err := s.updateDue(context.Background(), Policy{Check: true, Download: true}, time.Now()); err == nil {
		t.Fatal("missing check failure")
	}
	for range 3 {
		_ = s.updateDue(context.Background(), Policy{Check: true, Download: true}, time.Now())
	}
	if checks.Load() != 1 {
		t.Fatal("retried the failed check each poll")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Install(ctx); err == nil {
		t.Fatal("ignored App shutdown cancellation")
	}
	status, _ := s.Status()
	if status.Operation != "" || status.OperationError == "" {
		t.Fatalf("failed operation state: %+v", status)
	}
}
