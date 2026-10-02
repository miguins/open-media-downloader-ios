// Package worker processes queued jobs one at a time.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/logging"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

const (
	defaultPollInterval  = 5 * time.Second
	defaultWatchInterval = time.Second
	defaultRetryDelay    = 5 * time.Second
	// maxAttempts bounds extraction attempts for failures whose detail is transient.
	maxAttempts = 2
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
	store          *store.Store
	layout         *storage.Layout
	extractor      Extractor
	settings       Settings
	logger         *slog.Logger
	notify         chan struct{}
	now            func() time.Time
	pollInterval   time.Duration
	watchInterval  time.Duration
	retryDelay     time.Duration
	availableBytes func() (uint64, error)
	buildBundle    func(context.Context, string, []job.Item, int, int64, time.Time) (job.Bundle, error)
}

// New returns a Worker.
func New(st *store.Store, layout *storage.Layout, ext Extractor, settings Settings, logger *slog.Logger) *Worker {
	return &Worker{
		store:          st,
		layout:         layout,
		extractor:      ext,
		settings:       settings,
		logger:         logger,
		notify:         make(chan struct{}, 1),
		now:            func() time.Time { return time.Now().UTC() },
		pollInterval:   defaultPollInterval,
		watchInterval:  defaultWatchInterval,
		retryDelay:     defaultRetryDelay,
		availableBytes: layout.AvailableBytes,
		buildBundle:    layout.BuildBundle,
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
// Each attempt has its own trace, and a claimed job's identity is added to its log scope.
func (w *Worker) runOnce(ctx context.Context) bool {
	ctx = logging.NewTrace(ctx)
	j, err := w.store.ClaimNextJob(ctx, w.now())
	if errors.Is(err, store.ErrNotFound) {
		return false
	}
	if err != nil {
		if ctx.Err() == nil {
			w.logger.ErrorContext(ctx, "claim job failed")
		}

		return false
	}
	logging.Add(ctx, "job_id", j.ID, "platform", j.Platform, "key_id", j.OwnerID)
	if key, err := w.store.APIKey(ctx, j.OwnerID); err == nil {
		logging.Add(ctx, "key_name", key.Name)
	}
	w.logger.InfoContext(ctx, "job started")
	w.process(ctx, j)

	return true
}

func (w *Worker) process(ctx context.Context, j job.Job) {
	available, err := w.availableBytes()
	if err != nil {
		w.fail(ctx, j, job.ErrorInternal)

		return
	}
	// Merging or remuxing keeps the input and the output on disk at the same time.
	if available < uint64(w.settings.MinFreeBytes)+2*uint64(w.settings.MaxJobBytes)+uint64(storage.BundleOverhead(w.settings.MaxJobItems)) { //nolint:gosec // Settings are validated as non-negative.
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
			w.logger.ErrorContext(ctx, "remove work directory failed")
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

	files, err := w.extract(jobCtx, j, workDir)
	switch cause := context.Cause(jobCtx); {
	case ctx.Err() != nil:
		w.logger.InfoContext(ctx, "job interrupted by shutdown", "duration_ms", w.elapsed(j))

		return
	case errors.Is(cause, errCanceled):
		w.logger.InfoContext(ctx, "job canceled while running", "duration_ms", w.elapsed(j))

		return
	case err != nil && errors.Is(cause, errTimeout):
		w.fail(ctx, j, job.ErrorTimeout)

		return
	case errors.Is(err, extractor.ErrTooLarge):
		w.fail(ctx, j, job.ErrorTooLarge)

		return
	case errors.Is(err, extractor.ErrExtractionFailed):
		var failure *extractor.Failure
		_ = errors.As(err, &failure)
		w.failWith(ctx, j, job.ErrorExtractionFailed, failure)

		return
	case err != nil:
		w.fail(ctx, j, job.ErrorInternal)

		return
	}

	w.complete(ctx, jobCtx, j, files)
}

// extract runs the extractor, retrying once after a delay when the failure is transient. Each attempt
// starts from an empty work directory, and all attempts share the job's timeout and cancellation.
func (w *Worker) extract(ctx context.Context, j job.Job, workDir string) ([]extractor.File, error) {
	request := extractor.Request{
		URL:      j.SourceURL,
		Platform: j.Platform,
		WorkDir:  workDir,
		MaxBytes: w.settings.MaxJobBytes,
		MaxItems: w.settings.MaxJobItems,
	}
	for attempt := 1; ; attempt++ {
		files, err := w.extractor.Extract(ctx, request)
		var failure *extractor.Failure
		if attempt == maxAttempts || !errors.As(err, &failure) || !failure.Detail.Transient() {
			return files, err
		}
		w.logger.InfoContext(ctx, "retrying extraction",
			"attempt", attempt, "error_detail", string(failure.Detail), "delay_ms", w.retryDelay.Milliseconds())
		timer := time.NewTimer(w.retryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, err
		case <-timer.C:
		}
		if _, err := w.layout.PrepareWorkDir(j.ID); err != nil {
			return nil, err
		}
	}
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

func (w *Worker) complete(ctx, jobCtx context.Context, j job.Job, files []extractor.File) {
	items, bundle, err := w.prepareCompletion(jobCtx, j, files)
	if err == nil {
		err = jobCtx.Err()
	}
	if err != nil {
		w.completionFailure(ctx, jobCtx, j, err)
		return
	}
	done := j
	_ = done.Transition(job.StatusSucceeded, "", w.now())
	err = w.store.CompleteJob(jobCtx, done, items, bundle)
	if err != nil {
		w.completionFailure(ctx, jobCtx, j, err)
		return
	}
	var size int64
	for _, item := range items {
		size += item.SizeBytes
	}
	attrs := []any{"items", len(items), "bytes", size, "duration_ms", w.elapsed(j)}
	if bundle != nil {
		attrs = append(attrs, "bundle_bytes", bundle.SizeBytes)
	}
	w.logger.InfoContext(ctx, "job succeeded", attrs...)
}

func (w *Worker) prepareCompletion(ctx context.Context, j job.Job, files []extractor.File) ([]job.Item, *job.Bundle, error) {
	if len(files) > w.settings.MaxJobItems {
		return nil, nil, storage.ErrTooLarge
	}
	outputs := make([]storage.Output, 0, len(files))
	for _, file := range files {
		outputs = append(outputs, storage.Output{Name: file.Name, MediaType: file.MediaType})
	}
	items, err := w.layout.Ingest(ctx, j.ID, outputs, w.settings.MaxJobBytes, w.now())
	if err != nil {
		return nil, nil, err
	}
	if err = w.layout.RemoveWorkDir(j.ID); err != nil {
		return nil, nil, err
	}
	if len(items) == 1 {
		return items, nil, nil
	}
	var total int64
	for _, item := range items {
		total += item.SizeBytes
	}
	available, err := w.availableBytes()
	if err != nil {
		return nil, nil, err
	}
	//nolint:gosec // Configuration and ingested sizes are nonnegative and bounded.
	if available < uint64(w.settings.MinFreeBytes)+uint64(total)+uint64(storage.BundleOverhead(len(items))) {
		return nil, nil, storage.ErrTooLarge
	}
	bundle, err := w.buildBundle(ctx, j.ID, items, w.settings.MaxJobItems, w.settings.MaxJobBytes, w.now())
	if err != nil {
		return nil, nil, err
	}
	return items, &bundle, nil
}

func (w *Worker) completionFailure(ctx, jobCtx context.Context, j job.Job, err error) {
	w.discard(ctx, j)
	switch cause := context.Cause(jobCtx); {
	case ctx.Err() != nil:
		w.logger.InfoContext(ctx, "job interrupted by shutdown", "duration_ms", w.elapsed(j))
	case errors.Is(cause, errCanceled), errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrNotFound):
		w.logger.InfoContext(ctx, "job canceled while completing", "duration_ms", w.elapsed(j))
	case errors.Is(cause, errTimeout):
		w.fail(ctx, j, job.ErrorTimeout)
	case errors.Is(err, storage.ErrTooLarge):
		w.fail(ctx, j, job.ErrorTooLarge)
	case errors.Is(err, storage.ErrInvalidOutput):
		w.failWith(ctx, j, job.ErrorExtractionFailed, &extractor.Failure{Detail: job.DetailInvalidOutput})
	default:
		w.fail(ctx, j, job.ErrorInternal)
	}
}

func (w *Worker) discard(ctx context.Context, j job.Job) {
	if err := w.layout.RemoveJob(j.ID); err != nil {
		w.logger.ErrorContext(ctx, "remove job files failed")
	}
}

// fail records a failure without further detail.
func (w *Worker) fail(ctx context.Context, j job.Job, code job.ErrorCode) {
	w.failWith(ctx, j, code, nil)
}

// failWith records a failure, explained by failure when it is not nil, unless the job was canceled
// or deleted meanwhile. Sanitized tool diagnostics are logged only at debug level.
func (w *Worker) failWith(ctx context.Context, j job.Job, code job.ErrorCode, failure *extractor.Failure) {
	attrs := []any{"error_code", string(code), "duration_ms", w.elapsed(j)}
	failed := j
	if failure != nil {
		failed.ErrorDetail = failure.Detail
		attrs = append(attrs, "error_detail", string(failure.Detail))
		if failure.Tool != "" {
			attrs = append(attrs, "tool", failure.Tool, "exit_code", failure.ExitCode,
				"egress_rejected", failure.Egress.Rejected, "egress_failures", failure.Egress.UpstreamFailures)
		}
		if failure.Signal != "" {
			attrs = append(attrs, "signal", failure.Signal)
		}
	}
	w.logger.WarnContext(ctx, "job failed", attrs...)
	if failure != nil && len(failure.Diagnostics) > 0 {
		w.logger.DebugContext(ctx, "extractor diagnostics", "lines", failure.Diagnostics)
	}
	_ = failed.Transition(job.StatusFailed, code, w.now()) // Claimed jobs are running, and code is a valid constant.
	err := w.store.UpdateJobStatus(ctx, failed, job.StatusRunning)
	if err != nil && !errors.Is(err, store.ErrConflict) && !errors.Is(err, store.ErrNotFound) {
		w.logger.ErrorContext(ctx, "record job failure failed")
	}
}

// elapsed returns how long the job has run, in milliseconds.
func (w *Worker) elapsed(j job.Job) int64 {
	return w.now().Sub(j.StartedAt).Milliseconds()
}
