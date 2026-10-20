package hostdialog

import (
	"os"
	"path/filepath"
	"testing"
)

// This opt-in integration test opens Explorer and requires an interactive desktop.
func TestWindowsRevealPath(t *testing.T) {
	if os.Getenv("DENOVA_TEST_WINDOWS_REVEAL") != "1" {
		t.Skip("set DENOVA_TEST_WINDOWS_REVEAL=1 to verify native file selection")
	}
	directory := filepath.Join(t.TempDir(), "project 中文, with spaces")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(directory, "chapter 中文, one.txt")
	if err := os.WriteFile(file, []byte("reveal test"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{directory, file, file} {
		if err := NewPathRevealer().RevealPath(t.Context(), path); err != nil {
			t.Errorf("reveal %q: %v", path, err)
		}
	}
}
