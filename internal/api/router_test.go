package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/readiness"
)

func newTestRouter(logs io.Writer, checks ...readiness.Check) http.Handler {
	return NewRouter(readiness.New(checks...), slog.New(slog.NewJSONHandler(logs, nil)))
}

func TestHealthz(t *testing.T) {
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	newTestRouter(io.Discard).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got, want := response.Body.String(), "{\"status\":\"ok\"}\n"; got != want {
		t.Fatalf("body = %q; want %q", got, want)
	}

	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body) != 1 || body["status"] != "ok" {
		t.Fatalf("response disclosed unexpected fields: %#v", body)
	}
}

func TestRouterRejectsUnsupportedRequests(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{name: "wrong method", method: http.MethodPost, path: "/healthz", wantStatus: http.StatusMethodNotAllowed},
		{name: "unknown path", method: http.MethodGet, path: "/unknown", wantStatus: http.StatusNotFound},
		{name: "wrong readiness method", method: http.MethodPost, path: "/readyz", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(context.Background(), tt.method, tt.path, nil)
			response := httptest.NewRecorder()

			newTestRouter(io.Discard).ServeHTTP(response, request)

			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d; want %d", response.Code, tt.wantStatus)
			}
			if response.Body.String() == "{\"status\":\"ok\"}\n" {
				t.Fatal("unsupported request returned the health response")
			}
		})
	}
}

func TestReadyz(t *testing.T) {
	passing := readiness.Check{Name: "database", Run: func(context.Context) error { return nil }}
	failing := readiness.Check{Name: "storage", Run: func(context.Context) error {
		return errors.New("open /data/private/path: permission denied")
	}}
	tests := []struct {
		name       string
		checks     []readiness.Check
		wantStatus int
		wantBody   string
	}{
		{name: "ready", checks: []readiness.Check{passing}, wantStatus: http.StatusOK, wantBody: "{\"status\":\"ready\"}\n"},
		{name: "not ready", checks: []readiness.Check{passing, failing}, wantStatus: http.StatusServiceUnavailable, wantBody: "{\"status\":\"not_ready\"}\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &bytes.Buffer{}
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
			response := httptest.NewRecorder()

			newTestRouter(logs, tt.checks...).ServeHTTP(response, request)

			if response.Code != tt.wantStatus || response.Body.String() != tt.wantBody {
				t.Fatalf("response = %d %q; want %d %q", response.Code, response.Body.String(), tt.wantStatus, tt.wantBody)
			}
			if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
				t.Fatalf("Content-Type = %q", got)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q", got)
			}
			if tt.wantStatus != http.StatusOK {
				if !strings.Contains(logs.String(), `"check":"storage"`) {
					t.Fatalf("failing check not logged: %s", logs.String())
				}
				if strings.Contains(logs.String(), "/data/private") {
					t.Fatalf("logs disclosed internal path: %s", logs.String())
				}
			}
		})
	}
}
