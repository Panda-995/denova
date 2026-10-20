package app

import (
	"context"
	"log/slog"

	"denova/internal/interactive"
)

// withProjectStoryRead captures stable Project identity independently of the
// foreground selection. A temporary Store owns only rebuildable read indexes.
func (a *App) withProjectStoryRead(ctx context.Context, projectID string, read func(context.Context, *interactive.Store) error) error {
	operation, err := a.AcquireProjectOperation(ctx, projectID)
	if err != nil {
		return err
	}
	defer operation.Release()
	a.mu.RLock()
	var store *interactive.Store
	if a.cfg != nil && a.cfg.ProjectID == projectID {
		store = a.interactive
	}
	a.mu.RUnlock()
	if store == nil {
		store = interactive.NewStore(operation.Layout().ContentRoot)
		defer func() {
			if err := store.Close(); err != nil {
				slog.WarnContext(ctx, "[project-story-read] close temporary story store failed", "project_id", projectID, "error", err)
			}
		}()
	}
	if err := operation.Context().Err(); err != nil {
		return err
	}
	return read(operation.Context(), store)
}

func (a *App) ProjectStoryIndex(ctx context.Context, projectID string) (interactive.Index, error) {
	var result interactive.Index
	err := a.withProjectStoryRead(ctx, projectID, func(ctx context.Context, store *interactive.Store) error {
		var err error
		result, err = store.Index()
		visible := make([]interactive.StorySummary, 0, len(result.Stories))
		for _, story := range result.Stories {
			if !story.Preview {
				visible = append(visible, story)
			}
		}
		result.Stories = visible
		return err
	})
	return result, err
}

func (a *App) ProjectStoryBranches(ctx context.Context, projectID, storyID string) ([]interactive.BranchSummary, error) {
	var result []interactive.BranchSummary
	err := a.withProjectStoryRead(ctx, projectID, func(ctx context.Context, store *interactive.Store) error {
		var err error
		result, err = store.Branches(storyID)
		return err
	})
	return result, err
}

func (a *App) ProjectStoryMentions(ctx context.Context, projectID, storyID, branchID, query, before string) (interactive.NarrativeMentionPage, error) {
	var result interactive.NarrativeMentionPage
	err := a.withProjectStoryRead(ctx, projectID, func(ctx context.Context, store *interactive.Store) error {
		var err error
		result, err = store.FindNarrativeMentions(ctx, storyID, branchID, query, before)
		return err
	})
	return result, err
}

func (a *App) ProjectStoryNarrative(ctx context.Context, projectID, storyID, branchID, turnID, revision string) (interactive.NarrativeSource, error) {
	var result interactive.NarrativeSource
	err := a.withProjectStoryRead(ctx, projectID, func(ctx context.Context, store *interactive.Store) error {
		scene, err := store.ReadSceneAtTurn(ctx, storyID, branchID, turnID, revision)
		result = interactive.NarrativeSource{TurnID: scene.Turn.ID, Timestamp: scene.Turn.Ts, Narrative: scene.Turn.Narrative}
		return err
	})
	return result, err
}
