package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

func TestWebSearchPreviewUsesDraftAndLocalizesFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("q") != "draft query" {
			t.Errorf("unexpected search: %s", r.URL)
		}
		io.WriteString(w, `{"results":[{"title":"Draft result","url":"https://example.com/source","content":"Excerpt"}]}`)
	}))
	defer server.Close()
	for _, locale := range []string{"en-US", "zh-CN"} {
		request := app.NewContext(0)
		request.Request.Header.Set("X-Denova-Locale", locale)
		body, _ := json.Marshal(map[string]any{"query": "draft query", "settings": map[string]any{"searxng_base_url": server.URL}})
		request.Request.SetBody(body)
		(&Handlers{}).HandleWebSearchTest(context.Background(), request)
		if request.Response.StatusCode() != 200 || !strings.Contains(string(request.Response.Body()), `"provider":"searxng"`) || !strings.Contains(string(request.Response.Body()), "Draft result") {
			t.Fatalf("preview failed: %s", request.Response.Body())
		}
		request.Request.SetBodyString(`{"query":"draft query","settings":{"search_provider":"deepseek","search_fallback":"builtin"}}`)
		(&Handlers{}).HandleWebSearchTest(context.Background(), request)
		if request.Response.StatusCode() != 400 || !strings.Contains(string(request.Response.Body()), "settings.webAccess.error.configuration") {
			t.Fatalf("missing configuration hidden: %s", request.Response.Body())
		}
		phrase := "Enter the selected"
		if locale == "zh-CN" {
			phrase = "请填写所选"
		}
		if !strings.Contains(string(request.Response.Body()), phrase) {
			t.Fatalf("error not localized: %s", request.Response.Body())
		}
	}
}
