// Package api defines the public HTTP API.
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/miguins/open-media-downloader-ios/internal/readiness"
)

// NewRouter returns the application HTTP handler.
func NewRouter(checker *readiness.Checker, logger *slog.Logger) http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", healthz)
	router.Get("/readyz", readyz(checker, logger))

	return router
}
