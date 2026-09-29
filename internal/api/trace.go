package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/miguins/open-media-downloader-ios/internal/logging"
)

// traceHeader returns the request's trace ID so clients can report it.
const traceHeader = "X-Trace-Id"

// traceRequests gives every request a trace ID and a log scope, and logs one access line per request.
// The line names the route pattern rather than the path, so download tokens never reach the logs.
func traceRequests(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			started := time.Now()
			ctx := logging.NewTrace(request.Context())
			response.Header().Set(traceHeader, logging.TraceID(ctx))
			recorder := &statusRecorder{ResponseWriter: response}
			next.ServeHTTP(recorder, request.WithContext(ctx))

			route := chi.RouteContext(ctx).RoutePattern()
			if route == "/healthz" {
				// Platform health checks poll this route every few seconds; they would drown out other logs.
				return
			}
			if route == "" {
				route = "unmatched"
			}
			level := slog.LevelInfo
			if recorder.statusCode() >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			logger.LogAttrs(ctx, level, "request completed",
				slog.String("method", request.Method),
				slog.String("route", route),
				slog.Int("status", recorder.statusCode()),
				slog.Int64("bytes", recorder.bytes),
				slog.Int64("duration_ms", time.Since(started).Milliseconds()))
		})
	}
}

// statusRecorder captures the status code and body size of a response.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(p)
	r.bytes += int64(n)

	return n, err
}

func (r *statusRecorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}

	return r.status
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
