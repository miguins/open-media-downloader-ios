package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

type bundleResponse struct {
	FileName          string    `json:"file_name"`
	MediaType         string    `json:"media_type"`
	SizeBytes         int64     `json:"size_bytes"`
	DownloadURL       string    `json:"download_url"`
	DownloadExpiresAt time.Time `json:"download_expires_at"`
}

func (h *jobHandler) bundleLink(request *http.Request, j job.Job) (*bundleResponse, error) {
	b, err := h.deps.Store.Bundle(request.Context(), j.OwnerID, j.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	expires := now.Add(h.deps.Settings.TokenTTL)
	if expires.After(j.ExpiresAt) {
		expires = j.ExpiresAt
	}
	plaintext, hash := auth.NewDownloadToken()
	if err := h.deps.Store.ReplaceBundleToken(request.Context(), job.BundleToken{Hash: hash, JobID: j.ID, OwnerID: j.OwnerID, CreatedAt: now, ExpiresAt: expires}); err != nil {
		return nil, err
	}
	return &bundleResponse{FileName: b.FileName, MediaType: "application/zip", SizeBytes: b.SizeBytes, DownloadURL: h.deps.Settings.PublicURL + "/v1/bundles/" + plaintext, DownloadExpiresAt: expires}, nil
}
