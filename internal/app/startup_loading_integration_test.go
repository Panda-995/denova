package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"denova/config"
	"denova/internal/project"
)

func TestStartupDoesNotPrepareUnusedProjectContentOrStore(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "projects", "Unused")
	if err := os.MkdirAll(filepath.Join(workspace, "history"), 0o700); err != nil {
		t.Fatal(err)
	}
	// This unrelated file would fail a recursive portability check.
	if err := os.WriteFile(filepath.Join(workspace, "history", "CON.txt"), []byte("retained"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := project.NewRegistry(root)
	record, err := registry.Add(workspace, project.TypeBook, "Unused")
	if err != nil {
		t.Fatal(err)
	}
	layout, err := registry.Layout(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(layout.StoreRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(layout.StoreRoot, "migration.json")
	if err := os.WriteFile(receipt, []byte("unreadable migration receipt"), 0o600); err != nil {
		t.Fatal(err)
	}
	application, err := New(context.Background(), &config.Config{DenovaDir: root, OpenAIModel: "test-model"})
	if err != nil {
		t.Fatalf("unused Project blocked startup: %v", err)
	}
	application.Close()
	if raw, err := os.ReadFile(receipt); err != nil || string(raw) != "unreadable migration receipt" {
		t.Fatalf("startup modified unused Project Store: %q %v", raw, err)
	}
	if _, _, err := registry.Resolve(record.ID, true); err == nil {
		t.Fatal("using the Project did not validate its migration receipt")
	}
}
