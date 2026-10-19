package hostruntime

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"golang.org/x/net/http/httpproxy"
)

// NewHTTPProxy returns a proxy selector for a client's lifetime. Its first use
// snapshots the process environment over the host's system proxy defaults,
// including explicit empty overrides and NO_PROXY. It neither changes the
// process environment nor uses net/http's global environment cache.
func NewHTTPProxy() func(*http.Request) (*url.URL, error) {
	resolve := sync.OnceValue(func() func(*url.URL) (*url.URL, error) {
		return httpProxyFromEnvironment(WithSystemProxy(context.Background(), os.Environ()))
	})
	return func(request *http.Request) (*url.URL, error) {
		return resolve()(request.URL)
	}
}

func httpProxyFromEnvironment(environment []string) func(*url.URL) (*url.URL, error) {
	config := httpproxy.Config{}
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "HTTP_PROXY":
			config.HTTPProxy = value
		case "HTTPS_PROXY":
			config.HTTPSProxy = value
		case "NO_PROXY":
			config.NoProxy = value
		case "REQUEST_METHOD":
			config.CGI = value != ""
		}
	}
	// Proxy endpoints may contain credentials; only log whether they are set.
	slog.Debug("[hostruntime] HTTP proxy policy initialized", "http_proxy_configured", config.HTTPProxy != "", "https_proxy_configured", config.HTTPSProxy != "")
	return config.ProxyFunc()
}
