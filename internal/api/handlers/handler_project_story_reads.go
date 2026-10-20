package handlers

import (
	"context"
	"log/slog"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (h *Handlers) HandleProjectStories(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	index, err := h.app.ProjectStoryIndex(ctx, scope.ProjectID)
	if err != nil {
		writeProjectBookError(c, err, "api.projectBook.loreFailed")
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{"project_id": scope.ProjectID, "current_story_id": index.CurrentStoryID, "stories": index.Stories})
}

func (h *Handlers) HandleProjectStoryBranches(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	branches, err := h.app.ProjectStoryBranches(ctx, scope.ProjectID, c.Param("id"))
	if err != nil {
		writeProjectBookError(c, err, "api.projectBook.loreFailed")
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{"project_id": scope.ProjectID, "branches": branches})
}

func (h *Handlers) HandleProjectStoryMentions(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	page, err := h.app.ProjectStoryMentions(ctx, scope.ProjectID, c.Param("id"), c.Query("branch"), c.Query("q"), c.Query("before"))
	if err != nil {
		slog.WarnContext(ctx, "[lore-mentions] story lookup failed", "project_id", scope.ProjectID, "story_id", c.Param("id"), "error", err)
		writeProjectBookError(c, err, "api.projectBook.loreFailed")
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{"project_id": scope.ProjectID, "page": page})
}

func (h *Handlers) HandleProjectStoryNarrative(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	source, err := h.app.ProjectStoryNarrative(ctx, scope.ProjectID, c.Param("id"), c.Query("branch"), c.Query("turn_id"), c.Query("revision"))
	if err != nil {
		slog.WarnContext(ctx, "[lore-mentions] read narrative source failed", "project_id", scope.ProjectID, "story_id", c.Param("id"), "error", err)
		writeProjectBookError(c, err, "api.projectBook.loreFailed")
		return
	}
	writeJSON(c, consts.StatusOK, map[string]any{"project_id": scope.ProjectID, "source": source})
}
