package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeHandlesRequestAndStopsOnCancellation(t *testing.T) {
	listener := newTestListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- serve(ctx, listener, http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusNoContent)
		}), testLogger())
	}()

	response := getEventually(t, "http://"+listener.Addr().String())
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d; want %d", response.StatusCode, http.StatusNoContent)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}

	cancel()
	requireResult(t, result, nil)
}

func TestServeWithCanceledContext(t *testing.T) {
	listener := newTestListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := serve(ctx, listener, http.NotFoundHandler(), testLogger()); err != nil {
		t.Fatalf("serve() error = %v; want nil", err)
	}
}

func TestServeReturnsListenerError(t *testing.T) {
	sentinel := errors.New("sentinel")
	listener := failingListener{err: sentinel}

	err := serve(context.Background(), listener, http.NotFoundHandler(), testLogger())
	if !errors.Is(err, sentinel) {
		t.Fatalf("serve() error = %v; want wrapped sentinel", err)
	}
}

func TestServeWaitsForInFlightRequest(t *testing.T) {
	listener := newTestListener(t)
	started := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	handler := http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		response.WriteHeader(http.StatusNoContent)
	})
	ctx, cancel := context.WithCancel(context.Background())
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- serve(ctx, listener, handler, testLogger())
	}()

	type requestResult struct {
		statusCode int
		err        error
	}
	requestDone := make(chan requestResult, 1)
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+listener.Addr().String(), nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	go func() {
		response, requestErr := http.DefaultClient.Do(request)
		if requestErr != nil {
			requestDone <- requestResult{err: requestErr}

			return
		}
		closeErr := response.Body.Close()
		requestDone <- requestResult{statusCode: response.StatusCode, err: closeErr}
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case err := <-serveResult:
		t.Fatalf("serve returned before in-flight request completed: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case result := <-requestDone:
		if result.err != nil {
			t.Fatalf("request failed: %v", result.err)
		}
		if result.statusCode != http.StatusNoContent {
			t.Fatalf("status = %d; want %d", result.statusCode, http.StatusNoContent)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not finish")
	}
	requireResult(t, serveResult, nil)
}

func TestRunRejectsOccupiedAddressWithoutLeakingIt(t *testing.T) {
	listener := newTestListener(t)

	err := Run(context.Background(), listener.Addr().String(), http.NotFoundHandler(), testLogger())
	if err == nil || !strings.Contains(err.Error(), "listen") {
		t.Fatalf("Run() error = %v; want sanitized listen error", err)
	}
	if strings.Contains(err.Error(), listener.Addr().String()) {
		t.Fatalf("Run() leaked configured address: %v", err)
	}
}

func TestRunStopsWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := Run(ctx, "127.0.0.1:0", http.NotFoundHandler(), testLogger()); err != nil {
		t.Fatalf("Run() error = %v; want nil", err)
	}
}

func TestRunDoesNotListenWithCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listenCalled := false
	listen := func(context.Context, string, string) (net.Listener, error) {
		listenCalled = true

		return nil, errors.New("unexpected listen call")
	}

	if err := runWithListener(ctx, "example.invalid:8080", http.NotFoundHandler(), testLogger(), listen); err != nil {
		t.Fatalf("runWithListener() error = %v; want nil", err)
	}
	if listenCalled {
		t.Fatal("runWithListener() attempted to listen after cancellation")
	}
}

func TestRunCancelsPendingListen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	listen := func(listenContext context.Context, _, _ string) (net.Listener, error) {
		close(started)
		<-listenContext.Done()

		return nil, listenContext.Err()
	}
	result := make(chan error, 1)
	go func() {
		result <- runWithListener(ctx, "example.invalid:8080", http.NotFoundHandler(), testLogger(), listen)
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("listener did not start")
	}
	cancel()
	requireResult(t, result, nil)
}

func TestNormalizeServeError(t *testing.T) {
	if err := normalizeServeError(http.ErrServerClosed); err != nil {
		t.Fatalf("normalizeServeError(http.ErrServerClosed) = %v", err)
	}
	sentinel := errors.New("sentinel")
	if err := normalizeServeError(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("normalizeServeError() = %v; want wrapped sentinel", err)
	}
}

func newTestListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	return listener
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func getEventually(t *testing.T, url string) *http.Response {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		requestCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			t.Fatalf("create request: %v", err)
		}
		response, err := http.DefaultClient.Do(request)
		cancel()
		if err == nil {
			return response
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not accept requests: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func requireResult(t *testing.T, result <-chan error, want error) {
	t.Helper()
	select {
	case err := <-result:
		if !errors.Is(err, want) {
			t.Fatalf("serve() error = %v; want %v", err, want)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not stop")
	}
}

type failingListener struct {
	err error
}

func (listener failingListener) Accept() (net.Conn, error) {
	return nil, listener.err
}

func (failingListener) Close() error {
	return nil
}

func (failingListener) Addr() net.Addr {
	return &net.TCPAddr{}
}

func TestRunStopsBackgroundTasksOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	stopped := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- Run(ctx, "127.0.0.1:0", http.NotFoundHandler(), testLogger(), func(taskCtx context.Context) error {
			close(started)
			<-taskCtx.Done()
			close(stopped)

			return nil
		})
	}()

	<-started
	cancel()
	requireResult(t, result, nil)
	select {
	case <-stopped:
	default:
		t.Fatal("Run() returned before the background task stopped")
	}
}

func TestRunStopsServerWhenBackgroundTaskFails(t *testing.T) {
	sentinel := errors.New("worker failed")
	result := make(chan error, 1)
	go func() {
		result <- Run(context.Background(), "127.0.0.1:0", http.NotFoundHandler(), testLogger(),
			func(context.Context) error { return sentinel },
			func(taskCtx context.Context) error {
				<-taskCtx.Done()

				return nil
			})
	}()

	requireResult(t, result, sentinel)
}
