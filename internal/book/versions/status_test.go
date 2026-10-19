package versions

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
)

func TestStatusDoesNotReadAddedFileContents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file read permissions require POSIX permissions")
	}
	workspace := t.TempDir()
	service := newVersionTestService(t, workspace)
	writeFile(t, workspace, "draft.md", "original")
	settings := DefaultAutoSettings()
	if _, err := service.Create("initial", VersionSourceManual, settings); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "large-new.bin")
	if err := os.WriteFile(path, []byte("unreadable added file"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	status, err := service.Status(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	want := []VersionChange{{Path: "large-new.bin", Status: "added"}}
	if !reflect.DeepEqual(status.Changes, want) {
		t.Fatalf("status changes = %#v, want %#v", status.Changes, want)
	}
}

func TestStatusCancellation(t *testing.T) {
	service := newVersionTestService(t, t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Status(ctx, DefaultAutoSettings()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled status error = %v", err)
	}
}

func TestVersionStatusHashStreamsAndStopsOnCancellation(t *testing.T) {
	data := bytes.Repeat([]byte("snapshot\x00中文"), 100)
	buffer := make([]byte, 32)
	hash, err := hashVersionContent(context.Background(), bytes.NewReader(data), int64(len(data)), buffer)
	if err != nil || hash != plumbing.ComputeHash(plumbing.BlobObject, data) {
		t.Fatalf("streamed hash = %s, err = %v", hash, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &cancelVersionReader{Reader: bytes.NewReader(data), cancel: cancel}
	if _, err := hashVersionContent(ctx, reader, int64(len(data)), buffer); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted hash error = %v", err)
	}
	if reader.reads != 1 {
		t.Fatalf("read %d chunks after cancellation", reader.reads)
	}
}

type cancelVersionReader struct {
	*bytes.Reader
	cancel context.CancelFunc
	reads  int
}

func (reader *cancelVersionReader) Read(buffer []byte) (int, error) {
	reader.reads++
	n, err := reader.Reader.Read(buffer)
	reader.cancel()
	return n, err
}

func TestStatusDoesNotImportSourceRepository(t *testing.T) {
	workspace := t.TempDir()
	writeFile(t, workspace, "draft.md", "source repository content")
	source := NewService(workspace, filepath.Join(workspace, ".git"))
	if _, err := source.Create("source commit", VersionSourceManual, DefaultAutoSettings()); err != nil {
		t.Fatal(err)
	}
	service := newVersionTestService(t, workspace)
	status, err := service.Status(context.Background(), DefaultAutoSettings())
	if err != nil {
		t.Fatal(err)
	}
	if status.HasVersions || status.Latest != nil {
		t.Fatalf("source Git history became Denova history: %#v", status)
	}
	if _, err := os.Stat(service.repository); !os.IsNotExist(err) {
		t.Fatalf("status created a version repository: %v", err)
	}
}

func TestStatusDetectsSameSizeEditsWithUnchangedTimestamp(t *testing.T) {
	workspace := t.TempDir()
	service := newVersionTestService(t, workspace)
	writeFile(t, workspace, "draft.md", "before")
	settings := DefaultAutoSettings()
	if _, err := service.Create("initial", VersionSourceManual, settings); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "draft.md")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, workspace, "draft.md", "after!")
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	status, err := service.Status(context.Background(), settings)
	if err != nil {
		t.Fatal(err)
	}
	want := []VersionChange{{Path: "draft.md", Status: "modified"}}
	if !reflect.DeepEqual(status.Changes, want) {
		t.Fatalf("status changes = %#v, want %#v", status.Changes, want)
	}
}
