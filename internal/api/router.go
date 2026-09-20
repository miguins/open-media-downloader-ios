// Package api defines the public HTTP API.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NewRouter returns the application HTTP handler.
func NewRouter() http.Handler {
	router := chi.NewRouter()
	router.Get("/healthz", healthz)

	return router
}
