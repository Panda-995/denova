package handlers

import (
	"context"
	"log/slog"
	"time"

	"denova/config"
	"denova/internal/webaccess"
	"github.com/cloudwego/hertz/pkg/app"
)

// HandleWebSearchTest tests the current draft without saving or falling back.
// The preview deadline bounds an infrastructure probe, not an Agent run.
func (h *Handlers) HandleWebSearchTest(ctx context.Context, c *app.RequestContext) {
	c.Header("Cache-Control", "no-store")
	var body struct {
		Settings *config.WebAccessSettings `json:"settings"`
		Query    string                    `json:"query"`
	}
	if err := decodeStrictJSONRequest(c.Request.Body(), &body); err != nil || body.Settings == nil {
		writeWebSearchError(c, "invalid_request")
		return
	}
	if err := body.Settings.Validate(); err != nil {
		writeWebSearchError(c, "configuration")
		return
	}
	resolved := webaccess.ConfigFromSettings(config.ResolveWebAccessSettings(*body.Settings))
	resolved.SearchFallback = "none"
	client, err := webaccess.New(resolved)
	if err != nil {
		writeWebSearchError(c, "configuration")
		return
	}
	defer func() {
		if err := client.Close(ctx); err != nil {
			slog.WarnContext(ctx, "[webaccess] preview cleanup failed", "error", err)
		}
	}()
	probe, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := time.Now()
	response, err := client.Search(probe, webaccess.SearchRequest{Query: body.Query})
	if err != nil {
		code := webaccess.SearchErrorCode(err)
		slog.WarnContext(ctx, "[webaccess] search preview failed", "provider", resolved.SearchProvider, "code", code)
		writeWebSearchError(c, code)
		return
	}
	if response.Status == webaccess.SearchStatusProvidersUnavailable {
		writeWebSearchError(c, "unavailable")
		return
	}
	// Keep the preview API independent of Agent-only recovery instructions.
	c.JSON(200, struct {
		Provider  string                   `json:"provider"`
		Status    webaccess.SearchStatus   `json:"status"`
		Results   []webaccess.SearchResult `json:"results"`
		ElapsedMS int64                    `json:"elapsed_ms"`
	}{response.Provider, response.Status, response.Results, time.Since(started).Milliseconds()})
}

func writeWebSearchError(c *app.RequestContext, code string) {
	key := "settings.webAccess.error." + code
	c.JSON(400, map[string]string{"error": messageKey(c, key), "code": key})
}
