// Package job defines download job, media item, and download-token domain models.
package job

import (
	"errors"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/id"
)

// Status is the lifecycle state of a job.
type Status string

// Job statuses.
const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

// Terminal reports whether s allows no further transitions.
func (s Status) Terminal() bool {
	return s != StatusQueued && s != StatusRunning
}

// CanTransitionTo reports whether a job may move from s to next.
func (s Status) CanTransitionTo(next Status) bool {
	switch s {
	case StatusQueued:
		return next == StatusRunning || next == StatusCanceled
	case StatusRunning:
		return next == StatusSucceeded || next == StatusFailed || next == StatusCanceled
	default:
		return false
	}
}

// ErrorCode is the closed set of failure reasons exposed to clients.
type ErrorCode string

// Job error codes.
const (
	ErrorUnsupportedURL   ErrorCode = "unsupported_url"
	ErrorExtractionFailed ErrorCode = "extraction_failed"
	ErrorTooLarge         ErrorCode = "too_large"
	ErrorTimeout          ErrorCode = "timeout"
	ErrorInternal         ErrorCode = "internal"
)

// Valid reports whether c is a known error code.
func (c ErrorCode) Valid() bool {
	switch c {
	case ErrorUnsupportedURL, ErrorExtractionFailed, ErrorTooLarge, ErrorTimeout, ErrorInternal:
		return true
	default:
		return false
	}
}

// Job is a request by one owner to download media from one normalized URL.
type Job struct {
	ID         string
	OwnerID    string
	SourceURL  string
	Platform   string
	Status     Status
	ErrorCode  ErrorCode
	CreatedAt  time.Time
	UpdatedAt  time.Time
	StartedAt  time.Time
	FinishedAt time.Time
	ExpiresAt  time.Time
}

// New returns a queued job that expires after retention.
func New(ownerID, sourceURL, platform string, now time.Time, retention time.Duration) Job {
	return Job{
		ID:        id.New(),
		OwnerID:   ownerID,
		SourceURL: sourceURL,
		Platform:  platform,
		Status:    StatusQueued,
		CreatedAt: now,
		UpdatedAt: now,
		ExpiresAt: now.Add(retention),
	}
}

// Transition moves the job to next, recording lifecycle timestamps.
// A failure requires a valid error code; other statuses forbid one.
// The job is unchanged when the transition is rejected.
func (j *Job) Transition(next Status, code ErrorCode, now time.Time) error {
	if !j.Status.CanTransitionTo(next) {
		return errors.New("job: invalid status transition")
	}
	if (next == StatusFailed) != code.Valid() || (next != StatusFailed && code != "") {
		return errors.New("job: error code must be set only for failed jobs")
	}

	j.Status = next
	j.ErrorCode = code
	j.UpdatedAt = now
	if next == StatusRunning {
		j.StartedAt = now
	}
	if next.Terminal() {
		j.FinishedAt = now
	}

	return nil
}

// Item is one media file produced by a job.
type Item struct {
	ID        string
	JobID     string
	Position  int
	FileName  string
	MediaType string
	SizeBytes int64
	CreatedAt time.Time
}

// DownloadToken authorizes one owner to download one item until it expires.
// Only a hash of the opaque token value is stored.
type DownloadToken struct {
	Hash      []byte
	ItemID    string
	OwnerID   string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Expired reports whether the token is no longer valid at now.
func (t DownloadToken) Expired(now time.Time) bool {
	return !now.Before(t.ExpiresAt)
}
