package app

import (
	"context"
	"errors"

	reviewapp "denova/internal/app/review"
	"denova/internal/book"
	"denova/internal/workspace/documentreview"
)

// WithProjectDocumentReview binds comments and their source snapshots to one
// captured Project operation. Shared Lore is available in Book and General
// projects; resolving it must not depend on the foreground Writing workspace.
func (a *App) WithProjectDocumentReview(
	ctx context.Context,
	projectID string,
	action func(*documentreview.Service, documentreview.SnapshotResolver) error,
) (string, error) {
	if action == nil {
		return "", errors.New("document review action is nil")
	}
	operation, err := a.AcquireProjectOperation(ctx, projectID)
	if err != nil {
		return "", err
	}
	defer operation.Release()
	layout := operation.Layout()
	reviews, err := documentreview.ForWorkspaceAt(layout.ContentRoot, layout.StoreRoot)
	if err != nil {
		return "", err
	}
	resolver := reviewapp.NewTargetResolver(layout.ContentRoot, book.NewService(layout.ContentRoot))
	if err := action(reviews, resolver); err != nil {
		return "", err
	}
	return layout.ContentRoot, nil
}
