package handlers

import (
	"context"
	"errors"
	"log/slog"

	"denova/internal/book/lore"

	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (h *Handlers) HandleLoreIndex(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	result, err := h.app.Lore().IndexGuide(ctx, scope.ProjectID)
	if err != nil {
		writeProjectBookError(c, err, "api.projectBook.loreFailed")
		return
	}
	writeJSON(c, consts.StatusOK, result)
}

func (h *Handlers) HandleLoreIndexUpdate(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	var input lore.IndexGuideUpdate
	if err := c.BindJSON(&input); err != nil {
		writeErrorKey(c, consts.StatusBadRequest, "api.common.invalidRequest")
		return
	}
	result, err := h.app.Lore().UpdateIndexGuide(ctx, scope.ProjectID, input)
	if err != nil {
		slog.WarnContext(ctx, "[lore] index update failed", "project_id", scope.ProjectID, "error", err)
		switch {
		case errors.Is(err, lore.ErrRevisionConflict):
			writeErrorKey(c, consts.StatusConflict, "api.resource.revisionConflict")
		case errors.Is(err, lore.ErrIndexGuide):
			writeErrorKey(c, consts.StatusBadRequest, "lore.index.invalid")
		default:
			writeProjectBookError(c, err, "api.projectBook.loreFailed")
		}
		return
	}
	writeJSON(c, consts.StatusOK, result)
}

func (h *Handlers) HandleLoreIndexPreview(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	var guide lore.IndexGuide
	if err := c.BindJSON(&guide); err != nil {
		writeErrorKey(c, consts.StatusBadRequest, "api.common.invalidRequest")
		return
	}
	preview, err := h.app.Lore().PreviewIndexGuide(ctx, scope.ProjectID, guide)
	if err != nil {
		slog.WarnContext(ctx, "[lore] index preview failed", "project_id", scope.ProjectID, "error", err)
		writeErrorKey(c, consts.StatusBadRequest, "lore.index.previewFailed")
		return
	}
	writeJSON(c, consts.StatusOK, struct {
		lore.IndexPreview
		TokenEstimate int `json:"token_estimate"`
	}{preview, agentmodel.EstimateTextTokens(preview.Markdown)})
}
