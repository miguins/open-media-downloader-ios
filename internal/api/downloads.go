package api

import (
	"mime"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
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
		writeError(response, http.StatusNotFound, "not_found")

		return
	}
	token, err := h.deps.Store.ValidDownloadToken(ctx, hash, time.Now().UTC())
	if err != nil {
		writeError(response, http.StatusNotFound, "not_found")

		return
	}
	item, err := h.deps.Store.Item(ctx, token.ItemID)
	if err != nil {
		writeError(response, http.StatusNotFound, "not_found")

		return
	}
	file, err := h.deps.Layout.OpenItem(item.JobID, item.ID)
	if err != nil {
		h.deps.Logger.WarnContext(ctx, "download file unavailable", "job_id", item.JobID)
		writeError(response, http.StatusNotFound, "not_found")

		return
	}
	defer func() { _ = file.Close() }()

	header := response.Header()
	header.Set("Content-Type", item.MediaType)
	header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": item.FileName}))
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	http.ServeContent(newDeadlineWriter(response, downloadWriteExtension), request, "", item.CreatedAt, file)
}

// deadlineWriter extends the connection write deadline before every write, so large downloads are not
// cut off by the server-wide write timeout while stalled clients are still disconnected.
type deadlineWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
	extension  time.Duration
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

func (w *deadlineWriter) Write(p []byte) (int, error) {
	w.extend()

	return w.ResponseWriter.Write(p)
}

// Unwrap exposes the underlying writer to http.ResponseController.
func (w *deadlineWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
