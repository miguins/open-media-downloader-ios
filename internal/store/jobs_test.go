package store

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

func createTestJob(t *testing.T, s *Store, ownerID string) job.Job {
	t.Helper()
	j := job.New(ownerID, "https://vimeo.com/1", "vimeo", testNow, time.Hour)
	if err := s.CreateJob(context.Background(), j); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}

	return j
}

func TestJobOwnershipAndTransitions(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	createTestKey(t, s, "intruder", "intruder")
	created := createTestJob(t, s, "owner")

	got, err := s.Job(ctx, "owner", created.ID)
	if err != nil || !reflect.DeepEqual(got, created) {
		t.Fatalf("Job() = %#v, %v; want %#v", got, err, created)
	}
	if _, err := s.Job(ctx, "intruder", created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Job() error = %v; want ErrNotFound", err)
	}
	if _, err := s.Job(ctx, "owner", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing Job() error = %v; want ErrNotFound", err)
	}

	running := got
	if err := running.Transition(job.StatusRunning, "", testNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateJobStatus(ctx, running, job.StatusQueued); err != nil {
		t.Fatalf("UpdateJobStatus() error = %v", err)
	}
	failed := running
	if err := failed.Transition(job.StatusFailed, job.ErrorTimeout, testNow.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateJobStatus(ctx, failed, job.StatusRunning); err != nil {
		t.Fatalf("UpdateJobStatus() error = %v", err)
	}
	got, err = s.Job(ctx, "owner", created.ID)
	if err != nil || !reflect.DeepEqual(got, failed) {
		t.Fatalf("Job() = %#v, %v; want %#v", got, err, failed)
	}

	if err := s.UpdateJobStatus(ctx, failed, job.StatusRunning); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale UpdateJobStatus() error = %v; want ErrConflict", err)
	}
	stranger := failed
	stranger.OwnerID = "intruder"
	if err := s.UpdateJobStatus(ctx, stranger, job.StatusFailed); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner UpdateJobStatus() error = %v; want ErrNotFound", err)
	}
}

func TestCreateJobRequiresExistingOwner(t *testing.T) {
	s := openTestStore(t)
	j := job.New("missing", "https://vimeo.com/1", "vimeo", testNow, time.Hour)
	if err := s.CreateJob(context.Background(), j); err == nil {
		t.Fatal("CreateJob() error = nil for unknown owner")
	}
}

func TestItemsAreOwnerScoped(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	createTestKey(t, s, "intruder", "intruder")
	j := createTestJob(t, s, "owner")

	second := job.Item{ID: "item2", JobID: j.ID, Position: 1, FileName: "b.jpg", MediaType: "image/jpeg", SizeBytes: 2, CreatedAt: testNow}
	first := job.Item{ID: "item1", JobID: j.ID, Position: 0, FileName: "a.mp4", MediaType: "video/mp4", SizeBytes: 1, CreatedAt: testNow}
	for _, item := range []job.Item{second, first} {
		if err := s.CreateItem(ctx, item); err != nil {
			t.Fatalf("CreateItem() error = %v", err)
		}
	}
	duplicate := first
	duplicate.ID = "item3"
	if err := s.CreateItem(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate position error = %v; want ErrConflict", err)
	}

	items, err := s.Items(ctx, "owner", j.ID)
	if err != nil || !reflect.DeepEqual(items, []job.Item{first, second}) {
		t.Fatalf("Items() = %#v, %v", items, err)
	}
	if items, err := s.Items(ctx, "intruder", j.ID); err != nil || len(items) != 0 {
		t.Fatalf("cross-owner Items() = %#v, %v; want empty", items, err)
	}
}

func TestDownloadTokens(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	j := createTestJob(t, s, "owner")
	item := job.Item{ID: "item1", JobID: j.ID, FileName: "a.mp4", MediaType: "video/mp4", CreatedAt: testNow}
	if err := s.CreateItem(ctx, item); err != nil {
		t.Fatal(err)
	}

	live := job.DownloadToken{Hash: []byte("live"), ItemID: item.ID, OwnerID: "owner", CreatedAt: testNow, ExpiresAt: testNow.Add(time.Minute)}
	expired := job.DownloadToken{Hash: []byte("expired"), ItemID: item.ID, OwnerID: "owner", CreatedAt: testNow, ExpiresAt: testNow}
	for _, token := range []job.DownloadToken{live, expired} {
		if err := s.CreateDownloadToken(ctx, token); err != nil {
			t.Fatalf("CreateDownloadToken() error = %v", err)
		}
	}
	if err := s.CreateDownloadToken(ctx, live); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate token error = %v; want ErrConflict", err)
	}

	got, err := s.ValidDownloadToken(ctx, []byte("live"), testNow)
	if err != nil || !reflect.DeepEqual(got, live) {
		t.Fatalf("ValidDownloadToken() = %#v, %v", got, err)
	}
	for _, hash := range []string{"expired", "missing"} {
		if _, err := s.ValidDownloadToken(ctx, []byte(hash), testNow); !errors.Is(err, ErrNotFound) {
			t.Fatalf("ValidDownloadToken(%s) error = %v; want ErrNotFound", hash, err)
		}
	}

	deleted, err := s.DeleteExpiredDownloadTokens(ctx, testNow)
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteExpiredDownloadTokens() = %d, %v; want 1", deleted, err)
	}
	if _, err := s.ValidDownloadToken(ctx, []byte("live"), testNow); err != nil {
		t.Fatalf("live token removed: %v", err)
	}
}

func TestJobRepositoriesFailAfterClose(t *testing.T) {
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	ctx := context.Background()
	j := job.New("owner", "https://vimeo.com/1", "vimeo", testNow, time.Hour)
	checks := map[string]error{
		"CreateJob":           s.CreateJob(ctx, j),
		"UpdateJobStatus":     s.UpdateJobStatus(ctx, j, job.StatusQueued),
		"CreateItem":          s.CreateItem(ctx, job.Item{}),
		"CreateDownloadToken": s.CreateDownloadToken(ctx, job.DownloadToken{}),
	}
	_, checks["Job"] = s.Job(ctx, "owner", j.ID)
	_, checks["Items"] = s.Items(ctx, "owner", j.ID)
	_, checks["ValidDownloadToken"] = s.ValidDownloadToken(ctx, nil, testNow)
	_, checks["DeleteExpiredDownloadTokens"] = s.DeleteExpiredDownloadTokens(ctx, testNow)
	for name, err := range checks {
		if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
			t.Fatalf("%s() error = %v; want database error", name, err)
		}
	}
}

func TestJobErrorDetailRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	created := createTestJob(t, s, "owner")
	if got, err := s.Job(ctx, "owner", created.ID); err != nil || got.ErrorDetail != "" {
		t.Fatalf("new job detail = %q, %v; want none", got.ErrorDetail, err)
	}

	running := created
	if err := running.Transition(job.StatusRunning, "", testNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateJobStatus(ctx, running, job.StatusQueued); err != nil {
		t.Fatal(err)
	}
	failed := running
	if err := failed.Transition(job.StatusFailed, job.ErrorExtractionFailed, testNow.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	failed.ErrorDetail = job.DetailLoginRequired
	if err := s.UpdateJobStatus(ctx, failed, job.StatusRunning); err != nil {
		t.Fatalf("UpdateJobStatus() error = %v", err)
	}
	got, err := s.Job(ctx, "owner", created.ID)
	if err != nil || !reflect.DeepEqual(got, failed) {
		t.Fatalf("Job() = %#v, %v; want %#v", got, err, failed)
	}
}
