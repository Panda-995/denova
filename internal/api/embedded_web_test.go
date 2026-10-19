//go:build embedweb

package api

import (
	"compress/gzip"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"denova/internal/webfs"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestReleaseRoutesIgnoreStaleDiskFrontend(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, "web")
	if err := os.MkdirAll(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "index.html"), []byte("obsolete frontend"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DENOVA_WEB_DIR", stale)
	t.Chdir(root)
	server := NewServer(newTestApplication(t), "0")
	expected := httptest.NewRecorder()
	webfs.Handler().ServeHTTP(expected, httptest.NewRequest("GET", "/", nil))
	for _, path := range []string{"/", "/writing/book", "/game/story"} {
		response := ut.PerformRequest(server.engine.Engine, "GET", path, nil)
		if response.Code != 200 || response.Body.String() != expected.Body.String() || response.Header().Get("Cache-Control") != "no-cache" {
			t.Fatalf("release route %s selected an inconsistent frontend: status=%d", path, response.Code)
		}
	}
	compressed := ut.PerformRequest(server.engine.Engine, "GET", "/writing/book", nil, ut.Header{Key: "Accept-Encoding", Value: "gzip"})
	if compressed.Code != 200 || compressed.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("embedded assets bypassed compression")
	}
	reader, err := gzip.NewReader(compressed.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(body) != expected.Body.String() {
		t.Fatalf("compressed asset mismatch: %v", err)
	}
	if response := ut.PerformRequest(server.engine.Engine, "GET", "/api/status", nil); response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("static wildcard intercepted API: status=%d body=%s", response.Code, response.Body.String())
	}
}
