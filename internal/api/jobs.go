package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

const cancelAttempts = 3

type jobHandler struct {
	deps Dependencies
}

type jobResponse struct {
	ID        string         `json:"id"`
	Status    job.Status     `json:"status"`
	URL       string         `json:"url"`
	Platform  string         `json:"platform"`
	Error     *job.ErrorCode `json:"error"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	ExpiresAt time.Time      `json:"expires_at"`
	Items     []itemResponse `json:"items,omitempty"`
}

type itemResponse struct {
	ID                string    `json:"id"`
	FileName          string    `json:"file_name"`
	MediaType         string    `json:"media_type"`
	SizeBytes         int64     `json:"size_bytes"`
	DownloadURL       string    `json:"download_url"`
	DownloadExpiresAt time.Time `json:"download_expires_at"`
}

func newJobResponse(j job.Job) jobResponse {
	response := jobResponse{
		ID:        j.ID,
		Status:    j.Status,
		URL:       j.SourceURL,
		Platform:  j.Platform,
		CreatedAt: j.CreatedAt,
		UpdatedAt: j.UpdatedAt,
		ExpiresAt: j.ExpiresAt,
	}
	if j.ErrorCode != "" {
		code := j.ErrorCode
		response.Error = &code
	}

	return response
}

func (h *jobHandler) create(response http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	ownerID, _ := auth.OwnerID(ctx)

	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(response, http.StatusUnsupportedMediaType, "unsupported_media_type")

		return
	}
	var body struct {
		URL *string `json:"url"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, h.deps.Settings.MaxRequestBytes))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&body)
	if err == nil && decoder.Decode(&struct{}{}) != io.EOF {
		err = errors.New("request body must contain one JSON object")
	}
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		writeError(response, http.StatusRequestEntityTooLarge, "request_too_large")

		return
	case err != nil || body.URL == nil:
		writeError(response, http.StatusBadRequest, "invalid_request")

		return
	}

	normalized, err := h.deps.Policy.Normalize(*body.URL)
	if err != nil {
		h.deps.Logger.DebugContext(ctx, "rejected job URL", "reason", err.Error())
		writeError(response, http.StatusUnprocessableEntity, "unsupported_url")

		return
	}
	// The count and insert are not atomic; concurrent requests from one key may exceed the limit
	// by a few jobs, which is acceptable for a per-key fairness bound.
	active, err := h.deps.Store.ActiveJobCount(ctx, ownerID)
	if err != nil {
		h.internal(response, request, "count active jobs failed")

		return
	}
	if active >= h.deps.Settings.MaxQueuedJobs {
		writeError(response, http.StatusTooManyRequests, "too_many_jobs")

		return
	}

	j := job.New(ownerID, normalized.URL, normalized.Platform, time.Now().UTC(), h.deps.Settings.JobRetention)
	if err := h.deps.Store.CreateJob(ctx, j); err != nil {
		h.internal(response, request, "create job failed")

		return
	}
	h.deps.Notify()
	response.Header().Set("Location", "/v1/jobs/"+j.ID)
	writeJSON(response, http.StatusAccepted, newJobResponse(j))
}

func (h *jobHandler) get(response http.ResponseWriter, request *http.Request) {
	j, ok := h.load(response, request)
	if !ok {
		return
	}
	body := newJobResponse(j)
	if j.Status == job.StatusSucceeded {
		items, err := h.downloadLinks(request, j)
		if err != nil {
			h.internal(response, request, "issue download links failed")

			return
		}
		body.Items = items
	}
	writeJSON(response, http.StatusOK, body)
}

// downloadLinks issues a fresh token per item, replacing earlier ones, and never outliving the job.
func (h *jobHandler) downloadLinks(request *http.Request, j job.Job) ([]itemResponse, error) {
	items, err := h.deps.Store.Items(request.Context(), j.OwnerID, j.ID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(h.deps.Settings.TokenTTL)
	if expiresAt.After(j.ExpiresAt) {
		expiresAt = j.ExpiresAt
	}
	links := make([]itemResponse, 0, len(items))
	for _, item := range items {
		token, hash := auth.NewDownloadToken()
		if err := h.deps.Store.ReplaceDownloadToken(request.Context(), job.DownloadToken{
			Hash: hash, ItemID: item.ID, OwnerID: j.OwnerID, CreatedAt: now, ExpiresAt: expiresAt,
		}); err != nil {
			return nil, err
		}
		links = append(links, itemResponse{
			ID:                item.ID,
			FileName:          item.FileName,
			MediaType:         item.MediaType,
			SizeBytes:         item.SizeBytes,
			DownloadURL:       h.deps.Settings.PublicURL + "/v1/downloads/" + token,
			DownloadExpiresAt: expiresAt,
		})
	}

	return links, nil
}

func (h *jobHandler) cancel(response http.ResponseWriter, request *http.Request) {
	j, ok := h.load(response, request)
	if !ok {
		return
	}
	// The worker may change the status concurrently; retry on conflict with the fresh state.
	for range cancelAttempts {
		switch {
		case j.Status == job.StatusCanceled:
			writeJSON(response, http.StatusOK, newJobResponse(j))

			return
		case j.Status.Terminal():
			writeError(response, http.StatusConflict, "conflict")

			return
		}
		canceled := j
		_ = canceled.Transition(job.StatusCanceled, "", time.Now().UTC()) // Queued and running jobs can be canceled.
		err := h.deps.Store.UpdateJobStatus(request.Context(), canceled, j.Status)
		if err == nil {
			writeJSON(response, http.StatusOK, newJobResponse(canceled))

			return
		}
		if !errors.Is(err, store.ErrConflict) {
			h.storeError(response, request, err)

			return
		}
		if j, ok = h.load(response, request); !ok {
			return
		}
	}
	writeError(response, http.StatusConflict, "conflict")
}

// load returns the caller's job named in the path, writing 404 for invalid, unknown, and foreign IDs.
func (h *jobHandler) load(response http.ResponseWriter, request *http.Request) (job.Job, bool) {
	ownerID, _ := auth.OwnerID(request.Context())
	jobID := chi.URLParam(request, "id")
	if !id.Valid(jobID) {
		writeError(response, http.StatusNotFound, "not_found")

		return job.Job{}, false
	}
	j, err := h.deps.Store.Job(request.Context(), ownerID, jobID)
	if err != nil {
		h.storeError(response, request, err)

		return job.Job{}, false
	}

	return j, true
}

func (h *jobHandler) storeError(response http.ResponseWriter, request *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(response, http.StatusNotFound, "not_found")

		return
	}
	h.internal(response, request, "job request failed")
}

func (h *jobHandler) internal(response http.ResponseWriter, request *http.Request, message string) {
	h.deps.Logger.ErrorContext(request.Context(), message)
	writeError(response, http.StatusInternalServerError, "internal")
}
