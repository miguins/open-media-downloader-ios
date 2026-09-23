// Package api defines the public HTTP API.
package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/readiness"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

// Settings holds the configuration values the handlers need.
type Settings struct {
	PublicURL       string
	MaxRequestBytes int64
	MaxQueuedJobs   int
	JobRetention    time.Duration
	TokenTTL        time.Duration
}

// Dependencies are the collaborators of the HTTP API.
type Dependencies struct {
	Readiness     *readiness.Checker
	Authenticator *auth.Authenticator
	Store         *store.Store
	Layout        *storage.Layout
	Policy        *urlpolicy.Policy
	// Notify wakes the worker after a job is queued.
	Notify   func()
	Settings Settings
	Logger   *slog.Logger
}

// NewRouter returns the application HTTP handler.
func NewRouter(deps Dependencies) http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", healthz)
	router.Get("/readyz", readyz(deps.Readiness, deps.Logger))

	jobs := &jobHandler{deps: deps}
	router.Group(func(protected chi.Router) {
		protected.Use(deps.Authenticator.Middleware)
		protected.Post("/v1/jobs", jobs.create)
		protected.Get("/v1/jobs/{id}", jobs.get)
		protected.Delete("/v1/jobs/{id}", jobs.cancel)
	})

	downloads := &downloadHandler{deps: deps}
	router.Get("/v1/downloads/{token}", downloads.serve)
	router.Head("/v1/downloads/{token}", downloads.serve)

	return router
}
