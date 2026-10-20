package hostdialog

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRevealPathRejectsInvalidPaths(t *testing.T) {
	revealer := NewPathRevealer()
	if err := revealer.RevealPath(t.Context(), "relative.txt"); err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative path: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if err := revealer.RevealPath(t.Context(), missing); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing path: %v", err)
	}
}

func TestRevealPathCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := NewPathRevealer().RevealPath(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reveal: %v", err)
	}
}
