package publicnet

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"

	"golang.org/x/net/http/httpproxy"
)

func TestProxyFailureNeverDialsDirectly(t *testing.T) {
	var connects, directs atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connects.Add(1)
		http.Error(w, "proxy unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)
	address, _ := url.Parse(proxy.URL)
	client := NewHTTPClientWithProxy(http.ProxyURL(address))
	t.Cleanup(client.CloseIdleConnections)
	client.Transport.(*proxyTransport).direct.DialContext = func(context.Context, string, string) (net.Conn, error) {
		directs.Add(1)
		return nil, errors.New("unexpected direct connection")
	}
	_, err := client.Get("https://archive.invalid/resource.zip")
	if err == nil || connects.Load() != 1 || directs.Load() != 0 {
		t.Fatalf("proxy failure bypassed policy: connects=%d directs=%d error=%v", connects.Load(), directs.Load(), err)
	}
}

func TestProxyAndDirectRejectExplicitPrivateDestinations(t *testing.T) {
	for _, mode := range []string{"proxy", "direct"} {
		t.Run(mode, func(t *testing.T) {
			address, _ := url.Parse("http://127.0.0.1:7890")
			client := NewHTTPClientWithProxy(http.ProxyURL(address))
			t.Cleanup(client.CloseIdleConnections)
			transport := client.Transport.(*proxyTransport)
			if mode == "direct" {
				transport.proxied.Proxy = http.ProxyURL(nil)
			}
			var dials atomic.Int32
			noDial := func(context.Context, string, string) (net.Conn, error) {
				dials.Add(1)
				return nil, errors.New("unexpected network connection")
			}
			transport.direct.DialContext, transport.proxied.DialContext = noDial, noDial
			for _, host := range []string{"127.0.0.1", "localhost", "LOCALHOST.", "service.localhost", "10.0.0.1", "169.254.169.254", "192.88.99.1", "[::1]", "[::ffff:127.0.0.1]", "[64:ff9b::7f00:1]"} {
				_, err := client.Get("https://" + host + "/resource.zip")
				if !IsPolicyError(err) {
					t.Errorf("host=%s error=%v, want destination policy rejection", host, err)
				}
			}
			if dials.Load() != 0 {
				t.Fatalf("private targets reached the network %d times", dials.Load())
			}
		})
	}
}

type privateDestinationResolver struct{ calls int }

func (resolver *privateDestinationResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	resolver.calls++
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func TestNoProxyStillEnforcesDirectDNSPolicy(t *testing.T) {
	config := httpproxy.Config{HTTPSProxy: "http://127.0.0.1:7890", NoProxy: "archive.invalid"}
	selectProxy := config.ProxyFunc()
	client := NewHTTPClientWithProxy(func(r *http.Request) (*url.URL, error) { return selectProxy(r.URL) })
	t.Cleanup(client.CloseIdleConnections)
	transport := client.Transport.(*proxyTransport)
	resolver := &privateDestinationResolver{}
	transport.direct.DialContext = (&publicDialer{resolver: resolver, dialer: &net.Dialer{}}).DialContext
	var proxyDials atomic.Int32
	transport.proxied.DialContext = func(context.Context, string, string) (net.Conn, error) {
		proxyDials.Add(1)
		return nil, errors.New("unexpected proxy connection")
	}
	_, err := client.Get("https://archive.invalid/resource.zip")
	if !IsPolicyError(err) || resolver.calls != 1 || proxyDials.Load() != 0 {
		t.Fatalf("NO_PROXY lost direct DNS enforcement: lookups=%d proxy_dials=%d error=%v", resolver.calls, proxyDials.Load(), err)
	}
}
