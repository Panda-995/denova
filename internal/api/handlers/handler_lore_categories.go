package handlers

import (
	"context"
	"denova/internal/book/lore"
	"errors"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"log/slog"
)

func (h *Handlers) HandleLoreCategories(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	items, err := h.app.Lore().Categories(ctx, scope.ProjectID)
	if err != nil {
		writeProjectBookError(c, err, "api.projectBook.loreFailed")
		return
	}
	writeJSON(c, consts.StatusOK, items)
}

func (h *Handlers) HandleLoreCategoryMutation(ctx context.Context, c *app.RequestContext) {
	scope, ok := requireProjectScope(c)
	if !ok {
		return
	}
	var input lore.CategoryMutation
	if err := c.BindJSON(&input); err != nil {
		writeErrorKey(c, consts.StatusBadRequest, "api.common.invalidRequest")
		return
	}
	items, err := h.app.Lore().MutateCategory(ctx, scope.ProjectID, input)
	if err != nil {
		slog.WarnContext(ctx, "[lore] category mutation failed", "project_id", scope.ProjectID, "operation", input.Op, "error", err)
		if errors.Is(err, lore.ErrCategory) {
			writeErrorKey(c, consts.StatusBadRequest, "lore.categories.invalid")
			return
		}
		writeProjectBookError(c, err, "api.projectBook.loreFailed")
		return
	}
	writeJSON(c, consts.StatusOK, items)
}
