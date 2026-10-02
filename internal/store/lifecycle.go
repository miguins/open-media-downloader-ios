package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

const terminalStatuses = "('succeeded', 'failed', 'canceled')"

// JobFilter narrows Jobs; empty fields match everything.
type JobFilter struct {
	OwnerID string
	Status  job.Status
}

// ClaimNextJob atomically moves the oldest queued job to running and returns it, or ErrNotFound when the queue is empty.
func (s *Store) ClaimNextJob(ctx context.Context, now time.Time) (job.Job, error) {
	row := s.db.QueryRowContext(ctx,
		`UPDATE jobs SET status = 'running', started_at = ?, updated_at = ?
		WHERE id = (SELECT id FROM jobs WHERE status = 'queued' ORDER BY created_at, id LIMIT 1) AND status = 'queued'
		RETURNING `+jobColumns, toMillis(now), toMillis(now))
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return job.Job{}, ErrNotFound
	}
	if err != nil {
		return job.Job{}, fmt.Errorf("store: claim job: %w", err)
	}

	return j, nil
}

// JobStatus returns the current status of any job, or ErrNotFound. It is for internal use by the worker.
func (s *Store) JobStatus(ctx context.Context, id string) (job.Status, error) {
	var status string
	err := s.db.QueryRowContext(ctx, "SELECT status FROM jobs WHERE id = ?", id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("store: get job status: %w", err)
	}

	return job.Status(status), nil
}

// JobExists reports whether a job row exists regardless of owner.
func (s *Store) JobExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM jobs WHERE id = ?)", id).Scan(&exists); err != nil {
		return false, fmt.Errorf("store: check job: %w", err)
	}

	return exists, nil
}

// ActiveJobCount returns how many queued or running jobs ownerID has.
func (s *Store) ActiveJobCount(ctx context.Context, ownerID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT count(*) FROM jobs WHERE owner_id = ? AND status IN ('queued', 'running')", ownerID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count active jobs: %w", err)
	}

	return count, nil
}

// CompleteJob records a running job as succeeded together with its items in one transaction.
// It returns ErrConflict when the job is no longer running and ErrNotFound when it no longer exists.
func (s *Store) CompleteJob(ctx context.Context, j job.Job, items []job.Item, bundle *job.Bundle) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: complete job: %w", err)
	}
	if err := completeJob(ctx, tx, j, items, bundle); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: complete job: %w", err)
	}

	return nil
}

func completeJob(ctx context.Context, tx *sql.Tx, j job.Job, items []job.Item, bundle *job.Bundle) error {
	err := requireAffected(tx.ExecContext(ctx,
		`UPDATE jobs SET status = ?, error_code = NULL, updated_at = ?, finished_at = ?
		WHERE id = ? AND owner_id = ? AND status = 'running'`,
		string(j.Status), toMillis(j.UpdatedAt), nullableMillis(j.FinishedAt), j.ID, j.OwnerID))
	if errors.Is(err, ErrNotFound) {
		var exists bool
		if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM jobs WHERE id = ?)", j.ID).Scan(&exists); err != nil {
			return fmt.Errorf("store: complete job: %w", err)
		}
		if exists {
			return ErrConflict
		}

		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: complete job: %w", err)
	}
	if j.Status != job.StatusSucceeded || len(items) == 0 || (len(items) > 1) != (bundle != nil) {
		return errors.New("store: invalid completion")
	}
	if bundle != nil && (bundle.JobID != j.ID || bundle.SizeBytes <= 0) {
		return errors.New("store: invalid bundle")
	}
	for _, item := range items {
		if item.JobID != j.ID {
			return errors.New("store: invalid item ownership")
		}
	}
	for _, item := range items {
		if err := insertItem(ctx, tx, item); err != nil {
			return err
		}
	}

	if bundle != nil {
		if _, err := tx.ExecContext(ctx, "INSERT INTO bundles(job_id,file_name,size_bytes,created_at) VALUES(?,?,?,?)", bundle.JobID, bundle.FileName, bundle.SizeBytes, toMillis(bundle.CreatedAt)); err != nil {
			return fmt.Errorf("store: complete bundle: %w", err)
		}
	}
	return nil
}

// Item returns any item by ID, or ErrNotFound. Download tokens authorize access, so it is not owner-scoped.
func (s *Store) Item(ctx context.Context, id string) (job.Item, error) {
	var (
		item      job.Item
		createdAt int64
	)
	err := s.db.QueryRowContext(ctx,
		"SELECT id, job_id, position, file_name, media_type, size_bytes, created_at FROM items WHERE id = ?", id).
		Scan(&item.ID, &item.JobID, &item.Position, &item.FileName, &item.MediaType, &item.SizeBytes, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return job.Item{}, ErrNotFound
	}
	if err != nil {
		return job.Item{}, fmt.Errorf("store: get item: %w", err)
	}
	item.CreatedAt = fromMillis(createdAt)

	return item, nil
}

// Jobs lists jobs matching filter, newest first.
func (s *Store) Jobs(ctx context.Context, filter JobFilter) ([]job.Job, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+jobColumns+` FROM jobs WHERE (? = '' OR owner_id = ?) AND (? = '' OR status = ?)
		ORDER BY created_at DESC, id`,
		filter.OwnerID, filter.OwnerID, string(filter.Status), string(filter.Status))
	if err != nil {
		return nil, fmt.Errorf("store: list jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var jobs []job.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("store: list jobs: %w", err)
		}
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list jobs: %w", err)
	}

	return jobs, nil
}

// DeleteJob removes a job that is not running, with its items and tokens.
// It returns ErrConflict for running jobs and ErrNotFound for unknown IDs.
func (s *Store) DeleteJob(ctx context.Context, id string) error {
	err := requireAffected(s.db.ExecContext(ctx, "DELETE FROM jobs WHERE id = ? AND status <> 'running'", id))
	if !errors.Is(err, ErrNotFound) {
		if err != nil {
			return fmt.Errorf("store: delete job: %w", err)
		}

		return nil
	}
	exists, err := s.JobExists(ctx, id)
	if err != nil {
		return err
	}
	if exists {
		return ErrConflict
	}

	return ErrNotFound
}

// PurgeJobs cancels and deletes every job of ownerID, or of every owner when ownerID is empty,
// including running ones. It returns the deleted job IDs so their files can be removed.
func (s *Store) PurgeJobs(ctx context.Context, ownerID string, now time.Time) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: purge jobs: %w", err)
	}
	ids, err := purgeJobs(ctx, tx, ownerID, now)
	if err != nil {
		return nil, errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: purge jobs: %w", err)
	}

	return ids, nil
}

func purgeJobs(ctx context.Context, tx *sql.Tx, ownerID string, now time.Time) ([]string, error) {
	// Cancel first so a worker polling the row observes cancellation even if it reads before the delete commits.
	if _, err := tx.ExecContext(ctx,
		`UPDATE jobs SET status = 'canceled', error_code = NULL, updated_at = ?, finished_at = ?
		WHERE (? = '' OR owner_id = ?) AND status IN ('queued', 'running')`,
		toMillis(now), toMillis(now), ownerID, ownerID); err != nil {
		return nil, fmt.Errorf("store: purge jobs: %w", err)
	}
	rows, err := tx.QueryContext(ctx, "DELETE FROM jobs WHERE (? = '' OR owner_id = ?) RETURNING id", ownerID, ownerID)
	if err != nil {
		return nil, fmt.Errorf("store: purge jobs: %w", err)
	}

	return scanIDs(rows)
}

// ExpiredJobIDs returns terminal jobs whose retention ended at or before now.
func (s *Store) ExpiredJobIDs(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id FROM jobs WHERE status IN "+terminalStatuses+" AND expires_at <= ? ORDER BY expires_at, id", toMillis(now))
	if err != nil {
		return nil, fmt.Errorf("store: list expired jobs: %w", err)
	}

	return scanIDs(rows)
}

// FailRunningJobs marks every running job failed with the internal error code. It is used on startup,
// when no worker can still own a running job.
func (s *Store) FailRunningJobs(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		`UPDATE jobs SET status = 'failed', error_code = ?, updated_at = ?, finished_at = ? WHERE status = 'running'`,
		string(job.ErrorInternal), toMillis(now), toMillis(now))
	if err != nil {
		return 0, fmt.Errorf("store: fail running jobs: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: fail running jobs: %w", err)
	}

	return count, nil
}

// ReplaceDownloadToken stores token and deletes every other token for the same item, keeping one live token per item.
func (s *Store) ReplaceDownloadToken(ctx context.Context, token job.DownloadToken) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: replace download token: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM download_tokens WHERE item_id = ?", token.ItemID); err != nil {
		return errors.Join(fmt.Errorf("store: replace download token: %w", err), tx.Rollback())
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO download_tokens (token_hash, item_id, owner_id, created_at, expires_at) VALUES (?, ?, ?, ?, ?)",
		token.Hash, token.ItemID, token.OwnerID, toMillis(token.CreatedAt), toMillis(token.ExpiresAt)); err != nil {
		return errors.Join(fmt.Errorf("store: replace download token: %w", err), tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: replace download token: %w", err)
	}

	return nil
}

func scanIDs(rows *sql.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("store: scan IDs: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: scan IDs: %w", err)
	}

	return ids, nil
}
