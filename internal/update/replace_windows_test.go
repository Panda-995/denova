package update

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateManifestWaitsForTransientWindowsReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := writeJSONFile(path, "old", 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	result := make(chan error, 1)
	go func() {
		defer func() {
			if value := recover(); value != nil {
				result <- fmt.Errorf("manifest write panicked: %v", value)
			}
		}()
		result <- writeJSONFile(path, "new", 0o600)
	}()
	time.Sleep(75 * time.Millisecond)
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("transient reader aborted update: %v", err)
	}
	var got string
	if err := readJSONFile(path, &got); err != nil || got != "new" {
		t.Fatalf("manifest=%q err=%v", got, err)
	}
}

func TestUpdateManifestPreservesOriginalOnPersistentWindowsLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := writeJSONFile(path, "old", 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := writeJSONFile(path, "new", 0o600); err == nil {
		t.Fatal("persistent lock was ignored")
	}
	var got string
	if err := readJSONFile(path, &got); err != nil || got != "old" {
		t.Fatalf("original was damaged: %q %v", got, err)
	}
}
