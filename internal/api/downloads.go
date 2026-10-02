package api

import (
	"mime"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/logging"
)

// downloadWriteExtension is how long a download may stall between writes before the connection is dropped.
const downloadWriteExtension = 30 * time.Second

type downloadHandler struct {
	deps Dependencies
}

// serve streams the item a download token grants. Every failure returns the same 404 so tokens
// cannot be probed; the token itself is never logged.
func (h *downloadHandler) serve(response http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	hash, ok := auth.HashDownloadToken(chi.URLParam(request, "token"))
	if !ok {
		h.reject(response, request, "malformed_token")

		return
	}
	token, err := h.deps.Store.ValidDownloadToken(ctx, hash, time.Now().UTC())
	if err != nil {
		h.reject(response, request, "unknown_or_expired_token")

		return
	}
	logging.Add(ctx, "key_id", token.OwnerID)
	if key, err := h.deps.Store.APIKey(ctx, token.OwnerID); err == nil {
		logging.Add(ctx, "key_name", key.Name)
	}
	item, err := h.deps.Store.Item(ctx, token.ItemID)
	if err != nil {
		h.reject(response, request, "item_unavailable")

		return
	}
	logging.Add(ctx, "job_id", item.JobID, "item_id", item.ID)
	file, err := h.deps.Layout.OpenItem(item.JobID, item.ID)
	if err != nil {
		h.deps.Logger.WarnContext(ctx, "download file unavailable")
		writeError(response, http.StatusNotFound, "not_found")

		return
	}
	defer func() { _ = file.Close() }()
	h.deps.Logger.InfoContext(ctx, "download started", "media_type", item.MediaType, "size_bytes", item.SizeBytes)

	header := response.Header()
	header.Set("Content-Type", item.MediaType)
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.FileName}))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	http.ServeContent(newDeadlineWriter(response, downloadWriteExtension), request, "", item.CreatedAt, file)
}

// reject writes the uniform download 404 and logs a fixed reason, never the token.
func (h *downloadHandler) reject(response http.ResponseWriter, request *http.Request, reason string) {
	h.deps.Logger.InfoContext(request.Context(), "download rejected", "reason", reason)
	writeError(response, http.StatusNotFound, "not_found")
}

// deadlineWriter extends the connection write deadline before every write, so large downloads are not
// cut off by the server-wide write timeout while stalled clients are still disconnected.
type deadlineWriter struct {
	http.ResponseWriter
	controller    *http.ResponseController
	extension     time.Duration
	rangeRejected bool
}

func newDeadlineWriter(response http.ResponseWriter, extension time.Duration) *deadlineWriter {
	writer := &deadlineWriter{ResponseWriter: response, controller: http.NewResponseController(response), extension: extension}
	writer.extend()

	return writer
}

func (w *deadlineWriter) extend() {
	// Writers without deadline support, such as test recorders, keep their default behavior.
	_ = w.controller.SetWriteDeadline(time.Now().Add(w.extension))
}

// WriteHeader restores privacy headers and normalizes content-server range errors.
func (w *deadlineWriter) WriteHeader(status int) {
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusRequestedRangeNotSatisfiable {
		w.rangeRejected = true
		w.Header().Del("Content-Disposition")
		w.Header().Del("Content-Length")
		w.extend()
		writeError(w.ResponseWriter, status, "range_not_satisfiable")
		return
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *deadlineWriter) Write(p []byte) (int, error) {
	if w.rangeRejected {
		// ServeContent writes its text error after WriteHeader; the JSON is already sent.
		return len(p), nil
	}
	w.extend()

	return w.ResponseWriter.Write(p)
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *deadlineWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
