package hostdialog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// PathRevealer opens the host file manager at an existing absolute path.
type PathRevealer interface {
	RevealPath(context.Context, string) error
}

type nativePathRevealer struct{}

func NewPathRevealer() PathRevealer {
	return nativePathRevealer{}
}

func (nativePathRevealer) RevealPath(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("host reveal path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect host reveal path: %w", err)
	}
	if err := revealPath(ctx, filepath.Clean(path), info); err != nil {
		return fmt.Errorf("reveal path in host file manager: %w", err)
	}
	return nil
}
