//go:build !windows

package update

import "os"

func renameUpdatePath(source, target string) error { return os.Rename(source, target) }
