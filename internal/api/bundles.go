package api

import (
	"mime"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/logging"
)

type bundleHandler struct{ deps Dependencies }

func (h *bundleHandler) serve(response http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	hash, ok := auth.HashDownloadToken(chi.URLParam(request, "token"))
	if !ok {
		h.reject(response, request, "malformed_token")
		return
	}
	token, err := h.deps.Store.ValidBundleToken(ctx, hash, time.Now().UTC())
	if err != nil {
		h.reject(response, request, "unknown_or_expired_token")
		return
	}
	logging.Add(ctx, "key_id", token.OwnerID, "job_id", token.JobID)
	b, err := h.deps.Store.Bundle(ctx, token.OwnerID, token.JobID)
	if err != nil {
		h.reject(response, request, "bundle_unavailable")
		return
	}
	file, err := h.deps.Layout.OpenBundle(b.JobID, b.SizeBytes)
	if err != nil {
		h.reject(response, request, "file_unavailable")
		return
	}
	defer func() { _ = file.Close() }()
	h.deps.Logger.InfoContext(ctx, "bundle download started", "size_bytes", b.SizeBytes)
	header := response.Header()
	header.Set("Content-Type", "application/zip")
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": b.FileName}))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	if request.Method == http.MethodHead {
		request = request.Clone(ctx)
		request.Header.Del("Range")
	}
	http.ServeContent(newDeadlineWriter(response, downloadWriteExtension), request, "", b.CreatedAt, file)
}

func (h *bundleHandler) reject(response http.ResponseWriter, request *http.Request, reason string) {
	h.deps.Logger.InfoContext(request.Context(), "bundle download rejected", "reason", reason)
	if request.Method == http.MethodHead {
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNotFound)
		return
	}
	writeError(response, http.StatusNotFound, "not_found")
}
