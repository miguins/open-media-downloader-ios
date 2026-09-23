package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

const jobColumns = "id, owner_id, source_url, platform, status, error_code, created_at, updated_at, started_at, finished_at, expires_at"

// CreateJob stores a new job.
func (s *Store) CreateJob(ctx context.Context, j job.Job) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO jobs ("+jobColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		j.ID, j.OwnerID, j.SourceURL, j.Platform, string(j.Status), nullableString(string(j.ErrorCode)),
		toMillis(j.CreatedAt), toMillis(j.UpdatedAt), nullableMillis(j.StartedAt), nullableMillis(j.FinishedAt),
		toMillis(j.ExpiresAt))
	if err != nil {
		return fmt.Errorf("store: create job: %w", err)
	}

	return nil
}

// Job returns the job with id owned by ownerID. Jobs owned by anyone else are reported as ErrNotFound.
func (s *Store) Job(ctx context.Context, ownerID, id string) (job.Job, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id = ? AND owner_id = ?", id, ownerID)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return job.Job{}, ErrNotFound
	}
	if err != nil {
		return job.Job{}, fmt.Errorf("store: get job: %w", err)
	}

	return j, nil
}

// UpdateJobStatus persists a transition of j from the status previously read as from.
// It returns ErrConflict when the stored status is no longer from and ErrNotFound when
// the owner has no such job.
func (s *Store) UpdateJobStatus(ctx context.Context, j job.Job, from job.Status) error {
	err := requireAffected(s.db.ExecContext(ctx,
		`UPDATE jobs SET status = ?, error_code = ?, updated_at = ?, started_at = ?, finished_at = ?
		WHERE id = ? AND owner_id = ? AND status = ?`,
		string(j.Status), nullableString(string(j.ErrorCode)), toMillis(j.UpdatedAt),
		nullableMillis(j.StartedAt), nullableMillis(j.FinishedAt), j.ID, j.OwnerID, string(from)))
	if !errors.Is(err, ErrNotFound) {
		if err != nil {
			return fmt.Errorf("store: update job status: %w", err)
		}

		return nil
	}
	if _, err := s.Job(ctx, j.OwnerID, j.ID); err != nil {
		return err
	}

	return ErrConflict
}

// CreateItem stores a media item. It returns ErrConflict when the job already has an item at the same position.
func (s *Store) CreateItem(ctx context.Context, item job.Item) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO items (id, job_id, position, file_name, media_type, size_bytes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		item.ID, item.JobID, item.Position, item.FileName, item.MediaType, item.SizeBytes, toMillis(item.CreatedAt))
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("store: create item: %w", err)
	}

	return nil
}

// Items returns the items of a job owned by ownerID in position order. Other owners receive no items.
func (s *Store) Items(ctx context.Context, ownerID, jobID string) ([]job.Item, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT items.id, items.job_id, items.position, items.file_name, items.media_type, items.size_bytes, items.created_at
		FROM items JOIN jobs ON jobs.id = items.job_id
		WHERE items.job_id = ? AND jobs.owner_id = ?
		ORDER BY items.position`, jobID, ownerID)
	if err != nil {
		return nil, fmt.Errorf("store: list items: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var items []job.Item
	for rows.Next() {
		var (
			item      job.Item
			createdAt int64
		)
		if err := rows.Scan(&item.ID, &item.JobID, &item.Position, &item.FileName, &item.MediaType, &item.SizeBytes, &createdAt); err != nil {
			return nil, fmt.Errorf("store: list items: %w", err)
		}
		item.CreatedAt = fromMillis(createdAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list items: %w", err)
	}

	return items, nil
}

// CreateDownloadToken stores a download token hash. It returns ErrConflict for a duplicate hash.
func (s *Store) CreateDownloadToken(ctx context.Context, token job.DownloadToken) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO download_tokens (token_hash, item_id, owner_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		token.Hash, token.ItemID, token.OwnerID, toMillis(token.CreatedAt), toMillis(token.ExpiresAt))
	if isUniqueViolation(err) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("store: create download token: %w", err)
	}

	return nil
}

// ValidDownloadToken returns the unexpired token with hash, or ErrNotFound.
func (s *Store) ValidDownloadToken(ctx context.Context, hash []byte, now time.Time) (job.DownloadToken, error) {
	var (
		token                job.DownloadToken
		createdAt, expiresAt int64
	)
	err := s.db.QueryRowContext(ctx,
		"SELECT token_hash, item_id, owner_id, created_at, expires_at FROM download_tokens WHERE token_hash = ? AND expires_at > ?",
		hash, toMillis(now)).Scan(&token.Hash, &token.ItemID, &token.OwnerID, &createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return job.DownloadToken{}, ErrNotFound
	}
	if err != nil {
		return job.DownloadToken{}, fmt.Errorf("store: get download token: %w", err)
	}
	token.CreatedAt = fromMillis(createdAt)
	token.ExpiresAt = fromMillis(expiresAt)

	return token, nil
}

// DeleteExpiredDownloadTokens removes tokens expired at now and returns how many were removed.
func (s *Store) DeleteExpiredDownloadTokens(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, "DELETE FROM download_tokens WHERE expires_at <= ?", toMillis(now))
	if err != nil {
		return 0, fmt.Errorf("store: delete expired download tokens: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: delete expired download tokens: %w", err)
	}

	return deleted, nil
}

func scanJob(row scanner) (job.Job, error) {
	var (
		j                               job.Job
		status                          string
		errorCode                       sql.NullString
		createdAt, updatedAt, expiresAt int64
		startedAt, finishedAt           sql.NullInt64
	)
	if err := row.Scan(&j.ID, &j.OwnerID, &j.SourceURL, &j.Platform, &status, &errorCode,
		&createdAt, &updatedAt, &startedAt, &finishedAt, &expiresAt); err != nil {
		return job.Job{}, err
	}
	j.Status = job.Status(status)
	j.ErrorCode = job.ErrorCode(errorCode.String)
	j.CreatedAt = fromMillis(createdAt)
	j.UpdatedAt = fromMillis(updatedAt)
	j.StartedAt = fromNullableMillis(startedAt)
	j.FinishedAt = fromNullableMillis(finishedAt)
	j.ExpiresAt = fromMillis(expiresAt)

	return j, nil
}

func nullableString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}
