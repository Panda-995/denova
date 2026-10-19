package project

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryCachedReadsObserveExternalEditsAndAvailability(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "projects", "Book")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(root)
	record, err := registry.Add(workspace, TypeBook, "Before")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.Resolve(record.ID, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(registry.path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(registry.path)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"Before"`), []byte(`"After!"`), 1)
	if err := os.WriteFile(registry.path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(registry.path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	updated, err := registry.Get(record.ID)
	if err != nil || updated.Name != "After!" {
		t.Fatalf("external update = %#v, err=%v", updated, err)
	}
	if err := os.Rename(workspace, workspace+"-moved"); err != nil {
		t.Fatal(err)
	}
	missing, err := registry.Get(record.ID)
	if err != nil || missing.Status != StatusMissing {
		t.Fatalf("availability = %#v, err=%v", missing, err)
	}
	if _, _, err := registry.Resolve(record.ID, true); err == nil {
		t.Fatal("resolved missing Project from cache")
	}
}

func TestRegistryPreparedStoreRechecksReceipt(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "projects", "Book")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(root)
	record, err := registry.Add(workspace, TypeBook, "Book")
	if err != nil {
		t.Fatal(err)
	}
	_, layout, err := registry.Resolve(record.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(layout.StoreRoot, "migration.json")
	if err := os.WriteFile(receipt, []byte("invalid receipt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.Resolve(record.ID, true); err == nil {
		t.Fatal("accepted a corrupted migration receipt")
	}
	if err := os.Remove(receipt); err != nil {
		t.Fatal(err)
	}
	if _, _, err := registry.Resolve(record.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(receipt); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryMutationOwnsCachedSlices(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "projects", "Book")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(root)
	record, err := registry.Add(workspace, TypeGeneral, "Before")
	if err != nil {
		t.Fatal(err)
	}
	registry.mu.Lock()
	data, _, err := registry.loadOrMigrateLocked()
	if err != nil {
		t.Fatal(err)
	}
	data.Projects[0].Name = "Unsaved"
	data.Order[0] = "unsaved-id"
	registry.mu.Unlock()
	current, err := registry.Get(record.ID)
	if err != nil || current.Name != "Before" {
		t.Fatalf("unsaved mutation leaked: %#v, err=%v", current, err)
	}
}
