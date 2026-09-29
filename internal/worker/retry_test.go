package worker

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/job"
)

// flakyExtractor fails with detail for the first failures calls, leaving a partial file behind,
// and then writes one media file into a work directory that must be empty.
func flakyExtractor(calls *atomic.Int32, failures int32, detail job.ErrorDetail) extractFunc {
	return func(ctx context.Context, request extractor.Request) ([]extractor.File, error) {
		call := calls.Add(1)
		entries, err := os.ReadDir(request.WorkDir)
		if err != nil || len(entries) != 0 {
			return nil, errUnexpected
		}
		if call <= failures {
			_ = os.WriteFile(filepath.Join(request.WorkDir, "partial"), []byte("x"), 0o600)
			return nil, &extractor.Failure{Detail: detail, Tool: "yt-dlp", ExitCode: 1}
		}

		return writeFile("out.mp4", "video/mp4", "media")(ctx, request)
	}
}

type unexpectedError struct{}

func (unexpectedError) Error() string { return "work directory was not reset" }

var errUnexpected error = unexpectedError{}

func TestTransientFailureIsRetriedOnce(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, flakyExtractor(&calls, 1, job.DetailForbidden), defaultSettings())
	f.worker.retryDelay = time.Millisecond
	j := f.enqueue(t)

	f.worker.runOnce(context.Background())

	if got := f.job(t, j.ID); got.Status != job.StatusSucceeded || calls.Load() != 2 {
		t.Fatalf("job = %s after %d calls; want succeeded after a retry", got.Status, calls.Load())
	}
	retry := findLog(t, f.logLines(t), "retrying extraction")
	if retry["attempt"] != float64(1) || retry["error_detail"] != "forbidden" || retry["delay_ms"] != float64(1) || retry["job_id"] != j.ID {
		t.Fatalf("retry log = %v", retry)
	}
}

func TestRetryIsBounded(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, flakyExtractor(&calls, 5, job.DetailNetworkError), defaultSettings())
	f.worker.retryDelay = time.Millisecond
	j := f.enqueue(t)

	f.worker.runOnce(context.Background())

	got := f.job(t, j.ID)
	if got.Status != job.StatusFailed || got.ErrorDetail != job.DetailNetworkError || calls.Load() != 2 {
		t.Fatalf("job = %s/%s after %d calls; want failed after two attempts", got.Status, got.ErrorDetail, calls.Load())
	}
	f.assertNoFiles(t, j.ID)
}

func TestPersistentFailureIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, flakyExtractor(&calls, 1, job.DetailBlocked), defaultSettings())
	f.worker.retryDelay = time.Millisecond
	j := f.enqueue(t)

	f.worker.runOnce(context.Background())

	if got := f.job(t, j.ID); got.Status != job.StatusFailed || got.ErrorDetail != job.DetailBlocked || calls.Load() != 1 {
		t.Fatalf("job = %s/%s after %d calls; want one attempt", got.Status, got.ErrorDetail, calls.Load())
	}
}

func TestCancellationDuringRetryDelay(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, flakyExtractor(&calls, 1, job.DetailRateLimited), defaultSettings())
	f.worker.retryDelay = time.Hour
	j := f.enqueue(t)
	done := make(chan struct{})
	go func() {
		f.worker.runOnce(context.Background())
		close(done)
	}()
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	running := f.job(t, j.ID)
	if err := running.Transition(job.StatusCanceled, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateJobStatus(context.Background(), running, job.StatusRunning); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker waited out the retry delay after cancellation")
	}
	if got := f.job(t, j.ID); got.Status != job.StatusCanceled || calls.Load() != 1 {
		t.Fatalf("job = %s after %d calls; want canceled without a retry", got.Status, calls.Load())
	}
}

func TestTimeoutDuringRetryDelay(t *testing.T) {
	var calls atomic.Int32
	settings := defaultSettings()
	settings.JobTimeout = 50 * time.Millisecond
	f := newFixture(t, flakyExtractor(&calls, 1, job.DetailForbidden), settings)
	f.worker.retryDelay = time.Hour
	j := f.enqueue(t)

	f.worker.runOnce(context.Background())

	if got := f.job(t, j.ID); got.ErrorCode != job.ErrorTimeout || calls.Load() != 1 {
		t.Fatalf("job = %s after %d calls; want a timeout without a retry", got.ErrorCode, calls.Load())
	}
}

func TestRetryReportsWorkDirectoryFailure(t *testing.T) {
	var calls atomic.Int32
	f := newFixture(t, nil, defaultSettings())
	f.worker.extractor = extractFunc(func(context.Context, extractor.Request) ([]extractor.File, error) {
		calls.Add(1)
		// Replacing the work root with a file makes resetting the work directory fail.
		if err := os.RemoveAll(filepath.Join(f.dataDir, "work")); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(f.dataDir, "work"), nil, 0o600); err != nil {
			return nil, err
		}
		return nil, &extractor.Failure{Detail: job.DetailForbidden}
	})
	f.worker.retryDelay = time.Millisecond
	j := f.enqueue(t)

	f.worker.runOnce(context.Background())

	if got := f.job(t, j.ID); got.ErrorCode != job.ErrorInternal || calls.Load() != 1 {
		t.Fatalf("job = %s after %d calls; want an internal error", got.ErrorCode, calls.Load())
	}
}
