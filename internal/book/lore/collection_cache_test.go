package lore

import (
	"bytes"
	"os"
	"testing"
)

func TestCollectionCacheIsolatedFromCallersAndExternalReplacements(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	item, err := store.Create(ItemInput{ID: "cached", Type: "character", Name: "Original", Content: "first body", Tags: []string{"saved"}})
	if err != nil {
		t.Fatal(err)
	}
	read, err := store.Get(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	read.Tags[0] = "mutated"
	categories, err := store.Categories()
	if err != nil {
		t.Fatal(err)
	}
	categories[0].Name = "mutated"
	other := NewStore(workspace)
	read, err = other.Get(item.ID)
	if err != nil || read.Tags[0] != "saved" {
		t.Fatalf("cached item escaped: %#v err=%v", read, err)
	}
	categories, err = other.Categories()
	if err != nil || categories[0].Name == "mutated" {
		t.Fatalf("cached category escaped: %#v err=%v", categories, err)
	}
	path := ItemsPath(workspace)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte("first body"), []byte("other body"))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Editors can preserve timestamps while replacing equal-length content.
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	read, err = other.Get(item.ID)
	if err != nil || read.Content != "other body" {
		t.Fatalf("external replacement stayed stale: %#v err=%v", read, err)
	}
}
