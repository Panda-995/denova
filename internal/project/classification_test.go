package project

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestDetectTypeUsesBookWorkspaceMarkers(t *testing.T) {
	general := filepath.Join(t.TempDir(), "code-project")
	book := filepath.Join(t.TempDir(), "novel-project")
	if err := os.MkdirAll(general, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(book, "chapters"), 0o755); err != nil {
		t.Fatal(err)
	}

	if got, err := DetectType(general); err != nil || got != TypeGeneral {
		t.Fatalf("general detection = %q, %v", got, err)
	}
	if got, err := DetectType(book); err != nil || got != TypeBook {
		t.Fatalf("book detection = %q, %v", got, err)
	}
}

func TestClassificationRepairRequiresRegistryBackup(t *testing.T) {
	workspace, dataRoot := t.TempDir(), t.TempDir()
	registry := NewRegistry(dataRoot)
	record, err := registry.Add(workspace, TypeBook, "Code")
	if err != nil {
		t.Fatal(err)
	}
	userData := filepath.Join(workspace, ".denova")
	if err := os.Mkdir(userData, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userData, "projects.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataRoot, "projects.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// A file blocks creation of the backup directory on every platform.
	if err := os.WriteFile(filepath.Join(dataRoot, "backups"), []byte("blocked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRegistry(dataRoot).Get(record.ID); err == nil {
		t.Fatal("classification repair proceeded without a registry backup")
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("failed repair changed the original registry: %q, %v", after, err)
	}
}

func TestDetectTypeDistinguishesUserDataFromBookState(t *testing.T) {
	for _, test := range []struct {
		name     string
		stateDir string
		registry string
		book     bool
		want     Type
	}{
		{name: "current user data", stateDir: ".denova", registry: "projects.json", want: TypeGeneral},
		{name: "released user data", stateDir: ".nova", registry: "books.json", want: TypeGeneral},
		{name: "current Book state", stateDir: ".denova", want: TypeBook},
		{name: "released Book state", stateDir: ".nova", want: TypeBook},
		{name: "Book with user data", stateDir: ".denova", registry: "projects.json", book: true, want: TypeBook},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			directory := filepath.Join(workspace, test.stateDir)
			if err := os.MkdirAll(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			if test.registry != "" {
				if err := os.WriteFile(filepath.Join(directory, test.registry), []byte("{}"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if test.book {
				if err := os.Mkdir(filepath.Join(workspace, "chapters"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if got, err := DetectType(workspace); err != nil || got != test.want {
				t.Fatalf("detected type = %q, %v; want %q", got, err, test.want)
			}
		})
	}
}

func TestClassificationRepairKeepsCompletedBooksAndBookContent(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "Book content", true: "completed Store"}[completed], func(t *testing.T) {
			workspace, dataRoot := t.TempDir(), t.TempDir()
			registry := NewRegistry(dataRoot)
			record, err := registry.Add(workspace, TypeBook, "Book")
			if err != nil {
				t.Fatal(err)
			}
			if completed {
				if _, err := registry.EnsureStore(record); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Mkdir(filepath.Join(workspace, "chapters"), 0o700); err != nil {
				t.Fatal(err)
			}
			userData := filepath.Join(workspace, ".denova")
			if err := os.MkdirAll(userData, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(userData, "projects.json"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			loaded, err := NewRegistry(dataRoot).Get(record.ID)
			if err != nil || loaded.Type != TypeBook {
				t.Fatalf("existing Book changed type: %#v, %v", loaded, err)
			}
		})
	}
}
