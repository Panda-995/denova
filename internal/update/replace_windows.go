package update

import (
	"errors"
	"log/slog"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// Status readers and virus scanners can briefly deny replacement on Windows.
// Retry only the rename, keeping the synced temporary file and old target intact.
func renameUpdatePath(source, target string) error {
	deadline := time.Now().Add(time.Second)
	for attempt := 0; ; attempt++ {
		err := os.Rename(source, target)
		if err == nil {
			return nil
		}
		if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return err
		}
		if attempt == 0 {
			slog.Warn("update_file_locked_retrying", "target", target, "error", err)
		}
		time.Sleep(min(25*time.Millisecond, remaining))
	}
}
