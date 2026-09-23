package cleanup

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

var now = time.UnixMilli(1_800_000_000_000).UTC()

type fixture struct {
	cleaner *Cleaner
	store   *store.Store
	dataDir string
	logs    *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
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
	if err := st.CreateAPIKey(context.Background(), store.APIKey{ID: "owner", Name: "owner", SecretHash: []byte("x"), CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	c := New(st, layout, slog.New(slog.NewJSONHandler(logs, nil)))
	c.now = func() time.Time { return now }

	return &fixture{cleaner: c, store: st, dataDir: dataDir, logs: logs}
}

func (f *fixture) addJob(t *testing.T, createdAt time.Time, status job.Status) job.Job {
	t.Helper()
	ctx := context.Background()
	j := job.New("owner", "https://vimeo.com/1", "vimeo", createdAt, time.Hour)
	if err := f.store.CreateJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	for _, next := range map[job.Status][]job.Status{
		job.StatusQueued:    nil,
		job.StatusRunning:   {job.StatusRunning},
		job.StatusCanceled:  {job.StatusCanceled},
		job.StatusSucceeded: {job.StatusRunning, job.StatusSucceeded},
	}[status] {
		from := j.Status
		if err := j.Transition(next, "", createdAt); err != nil {
			t.Fatal(err)
		}
		if err := f.store.UpdateJobStatus(ctx, j, from); err != nil {
			t.Fatal(err)
		}
	}
	f.mkdir(t, "jobs", j.ID)

	return j
}

func (f *fixture) mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{f.dataDir}, parts...)...)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}

	return path
}

func (f *fixture) exists(parts ...string) bool {
	_, err := os.Lstat(filepath.Join(append([]string{f.dataDir}, parts...)...))

	return err == nil
}

func TestRecover(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	running := f.addJob(t, now, job.StatusRunning)
	queued := f.addJob(t, now, job.StatusQueued)
	orphan := id.New()
	f.mkdir(t, "jobs", orphan)
	f.mkdir(t, "work", running.ID)

	if err := f.cleaner.Recover(ctx); err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	got, err := f.store.Job(ctx, "owner", running.ID)
	if err != nil || got.Status != job.StatusFailed || got.ErrorCode != job.ErrorInternal {
		t.Fatalf("recovered job = %#v, %v", got, err)
	}
	if got, _ := f.store.Job(ctx, "owner", queued.ID); got.Status != job.StatusQueued {
		t.Fatalf("queued job changed to %s", got.Status)
	}
	if f.exists("jobs", orphan) || f.exists("work", running.ID) {
		t.Fatal("Recover() left orphaned or work directories")
	}
	if !f.exists("jobs", queued.ID) || !f.exists("work") {
		t.Fatal("Recover() removed live data")
	}
}

func TestRecoverFailures(t *testing.T) {
	f := newFixture(t)
	_ = f.store.Close()
	if err := f.cleaner.Recover(context.Background()); err == nil {
		t.Fatal("Recover() error = nil with a closed store")
	}

	f = newFixture(t)
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	if err := os.Chmod(f.dataDir, 0o500); err != nil { //nolint:gosec // Read-only directory fixture.
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.dataDir, 0o700) }) //nolint:gosec // Restores the fixture for cleanup.
	if err := f.cleaner.Recover(context.Background()); err == nil {
		t.Fatal("Recover() error = nil when work cannot be cleared")
	}
}

func TestSweep(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	expired := f.addJob(t, now.Add(-2*time.Hour), job.StatusSucceeded)
	expiredQueued := f.addJob(t, now.Add(-2*time.Hour), job.StatusQueued)
	fresh := f.addJob(t, now, job.StatusSucceeded)
	orphan := id.New()
	f.mkdir(t, "jobs", orphan)

	item := job.Item{ID: "item", JobID: fresh.ID, FileName: "a", MediaType: "video/mp4", CreatedAt: now}
	if err := f.store.CreateItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	for _, token := range []job.DownloadToken{
		{Hash: []byte("old"), ItemID: item.ID, OwnerID: "owner", CreatedAt: now, ExpiresAt: now},
		{Hash: []byte("live"), ItemID: item.ID, OwnerID: "owner", CreatedAt: now, ExpiresAt: now.Add(time.Minute)},
	} {
		if err := f.store.CreateDownloadToken(ctx, token); err != nil {
			t.Fatal(err)
		}
	}

	f.cleaner.sweep(ctx)

	if exists, _ := f.store.JobExists(ctx, expired.ID); exists || f.exists("jobs", expired.ID) {
		t.Fatal("expired terminal job was not removed")
	}
	if exists, _ := f.store.JobExists(ctx, expiredQueued.ID); !exists {
		t.Fatal("expired queued job was removed")
	}
	if exists, _ := f.store.JobExists(ctx, fresh.ID); !exists || !f.exists("jobs", fresh.ID) {
		t.Fatal("fresh job was removed")
	}
	if f.exists("jobs", orphan) {
		t.Fatal("orphaned directory was not removed")
	}
	if _, err := f.store.ValidDownloadToken(ctx, []byte("live"), now); err != nil {
		t.Fatalf("live token removed: %v", err)
	}
	if deleted, _ := f.store.DeleteExpiredDownloadTokens(ctx, now); deleted != 0 {
		t.Fatal("expired token was not removed")
	}
}

func TestSweepLogsFailures(t *testing.T) {
	f := newFixture(t)
	f.mkdir(t, "jobs", id.New())
	_ = f.store.Close()
	f.cleaner.sweep(context.Background())
	for _, message := range []string{"delete expired download tokens failed", "list expired jobs failed", "sweep orphaned job directories failed"} {
		if !strings.Contains(f.logs.String(), message) {
			t.Fatalf("missing %q in logs: %s", message, f.logs.String())
		}
	}
	if strings.Contains(f.logs.String(), f.dataDir) {
		t.Fatal("logs leaked the data directory")
	}
}

func TestSweepLogsJobDirectoryFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	f := newFixture(t)
	expired := f.addJob(t, now.Add(-2*time.Hour), job.StatusSucceeded)
	if err := os.WriteFile(filepath.Join(f.dataDir, "jobs", expired.ID, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	jobs := filepath.Join(f.dataDir, "jobs")
	if err := os.Chmod(jobs, 0o500); err != nil { //nolint:gosec // Read-only directory fixture.
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(jobs, 0o700) }) //nolint:gosec // Restores the fixture for cleanup.

	f.cleaner.sweep(context.Background())
	if !strings.Contains(f.logs.String(), "remove expired job failed") {
		t.Fatalf("logs = %s", f.logs.String())
	}
	if exists, _ := f.store.JobExists(context.Background(), expired.ID); !exists {
		t.Fatal("job row deleted although its files remain")
	}
}

func TestRunSweepsUntilCanceled(t *testing.T) {
	f := newFixture(t)
	f.cleaner.interval = 5 * time.Millisecond
	orphan := id.New()
	f.mkdir(t, "jobs", orphan)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.cleaner.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for f.exists("jobs", orphan) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil || f.exists("jobs", orphan) {
		t.Fatalf("Run() = %v; orphan exists = %v", err, f.exists("jobs", orphan))
	}
}

func TestNewDefaults(t *testing.T) {
	c := New(nil, nil, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if c.interval != time.Minute || time.Since(c.now()) > time.Minute {
		t.Fatalf("New() = %#v", c)
	}
}
