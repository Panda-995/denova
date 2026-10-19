package publicnet

import (
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// NewHTTPClientWithProxy preserves public-only dialing for direct requests and
// trusts the supplied proxy selector for proxied requests. The selector must be
// non-nil and stable for the client's lifetime. Callers still own URL scheme,
// credentials, redirect and download limits.
//
// A configured proxy is a trusted network boundary: it resolves destination DNS
// and may itself be on a private network. Local DNS preflight would both break
// proxy-only DNS and fail to constrain the proxy's actual destination. Reject
// explicit private destinations, but delegate DNS enforcement to the proxy.
func NewHTTPClientWithProxy(proxy func(*http.Request) (*url.URL, error)) *http.Client {
	proxied := http.DefaultTransport.(*http.Transport).Clone()
	proxied.Proxy = proxy
	return &http.Client{Transport: &proxyTransport{direct: newPublicTransport(), proxied: proxied}}
}

type proxyTransport struct {
	direct  *http.Transport
	proxied *http.Transport
}

func (transport *proxyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	host := strings.ToLower(strings.TrimSuffix(request.URL.Hostname(), "."))
	if address, err := netip.ParseAddr(host); err == nil {
		if !isPublicAddress(address) {
			return nil, &policyError{message: fmt.Sprintf("public destination %q is a blocked address", host)}
		}
	} else if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, &policyError{message: fmt.Sprintf("public destination %q is a local host", host)}
	}
	proxy, err := transport.proxied.Proxy(request)
	if err != nil {
		return nil, err
	}
	if proxy == nil {
		return transport.direct.RoundTrip(request)
	}
	// Never retry a failed proxy connection directly: that would silently bypass
	// the user's chosen network boundary.
	return transport.proxied.RoundTrip(request)
}

func (transport *proxyTransport) CloseIdleConnections() {
	transport.direct.CloseIdleConnections()
	transport.proxied.CloseIdleConnections()
}
