package urlpolicy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProxyHTTPBudgetRetries(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "600000")
		_, _ = io.CopyN(w, strings.NewReader(strings.Repeat("x", 600000)), 600000)
	}))
	defer upstream.Close()
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}))
	s := beginProxy(t, p, 1)
	client := proxyClient(t, s)
	var received int64
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://target.example/retry", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		n, readErr := io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		received += n
		if attempt == 0 && (n != 600000 || readErr != nil) {
			t.Fatalf("first transfer = %d, %v", n, readErr)
		}
		if attempt == 1 && readErr == nil {
			t.Fatal("retry exceeded budget without closing connection")
		}
	}
	if received > 1048577 || !errors.Is(s.Err(), ErrEgressTooLarge) {
		t.Fatalf("budget = %d bytes, %v", received, s.Err())
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://target.example/retry", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatalf("exhausted session emitted another response: %d", resp.StatusCode)
	}
}

func TestProxyHTTPBudgetExactLimit(t *testing.T) {
	// The serialized status and headers occupy 140 bytes; body plus headers is 1 MiB.
	const bodySize = 1048436
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(bodySize))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Date", "Mon, 02 Jan 2006 15:04:05 GMT")
		_, _ = w.Write(bytes.Repeat([]byte{'x'}, bodySize))
	}))
	defer upstream.Close()
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}))
	s := beginProxy(t, p, 0)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://target.example/exact", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := proxyClient(t, s).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if err != nil || n != bodySize || s.Err() != nil {
		t.Fatalf("exact-limit transfer = %d, %v, %v", n, err, s.Err())
	}
	s.mu.Lock()
	remaining := s.remaining
	s.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("serialized headers were not counted exactly: %d bytes remain", remaining)
	}
}

func TestProxyCONNECTBudget(t *testing.T) {
	for _, size := range []int{1048577, 1048578} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
				local, remote := net.Pipe()
				go func() { defer func() { _ = remote.Close() }(); _, _ = remote.Write(bytes.Repeat([]byte{'x'}, size)) }()
				return local, nil
			}))
			s := beginProxy(t, p, 1)
			_, reader, resp := openProxyTunnel(t, s, "target.example:443")
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("CONNECT status = %d", resp.StatusCode)
			}
			n, err := io.Copy(io.Discard, reader)
			if err != nil {
				t.Fatal(err)
			}
			if n > 1048577 {
				t.Fatalf("sent %d bytes past the expanded budget", n)
			}
			if size == 1048577 && (n != 1048577 || s.Err() != nil) {
				t.Fatalf("exact tunnel limit = %d, %v", n, s.Err())
			}
			if size > 1048577 && !errors.Is(s.Err(), ErrEgressTooLarge) {
				t.Fatalf("overflow = %v", s.Err())
			}
		})
	}
}

func TestProxyCONNECTBudgetRetries(t *testing.T) {
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
		local, remote := net.Pipe()
		go func() { defer func() { _ = remote.Close() }(); _, _ = remote.Write(bytes.Repeat([]byte{'x'}, 600000)) }()
		return local, nil
	}))
	s := beginProxy(t, p, 1)
	var total int64
	for attempt := 0; attempt < 2; attempt++ {
		_, reader, resp := openProxyTunnel(t, s, "target.example:443")
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("CONNECT status = %d", resp.StatusCode)
		}
		n, err := io.Copy(io.Discard, reader)
		if err != nil {
			t.Fatal(err)
		}
		total += n
	}
	if total > 1048577 || !errors.Is(s.Err(), ErrEgressTooLarge) {
		t.Fatalf("tunnel retries = %d, %v", total, s.Err())
	}
}

func TestProxyBudgetConcurrentWrites(t *testing.T) {
	p := startProxy(t, publicProxyResolver(), &net.Dialer{})
	s := beginProxy(t, p, 1)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Go(func() { _, _ = (&sessionWriter{session: s, writer: io.Discard}).Write(make([]byte, 200000)) })
	}
	wg.Wait()
	if !errors.Is(s.Err(), ErrEgressTooLarge) {
		t.Fatalf("parallel budget = %v", s.Err())
	}
}

func openProxyTunnel(t *testing.T, s *ProxySession, authority string) (net.Conn, *bufio.Reader, *http.Response) {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp4", strings.TrimPrefix(s.URL(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", authority, authority); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return conn, reader, resp
}

func TestProxyCONNECT(t *testing.T) {
	for _, authority := range []string{"target.example", "target.example:443", "target.example:80"} {
		t.Run(authority, func(t *testing.T) {
			upstreamDone := make(chan struct{})
			p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(_ context.Context, _, address string) (net.Conn, error) {
				want := authority
				if !strings.Contains(want, ":") {
					want += ":443"
				}
				if address != want {
					t.Errorf("CONNECT address = %s; want %s", address, want)
				}
				local, upstream := net.Pipe()
				go func() {
					defer close(upstreamDone)
					defer func() { _ = upstream.Close() }()
					_, _ = io.Copy(upstream, upstream)
				}()
				return local, nil
			}))
			s := beginProxy(t, p, 1<<20)
			conn, reader, resp := openProxyTunnel(t, s, authority)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("CONNECT status = %d", resp.StatusCode)
			}
			if _, err := conn.Write([]byte("opaque request")); err != nil {
				t.Fatal(err)
			}
			body := make([]byte, len("opaque request"))
			if _, err := io.ReadFull(reader, body); err != nil || string(body) != "opaque request" {
				t.Fatalf("tunnel body = %q: %v", body, err)
			}
			_ = s.Close()
			if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
				t.Fatalf("tunnel remains open: %v", err)
			}
			select {
			case <-upstreamDone:
			case <-time.After(time.Second):
				t.Fatal("upstream did not close")
			}
		})
	}
}

func TestProxyCONNECTRejectsDestinations(t *testing.T) {
	for _, authority := range []string{"target.example:8443", "user@target.example:443", "target.example:", "target.example/path", "target.example:abc"} {
		t.Run(authority, func(t *testing.T) {
			p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
				t.Error("rejected authority was dialed")
				return nil, ErrNonPublicAddress
			}))
			beginProxy(t, p, 1<<20)
			req := &http.Request{Method: http.MethodConnect, URL: &url.URL{Host: authority}, Host: authority, Header: make(http.Header)}
			w := httptest.NewRecorder()
			p.serveHTTP(w, req)
			if w.Code != http.StatusBadGateway || strings.Contains(w.Body.String(), "target.example") {
				t.Fatalf("rejection = %d %q", w.Code, w.Body.String())
			}
		})
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, "TUNNEL"} {
		t.Run(method, func(t *testing.T) {
			p := startProxy(t, publicProxyResolver(), &net.Dialer{})
			beginProxy(t, p, 1<<20)
			w := httptest.NewRecorder()
			p.serveHTTP(w, &http.Request{Method: method, URL: &url.URL{Host: "target.example:443"}, Header: make(http.Header)})
			if w.Code != http.StatusBadGateway {
				t.Fatalf("non-CONNECT tunnel status = %d", w.Code)
			}
		})
	}
}

func TestProxyCONNECTRevalidatesRedirects(t *testing.T) {
	resolver := proxyResolverFunc(func(_ context.Context, _, host string) ([]netip.Addr, error) {
		addrs := []netip.Addr{netip.MustParseAddr("93.184.216.34")}
		if host == "redirect.example" {
			addrs = append(addrs, netip.MustParseAddr("10.0.0.1"))
		}
		return addrs, nil
	})
	p := startProxy(t, resolver, proxyDialFunc(func(_ context.Context, _, address string) (net.Conn, error) {
		if address != "target.example:443" {
			t.Errorf("private redirect reached dialer: %s", address)
		}
		local, remote := net.Pipe()
		go func() { defer func() { _ = remote.Close() }(); _, _ = io.Copy(io.Discard, remote) }()
		return local, nil
	}))
	s := beginProxy(t, p, 1<<20)
	conn, _, resp := openProxyTunnel(t, s, "target.example:443")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initial status = %d", resp.StatusCode)
	}
	_ = conn.Close()
	_ = resp.Body.Close()
	_, _, resp = openProxyTunnel(t, s, "redirect.example:443")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("redirect status = %d", resp.StatusCode)
	}
}

func TestProxyCONNECTDialFailureIsPrivate(t *testing.T) {
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
		return nil, fmt.Errorf("target.example/private-path: %w", ErrNonPublicAddress)
	}))
	s := beginProxy(t, p, 1<<20)
	_, _, resp := openProxyTunnel(t, s, "target.example:443")
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusBadGateway || strings.Contains(string(body), "target.example") || strings.Contains(string(body), "private-path") {
		t.Fatalf("dial failure = %d %q %v", resp.StatusCode, body, err)
	}
}

func TestProxyCONNECTClientCancellation(t *testing.T) {
	done := make(chan struct{})
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
		local, remote := net.Pipe()
		go func() { defer close(done); defer func() { _ = remote.Close() }(); _, _ = io.Copy(io.Discard, remote) }()
		return local, nil
	}))
	s := beginProxy(t, p, 1<<20)
	conn, _, resp := openProxyTunnel(t, s, "target.example:443")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d", resp.StatusCode)
	}
	_ = conn.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("client cancellation left upstream open")
	}
}

type proxyResolverFunc func(context.Context, string, string) ([]netip.Addr, error)

func (f proxyResolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error) {
	return f(ctx, network, host)
}

type proxyDialFunc func(context.Context, string, string) (net.Conn, error)

func (f proxyDialFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}
func publicProxyResolver() Resolver {
	return proxyResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	})
}

func proxyClient(t *testing.T, session *ProxySession) *http.Client {
	t.Helper()
	u, err := url.Parse(session.URL())
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 3 * time.Second}
}

func beginProxy(t *testing.T, p *Proxy, maxBytes int64) *ProxySession {
	t.Helper()
	s, err := p.Begin(t.Context(), maxBytes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestProxyHTTPForwarding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/private-path" || r.Host != "target.example" {
			t.Errorf("wrong destination: %s %s", r.URL.Path, r.Host)
		}
		for _, key := range []string{"Proxy-Authorization", "Proxy-Connection", "X-Remove", "Upgrade"} {
			if r.Header.Get(key) != "" {
				t.Errorf("forwarded hop header %s", key)
			}
		}
		w.Header().Set("X-Upstream", "forwarded")
		w.Header().Set("Connection", "X-Remove-Response")
		w.Header().Set("X-Remove-Response", "private")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(append([]byte("response:"), body...))
	}))
	defer upstream.Close()
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "target.example:80" {
			t.Errorf("dial address = %q", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}))
	s := beginProxy(t, p, 1<<20)
	client := proxyClient(t, s)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut} {
		req, err := http.NewRequestWithContext(t.Context(), method, "http://target.example/private-path", strings.NewReader("payload"))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Connection", "X-Remove, Upgrade")
		req.Header.Set("X-Remove", "secret")
		req.Header.Set("Proxy-Authorization", "secret")
		req.Header.Set("Proxy-Connection", "keep-alive")
		req.Header.Set("Upgrade", "websocket")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || string(body) != "response:payload" || resp.StatusCode != http.StatusCreated || resp.Header.Get("X-Upstream") != "forwarded" || resp.Header.Get("X-Remove-Response") != "" {
			t.Fatalf("forwarded response = %d %q %v, %v", resp.StatusCode, body, resp.Header, err)
		}
	}
}

func TestProxyHTTPRejectsDestinations(t *testing.T) {
	for _, tt := range []struct {
		name, target string
		resolver     Resolver
	}{
		{"userinfo", "http://user:secret@target.example/private-path", publicProxyResolver()},
		{"scheme", "ftp://target.example/private-path", publicProxyResolver()},
		{"port", "http://target.example:22/private-path", publicProxyResolver()},
		{"empty authority", "http:///private-path", publicProxyResolver()},
		{"bad authority", "http://target.example:/private-path", publicProxyResolver()},
		{"DNS failure", "http://target.example/private-path", proxyResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return nil, errors.New("target.example/private-path")
		})},
		{"no addresses", "http://target.example/private-path", proxyResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) { return nil, nil })},
		{"mixed addresses", "http://target.example/private-path", proxyResolverFunc(func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("127.0.0.1")}, nil
		})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := startProxy(t, tt.resolver, proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
				t.Error("invalid target reached dialer")
				return nil, errors.New("unexpected dial")
			}))
			beginProxy(t, p, 1<<20)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.target, nil)
			w := httptest.NewRecorder()
			p.serveHTTP(w, req)
			if w.Code != http.StatusBadGateway {
				t.Fatalf("status = %d; want 502", w.Code)
			}
			if strings.Contains(w.Body.String(), "target.example") || strings.Contains(w.Body.String(), "private-path") {
				t.Fatalf("leaked target: %s", w.Body.String())
			}
		})
	}
}

func TestProxyHTTPBoundsResponseHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Large", strings.Repeat("a", 65<<10))
		_, _ = w.Write([]byte("body"))
	}))
	defer upstream.Close()
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}))
	s := beginProxy(t, p, 1<<20)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://target.example/private-path", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := proxyClient(t, s).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusBadGateway || strings.Contains(string(body), "target.example") || strings.Contains(string(body), "private-path") {
		t.Fatalf("header failure = %d %q %v", resp.StatusCode, body, err)
	}
}

func TestEgressBudget(t *testing.T) {
	for _, tt := range []struct{ input, want int64 }{{1 << 20, 2 << 20}, {100 << 20, 105 << 20}} {
		if got := EgressBudget(tt.input); got != tt.want {
			t.Fatalf("EgressBudget(%d) = %d; want %d", tt.input, got, tt.want)
		}
	}
}

func TestProxyProductionDialerRejectsRebinding(t *testing.T) {
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	dialResult := make(chan error, 1)
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
		conn, err := (&net.Dialer{Control: DialControl, Timeout: time.Second}).DialContext(ctx, network, listener.Addr().String())
		dialResult <- err
		return conn, err
	}))
	s := beginProxy(t, p, 1)
	_, _, resp := openProxyTunnel(t, s, "target.example:443")
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("rebinding status = %d", resp.StatusCode)
	}
	if err := <-dialResult; !errors.Is(err, ErrNonPublicAddress) {
		t.Fatalf("connect-time check = %v", err)
	}
}

func TestProxyRunFailureClosesSession(t *testing.T) {
	p, err := NewProxy(publicProxyResolver(), &net.Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	s := beginProxy(t, p, 1)
	local, remote := net.Pipe()
	defer func() { _ = remote.Close() }()
	s.track(local)
	_ = p.listener.Close()
	if err := p.Run(t.Context()); err == nil {
		t.Fatal("listener failure was lost")
	}
	_ = remote.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := remote.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("failed server left session open: %v", err)
	}
	if _, err := p.Begin(t.Context(), 1); err == nil {
		t.Fatal("stopped proxy accepted session")
	}
}

func TestProxyHTTPAccountsSerializedResponse(t *testing.T) {
	for _, fixture := range []string{
		"HTTP/1.1 200 OK\r\nConnection: close\r\n\r\nbody",
		"HTTP/1.1 299 Custom\r\nContent-Length: 4\r\n\r\nbody",
		"HTTP/1.1 200 OK\r\nContent-Length: 4\r\nConnection: Content-Length\r\n\r\nbody",
	} {
		t.Run(strings.Split(fixture, "\r\n")[0]+strconv.Itoa(len(fixture)), func(t *testing.T) {
			p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
				local, remote := net.Pipe()
				go func() {
					defer func() { _ = remote.Close() }()
					req, err := http.ReadRequest(bufio.NewReader(remote))
					if err != nil {
						return
					}
					_ = req.Body.Close()
					_, _ = io.WriteString(remote, fixture)
				}()
				return local, nil
			}))
			s := beginProxy(t, p, 1)
			conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp4", strings.TrimPrefix(s.URL(), "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			_, err = io.WriteString(conn, "GET http://target.example/path HTTP/1.1\r\nHost: target.example\r\n\r\n")
			if err != nil {
				t.Fatal(err)
			}
			wire, err := io.ReadAll(conn)
			if err != nil || !bytes.HasSuffix(wire, []byte("\r\n\r\nbody")) {
				t.Fatalf("wire response = %q, %v", wire, err)
			}
			s.mu.Lock()
			remaining := s.remaining
			s.mu.Unlock()
			if int64(len(wire)) != 1048577-remaining {
				t.Fatalf("charged %d, sent %d: %q", 1048577-remaining, len(wire), wire)
			}
		})
	}
}

func rawProxyResponse(t *testing.T, s *ProxySession, request string) []byte {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp4", strings.TrimPrefix(s.URL(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.WriteString(conn, request); err != nil {
		t.Fatal(err)
	}
	wire, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	return wire
}

func rawResponseDialer(response string) Dialer {
	return proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
		local, remote := net.Pipe()
		go func() {
			defer func() { _ = remote.Close() }()
			req, err := http.ReadRequest(bufio.NewReader(remote))
			if err != nil {
				return
			}
			_, err = io.Copy(io.Discard, req.Body)
			_ = req.Body.Close()
			if err != nil {
				return
			}
			_, _ = io.WriteString(remote, response)
		}()
		return local, nil
	})
}

func TestProxyHTTPContinueWireAccounting(t *testing.T) {
	const request = "POST http://target.example/path HTTP/1.1\r\nHost: target.example\r\nContent-Length: 4\r\nExpect: 100-continue\r\n\r\ndata"
	const want = "HTTP/1.1 100 Continue\r\n\r\nHTTP/1.1 200 OK\r\nConnection: close\r\nContent-Length: 4\r\n\r\nbody"
	for _, limit := range []int64{86, 85, 24} {
		t.Run(strconv.FormatInt(limit, 10), func(t *testing.T) {
			p := startProxy(t, publicProxyResolver(), rawResponseDialer("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nbody"))
			s := beginProxy(t, p, 1)
			if err := s.charge(1048577 - limit); err != nil {
				t.Fatal(err)
			}
			wire := rawProxyResponse(t, s, request)
			s.mu.Lock()
			charged := limit - s.remaining
			s.mu.Unlock()
			if int64(len(wire)) > limit || charged != int64(len(wire)) {
				t.Fatalf("limit %d: emitted %d bytes, charged %d: %q", limit, len(wire), charged, wire)
			}
			if limit == 86 && (string(wire) != want || s.Err() != nil) {
				t.Fatalf("exact-limit response = %q, %v", wire, s.Err())
			}
			if limit < 86 && !errors.Is(s.Err(), ErrEgressTooLarge) {
				t.Fatalf("over-budget response did not fail: %v", s.Err())
			}
		})
	}
}

func TestProxyHTTPNotModifiedWireAccounting(t *testing.T) {
	const want = "HTTP/1.1 304 Not Modified\r\nConnection: close\r\n\r\n"
	for _, limit := range []int64{100, 48, 47} {
		t.Run(strconv.FormatInt(limit, 10), func(t *testing.T) {
			p := startProxy(t, publicProxyResolver(), rawResponseDialer("HTTP/1.1 304 Not Modified\r\nContent-Type: text/plain\r\n\r\n"))
			s := beginProxy(t, p, 1)
			if err := s.charge(1048577 - limit); err != nil {
				t.Fatal(err)
			}
			wire := rawProxyResponse(t, s, "GET http://target.example/path HTTP/1.1\r\nHost: target.example\r\n\r\n")
			s.mu.Lock()
			charged := limit - s.remaining
			s.mu.Unlock()
			if charged != int64(len(wire)) {
				t.Fatalf("emitted %d bytes, charged %d: %q", len(wire), charged, wire)
			}
			if limit >= 48 && (string(wire) != want || s.Err() != nil) {
				t.Fatalf("fitting response rejected: %q, %v", wire, s.Err())
			}
			if limit < 48 && (len(wire) != 0 || !errors.Is(s.Err(), ErrEgressTooLarge)) {
				t.Fatalf("oversize headers = %q, %v", wire, s.Err())
			}
		})
	}
}

func TestProxyAutomaticResponsesUseSessionBudget(t *testing.T) {
	for _, tt := range []struct{ name, request, status string }{
		{"malformed", "INVALID\r\n\r\n", "HTTP/1.1 400 Bad Request\r\n"},
		{"unsupported expectation", "POST http://target.example/path HTTP/1.1\r\nHost: target.example\r\nExpect: unsupported\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", "HTTP/1.1 417 Expectation Failed\r\n"},
		{"general options", "OPTIONS * HTTP/1.1\r\nHost: target.example\r\nConnection: close\r\n\r\n", "HTTP/1.1 200 OK\r\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, budget := range []int64{1048577, 0} {
				t.Run(strconv.FormatInt(budget, 10), func(t *testing.T) {
					p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
						t.Error("automatic response dialed upstream")
						return nil, ErrNonPublicAddress
					}))
					s := beginProxy(t, p, 1)
					if err := s.charge(1048577 - budget); err != nil {
						t.Fatal(err)
					}
					wire := rawProxyResponse(t, s, tt.request)
					s.mu.Lock()
					charged := budget - s.remaining
					tracked := len(s.conns)
					s.mu.Unlock()
					if tracked != 0 {
						t.Fatalf("closed automatic-response connection remains tracked: %d", tracked)
					}
					if int64(len(wire)) > budget || charged != int64(len(wire)) {
						t.Fatalf("automatic response emitted %d bytes, charged %d, budget %d: %q", len(wire), charged, budget, wire)
					}
					if budget > 0 && !strings.HasPrefix(string(wire), tt.status) {
						t.Fatalf("automatic response = %q", wire)
					}
					if budget == 0 && !errors.Is(s.Err(), ErrEgressTooLarge) {
						t.Fatalf("automatic response overflow = %v", s.Err())
					}
				})
			}
		})
	}
}

func TestProxyAutomaticOptionsConnectionClosesWithSession(t *testing.T) {
	p := startProxy(t, publicProxyResolver(), &net.Dialer{})
	s := beginProxy(t, p, 1)
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp4", strings.TrimPrefix(s.URL(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(conn, "OPTIONS * HTTP/1.1\r\nHost: target.example\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodOptions})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("OPTIONS status = %d", resp.StatusCode)
	}
	_ = s.Close()
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("OPTIONS connection remained open after Close: %v", err)
	}
}

func TestProxyUnboundConnectionCannotOutliveBegin(t *testing.T) {
	p, err := NewProxy(publicProxyResolver(), &net.Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.listener.Close() }()
	client, err := (&net.Dialer{}).DialContext(t.Context(), "tcp4", p.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	accepted, err := p.listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accepted.Close() }()
	beginProxy(t, p, 1)
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("unbound connection survived Begin: %v", err)
	}
}

func TestProxyHTTPRejectsProtocolUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusSwitchingProtocols) }))
	defer upstream.Close()
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}))
	s := beginProxy(t, p, 1)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://target.example/path", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := proxyClient(t, s).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("upgrade status = %d", resp.StatusCode)
	}
}

func TestProxyRejectsMalformedAuthority(t *testing.T) {
	for _, authority := range []string{"[not-ip]:443", "a:b:c:80", "[::1]junk:443", "[2001:4860::8888%25eth0]:443"} {
		if _, err := proxyAddress(authority, "443"); err == nil {
			t.Errorf("accepted malformed authority %q", authority)
		}
	}
}

func TestProxyHeaderBudgetAbortsBeforeResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("payload")) }))
	defer upstream.Close()
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, upstream.Listener.Addr().String())
	}))
	s := beginProxy(t, p, 1)
	if _, err := (&sessionWriter{session: s, writer: io.Discard}).Write(make([]byte, 1048577)); err != nil {
		t.Fatal(err)
	}
	wire := rawProxyResponse(t, s, "GET http://target.example/path HTTP/1.1\r\nHost: target.example\r\n\r\n")
	if len(wire) != 0 || !errors.Is(s.Err(), ErrEgressTooLarge) {
		t.Fatalf("exhausted header did not abort response: %q, %v", wire, s.Err())
	}
}

func TestProxyListenerFailureIsPrivate(t *testing.T) {
	_, err := newProxy(publicProxyResolver(), &net.Dialer{}, func(_ context.Context, network, address string) (net.Listener, error) {
		if network != "tcp4" || address != "127.0.0.1:0" {
			t.Errorf("listener bind = %s %s", network, address)
		}
		return nil, errors.New("private listener diagnostics")
	})
	if err == nil || strings.Contains(err.Error(), "private listener diagnostics") {
		t.Fatalf("constructor error = %v", err)
	}
}

func TestProxyDialGuards(t *testing.T) {
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
		t.Error("invalid address reached dialer")
		return nil, ErrNonPublicAddress
	}))
	s := beginProxy(t, p, 1)
	for _, address := range []string{"target.example", "target.example:22"} {
		if _, err := p.dial(t.Context(), s, "tcp", address); !errors.Is(err, ErrNonPublicAddress) {
			t.Fatalf("dial(%q) = %v", address, err)
		}
	}
}

func TestProxyCancellationDuringDial(t *testing.T) {
	local, remote := net.Pipe()
	defer func() { _ = remote.Close() }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) { cancel(); return local, nil }))
	s, err := p.Begin(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, err := p.dial(t.Context(), s, "tcp", "target.example:443"); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("canceled dial = %v", err)
	}
	_ = remote.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := remote.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("late dial remains open: %v", err)
	}
	if _, err := (&sessionWriter{session: s, writer: io.Discard}).Write([]byte("late")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled write = %v", err)
	}
}

func TestProxyCancellationBeforeAccept(t *testing.T) {
	p, err := NewProxy(publicProxyResolver(), &net.Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.listener.Close() }()
	s := beginProxy(t, p, 1)
	client, err := (&net.Dialer{}).DialContext(t.Context(), "tcp4", p.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	s.cancel()
	accepted, err := p.listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = accepted.Close() }()
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("canceled client remains open: %v", err)
	}
}

func TestProxyHTTPSDefaultPort(t *testing.T) {
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(_ context.Context, _, address string) (net.Conn, error) {
		if address != "target.example:443" {
			t.Errorf("HTTPS destination = %q", address)
		}
		return nil, ErrNonPublicAddress
	}))
	beginProxy(t, p, 1)
	w := httptest.NewRecorder()
	p.serveHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://target.example/path", nil))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("failed HTTPS status = %d", w.Code)
	}
}

type proxyHijacker struct {
	*httptest.ResponseRecorder
	hijack func() (net.Conn, *bufio.ReadWriter, error)
}

func (w proxyHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) { return w.hijack() }

func TestProxyCONNECTHijackFailuresCloseUpstream(t *testing.T) {
	for _, failure := range []string{"unsupported", "canceled", "handshake"} {
		t.Run(failure, func(t *testing.T) {
			local, remote := net.Pipe()
			defer func() { _ = remote.Close() }()
			p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) { return local, nil }))
			s := beginProxy(t, p, 1)
			recorder := httptest.NewRecorder()
			var w http.ResponseWriter = recorder
			if failure != "unsupported" {
				client, peer := net.Pipe()
				_ = peer.Close()
				w = proxyHijacker{ResponseRecorder: recorder, hijack: func() (net.Conn, *bufio.ReadWriter, error) {
					if failure == "canceled" {
						s.cancel()
					}
					return client, bufio.NewReadWriter(bufio.NewReader(client), bufio.NewWriter(client)), nil
				}}
			}
			p.serveHTTP(w, &http.Request{Method: http.MethodConnect, URL: &url.URL{Host: "target.example:443"}, Header: make(http.Header)})
			if failure == "unsupported" && recorder.Code != http.StatusBadGateway {
				t.Fatalf("hijack failure = %d", recorder.Code)
			}
			_ = remote.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := remote.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
				t.Fatalf("upstream remained open: %v", err)
			}
		})
	}
}

func TestProxyCONNECTBufferedUploadIsNotCharged(t *testing.T) {
	p := startProxy(t, publicProxyResolver(), proxyDialFunc(func(context.Context, string, string) (net.Conn, error) {
		local, remote := net.Pipe()
		go func() {
			defer func() { _ = remote.Close() }()
			if n, err := io.CopyN(io.Discard, remote, 2<<20); err != nil || n != 2<<20 {
				return
			}
			_, _ = remote.Write([]byte("ok"))
		}()
		return local, nil
	}))
	s := beginProxy(t, p, 1)
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp4", strings.TrimPrefix(s.URL(), "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, err = io.WriteString(conn, "CONNECT target.example HTTP/1.1\r\nHost: target.example\r\n\r\nfirst")
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT status = %d", resp.StatusCode)
	}
	if _, err := conn.Write(make([]byte, (2<<20)-5)); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(reader)
	if err != nil || string(body) != "ok" || s.Err() != nil {
		t.Fatalf("upload = %q, %v, %v", body, err, s.Err())
	}
	s.mu.Lock()
	remaining := s.remaining
	s.mu.Unlock()
	if remaining != 1048575 {
		t.Fatalf("upstream request bytes were charged: %d", remaining)
	}
}

func startProxy(t *testing.T, resolver Resolver, dialer Dialer) *Proxy {
	t.Helper()
	p, err := NewProxy(resolver, dialer)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() = %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("Run did not stop")
		}
	})
	return p
}

func TestProxyLifecycle(t *testing.T) {
	p := startProxy(t, net.DefaultResolver, &net.Dialer{})
	s, err := p.Begin(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s.URL(), "http://127.0.0.1:") {
		t.Fatalf("URL() = %q", s.URL())
	}
	if _, err := p.Begin(t.Context(), 1); !errors.Is(err, ErrProxyBusy) {
		t.Fatalf("second Begin = %v", err)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
	upstream, remote := net.Pipe()
	defer func() { _ = remote.Close() }()
	if !s.track(upstream) {
		t.Fatal("session rejected connection")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_ = remote.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := remote.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("session connection remains open: %v", err)
	}
	client := &http.Client{Timeout: time.Second}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, s.URL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("inactive status = %d", resp.StatusCode)
	}
	next, err := p.Begin(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := p.Begin(t.Context(), 1); !errors.Is(err, ErrProxyBusy) {
		t.Fatalf("old Close released new session: %v", err)
	}
	_ = next.Close()
}
