package projectfiles

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"

	"denova/internal/portablepath"
	"denova/internal/revisionfile"
)

// CreateAsset adopts an immutable media result under the same workspace lock
// and portable path boundary as editor changes. It never replaces user content.
func (service *Service) CreateAsset(ctx context.Context, projectID, path string, data []byte) (SaveResult, error) {
	runtime, err := service.resolve(projectID)
	if err != nil {
		return SaveResult{}, err
	}
	rel, err := normalizeRelativePath(path, false)
	if err != nil {
		return SaveResult{}, err
	}
	var revision string
	err = runtime.changes.WithExclusiveWorkspace(ctx, func() error {
		if err := portablepath.CheckNoCollision(runtime.layout.ContentRoot, rel); err != nil {
			return err
		}
		if err := rejectSymlinkComponents(runtime.layout.ContentRoot, rel); err != nil {
			return err
		}
		saved, err := revisionfile.ReplaceIfRevision(ctx, filepath.Join(runtime.layout.ContentRoot, filepath.FromSlash(rel)), revisionfile.MissingRevision, data, revisionfile.Options{FileMode: 0600, DirectoryMode: 0700})
		revision = saved.Revision
		if errors.Is(err, revisionfile.ErrRevisionConflict) {
			existing, readErr := revisionfile.Read(ctx, filepath.Join(runtime.layout.ContentRoot, filepath.FromSlash(rel)))
			if readErr == nil && existing.Exists && bytes.Equal(existing.Content, data) {
				revision = existing.Revision
				return nil
			}
		}
		return err
	})
	if err != nil {
		return SaveResult{}, err
	}
	versioning := service.mutationVersioning(runtime)
	if versioning.Service != nil {
		versioning.Service.ScheduleAutoVersion(versioning.Settings)
	}
	return SaveResult{ProjectID: projectID, Path: rel, Revision: revision, Changed: true}, nil
}
