// Package cleanup recovers state on startup and periodically removes expired and orphaned data.
package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/logging"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

const defaultInterval = time.Minute

// Cleaner removes expired download tokens, expired jobs with their files, and orphaned job directories.
type Cleaner struct {
	store    *store.Store
	layout   *storage.Layout
	logger   *slog.Logger
	now      func() time.Time
	interval time.Duration
}

// New returns a Cleaner.
func New(st *store.Store, layout *storage.Layout, logger *slog.Logger) *Cleaner {
	return &Cleaner{
		store:    st,
		layout:   layout,
		logger:   logger,
		now:      func() time.Time { return time.Now().UTC() },
		interval: defaultInterval,
	}
}

// Recover prepares state left by a previous process. It must run before the worker starts:
// running jobs are marked failed rather than re-queued, work directories are cleared, and
// orphaned job directories are removed.
func (c *Cleaner) Recover(ctx context.Context) error {
	failed, err := c.store.FailRunningJobs(ctx, c.now())
	if err != nil {
		return fmt.Errorf("cleanup: recover running jobs: %w", err)
	}
	if failed > 0 {
		c.logger.WarnContext(ctx, "marked interrupted jobs as failed", "count", failed)
	}
	if err := c.layout.ClearWork(); err != nil {
		return fmt.Errorf("cleanup: clear work directories: %w", err)
	}

	for _, status := range []job.Status{job.StatusFailed, job.StatusCanceled} {
		jobs, err := c.store.Jobs(ctx, store.JobFilter{Status: status})
		if err != nil {
			return fmt.Errorf("cleanup: list interrupted jobs: %w", err)
		}
		for _, j := range jobs {
			if err := c.layout.RemoveJob(j.ID); err != nil {
				return fmt.Errorf("cleanup: remove interrupted files: %w", err)
			}
		}
	}
	orphans, err := c.sweepOrphans(ctx)
	if err != nil {
		return err
	}
	c.logger.InfoContext(ctx, "recovery completed", "interrupted_jobs", failed, "orphaned_directories", orphans)

	return nil
}

// Run sweeps periodically until ctx is canceled.
func (c *Cleaner) Run(ctx context.Context) error {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.sweep(ctx)
		}
	}
}

// sweep performs one cleanup pass under its own trace and logs a summary, at debug level when
// nothing was removed. Failures are logged and retried on the next pass.
func (c *Cleaner) sweep(ctx context.Context) {
	ctx = logging.NewTrace(ctx)
	now := c.now()
	tokens, err := c.store.DeleteExpiredDownloadTokens(ctx, now)
	if err != nil {
		c.logger.ErrorContext(ctx, "delete expired download tokens failed")
	}
	bundleTokens, err := c.store.DeleteExpiredBundleTokens(ctx, now)
	if err != nil {
		c.logger.ErrorContext(ctx, "delete expired bundle tokens failed")
	}
	tokens += bundleTokens
	jobs := c.removeExpiredJobs(ctx, now)
	orphans, err := c.sweepOrphans(ctx)
	if err != nil {
		c.logger.ErrorContext(ctx, "sweep orphaned job directories failed")
	}
	level := slog.LevelDebug
	if tokens+int64(jobs+orphans) > 0 {
		level = slog.LevelInfo
	}
	c.logger.Log(ctx, level, "cleanup sweep completed",
		"expired_tokens", tokens, "expired_jobs", jobs, "orphaned_directories", orphans)
}

// removeExpiredJobs removes expired jobs with their files and returns how many were removed.
func (c *Cleaner) removeExpiredJobs(ctx context.Context, now time.Time) int {
	ids, err := c.store.ExpiredJobIDs(ctx, now)
	if err != nil {
		c.logger.ErrorContext(ctx, "list expired jobs failed")

		return 0
	}
	removed := 0
	for _, jobID := range ids {
		// Remove files before the row, so a failure leaves the row for the next pass instead of orphaning files.
		if err := c.layout.RemoveJob(jobID); err != nil {
			c.logger.ErrorContext(ctx, "remove expired job failed", "job_id", jobID)

			continue
		}
		if err := c.store.DeleteJob(ctx, jobID); err != nil && !errors.Is(err, store.ErrNotFound) {
			c.logger.ErrorContext(ctx, "delete expired job failed", "job_id", jobID)

			continue
		}
		removed++
	}

	return removed
}

// sweepOrphans removes job directories that have no job row and returns how many were removed.
func (c *Cleaner) sweepOrphans(ctx context.Context) (int, error) {
	ids, err := c.layout.JobDirIDs()
	if err != nil {
		return 0, fmt.Errorf("cleanup: list job directories: %w", err)
	}
	removed := 0
	for _, jobID := range ids {
		exists, err := c.store.JobExists(ctx, jobID)
		if err != nil {
			return removed, fmt.Errorf("cleanup: check job: %w", err)
		}
		if !exists {
			if err := c.layout.RemoveJob(jobID); err != nil {
				return removed, fmt.Errorf("cleanup: remove orphaned job directory: %w", err)
			}
			removed++
		}
	}

	return removed, nil
}
