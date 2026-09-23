package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

type extractFunc func(ctx context.Context, request extractor.Request) ([]extractor.File, error)

func (f extractFunc) Extract(ctx context.Context, request extractor.Request) ([]extractor.File, error) {
	return f(ctx, request)
}

type fixture struct {
	worker  *Worker
	store   *store.Store
	dataDir string
	logs    *syncBuffer
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func newFixture(t *testing.T, ext Extractor, settings Settings) *fixture {
	t.Helper()
	dataDir := t.TempDir()
	st, err := store.Open(context.Background(), dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	layout, err := storage.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAPIKey(context.Background(), store.APIKey{ID: "owner", Name: "owner", SecretHash: []byte("x"), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	w := New(st, layout, ext, settings, slog.New(slog.NewJSONHandler(logs, nil)))
	w.watchInterval = 5 * time.Millisecond
	w.pollInterval = 10 * time.Millisecond

	return &fixture{worker: w, store: st, dataDir: dataDir, logs: logs}
}

func defaultSettings() Settings {
	return Settings{JobTimeout: time.Minute, MaxJobBytes: 1 << 20, MinFreeBytes: 0}
}

func (f *fixture) enqueue(t *testing.T) job.Job {
	t.Helper()
	j := job.New("owner", "https://vimeo.com/1", "vimeo", time.Now().UTC(), time.Hour)
	if err := f.store.CreateJob(context.Background(), j); err != nil {
		t.Fatal(err)
	}

	return j
}

func (f *fixture) job(t *testing.T, id string) job.Job {
	t.Helper()
	j, err := f.store.Job(context.Background(), "owner", id)
	if err != nil {
		t.Fatalf("Job() error = %v", err)
	}

	return j
}

func (f *fixture) assertNoFiles(t *testing.T, jobID string) {
	t.Helper()
	for _, dir := range []string{"jobs", "work"} {
		if _, err := os.Lstat(filepath.Join(f.dataDir, dir, jobID)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s directory for job still exists", dir)
		}
	}
}

func writeFile(name, mediaType, contents string) extractFunc {
	return func(_ context.Context, request extractor.Request) ([]extractor.File, error) {
		if err := os.WriteFile(filepath.Join(request.WorkDir, name), []byte(contents), 0o600); err != nil {
			return nil, err
		}

		return []extractor.File{{Name: name, MediaType: mediaType}}, nil
	}
}

func TestRunOnceSucceeds(t *testing.T) {
	var got extractor.Request
	ext := extractFunc(func(ctx context.Context, request extractor.Request) ([]extractor.File, error) {
		got = request

		return writeFile("out.mp4", "video/mp4", "media")(ctx, request)
	})
	f := newFixture(t, ext, defaultSettings())
	j := f.enqueue(t)

	if !f.worker.runOnce(context.Background()) {
		t.Fatal("runOnce() = false with a queued job")
	}
	if got.URL != j.SourceURL || got.Platform != "vimeo" || got.MaxBytes != 1<<20 || got.WorkDir == "" {
		t.Fatalf("extractor request = %#v", got)
	}
	done := f.job(t, j.ID)
	if done.Status != job.StatusSucceeded || done.StartedAt.IsZero() || done.FinishedAt.IsZero() {
		t.Fatalf("job = %#v", done)
	}
	items, err := f.store.Items(context.Background(), "owner", j.ID)
	if err != nil || len(items) != 1 || items[0].SizeBytes != 5 {
		t.Fatalf("items = %#v, %v", items, err)
	}
	if _, err := os.Lstat(filepath.Join(f.dataDir, "jobs", j.ID, items[0].ID)); err != nil {
		t.Fatalf("stored file missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(f.dataDir, "work", j.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("work directory was not removed")
	}
	if f.worker.runOnce(context.Background()) {
		t.Fatal("runOnce() = true with an empty queue")
	}
}

func TestRunOnceFailures(t *testing.T) {
	blockUntilDone := extractFunc(func(ctx context.Context, _ extractor.Request) ([]extractor.File, error) {
		<-ctx.Done()

		return nil, ctx.Err()
	})
	tests := []struct {
		name     string
		ext      Extractor
		settings func(*Settings)
		want     job.ErrorCode
	}{
		{name: "extractor error", ext: extractFunc(func(context.Context, extractor.Request) ([]extractor.File, error) {
			return nil, errors.New("raw extractor diagnostic with https://signed.example/secret")
		}), want: job.ErrorExtractionFailed},
		{name: "unsafe output", ext: writeFile("../escape", "video/mp4", "x"), want: job.ErrorExtractionFailed},
		{name: "unsupported type", ext: writeFile("page.html", "text/html", "x"), want: job.ErrorExtractionFailed},
		{name: "over budget", ext: writeFile("big.mp4", "video/mp4", strings.Repeat("x", 11)),
			settings: func(s *Settings) { s.MaxJobBytes = 10 }, want: job.ErrorTooLarge},
		{name: "timeout", ext: blockUntilDone, settings: func(s *Settings) { s.JobTimeout = 20 * time.Millisecond }, want: job.ErrorTimeout},
		{name: "insufficient disk", ext: writeFile("a.mp4", "video/mp4", "x"),
			settings: func(s *Settings) { s.MinFreeBytes = 1 << 62 }, want: job.ErrorTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := defaultSettings()
			if tt.settings != nil {
				tt.settings(&settings)
			}
			f := newFixture(t, tt.ext, settings)
			j := f.enqueue(t)
			f.worker.runOnce(context.Background())

			failed := f.job(t, j.ID)
			if failed.Status != job.StatusFailed || failed.ErrorCode != tt.want {
				t.Fatalf("job = %s/%s; want failed/%s", failed.Status, failed.ErrorCode, tt.want)
			}
			f.assertNoFiles(t, j.ID)
			if strings.Contains(f.logs.String(), "signed.example") || strings.Contains(f.logs.String(), f.dataDir) {
				t.Fatalf("logs leaked details: %s", f.logs.String())
			}
		})
	}
}

// blockingExtractor signals when it starts and waits for cancellation.
func blockingExtractor(started chan<- struct{}) extractFunc {
	return func(ctx context.Context, request extractor.Request) ([]extractor.File, error) {
		if err := os.WriteFile(filepath.Join(request.WorkDir, "partial"), []byte("x"), 0o600); err != nil {
			return nil, err
		}
		close(started)
		<-ctx.Done()

		return nil, ctx.Err()
	}
}

func TestCancellationThroughDatabase(t *testing.T) {
	started := make(chan struct{})
	f := newFixture(t, blockingExtractor(started), defaultSettings())
	j := f.enqueue(t)

	done := make(chan struct{})
	go func() {
		f.worker.runOnce(context.Background())
		close(done)
	}()
	<-started
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
		t.Fatal("worker did not observe cancellation")
	}
	if got := f.job(t, j.ID); got.Status != job.StatusCanceled || got.ErrorCode != "" {
		t.Fatalf("job = %#v", got)
	}
	f.assertNoFiles(t, j.ID)
}

func TestPurgeDuringRun(t *testing.T) {
	started := make(chan struct{})
	f := newFixture(t, blockingExtractor(started), defaultSettings())
	j := f.enqueue(t)

	done := make(chan struct{})
	go func() {
		f.worker.runOnce(context.Background())
		close(done)
	}()
	<-started
	if _, err := f.store.PurgeJobs(context.Background(), "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not observe deletion")
	}
	if exists, _ := f.store.JobExists(context.Background(), j.ID); exists {
		t.Fatal("purged job reappeared")
	}
	f.assertNoFiles(t, j.ID)
}

func TestCompletionLosesRaceToCancellation(t *testing.T) {
	var f *fixture
	var jobID string
	ext := extractFunc(func(ctx context.Context, request extractor.Request) ([]extractor.File, error) {
		running := f.job(t, jobID)
		if err := running.Transition(job.StatusCanceled, "", time.Now().UTC()); err != nil {
			return nil, err
		}
		if err := f.store.UpdateJobStatus(context.Background(), running, job.StatusRunning); err != nil {
			return nil, err
		}

		return writeFile("a.mp4", "video/mp4", "x")(ctx, request)
	})
	f = newFixture(t, ext, defaultSettings())
	f.worker.watchInterval = time.Hour // the watch cannot win; only the completion check can
	jobID = f.enqueue(t).ID

	f.worker.runOnce(context.Background())
	if got := f.job(t, jobID); got.Status != job.StatusCanceled {
		t.Fatalf("job status = %s; want canceled", got.Status)
	}
	if items, _ := f.store.Items(context.Background(), "owner", jobID); len(items) != 0 {
		t.Fatalf("items recorded for canceled job: %#v", items)
	}
	f.assertNoFiles(t, jobID)
}

func TestShutdownLeavesJobForRecovery(t *testing.T) {
	started := make(chan struct{})
	f := newFixture(t, blockingExtractor(started), defaultSettings())
	j := f.enqueue(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		f.worker.runOnce(ctx)
		close(done)
	}()
	<-started
	cancel()
	<-done
	if got := f.job(t, j.ID); got.Status != job.StatusRunning {
		t.Fatalf("job status = %s; want running for startup recovery", got.Status)
	}
	if _, err := os.Lstat(filepath.Join(f.dataDir, "work", j.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("work directory was not removed")
	}
}

func TestRunProcessesNotifiedAndPolledJobs(t *testing.T) {
	f := newFixture(t, writeFile("a.mp4", "video/mp4", "x"), defaultSettings())
	f.worker.pollInterval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() { runErr <- f.worker.Run(ctx) }()

	j := f.enqueue(t)
	f.worker.Notify()
	f.worker.Notify() // a second notification must not block
	waitForStatus(t, f, j.ID, job.StatusSucceeded)

	f.worker.pollInterval = 10 * time.Millisecond
	cancel()
	if err := <-runErr; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestRunPollsWithoutNotification(t *testing.T) {
	f := newFixture(t, writeFile("a.mp4", "video/mp4", "x"), defaultSettings())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = f.worker.Run(ctx) }()

	j := f.enqueue(t)
	waitForStatus(t, f, j.ID, job.StatusSucceeded)
}

func waitForStatus(t *testing.T, f *fixture, id string, want job.Status) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f.job(t, id).Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("job did not reach %s", want)
}

func TestStoreFailuresAreLogged(t *testing.T) {
	f := newFixture(t, writeFile("a.mp4", "video/mp4", "x"), defaultSettings())
	_ = f.store.Close()
	if f.worker.runOnce(context.Background()) {
		t.Fatal("runOnce() = true with a closed store")
	}
	if !strings.Contains(f.logs.String(), "claim job failed") {
		t.Fatalf("logs = %s", f.logs.String())
	}
}

func readOnlyDir(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	if err := os.Chmod(path, 0o500); err != nil { //nolint:gosec // Read-only directory fixture.
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o700) }) //nolint:gosec // Restores the fixture for cleanup.
}

func TestStorageFailuresFailJobAsInternal(t *testing.T) {
	tests := map[string]func(t *testing.T, f *fixture){
		"work root read-only": func(t *testing.T, f *fixture) { readOnlyDir(t, filepath.Join(f.dataDir, "work")) },
		"jobs root read-only": func(t *testing.T, f *fixture) { readOnlyDir(t, filepath.Join(f.dataDir, "jobs")) },
		"data directory unavailable": func(t *testing.T, f *fixture) {
			f.worker.layout = removedLayout(t)
		},
	}
	for name, setup := range tests {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, writeFile("a.mp4", "video/mp4", "x"), defaultSettings())
			j := f.enqueue(t)
			setup(t, f)
			f.worker.runOnce(context.Background())
			if got := f.job(t, j.ID); got.Status != job.StatusFailed || got.ErrorCode != job.ErrorInternal {
				t.Fatalf("job = %s/%s; want failed/internal", got.Status, got.ErrorCode)
			}
		})
	}
}

// removedLayout returns a layout whose data directory no longer exists.
func removedLayout(t *testing.T) *storage.Layout {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	layout, err := storage.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	return layout
}

func TestDatabaseFailureAfterExtractionIsLogged(t *testing.T) {
	var f *fixture
	ext := extractFunc(func(ctx context.Context, request extractor.Request) ([]extractor.File, error) {
		_ = f.store.Close()

		return writeFile("a.mp4", "video/mp4", "x")(ctx, request)
	})
	f = newFixture(t, ext, defaultSettings())
	j := f.enqueue(t)
	f.worker.runOnce(context.Background())

	if !strings.Contains(f.logs.String(), "record job failure failed") {
		t.Fatalf("logs = %s", f.logs.String())
	}
	f.assertNoFiles(t, j.ID)
}

func TestNewDefaults(t *testing.T) {
	w := New(nil, nil, nil, defaultSettings(), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if w.pollInterval != defaultPollInterval || w.watchInterval != defaultWatchInterval || time.Since(w.now()) > time.Minute {
		t.Fatalf("New() = %#v", w)
	}
}
