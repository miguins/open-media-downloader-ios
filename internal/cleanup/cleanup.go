// Package cleanup recovers state on startup and periodically removes expired and orphaned data.
package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

	return c.sweepOrphans(ctx)
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

// sweep performs one cleanup pass. Failures are logged and retried on the next pass.
func (c *Cleaner) sweep(ctx context.Context) {
	now := c.now()
	if _, err := c.store.DeleteExpiredDownloadTokens(ctx, now); err != nil {
		c.logger.ErrorContext(ctx, "delete expired download tokens failed")
	}
	c.removeExpiredJobs(ctx, now)
	if err := c.sweepOrphans(ctx); err != nil {
		c.logger.ErrorContext(ctx, "sweep orphaned job directories failed")
	}
}

func (c *Cleaner) removeExpiredJobs(ctx context.Context, now time.Time) {
	ids, err := c.store.ExpiredJobIDs(ctx, now)
	if err != nil {
		c.logger.ErrorContext(ctx, "list expired jobs failed")

		return
	}
	for _, jobID := range ids {
		// Remove files before the row, so a failure leaves the row for the next pass instead of orphaning files.
		if err := c.layout.RemoveJob(jobID); err != nil {
			c.logger.ErrorContext(ctx, "remove expired job failed", "job_id", jobID)

			continue
		}
		if err := c.store.DeleteJob(ctx, jobID); err != nil && !errors.Is(err, store.ErrNotFound) {
			c.logger.ErrorContext(ctx, "delete expired job failed", "job_id", jobID)
		}
	}
}

// sweepOrphans removes job directories that have no job row.
func (c *Cleaner) sweepOrphans(ctx context.Context) error {
	ids, err := c.layout.JobDirIDs()
	if err != nil {
		return fmt.Errorf("cleanup: list job directories: %w", err)
	}
	for _, jobID := range ids {
		exists, err := c.store.JobExists(ctx, jobID)
		if err != nil {
			return fmt.Errorf("cleanup: check job: %w", err)
		}
		if !exists {
			if err := c.layout.RemoveJob(jobID); err != nil {
				return fmt.Errorf("cleanup: remove orphaned job directory: %w", err)
			}
		}
	}

	return nil
}
