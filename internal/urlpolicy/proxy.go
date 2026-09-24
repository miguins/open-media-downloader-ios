package urlpolicy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrProxyBusy reports an extraction session already holding the proxy.
var ErrProxyBusy = errors.New("egress proxy is busy")

// ErrEgressTooLarge reports an exhausted extraction transfer budget.
var ErrEgressTooLarge = errors.New("egress budget exceeded")

// Dialer opens upstream connections. Production dialers must use DialControl.
type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// Proxy owns a loopback listener and one extraction session at a time.
type Proxy struct {
	listener net.Listener
	server   *http.Server
	resolver Resolver
	dialer   Dialer
	mu       sync.Mutex
	active   *ProxySession
	stopped  bool
}

// NewProxy binds an ephemeral IPv4 loopback listener.
func NewProxy(resolver Resolver, dialer Dialer) (*Proxy, error) {
	return newProxy(resolver, dialer, (&net.ListenConfig{}).Listen)
}

func newProxy(resolver Resolver, dialer Dialer, listen func(context.Context, string, string) (net.Listener, error)) (*Proxy, error) {
	listener, err := listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("egress proxy listener unavailable")
	}
	p := &Proxy{listener: listener, resolver: resolver, dialer: dialer}
	p.server = &http.Server{
		Handler:           http.HandlerFunc(p.serveHTTP),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          log.New(io.Discard, "", 0),
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			return context.WithValue(ctx, proxyConnKey{}, conn)
		},
	}
	return p, nil
}

// EgressBudget allows protocol and metadata overhead above the output limit.
func EgressBudget(maxJobBytes int64) int64 {
	margin := maxJobBytes / 20
	if margin < 1<<20 {
		margin = 1 << 20
	}
	return maxJobBytes + margin
}

// Run serves requests until cancellation and closes all session connections.
func (p *Proxy) Run(ctx context.Context) error {
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		p.shutdown()
		close(done)
	})
	err := p.server.Serve(p.listener)
	if stop() {
		p.shutdown()
	} else {
		<-done
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (p *Proxy) shutdown() {
	p.mu.Lock()
	p.stopped = true
	s := p.active
	p.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = p.server.Shutdown(shutdownCtx)
	_ = p.server.Close()
}

// Begin acquires the proxy for one job; maxBytes is the raw output-byte limit.
func (p *Proxy) Begin(ctx context.Context, maxBytes int64) (*ProxySession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return nil, errors.New("egress proxy stopped")
	}
	if p.active != nil {
		return nil, ErrProxyBusy
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	s := &ProxySession{proxy: p, ctx: sessionCtx, cancel: cancel, conns: make(map[net.Conn]struct{}), remaining: EgressBudget(maxBytes)}
	p.active = s
	context.AfterFunc(sessionCtx, s.closeConnections)
	return s, nil
}

// ProxySession owns one job's connections and transfer allowance.
type ProxySession struct {
	proxy     *Proxy
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	conns     map[net.Conn]struct{}
	remaining int64
	err       error
}

// URL returns the loopback proxy URL passed to the extractor.
func (s *ProxySession) URL() string { return "http://" + s.proxy.listener.Addr().String() }

// Err reports a session-level transfer failure.
func (s *ProxySession) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close cancels this session, closes connections, and releases its proxy slot.
func (s *ProxySession) Close() error {
	s.cancel()
	s.closeConnections()
	s.proxy.mu.Lock()
	if s.proxy.active == s {
		s.proxy.active = nil
	}
	s.proxy.mu.Unlock()
	return nil
}

func (s *ProxySession) closeConnections() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for conn := range s.conns {
		_ = conn.Close()
		delete(s.conns, conn)
	}
}

func (s *ProxySession) track(conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		_ = conn.Close()
		return false
	}
	s.conns[conn] = struct{}{}
	return true
}

type proxyConnKey struct{}

func (s *ProxySession) untrack(conn net.Conn) {
	s.mu.Lock()
	delete(s.conns, conn)
	s.mu.Unlock()
}

func (p *Proxy) serveHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	s := p.active
	p.mu.Unlock()
	if s == nil || s.ctx.Err() != nil {
		http.Error(w, "egress unavailable", http.StatusServiceUnavailable)
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	if conn, ok := r.Context().Value(proxyConnKey{}).(net.Conn); ok {
		if !s.track(conn) {
			return
		}
		defer s.untrack(conn)
	}
	if r.Method == http.MethodConnect {
		p.connect(w, r.WithContext(ctx), s)
		return
	}
	if r.URL.User != nil || r.URL.Opaque != "" || r.URL.Fragment != "" || (r.URL.Scheme != "http" && r.URL.Scheme != "https") {
		http.Error(w, "egress request rejected", http.StatusBadGateway)
		return
	}
	port := "80"
	if r.URL.Scheme == "https" {
		port = "443"
	}
	if _, err := proxyAddress(r.URL.Host, port); err != nil {
		http.Error(w, "egress request rejected", http.StatusBadGateway)
		return
	}
	p.forwardHTTP(w, r.WithContext(ctx), s)
}

func proxyAddress(authority, defaultPort string) (string, error) {
	u, err := url.Parse("//" + authority)
	if err != nil || u.Host != authority || u.User != nil || u.Hostname() == "" || strings.HasSuffix(authority, ":") {
		return "", ErrNonPublicAddress
	}
	port := u.Port()
	if port == "" {
		port = defaultPort
	}
	if port != "80" && port != "443" {
		return "", ErrNonPublicAddress
	}
	if u.Port() != "" {
		if _, _, err := net.SplitHostPort(authority); err != nil {
			return "", ErrNonPublicAddress
		}
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}

func (p *Proxy) dial(ctx context.Context, s *ProxySession, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || (port != "80" && port != "443") {
		return nil, ErrNonPublicAddress
	}
	if err := CheckResolved(ctx, p.resolver, host); err != nil {
		return nil, err
	}
	conn, err := p.dialer.DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	if !s.track(conn) {
		return nil, net.ErrClosed
	}
	return &proxyConn{Conn: conn, session: s}, nil
}

type proxyConn struct {
	net.Conn
	session *ProxySession
}

func (c *proxyConn) Close() error {
	c.session.untrack(c.Conn)
	return c.Conn.Close()
}

func removeProxyHeaders(header http.Header) {
	for _, value := range header.Values("Connection") {
		for key := range strings.SplitSeq(value, ",") {
			header.Del(strings.TrimSpace(key))
		}
	}
	for _, key := range []string{"Connection", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(key)
	}
}

func (p *Proxy) forwardHTTP(w http.ResponseWriter, r *http.Request, s *ProxySession) {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return p.dial(ctx, s, network, address)
		},
		DisableCompression:     true,
		MaxResponseHeaderBytes: 64 << 10,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  30 * time.Second,
	}
	defer transport.CloseIdleConnections()
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Host = out.URL.Host
	removeProxyHeaders(out.Header)
	resp, err := transport.RoundTrip(out)
	if err != nil {
		http.Error(w, "egress upstream unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		http.Error(w, "egress request rejected", http.StatusBadGateway)
		return
	}
	removeProxyHeaders(resp.Header)
	for key, values := range resp.Header {
		w.Header()[key] = values
	}
	// Suppress automatic header additions so accounting matches serialization.
	if _, ok := w.Header()["Date"]; !ok {
		w.Header()["Date"] = nil
	}
	if _, ok := w.Header()["Content-Type"]; !ok {
		w.Header()["Content-Type"] = nil
	}
	w.Header().Set("Connection", "close")
	var header bytes.Buffer
	status := http.StatusText(resp.StatusCode)
	if status == "" {
		status = "status code " + strconv.Itoa(resp.StatusCode)
	}
	_, _ = fmt.Fprintf(&header, "HTTP/1.1 %d %s\r\n", resp.StatusCode, status)
	_ = w.Header().Write(&header)
	header.WriteString("\r\n")
	if _, err := (&sessionWriter{session: s, writer: io.Discard}).Write(header.Bytes()); err != nil {
		panic(http.ErrAbortHandler)
	}
	// A close-delimited body avoids implicit chunk framing or content-length additions.
	if w.Header().Get("Content-Length") == "" {
		w.Header().Set("Transfer-Encoding", "identity")
	}
	w.WriteHeader(resp.StatusCode)
	_ = http.NewResponseController(w).Flush()
	if _, err := io.Copy(&sessionWriter{session: s, writer: w}, resp.Body); err != nil {
		panic(http.ErrAbortHandler)
	}
}

type sessionWriter struct {
	session *ProxySession
	writer  io.Writer
}

func (p *Proxy) connect(w http.ResponseWriter, r *http.Request, s *ProxySession) {
	address, err := proxyAddress(r.URL.Host, "443")
	if err != nil || r.URL.User != nil || r.URL.Scheme != "" || r.URL.Path != "" || r.URL.RawQuery != "" || r.URL.Fragment != "" || r.URL.Opaque != "" {
		http.Error(w, "egress request rejected", http.StatusBadGateway)
		return
	}
	upstream, err := p.dial(r.Context(), s, "tcp", address)
	if err != nil {
		http.Error(w, "egress upstream unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstream.Close() }()
	client, buffered, err := http.NewResponseController(w).Hijack()
	if err != nil {
		http.Error(w, "egress tunnel unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = client.Close() }()
	if !s.track(client) {
		return
	}
	defer s.untrack(client)
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(&sessionWriter{session: s, writer: client}, upstream)
		_ = client.Close()
		_ = upstream.Close()
	}()
	_, _ = io.Copy(upstream, buffered.Reader)
	_ = upstream.Close()
	_ = client.Close()
	<-done
}

func (w *sessionWriter) Write(data []byte) (int, error) {
	if err := w.session.charge(int64(len(data))); err != nil {
		return 0, err
	}
	return w.writer.Write(data)
}

func (s *ProxySession) charge(size int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if size > s.remaining {
		s.err = ErrEgressTooLarge
		s.cancel()
		return s.err
	}
	s.remaining -= size
	return nil
}
