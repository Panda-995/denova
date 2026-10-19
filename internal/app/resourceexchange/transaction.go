package resourceexchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"denova/internal/localfs"
	"denova/internal/portablepath"
	"denova/internal/project"
	"denova/internal/revisionfile"
	"github.com/google/uuid"
)

// FileTarget persists a logical location; a Project's current host path is
// resolved only while applying or recovering a transaction.
type FileTarget struct {
	ProjectID string `json:"project_id,omitempty"`
	Path      string `json:"path"`
}

type fileChange struct {
	Target   FileTarget `json:"target"`
	Expected string     `json:"expected"`
	After    []byte     `json:"after"`
	Delete   bool       `json:"delete,omitempty"`
	Before   []byte     `json:"before,omitempty"`
	Existed  bool       `json:"existed"`
}

type transaction struct {
	CreatedAt time.Time    `json:"created_at"`
	ID        string       `json:"id"`
	State     string       `json:"state"`
	Changes   []fileChange `json:"changes"`
}

func resolveTarget(root string, registry *project.Registry, target FileTarget) (string, error) {
	if err := portablepath.Validate(target.Path); err != nil {
		return "", err
	}
	base := root
	if target.ProjectID != "" {
		if registry == nil {
			return "", fmt.Errorf("project registry is unavailable")
		}
		_, layout, err := registry.Resolve(target.ProjectID, true)
		if err != nil {
			return "", err
		}
		base = layout.ContentRoot
	}
	if err := portablepath.CheckPath(base, target.Path); err != nil {
		return "", err
	}
	return filepath.Join(base, filepath.FromSlash(target.Path)), nil
}

func saveTransaction(ctx context.Context, root string, txn transaction) error {
	raw, err := json.Marshal(txn)
	if err != nil {
		return err
	}
	file, err := resolveTarget(root, nil, FileTarget{Path: "resource-exchange/transactions/" + txn.ID + ".json"})
	if err != nil {
		return err
	}
	_, err = revisionfile.ReplaceIfRevision(ctx, file, "", raw, revisionfile.Options{})
	return err
}

// archiveTransaction moves only a durably settled record. A crash before the
// rename leaves its final state in the recovery directory; after the rename it
// is a backup and must never be replayed as an interrupted transaction.
func archiveTransaction(root, id string) error {
	if _, err := uuid.Parse(id); err != nil {
		return err
	}
	file := id + ".json"
	source, err := resolveTarget(root, nil, FileTarget{Path: "resource-exchange/transactions/" + file})
	if err != nil {
		return err
	}
	destination, err := resolveTarget(root, nil, FileTarget{Path: "resource-exchange/backups/" + file})
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("transaction backup already exists: %s", id)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return err
	}
	if err := os.Rename(source, destination); err != nil {
		return err
	}
	return errors.Join(localfs.SyncDirectory(filepath.Dir(destination)), localfs.SyncDirectory(filepath.Dir(source)))
}

// readTransaction supports response retries during the interval between a
// durable commit and its archival. Both locations contain the same record type.
func readTransaction(ctx context.Context, root, id string) (revisionfile.Snapshot, error) {
	for _, directory := range []string{"transactions", "backups"} {
		file, err := resolveTarget(root, nil, FileTarget{Path: "resource-exchange/" + directory + "/" + id + ".json"})
		if err != nil {
			return revisionfile.Snapshot{}, err
		}
		snapshot, err := revisionfile.Read(ctx, file)
		if err != nil || snapshot.Exists {
			return snapshot, err
		}
	}
	return revisionfile.Snapshot{}, nil
}

func commitFiles(ctx context.Context, root string, registry *project.Registry, id string, changes []fileChange) error {
	paths := make([]string, len(changes))
	seen := map[string]bool{}
	for i, change := range changes {
		path, err := resolveTarget(root, registry, change.Target)
		if err != nil {
			return err
		}
		if seen[path] {
			return fmt.Errorf("duplicate transaction target")
		}
		seen[path], paths[i] = true, path
	}
	return revisionfile.WithFiles(ctx, paths, func(files *revisionfile.LockedFiles) error {
		for i := range changes {
			before, err := files.Snapshot(paths[i])
			if err != nil {
				return err
			}
			if before.Revision != changes[i].Expected {
				return &revisionfile.ConflictError{Path: paths[i], Expected: changes[i].Expected, Actual: before.Revision}
			}
			changes[i].Before, changes[i].Existed = before.Content, before.Exists
		}
		txn := transaction{ID: id, State: "applying", Changes: changes, CreatedAt: time.Now().UTC()}
		if err := saveTransaction(ctx, root, txn); err != nil {
			return err
		}
		// Once the intent is durable, client disconnect/cancellation cannot leave
		// a partially applied package. Finish or restore under the same locks.
		rollback := func(cause error) error {
			for i := len(changes) - 1; i >= 0; i-- {
				if err := files.Replace(paths[i], changes[i].Before, changes[i].Existed); err != nil {
					return errors.Join(cause, err)
				}
			}
			txn.State = "rolled_back"
			if err := saveTransaction(context.Background(), root, txn); err != nil {
				return errors.Join(cause, err)
			}
			return errors.Join(cause, archiveTransaction(root, txn.ID))
		}
		for i, change := range changes {
			if err := files.Replace(paths[i], change.After, !change.Delete); err != nil {
				return rollback(err)
			}
		}
		txn.State = "committed"
		if err := saveTransaction(context.Background(), root, txn); err != nil {
			return rollback(err)
		}
		if err := archiveTransaction(root, txn.ID); err != nil {
			// The commit is already durable. Recovery can finish archival without
			// turning a housekeeping failure into an installation rollback.
			slog.WarnContext(ctx, "resource_exchange_archive_deferred", "operation", txn.ID, "error", err)
		}
		slog.InfoContext(ctx, "resource_exchange_committed", "operation", id, "files", len(changes))
		return nil
	})
}

// Recover must run before resource readers or runtimes start. Only pending
// records are enumerated; settled records retain before-images under backups.
func Recover(root string, registry *project.Registry) error {
	dir := filepath.Join(root, "resource-exchange", "transactions")
	if err := portablepath.CheckPath(root, "resource-exchange/transactions"); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") || entry.IsDir() {
			continue
		}
		if err := portablepath.CheckPath(dir, entry.Name()); err != nil {
			return err
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		var txn transaction
		if err := json.Unmarshal(raw, &txn); err != nil {
			return err
		}
		if _, err := uuid.Parse(txn.ID); err != nil || entry.Name() != txn.ID+".json" {
			return fmt.Errorf("invalid exchange transaction identity")
		}
		switch txn.State {
		case "committed", "rolled_back":
			if err := archiveTransaction(root, txn.ID); err != nil {
				return err
			}
			continue
		case "applying":
		default:
			return fmt.Errorf("unknown exchange transaction state")
		}
		paths := make([]string, len(txn.Changes))
		for i, change := range txn.Changes {
			paths[i], err = resolveTarget(root, registry, change.Target)
			if err != nil {
				return err
			}
		}
		err = revisionfile.WithFiles(context.Background(), paths, func(files *revisionfile.LockedFiles) error {
			for i, change := range txn.Changes {
				current, err := files.Snapshot(paths[i])
				if err != nil {
					return err
				}
				after := revisionfile.Revision(change.After)
				if change.Delete {
					after = revisionfile.MissingRevision
				}
				if current.Revision != change.Expected && current.Revision != after {
					return fmt.Errorf("recovery target changed outside interrupted transaction: %s", change.Target.Path)
				}
			}
			for i := len(txn.Changes) - 1; i >= 0; i-- {
				change := txn.Changes[i]
				if err := files.Replace(paths[i], change.Before, change.Existed); err != nil {
					return err
				}
			}
			txn.State = "rolled_back"
			return saveTransaction(context.Background(), root, txn)
		})
		if err != nil {
			return err
		}
		slog.Info("resource_exchange_recovered", "operation", txn.ID)
		if err := archiveTransaction(root, txn.ID); err != nil {
			return err
		}
	}
	return nil
}
