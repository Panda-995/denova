package platform

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGitHubPinnedPreviewDownloadsWithoutAPI(t *testing.T) {
	m, projectID := testManager(t)
	original := testCandidate(t, m, projectID, "static", "test.pinned", Game)
	sha := strings.Repeat("a", 40)
	archive := githubTestArchive(t, original.files)
	var requests []string
	m.githubHTTP.Transport = githubTestTransport(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.URL.String())
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(archive))), Request: r}, nil
	})
	candidate, err := m.PreviewGitHub(context.Background(), GitHubSource{URL: "https://github.com/author/repo", Ref: "main", Commit: sha})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Source.Commit != sha || candidate.Digest != original.Digest || len(requests) != 1 || requests[0] != "https://codeload.github.com/author/repo/zip/"+sha {
		t.Fatalf("pinned preview: source=%+v digest=%s requests=%v", candidate.Source, candidate.Digest, requests)
	}
	testInstall(t, m, candidate)
	if len(requests) != 1 {
		t.Fatal("installation fetched bytes after preview")
	}
}

func TestGitHubMetadataRevalidatesAndIsolatesTokens(t *testing.T) {
	m, _ := testManager(t)
	t.Setenv("GITHUB_TOKEN", "environment-token")
	t.Setenv("GH_TOKEN", "fallback-token")
	setting := ""
	m.ConfigureGitHubToken(func() string { return setting })
	var calls int
	m.githubHTTP.Transport = githubTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Hostname() == "codeload.github.com" {
			if r.Header.Get("Authorization") != "" || r.Header.Get("If-None-Match") != "" {
				t.Fatal("credentials or metadata validators reached archive origin")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("archive")), Request: r}, nil
		}
		want := "environment-token"
		if setting != "" {
			want = setting
		}
		if r.Header.Get("Authorization") != "Bearer "+want {
			t.Fatal("incorrect token precedence")
		}
		status, data := 200, want
		if calls == 2 {
			if r.Header.Get("If-None-Match") != `"v1"` {
				t.Fatal("metadata was not conditionally revalidated")
			}
			status, data = 304, ""
		} else if r.Header.Get("If-None-Match") != "" {
			t.Fatal("cached validators crossed token identities")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Etag": {`"v1"`}}, Body: io.NopCloser(strings.NewReader(data)), Request: r}, nil
	})
	for range 2 {
		data, err := m.githubGet(context.Background(), "https://api.github.com/repos/author/repo", "application/json", 1024)
		if err != nil || string(data) != "environment-token" {
			t.Fatalf("conditional response: %q %v", data, err)
		}
	}
	setting = "setting-token"
	data, err := m.githubGet(context.Background(), "https://api.github.com/repos/author/repo", "application/json", 1024)
	if err != nil || string(data) != setting {
		t.Fatalf("changed token reused old metadata: %q %v", data, err)
	}
	setting = ""
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "environment-token")
	data, err = m.githubGet(context.Background(), "https://api.github.com/repos/author/repo", "application/json", 1024)
	if err != nil || string(data) != "environment-token" {
		t.Fatalf("GH_TOKEN fallback: %q %v", data, err)
	}
	if _, err := m.githubGet(context.Background(), "https://codeload.github.com/author/repo/zip/commit", "application/zip", 1024); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubRedirectDoesNotForwardToken(t *testing.T) {
	m, _ := testManager(t)
	t.Setenv("GITHUB_TOKEN", "test-token")
	m.githubHTTP.Transport = githubTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Hostname() == "api.github.com" {
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatal("metadata request omitted the token")
			}
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://codeload.github.com/author/repo/zip/commit"}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("redirect forwarded the token to codeload")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("archive")), Request: r}, nil
	})
	if _, err := m.githubGet(context.Background(), "https://api.github.com/repos/author/repo", "application/json", 1024); err != nil {
		t.Fatal(err)
	}
}

func TestGitHubFailureClassification(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		header http.Header
		body   string
		code   string
		retry  bool
	}{
		{"primary limit", 403, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1893456000"}}, "", "GITHUB_RATE_LIMIT", true},
		{"secondary limit", 403, http.Header{}, `{"message":"You have exceeded a secondary rate limit."}`, "GITHUB_RATE_LIMIT", false},
		{"retry after", 429, http.Header{"Retry-After": {"60"}}, "", "GITHUB_RATE_LIMIT", true},
		{"permission denied", 403, http.Header{}, `{"message":"Resource not accessible"}`, "GITHUB_FORBIDDEN", false},
		{"invalid token", 401, http.Header{}, `{"message":"Bad credentials"}`, "GITHUB_AUTH_FAILED", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			m, _ := testManager(t)
			m.githubHTTP.Transport = githubTestTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: scenario.status, Header: scenario.header, Body: io.NopCloser(strings.NewReader(scenario.body)), Request: r}, nil
			})
			_, err := m.githubGet(context.Background(), "https://api.github.com/repos/author/repo", "application/json", 1024)
			status, problem := ErrorResponse(err)
			if problem.Code != scenario.code || (problem.RetryAt != "") != scenario.retry || scenario.code == "GITHUB_RATE_LIMIT" && status != 429 {
				t.Fatalf("incorrect failure classification: %d %+v", status, problem)
			}
			if scenario.retry {
				if _, err := time.Parse(time.RFC3339, problem.RetryAt); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
