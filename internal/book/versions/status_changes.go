package versions

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
)

func (s *Service) statusChanges(ctx context.Context, baseline map[string]versionFileData) ([]VersionChange, error) {
	current := make(map[string]versionFileData)
	buffer := make([]byte, 64*1024)
	err := walkVersionFiles(ctx, s.workspace, s.workspace, func(path, rel string, info os.FileInfo) error {
		mode, err := filemode.NewFromOSFileMode(info.Mode())
		if err != nil {
			return fmt.Errorf("map version file mode %q: %w", path, err)
		}
		file := versionFileData{Path: rel, Size: info.Size(), Mode: mode}
		if old, exists := baseline[rel]; exists && old.Size == file.Size && old.Mode == mode {
			reader, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("open version status file %q: %w", path, err)
			}
			hash, readErr := hashVersionContent(ctx, reader, info.Size(), buffer)
			closeErr := reader.Close()
			if readErr != nil {
				return fmt.Errorf("hash version status file %q: %w", path, readErr)
			}
			if closeErr != nil {
				return closeErr
			}
			file.Hash = hash.String()
		}
		// Added files and size/mode changes are already known to differ. Reading
		// them would add work without changing the status decision.
		current[rel] = file
		return nil
	})
	if err != nil {
		return nil, err
	}
	return diffFileStates(baseline, current), nil
}

// Hash bounded chunks so a Project cancellation interrupts large files without
// buffering them in memory. Size/mtime alone cannot detect same-size edits.
func hashVersionContent(ctx context.Context, reader io.Reader, size int64, buffer []byte) (plumbing.Hash, error) {
	hasher := plumbing.NewHasher(plumbing.BlobObject, size)
	for {
		if err := ctx.Err(); err != nil {
			return plumbing.ZeroHash, err
		}
		n, err := reader.Read(buffer)
		if n > 0 {
			_, _ = hasher.Write(buffer[:n])
		}
		if err == io.EOF {
			return hasher.Sum(), ctx.Err()
		}
		if err != nil {
			return plumbing.ZeroHash, err
		}
	}
}
