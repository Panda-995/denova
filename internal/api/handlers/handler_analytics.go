package handlers

import (
	"context"
	"log/slog"

	"denova/internal/analytics"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (h *Handlers) HandleAnalyticsGet(_ context.Context, c *app.RequestContext) {
	writeJSON(c, consts.StatusOK, h.app.Analytics().Preferences())
}

func (h *Handlers) HandleAnalyticsPatch(ctx context.Context, c *app.RequestContext) {
	if len(c.Request.Body()) > 1024 {
		writeErrorKey(c, consts.StatusBadRequest, "settings.analytics.invalidRequest")
		return
	}
	var request struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeStrictJSONRequest(c.Request.Body(), &request); err != nil || request.Enabled == nil {
		writeErrorKey(c, consts.StatusBadRequest, "settings.analytics.invalidRequest")
		return
	}
	preferences, err := h.app.Analytics().SetEnabled(*request.Enabled)
	if err != nil {
		slog.ErrorContext(ctx, "analytics_preferences_save_failed", "error", err)
		writeErrorKey(c, consts.StatusInternalServerError, "settings.analytics.saveFailed")
		return
	}
	writeJSON(c, consts.StatusOK, preferences)
}

// Only a feature category is accepted. No caller-supplied URL, title, payload,
// identity, runtime, or arbitrary event/property names can reach the exporter.
func (h *Handlers) HandleAnalyticsActivity(_ context.Context, c *app.RequestContext) {
	var request struct {
		Feature analytics.Feature `json:"feature"`
	}
	if len(c.Request.Body()) > 1024 {
		writeErrorKey(c, consts.StatusBadRequest, "settings.analytics.invalidRequest")
		return
	}
	if err := decodeStrictJSONRequest(c.Request.Body(), &request); err != nil || !request.Feature.Valid() {
		writeErrorKey(c, consts.StatusBadRequest, "settings.analytics.invalidRequest")
		return
	}
	h.app.Analytics().Activity(request.Feature)
	writeJSON(c, consts.StatusOK, struct{}{})
}
