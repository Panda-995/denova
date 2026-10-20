//go:build !windows

package hostdialog

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func revealPath(ctx context.Context, path string, info os.FileInfo) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{"-R", path}
	case "linux":
		if !info.IsDir() {
			path = filepath.Dir(path)
		}
		name, args = "xdg-open", []string{path}
	default:
		return fmt.Errorf("%w: unsupported host platform %s", ErrUnavailable, runtime.GOOS)
	}
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%w: %s", ErrUnavailable, name)
	}
	return exec.CommandContext(ctx, name, args...).Run()
}
