// Package worker processes queued jobs one at a time.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

const (
	defaultPollInterval  = 5 * time.Second
	defaultWatchInterval = time.Second
)

var (
	errCanceled = errors.New("worker: job canceled")
	errTimeout  = errors.New("worker: job timed out")
)

// Extractor downloads media for one request into its work directory.
type Extractor interface {
	Extract(ctx context.Context, request extractor.Request) ([]extractor.File, error)
}

// Settings bounds each job.
type Settings struct {
	JobTimeout   time.Duration
	MaxJobBytes  int64
	MinFreeBytes int64
	MaxJobItems  int
}

// Worker claims queued jobs from the store and runs them through the extractor.
type Worker struct {
	store         *store.Store
	layout        *storage.Layout
	extractor     Extractor
	settings      Settings
	logger        *slog.Logger
	notify        chan struct{}
	now           func() time.Time
	pollInterval  time.Duration
	watchInterval time.Duration
}

// New returns a Worker.
func New(st *store.Store, layout *storage.Layout, ext Extractor, settings Settings, logger *slog.Logger) *Worker {
	return &Worker{
		store:         st,
		layout:        layout,
		extractor:     ext,
		settings:      settings,
		logger:        logger,
		notify:        make(chan struct{}, 1),
		now:           func() time.Time { return time.Now().UTC() },
		pollInterval:  defaultPollInterval,
		watchInterval: defaultWatchInterval,
	}
}

// Notify wakes the worker to check the queue. It never blocks.
func (w *Worker) Notify() {
	select {
	case w.notify <- struct{}{}:
	default:
	}
}

// Run processes jobs until ctx is canceled. A job interrupted by shutdown stays running in the
// store and is recovered on the next startup.
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		w.drain(ctx)
		select {
		case <-ctx.Done():
			return nil
		case <-w.notify:
		case <-ticker.C:
		}
	}
}

// drain runs queued jobs until the queue is empty or ctx is canceled.
func (w *Worker) drain(ctx context.Context) {
	for ctx.Err() == nil {
		if !w.runOnce(ctx) {
			return
		}
	}
}

// runOnce claims and runs one job and reports whether a job was claimed.
func (w *Worker) runOnce(ctx context.Context) bool {
	j, err := w.store.ClaimNextJob(ctx, w.now())
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		w.logger.ErrorContext(ctx, "claim job failed")

		return false
	}
	w.process(ctx, j)

	return true
}

func (w *Worker) process(ctx context.Context, j job.Job) {
	available, err := w.layout.AvailableBytes()
	if err != nil {
		w.fail(ctx, j, job.ErrorInternal)

		return
	}
	if available < uint64(w.settings.MinFreeBytes)+uint64(w.settings.MaxJobBytes) { //nolint:gosec // Settings are validated as non-negative.
		w.fail(ctx, j, job.ErrorTooLarge)

		return
	}
	workDir, err := w.layout.PrepareWorkDir(j.ID)
	if err != nil {
		w.fail(ctx, j, job.ErrorInternal)

		return
	}
	defer func() {
		if err := w.layout.RemoveWorkDir(j.ID); err != nil {
			w.logger.ErrorContext(ctx, "remove work directory failed", "job_id", j.ID)
		}
	}()

	runCtx, cancelRun := context.WithCancelCause(ctx)
	defer cancelRun(nil)
	jobCtx, cancelTimeout := context.WithTimeoutCause(runCtx, w.settings.JobTimeout, errTimeout)
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		w.watch(jobCtx, j.ID, cancelRun)
	}()
	// Deferred calls run in reverse: stop the job context first, then wait for the watch to exit.
	defer func() { <-watchDone }()
	defer cancelTimeout()

	files, err := w.extractor.Extract(jobCtx, extractor.Request{
		URL:      j.SourceURL,
		Platform: j.Platform,
		WorkDir:  workDir,
		MaxBytes: w.settings.MaxJobBytes,
		MaxItems: w.settings.MaxJobItems,
	})
	switch cause := context.Cause(jobCtx); {
	case ctx.Err() != nil, errors.Is(cause, errCanceled):
		return
	case err != nil && errors.Is(cause, errTimeout):
		w.fail(ctx, j, job.ErrorTimeout)

		return
	case errors.Is(err, extractor.ErrTooLarge):
		w.fail(ctx, j, job.ErrorTooLarge)

		return
	case errors.Is(err, extractor.ErrExtractionFailed):
		w.fail(ctx, j, job.ErrorExtractionFailed)

		return
	case err != nil:
		w.fail(ctx, j, job.ErrorInternal)

		return
	}

	w.complete(ctx, j, files)
}

// watch cancels the run when the job row stops being running, which is how the API and CLI cancel work.
func (w *Worker) watch(ctx context.Context, jobID string, cancel context.CancelCauseFunc) {
	ticker := time.NewTicker(w.watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			status, err := w.store.JobStatus(ctx, jobID)
			if errors.Is(err, store.ErrNotFound) || (err == nil && status != job.StatusRunning) {
				cancel(errCanceled)

				return
			}
		}
	}
}

func (w *Worker) complete(ctx context.Context, j job.Job, files []extractor.File) {
	outputs := make([]storage.Output, 0, len(files))
	for _, file := range files {
		outputs = append(outputs, storage.Output{Name: file.Name, MediaType: file.MediaType})
	}
	items, err := w.layout.Ingest(j.ID, outputs, w.settings.MaxJobBytes, w.now())
	switch {
	case errors.Is(err, storage.ErrTooLarge):
		w.fail(ctx, j, job.ErrorTooLarge)

		return
	case errors.Is(err, storage.ErrInvalidOutput):
		w.fail(ctx, j, job.ErrorExtractionFailed)

		return
	case err != nil:
		w.fail(ctx, j, job.ErrorInternal)

		return
	}

	done := j
	_ = done.Transition(job.StatusSucceeded, "", w.now()) // Claimed jobs are running, so the transition is valid.
	err = w.store.CompleteJob(ctx, done, items)
	switch {
	case err == nil:
		w.logger.InfoContext(ctx, "job succeeded", "job_id", j.ID, "items", len(items))
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrNotFound):
		// The job was canceled or deleted while it ran; its files are no longer wanted.
		w.discard(ctx, j)
	default:
		w.discard(ctx, j)
		w.fail(ctx, j, job.ErrorInternal)
	}
}

func (w *Worker) discard(ctx context.Context, j job.Job) {
	if err := w.layout.RemoveJob(j.ID); err != nil {
		w.logger.ErrorContext(ctx, "remove job files failed", "job_id", j.ID)
	}
}

// fail records a failure unless the job was canceled or deleted meanwhile.
func (w *Worker) fail(ctx context.Context, j job.Job, code job.ErrorCode) {
	w.logger.WarnContext(ctx, "job failed", "job_id", j.ID, "platform", j.Platform, "error_code", string(code))
	failed := j
	_ = failed.Transition(job.StatusFailed, code, w.now()) // Claimed jobs are running, and code is a valid constant.
	err := w.store.UpdateJobStatus(ctx, failed, job.StatusRunning)
	if err != nil && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
		w.logger.ErrorContext(ctx, "record job failure failed", "job_id", j.ID)
	}
}
