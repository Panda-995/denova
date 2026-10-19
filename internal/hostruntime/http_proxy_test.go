package hostruntime

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"testing"
)

func TestHTTPProxySelection(t *testing.T) {
	system := []string{"HTTP_PROXY=http://system:7890", "HTTPS_PROXY=http://system:7890", "NO_PROXY=localhost,.bypass.test"}
	for _, test := range []struct {
		name        string
		environment []string
		address     string
		want        string
	}{
		{"system HTTPS", nil, "https://api.github.com", "http://system:7890"},
		{"system HTTP", nil, "http://example.com", "http://system:7890"},
		{"environment wins", []string{"HTTPS_PROXY=http://explicit:9000"}, "https://api.github.com", "http://explicit:9000"},
		{"lowercase wins", []string{"HTTPS_PROXY=http://upper:9000", "https_proxy=http://lower:9001"}, "https://api.github.com", "http://lower:9001"},
		{"explicit direct", []string{"HTTPS_PROXY="}, "https://api.github.com", ""},
		{"system bypass", nil, "https://assets.bypass.test", ""},
		{"environment bypass", []string{"NO_PROXY=api.github.com"}, "https://api.github.com", ""},
		{"clear bypass", []string{"NO_PROXY="}, "https://assets.bypass.test", "http://system:7890"},
		{"HTTPS does not use HTTP proxy", []string{"HTTP_PROXY=http://explicit:9000", "HTTPS_PROXY="}, "https://api.github.com", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, platform := range []string{"windows", "darwin", "linux"} {
				selector := httpProxyFromEnvironment(mergeProxyEnvironment(platform, system, test.environment))
				address, err := url.Parse(test.address)
				if err != nil {
					t.Fatal(err)
				}
				got, err := selector(address)
				if err != nil {
					t.Fatal(err)
				}
				actual := ""
				if got != nil {
					actual = got.String()
				}
				if actual != test.want {
					t.Fatalf("%s proxy=%q, want %q", platform, actual, test.want)
				}
			}
		})
	}
}

func TestNewHTTPProxySnapshotsOnFirstRequest(t *testing.T) {
	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(key, "")
	}
	first := NewHTTPProxy()
	for _, key := range []string{"HTTPS_PROXY", "https_proxy"} {
		t.Setenv(key, "http://first:7890")
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.github.com", nil)
	got, err := first(request)
	if err != nil || got == nil || got.Host != "first:7890" {
		t.Fatalf("initial proxy=%v error=%v", got, err)
	}
	for _, key := range []string{"HTTPS_PROXY", "https_proxy"} {
		t.Setenv(key, "http://second:7890")
	}
	got, err = first(request)
	if err != nil || got == nil || got.Host != "first:7890" {
		t.Fatalf("existing client lost its proxy snapshot: proxy=%v error=%v", got, err)
	}
	got, err = NewHTTPProxy()(request)
	if err != nil || got == nil || got.Host != "second:7890" {
		t.Fatalf("new client reused a global proxy cache: proxy=%v error=%v", got, err)
	}
}

func TestNewHTTPProxyReadsHostSystemDefaults(t *testing.T) {
	// Clear only this test process's overrides; never change OS proxy settings.
	for _, key := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	defaults, err := systemProxyEnvironment(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(defaults) == 0 {
		t.Log("Host has no static system proxy configured")
	} else {
		t.Log("Verifying the host's static system proxy without exposing its values")
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.github.com", nil)
	want, err := httpProxyFromEnvironment(defaults)(request.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, err := NewHTTPProxy()(request)
	if err != nil {
		t.Fatal(err)
	}
	if (got == nil) != (want == nil) || (got != nil && got.String() != want.String()) {
		t.Fatal("HTTP client proxy does not match the host's system defaults")
	}
}
