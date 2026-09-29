package urlpolicy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"syscall"
	"testing"
)

func refusingDialer(t *testing.T) Dialer {
	t.Helper()

	return proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	})
}

func TestProxyStatsCountHTTPRejections(t *testing.T) {
	for _, tt := range []struct {
		name, target string
		resolver     Resolver
		dialer       Dialer
		want         EgressStats
	}{
		{"userinfo", "http://user:secret@target.example/", publicProxyResolver(), nil, EgressStats{Rejected: 1}},
		{"port", "http://target.example:22/", publicProxyResolver(), nil, EgressStats{Rejected: 1}},
		{"private address", "http://target.example/", proxyResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("10.0.0.1")}, nil
		}), nil, EgressStats{Rejected: 1}},
		{"DNS failure", "http://target.example/", proxyResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return nil, errors.New("no such host")
		}), nil, EgressStats{UpstreamFailures: 1}},
		{"connection refused", "http://target.example/", publicProxyResolver(), refusingDialer(t), EgressStats{UpstreamFailures: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dialer := tt.dialer
			if dialer == nil {
				dialer = &net.Dialer{}
			}
			p := startProxy(t, tt.resolver, dialer)
			s := beginProxy(t, p, 1<<20)
			p.serveHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, nil))
			if got := s.Stats(); got != tt.want {
				t.Fatalf("Stats() = %+v; want %+v", got, tt.want)
			}
		})
	}
}

func TestProxyStatsCountCONNECTRejections(t *testing.T) {
	for _, tt := range []struct {
		name, authority string
		dialer          Dialer
		want            EgressStats
	}{
		{"port", "target.example:8443", &net.Dialer{}, EgressStats{Rejected: 1}},
		{"connection refused", "target.example:443", refusingDialer(t), EgressStats{UpstreamFailures: 1}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := startProxy(t, publicProxyResolver(), tt.dialer)
			s := beginProxy(t, p, 1<<20)
			req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Host: tt.authority}, Host: tt.authority, Header: make(http.Header)}
			p.serveHTTP(httptest.NewRecorder(), req.WithContext(t.Context()))
			if got := s.Stats(); got != tt.want {
				t.Fatalf("Stats() = %+v; want %+v", got, tt.want)
			}
		})
	}
}

func TestProxyStatsCountProtocolUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusSwitchingProtocols) }))
	defer upstream.Close()
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}))
	s := beginProxy(t, p, 1<<20)
	p.serveHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://target.example/", nil))
	if got := s.Stats(); got != (EgressStats{Rejected: 1}) {
		t.Fatalf("Stats() = %+v; want one rejection", got)
	}
}

func TestProxyStatsStartEmpty(t *testing.T) {
	p := startProxy(t, publicProxyResolver(), &net.Dialer{})
	if got := beginProxy(t, p, 1<<20).Stats(); got != (EgressStats{}) {
		t.Fatalf("Stats() = %+v; want zero", got)
	}
}

func TestCheckResolvedDistinguishesUnresolvedHosts(t *testing.T) {
	unresolved := CheckResolved(t.Context(), proxyResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return nil, nil
	}), "target.example")
	if !errors.Is(unresolved, ErrNonPublicAddress) || !errors.Is(unresolved, ErrUnresolvedHost) {
		t.Fatalf("CheckResolved() = %v; want an unresolved non-public error", unresolved)
	}
	private := CheckResolved(t.Context(), proxyResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}), "target.example")
	if !errors.Is(private, ErrNonPublicAddress) || errors.Is(private, ErrUnresolvedHost) {
		t.Fatalf("CheckResolved() = %v; want a resolved non-public error", private)
	}
}
