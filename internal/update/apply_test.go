package update

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestApplySchedulerStartsUpdaterThenExits(t *testing.T) {
	dir := t.TempDir()
	updaterPath := filepath.Join(dir, updaterExecutableName())
	if err := os.WriteFile(updaterPath, []byte("updater"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, manifestFileName)
	if err := os.WriteFile(manifestPath, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := ApplyInvocation{
		Executable: updaterPath,
		Args:       []string{updaterPath, "--manifest", manifestPath},
	}
	started := make(chan ApplyInvocation, 1)
	cleaned := make(chan struct{})
	exited := make(chan int, 1)
	scheduler := ApplyScheduler{
		Delay:        10 * time.Millisecond,
		ManifestPath: manifestPath,
		Manifest: ApplyManifest{
			UpdaterExecutable: updaterPath,
		},
		Sleep: func(got time.Duration) {
			if got != 10*time.Millisecond {
				t.Fatalf("delay = %s", got)
			}
		},
		Start: func(got ApplyInvocation) error {
			got.Env = nil
			started <- got
			return nil
		},
		Shutdown: func() { close(cleaned) },
		Exit: func(code int) {
			select {
			case <-cleaned:
			default:
				t.Error("exit before cleanup")
			}
			exited <- code
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if err := scheduler.Schedule(context.Background()); err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}
	select {
	case got := <-started:
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("invocation = %#v, want %#v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("updater was not started")
	}
	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("exit code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("process exit was not requested")
	}
}

func TestApplySchedulerReturnsHandoffFailureWithoutExiting(t *testing.T) {
	dir := t.TempDir()
	updater := filepath.Join(dir, updaterExecutableName())
	path := filepath.Join(dir, manifestFileName)
	for _, file := range []string{updater, path} {
		if err := os.WriteFile(file, []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cause := errors.New("handoff failed")
	scheduler := ApplyScheduler{ManifestPath: path, Manifest: ApplyManifest{UpdaterExecutable: updater}, Start: func(ApplyInvocation) error { return cause }, Shutdown: func() { t.Error("closed app after failed handoff") }, Exit: func(int) { t.Error("exited after failed handoff") }}
	if err := scheduler.Schedule(context.Background()); !errors.Is(err, cause) {
		t.Fatal(err)
	}
}
