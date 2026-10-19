package session

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"denova/internal/agents/conversationjournal"
	"denova/internal/localfs"
)

// migratePageConversations retires the unreleased page-owned Agent journals.
// Preserve every transcript/context record and timestamp in one ordinary
// conversation, but never transplant a frozen executor or pending paid run.
// The original journal is backed up before publication. Stable destination IDs
// let a restart complete an interrupted migration without duplicating history.
func migratePageConversations(dir string) error {
	paths, err := filepath.Glob(filepath.Join(dir, "platform-*.jsonl"))
	if err != nil || len(paths) == 0 {
		return err
	}
	release, err := localfs.AcquireLease(context.Background(), filepath.Join(dir, ".page-conversations.lock"))
	if err != nil {
		return err
	}
	defer release()
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		if err := migratePageConversation(path); err != nil {
			return fmt.Errorf("migrate page conversation %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}

func migratePageConversation(path string) error {
	original, err := loadSession(path)
	if err != nil {
		return err
	}
	defer original.Close()
	raw, _, err := original.PlatformRecord(context.Background(), "configuration")
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	var config struct {
		Caller struct {
			Kind string `json:"kind"`
		} `json:"caller"`
		Definition string `json:"definition"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}
	if config.Caller.Kind != "plugin" || !strings.HasPrefix(config.Definition, "page:") {
		return nil
	}

	backup := path + ".pre-project-agent.bak"
	if _, err := os.Stat(backup); os.IsNotExist(err) {
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer source.Close()
		temp, err := os.CreateTemp(filepath.Dir(path), ".page-backup-*")
		if err != nil {
			return err
		}
		defer os.Remove(temp.Name())
		_, copyErr := io.Copy(temp, source)
		if err := errors.Join(copyErr, source.Close(), temp.Sync(), temp.Close()); err != nil {
			return err
		}
		if err := os.Rename(temp.Name(), backup); err != nil {
			return err
		}
		if err := syncParentDirectory(backup); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		// An interrupted attempt may leave a backup. Never retire the source
		// unless that backup still contains its exact bytes.
		var digests [2][sha256.Size]byte
		for i, name := range []string{path, backup} {
			file, err := os.Open(name)
			if err != nil {
				return err
			}
			hash := sha256.New()
			_, copyErr := io.Copy(hash, file)
			if err := errors.Join(copyErr, file.Close()); err != nil {
				return err
			}
			copy(digests[i][:], hash.Sum(nil))
		}
		if digests[0] != digests[1] {
			return fmt.Errorf("page conversation backup differs from source")
		}
	}

	id := "s-page-" + strings.TrimPrefix(original.ID, "platform-")
	destination := filepath.Join(filepath.Dir(path), id+".jsonl")
	if _, err := os.Stat(destination); os.IsNotExist(err) && original.MessageCountTotal() > 0 {
		// Export through the journal reader so transaction checksums are verified.
		exported, err := os.CreateTemp(filepath.Dir(path), ".page-export-*")
		if err != nil {
			return err
		}
		defer os.Remove(exported.Name())
		defer exported.Close()
		if err := original.ExportHistoryJSONL(context.Background(), exported); err != nil {
			return err
		}
		if _, err := exported.Seek(0, io.SeekStart); err != nil {
			return err
		}
		next, err := os.CreateTemp(filepath.Dir(path), ".page-conversation-*")
		if err != nil {
			return err
		}
		defer os.Remove(next.Name())
		defer next.Close()
		reader := bufio.NewReader(exported)
		for {
			line, readErr := reader.ReadBytes('\n')
			if readErr != nil && readErr != io.EOF {
				return readErr
			}
			if len(line) > 0 {
				var record struct {
					Type string `json:"type"`
				}
				if err := json.Unmarshal(line, &record); err != nil {
					return err
				}
				switch record.Type {
				case "platform_session", "agent_session":
					// These records own the retired executor, not the creator's transcript.
				case "session":
					var header sessionHeader
					if err := json.Unmarshal(line, &header); err != nil {
						return err
					}
					header.ID, header.IncarnationID = id, newSessionJournalIncarnationID()
					line, err = marshalJSONLine(header)
					if err != nil {
						return err
					}
					if _, err := next.Write(line); err != nil {
						return err
					}
				default:
					if _, err := next.Write(line); err != nil {
						return err
					}
				}
			}
			if readErr == io.EOF {
				break
			}
		}
		if err := errors.Join(next.Sync(), next.Close()); err != nil {
			return err
		}
		if err := os.Rename(next.Name(), destination); err != nil {
			return err
		}
		if err := syncParentDirectory(destination); err != nil {
			return err
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	} else if err == nil {
		published, err := loadSession(destination)
		if err != nil {
			return err
		}
		if err := published.Close(); err != nil {
			return err
		}
	}
	if err := original.Close(); err != nil {
		return err
	}
	// Publication succeeded; the backup is now the only retired copy. Its name
	// excludes it from Store.List and from subsequent migration scans.
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := os.Remove(conversationjournal.SidecarPath(path)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := syncParentDirectory(path); err != nil {
		return err
	}
	slog.Info("page_conversation_migrated", "from", filepath.Base(path), "to", id, "backup", filepath.Base(backup))
	return nil
}
