package worker

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
)

func mixedExtractor(ctx context.Context, r extractor.Request) ([]extractor.File, error) {
	for name, body := range map[string]string{"video": "abc", "photo": "defg", "leftover": "unused"} {
		if err := os.WriteFile(filepath.Join(r.WorkDir, name), []byte(body), 0o600); err != nil {
			return nil, err
		}
	}
	return []extractor.File{{Name: "video", MediaType: "video/mp4"}, {Name: "photo", MediaType: "image/jpeg"}}, ctx.Err()
}
func TestWorkerCompletesMixedMediaBundle(t *testing.T) {
	f := newFixture(t, extractFunc(mixedExtractor), defaultSettings())
	j := f.enqueue(t)
	if !f.worker.runOnce(context.Background()) {
		t.Fatal("job not claimed")
	}
	done := f.job(t, j.ID)
	if done.Status != job.StatusSucceeded {
		t.Fatalf("job: %#v", done)
	}
	b, err := f.store.Bundle(context.Background(), "owner", j.ID)
	if err != nil || b.SizeBytes <= 7 {
		t.Fatalf("bundle %#v %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(f.dataDir, "work", j.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("extraction leftovers retained")
	}
}
func TestWorkerBundleDiskAdmission(t *testing.T) {
	for _, name := range []string{"initial below", "exact", "build below", "disk unavailable"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, extractFunc(mixedExtractor), defaultSettings())
			j := f.enqueue(t)
			calls := 0
			f.worker.availableBytes = func() (uint64, error) {
				calls++
				if name == "disk unavailable" {
					return 0, errors.New("disk unavailable")
				}
				if calls == 1 {
					if name == "initial below" {
						return 2118655, nil
					}
					return 2118656, nil
				}
				if name == "build below" {
					return 3078, nil
				}
				return 3079, nil
			}
			f.worker.runOnce(context.Background())
			done := f.job(t, j.ID)
			if name == "exact" {
				if done.Status != job.StatusSucceeded {
					t.Fatalf("exact admission %#v", done)
				}
			} else {
				want := job.ErrorTooLarge
				if name == "disk unavailable" {
					want = job.ErrorInternal
				}
				if done.Status != job.StatusFailed || done.ErrorCode != want {
					t.Fatalf("admission %#v", done)
				}
				f.assertNoFiles(t, j.ID)
			}
		})
	}
}
func TestWorkerBundleFailures(t *testing.T) {
	for _, name := range []string{"size", "unsafe", "write", "item count"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, extractFunc(mixedExtractor), defaultSettings())
			j := f.enqueue(t)
			if name == "item count" {
				f.worker.settings.MaxJobItems = 1
			} else {
				f.worker.buildBundle = func(context.Context, string, []job.Item, int, int64, time.Time) (job.Bundle, error) {
					switch name {
					case "size":
						return job.Bundle{}, storage.ErrTooLarge
					case "unsafe":
						return job.Bundle{}, storage.ErrInvalidOutput
					default:
						if err := os.WriteFile(filepath.Join(f.dataDir, "jobs", j.ID, ".bundle-partial"), []byte("partial ZIP"), 0o600); err != nil {
							t.Fatal(err)
						}
						return job.Bundle{}, errors.New("disk full")
					}
				}
			}
			f.worker.runOnce(context.Background())
			done := f.job(t, j.ID)
			want := job.ErrorInternal
			if name == "size" || name == "item count" {
				want = job.ErrorTooLarge
			}
			if name == "unsafe" {
				want = job.ErrorExtractionFailed
			}
			if done.Status != job.StatusFailed || done.ErrorCode != want {
				t.Fatalf("failure %#v", done)
			}
			f.assertNoFiles(t, j.ID)
		})
	}
}

func TestWorkerDiscardsInstalledBundleWhenCompletionFails(t *testing.T) {
	f := newFixture(t, extractFunc(mixedExtractor), defaultSettings())
	j := f.enqueue(t)
	db, err := sql.Open("sqlite", filepath.Join(f.dataDir, "omdi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(), `CREATE TRIGGER reject_bundle BEFORE INSERT ON bundles BEGIN SELECT RAISE(ABORT, 'completion failed'); END`); err != nil {
		t.Fatal(err)
	}
	build := f.worker.buildBundle
	installed := false
	f.worker.buildBundle = func(ctx context.Context, id string, items []job.Item, maxItems int, maxBytes int64, now time.Time) (job.Bundle, error) {
		bundle, err := build(ctx, id, items, maxItems, maxBytes, now)
		if err == nil {
			file, openErr := f.worker.layout.OpenBundle(id, bundle.SizeBytes)
			if openErr != nil {
				t.Fatal(openErr)
			}
			_ = file.Close()
			installed = true
		}
		return bundle, err
	}
	f.worker.runOnce(context.Background())
	if !installed {
		t.Fatal("bundle was never installed")
	}
	done := f.job(t, j.ID)
	if done.Status != job.StatusFailed || done.ErrorCode != job.ErrorInternal {
		t.Fatalf("completion failure: %#v", done)
	}
	f.assertNoFiles(t, j.ID)
	items, err := f.store.Items(context.Background(), "owner", j.ID)
	if err != nil || len(items) != 0 {
		t.Fatalf("partial items persisted: %#v %v", items, err)
	}
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM bundles").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial bundle persisted: %d %v", count, err)
	}
}
func TestWorkerBundleCancellationAndShutdown(t *testing.T) {
	for _, name := range []string{"timeout", "cancel", "purge", "shutdown"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, extractFunc(mixedExtractor), defaultSettings())
			j := f.enqueue(t)
			started := make(chan struct{})
			finished := make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "timeout" {
				f.worker.settings.JobTimeout = 40 * time.Millisecond
			}
			f.worker.buildBundle = func(bctx context.Context, _ string, _ []job.Item, _ int, _ int64, _ time.Time) (job.Bundle, error) {
				close(started)
				<-bctx.Done()
				return job.Bundle{}, bctx.Err()
			}
			go func() { defer close(finished); f.worker.runOnce(ctx) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("builder not started")
			}
			switch name {
			case "cancel":
				claimed := f.job(t, j.ID)
				from := claimed.Status
				_ = claimed.Transition(job.StatusCanceled, "", time.Now())
				if err := f.store.UpdateJobStatus(context.Background(), claimed, from); err != nil {
					t.Fatal(err)
				}
			case "purge":
				if _, err := f.store.PurgeJobs(context.Background(), "owner", time.Now()); err != nil {
					t.Fatal(err)
				}
			case "shutdown":
				cancel()
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("builder did not stop")
			}
			if name == "purge" {
				f.assertNoFiles(t, j.ID)
				return
			}
			done := f.job(t, j.ID)
			if done.Status == job.StatusSucceeded {
				t.Fatal("canceled work succeeded")
			}
			if name == "timeout" && done.ErrorCode != job.ErrorTimeout {
				t.Fatalf("timeout %#v", done)
			}
			if name != "shutdown" {
				f.assertNoFiles(t, j.ID)
			}
		})
	}
}
