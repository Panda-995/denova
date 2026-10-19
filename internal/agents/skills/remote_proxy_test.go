package skills

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSkillDownloadUsesConfiguredProxy(t *testing.T) {
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != "archive.invalid:443" {
			t.Errorf("unexpected proxy request: %s %s", r.Method, r.Host)
		}
		connects.Add(1)
		http.Error(w, "fixture proxy unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)
	for _, key := range []string{"HTTPS_PROXY", "https_proxy"} {
		t.Setenv(key, proxy.URL)
	}
	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(key, "")
	}
	client := newSkillInstallHTTPClient()
	client.Timeout = time.Second
	t.Cleanup(client.CloseIdleConnections)
	response, err := client.Get("https://archive.invalid/resource.zip")
	if response != nil {
		response.Body.Close()
	}
	if err == nil || connects.Load() != 1 {
		t.Fatalf("download did not use the configured proxy: connects=%d error=%v", connects.Load(), err)
	}
}

func TestArchiveDownloadsThroughProxyAndRedirects(t *testing.T) {
	archive := makeSkillZip(t, map[string]string{"bundle/skills/proxied/SKILL.md": DefaultContent("proxied", "Downloaded through the configured proxy")})
	connects := useArchiveProxy(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Host + r.URL.Path {
		case "archive.invalid/start":
			http.Redirect(w, r, "https://cdn.invalid/skills.zip", http.StatusFound)
		case "api.github.com/repos/owner/repo":
			fmt.Fprint(w, `{"default_branch":"main"}`)
		case "api.github.com/repos/owner/repo/zipball/main":
			http.Redirect(w, r, "https://codeload.github.com/owner/repo/zip/main", http.StatusFound)
		case "cdn.invalid/skills.zip", "codeload.github.com/owner/repo/zip/main":
			w.Write(archive)
		default:
			t.Errorf("unexpected archive request: %s%s", r.Host, r.URL.Path)
			http.NotFound(w, r)
		}
	})
	data, err := DownloadRemoteArchive(context.Background(), "https://archive.invalid/start")
	if err != nil || !bytes.Equal(data, archive) {
		t.Fatalf("remote ZIP download failed: bytes=%d error=%v", len(data), err)
	}
	preview, err := PreviewRemoteArchive(context.Background(), nil, "", RemoteArchiveSource{URL: "https://archive.invalid/start"})
	if err != nil || !candidateNames(preview.Candidates)["proxied"] {
		t.Fatalf("proxied Skill preview failed: preview=%+v error=%v", preview, err)
	}
	data, err = DownloadGitHubArchive(context.Background(), GitHubRepository{Owner: "owner", Repo: "repo"})
	if err != nil || !bytes.Equal(data, archive) {
		t.Fatalf("GitHub Skill download failed: bytes=%d error=%v", len(data), err)
	}
	var got []string
	for len(connects) > 0 {
		got = append(got, <-connects)
	}
	want := []string{"archive.invalid:443", "cdn.invalid:443", "archive.invalid:443", "cdn.invalid:443", "api.github.com:443", "api.github.com:443", "codeload.github.com:443"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("proxy destinations=%v, want %v", got, want)
	}
}

func TestProxiedArchiveRejectsUnsafeRedirects(t *testing.T) {
	for _, destination := range []string{"https://127.0.0.1/private.zip", "https://localhost/private.zip", "https://10.0.0.1/private.zip", "http://archive.invalid/insecure.zip", "https://user:password@archive.invalid/private.zip"} {
		t.Run(destination, func(t *testing.T) {
			connects := useArchiveProxy(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, destination, http.StatusFound)
			})
			_, err := DownloadRemoteArchive(context.Background(), "https://archive.invalid/start")
			if err == nil || len(connects) != 1 {
				t.Fatalf("unsafe redirect reached a destination: connects=%d error=%v", len(connects), err)
			}
		})
	}
}

// useArchiveProxy serves HTTPS inside real loopback CONNECT tunnels. All target
// names, including .invalid names that cannot resolve locally, terminate here.
// The fixture certificate is trusted only by clients created in this test.
func useArchiveProxy(t *testing.T, handler http.HandlerFunc) <-chan string {
	t.Helper()
	certificateServer := httptest.NewTLSServer(handler)
	serverTLS := certificateServer.TLS.Clone()
	trusted := http.DefaultTransport.(*http.Transport).Clone()
	trusted.TLSClientConfig = certificateServer.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	trusted.TLSClientConfig.ServerName = "example.com"
	certificateServer.Close()
	previousTransport := http.DefaultTransport
	http.DefaultTransport = trusted
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	connects := make(chan string, 20)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("unexpected proxy method: %s", r.Method)
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		connects <- r.Host
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		connection.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := fmt.Fprint(connection, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
			t.Error(err)
			return
		}
		tunnel := tls.Server(connection, serverTLS)
		defer tunnel.Close()
		request, err := http.ReadRequest(bufio.NewReader(tunnel))
		if err != nil {
			t.Error(err)
			return
		}
		defer request.Body.Close()
		if !strings.HasPrefix(r.Host, request.Host+":") {
			t.Errorf("CONNECT host=%s differs from HTTPS host=%s", r.Host, request.Host)
		}
		response := httptest.NewRecorder()
		handler(response, request)
		result := response.Result()
		defer result.Body.Close()
		result.Close = true
		if err := result.Write(tunnel); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(proxy.Close)
	for _, key := range []string{"HTTPS_PROXY", "https_proxy"} {
		t.Setenv(key, proxy.URL)
	}
	for _, key := range []string{"NO_PROXY", "no_proxy"} {
		t.Setenv(key, "")
	}
	previousClient := skillInstallHTTPClient
	client := newSkillInstallHTTPClient()
	client.Timeout = time.Second
	skillInstallHTTPClient = client
	t.Cleanup(func() {
		client.CloseIdleConnections()
		skillInstallHTTPClient = previousClient
	})
	return connects
}
