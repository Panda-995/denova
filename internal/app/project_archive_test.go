package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"denova/config"
	projectdomain "denova/internal/project"
)

func TestArchiveProjectCancelsReadsAndPreservesContentAndStore(t *testing.T) {
	root, workspace := t.TempDir(), t.TempDir()
	registry := projectdomain.NewRegistry(root)
	record, err := registry.Add(workspace, projectdomain.TypeGeneral, "Archive test")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := registry.EnsureStore(record)
	if err != nil {
		t.Fatal(err)
	}
	contentPath := filepath.Join(workspace, "draft.md")
	storePath := filepath.Join(layout.StoreRoot, "preserved.jsonl")
	for _, path := range []string{contentPath, storePath} {
		if err := os.WriteFile(path, []byte("preserved"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	application := &App{cfg: &config.Config{NovaDir: root}, projectRegistry: registry}
	t.Cleanup(application.Close)
	operation, err := application.AcquireProjectOperation(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer operation.Release()
	done := make(chan error, 1)
	runAppErrorTestGoroutine(done, "archive Project", func() error {
		_, err := application.ArchiveProject(context.Background(), record.ID)
		return err
	})
	select {
	case <-operation.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("archive did not cancel the Project read")
	}
	if _, err := application.ProjectVersionStatus(operation.Context(), record.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("version read after archive began = %v", err)
	}
	operation.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("archive did not finish after the canceled read exited")
	}
	record, err = registry.Get(record.ID)
	if err != nil || record.ArchivedAt == nil {
		t.Fatalf("Project not archived: %#v, %v", record, err)
	}
	for _, path := range []string{contentPath, storePath} {
		if data, err := os.ReadFile(path); err != nil || string(data) != "preserved" {
			t.Fatalf("archive changed user data: %q, %v", data, err)
		}
	}
}
