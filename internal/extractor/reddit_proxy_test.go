package extractor

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

type redditResolver struct{}

func (redditResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	if host == "private.reddit.com" {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
}

type redditDialer func(context.Context, string, string) (net.Conn, error)

func (f redditDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

func TestRedditResolutionThroughProxy(t *testing.T) {
	for _, scenario := range []string{"success", "private", "headers", "budget", "untrusted"} {
		t.Run(scenario, func(t *testing.T) {
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				location := "/comments/abc123/"
				if scenario == "private" {
					location = "https://private.reddit.com/r/example/s/Ab12Cd34Ef/"
				}
				if scenario == "headers" {
					w.Header().Set("X-Padding", strings.Repeat("x", 65<<10))
				}
				w.Header().Set("Location", location)
				w.WriteHeader(http.StatusFound)
			}))
			defer upstream.Close()
			// Header/certificate failures are expected; discard only this fixture's logs.
			upstream.Config.ErrorLog = log.New(io.Discard, "", 0)
			warmup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, strings.Repeat("x", (1<<20)-512))
			}))
			defer warmup.Close()
			var dials atomic.Int32
			proxy, err := urlpolicy.NewProxy(redditResolver{}, redditDialer(func(ctx context.Context, network, address string) (net.Conn, error) {
				dials.Add(1)
				if strings.HasPrefix(address, "private.") {
					t.Error("private target reached dialer")
				}
				target := upstream.Listener.Addr().String()
				if address == "warmup.example:80" {
					target = warmup.Listener.Addr().String()
				}
				return (&net.Dialer{}).DialContext(ctx, network, target)
			}))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)
			go func() { done <- proxy.Run(ctx) }()
			defer func() {
				cancel()
				if err := <-done; err != nil {
					t.Error(err)
				}
			}()
			session, err := proxy.Begin(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = session.Close() }()
			proxyURL, _ := url.Parse(session.URL())
			transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSClientConfig: upstream.Client().Transport.(*http.Transport).TLSClientConfig.Clone(), MaxResponseHeaderBytes: 64 << 10}
			transport.TLSClientConfig.ServerName = "example.com"
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: time.Second}
			if scenario == "budget" {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://warmup.example/body", nil)
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			var got string
			if scenario == "untrusted" {
				got, err = resolveRedditURL(ctx, testRedditShare, session.URL(), redditPolicy(t))
			} else {
				got, err = resolveRedditRedirects(ctx, testRedditShare, redditPolicy(t), client)
			}
			if scenario == "success" {
				if err != nil || got != "https://www.reddit.com/comments/abc123/" {
					t.Fatalf("resolved = %q, %v", got, err)
				}
			} else {
				_ = requireFailure(t, err, job.DetailNetworkError, "reddit")
			}
			if scenario == "private" && (session.Stats().Rejected != 1 || dials.Load() != 1) {
				t.Fatalf("private destination not rejected: %+v; dials %d", session.Stats(), dials.Load())
			}
			if scenario == "budget" && !errors.Is(session.Err(), urlpolicy.ErrEgressTooLarge) {
				t.Fatalf("budget = %v", session.Err())
			}
			_ = session.Close()
			next, err := proxy.Begin(ctx, 1)
			if err != nil {
				t.Fatal("session did not release proxy")
			}
			_ = next.Close()
		})
	}
}
