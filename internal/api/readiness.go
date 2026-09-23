package api

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/miguins/open-media-downloader-ios/internal/readiness"
)

const (
	readyBody    = "{\"status\":\"ready\"}\n"
	notReadyBody = "{\"status\":\"not_ready\"}\n"
)

// readyz reports whether every dependency check passes. Only the failing check's name is logged.
func readyz(checker *readiness.Checker, logger *slog.Logger) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")

		failed, ok := checker.Ready(request.Context())
		if !ok {
			logger.WarnContext(request.Context(), "readiness check failed", "check", failed)
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(response, notReadyBody)

			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(response, readyBody)
	}
}
