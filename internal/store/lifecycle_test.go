package store

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

func createJobAt(t *testing.T, s *Store, ownerID string, at time.Time) job.Job {
	t.Helper()
	j := job.New(ownerID, "https://vimeo.com/1", "vimeo", at, time.Hour)
	if err := s.CreateJob(context.Background(), j); err != nil {
		t.Fatalf("CreateJob() error = %v", err)
	}

	return j
}

func TestClaimNextJob(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	if _, err := s.ClaimNextJob(ctx, testNow); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ClaimNextJob(empty) error = %v; want ErrNotFound", err)
	}
	later := createJobAt(t, s, "owner", testNow.Add(time.Second))
	earlier := createJobAt(t, s, "owner", testNow)

	claimAt := testNow.Add(time.Minute)
	claimed, err := s.ClaimNextJob(ctx, claimAt)
	if err != nil || claimed.ID != earlier.ID || claimed.Status != job.StatusRunning ||
		!claimed.StartedAt.Equal(claimAt) || !claimed.UpdatedAt.Equal(claimAt) {
		t.Fatalf("ClaimNextJob() = %#v, %v", claimed, err)
	}
	next, err := s.ClaimNextJob(ctx, claimAt)
	if err != nil || next.ID != later.ID {
		t.Fatalf("second ClaimNextJob() = %#v, %v", next, err)
	}
	if _, err := s.ClaimNextJob(ctx, claimAt); !errors.Is(err, ErrNotFound) {
		t.Fatalf("drained ClaimNextJob() error = %v", err)
	}
	if status, err := s.JobStatus(ctx, earlier.ID); err != nil || status != job.StatusRunning {
		t.Fatalf("JobStatus() = %q, %v", status, err)
	}
	if _, err := s.JobStatus(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("JobStatus(missing) error = %v", err)
	}
}

func TestActiveJobCount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	createTestKey(t, s, "other", "other")
	createJobAt(t, s, "owner", testNow)
	running := createJobAt(t, s, "owner", testNow)
	done := createJobAt(t, s, "owner", testNow)
	createJobAt(t, s, "other", testNow)
	transitionJob(t, s, &running, job.StatusRunning, "")
	transitionJob(t, s, &done, job.StatusCanceled, "")

	if count, err := s.ActiveJobCount(ctx, "owner"); err != nil || count != 2 {
		t.Fatalf("ActiveJobCount() = %d, %v; want 2", count, err)
	}
}

func transitionJob(t *testing.T, s *Store, j *job.Job, next job.Status, code job.ErrorCode) {
	t.Helper()
	from := j.Status
	if err := j.Transition(next, code, testNow); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateJobStatus(context.Background(), *j, from); err != nil {
		t.Fatalf("UpdateJobStatus() error = %v", err)
	}
}

func TestCompleteJob(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	j := createJobAt(t, s, "owner", testNow)
	transitionJob(t, s, &j, job.StatusRunning, "")

	done := j
	if err := done.Transition(job.StatusSucceeded, "", testNow.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	items := []job.Item{
		{ID: "item0", JobID: j.ID, Position: 0, FileName: "a.mp4", MediaType: "video/mp4", SizeBytes: 3, CreatedAt: testNow},
		{ID: "item1", JobID: j.ID, Position: 1, FileName: "b.jpg", MediaType: "image/jpeg", SizeBytes: 4, CreatedAt: testNow},
	}
	if err := s.CompleteJob(ctx, done, items); err != nil {
		t.Fatalf("CompleteJob() error = %v", err)
	}
	got, err := s.Job(ctx, "owner", j.ID)
	if err != nil || !reflect.DeepEqual(got, done) {
		t.Fatalf("Job() = %#v, %v", got, err)
	}
	stored, err := s.Items(ctx, "owner", j.ID)
	if err != nil || !reflect.DeepEqual(stored, items) {
		t.Fatalf("Items() = %#v, %v", stored, err)
	}
	item, err := s.Item(ctx, "item1")
	if err != nil || !reflect.DeepEqual(item, items[1]) {
		t.Fatalf("Item() = %#v, %v", item, err)
	}
	if _, err := s.Item(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Item(missing) error = %v", err)
	}

	if err := s.CompleteJob(ctx, done, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("CompleteJob(not running) error = %v; want ErrConflict", err)
	}
	gone := done
	gone.ID = "missing"
	if err := s.CompleteJob(ctx, gone, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CompleteJob(missing) error = %v; want ErrNotFound", err)
	}
}

func TestCompleteJobRollsBackOnItemFailure(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	j := createJobAt(t, s, "owner", testNow)
	transitionJob(t, s, &j, job.StatusRunning, "")
	done := j
	if err := done.Transition(job.StatusSucceeded, "", testNow); err != nil {
		t.Fatal(err)
	}
	duplicate := job.Item{ID: "item0", JobID: j.ID, FileName: "a", MediaType: "video/mp4", CreatedAt: testNow}
	if err := s.CompleteJob(ctx, done, []job.Item{duplicate, duplicate}); err == nil {
		t.Fatal("CompleteJob() error = nil for duplicate items")
	}
	if status, err := s.JobStatus(ctx, j.ID); err != nil || status != job.StatusRunning {
		t.Fatalf("status after rollback = %q, %v; want running", status, err)
	}
	if items, err := s.Items(ctx, "owner", j.ID); err != nil || len(items) != 0 {
		t.Fatalf("items after rollback = %#v, %v", items, err)
	}
}

func TestJobsListingAndFilters(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	createTestKey(t, s, "other", "other")
	first := createJobAt(t, s, "owner", testNow)
	second := createJobAt(t, s, "owner", testNow.Add(time.Second))
	third := createJobAt(t, s, "other", testNow.Add(2*time.Second))
	transitionJob(t, s, &second, job.StatusCanceled, "")

	tests := []struct {
		filter JobFilter
		want   []string
	}{
		{filter: JobFilter{}, want: []string{third.ID, second.ID, first.ID}},
		{filter: JobFilter{OwnerID: "owner"}, want: []string{second.ID, first.ID}},
		{filter: JobFilter{Status: job.StatusQueued}, want: []string{third.ID, first.ID}},
		{filter: JobFilter{OwnerID: "owner", Status: job.StatusCanceled}, want: []string{second.ID}},
		{filter: JobFilter{OwnerID: "nobody"}, want: nil},
	}
	for _, tt := range tests {
		jobs, err := s.Jobs(ctx, tt.filter)
		if err != nil {
			t.Fatalf("Jobs(%+v) error = %v", tt.filter, err)
		}
		var ids []string
		for _, j := range jobs {
			ids = append(ids, j.ID)
		}
		if !reflect.DeepEqual(ids, tt.want) {
			t.Fatalf("Jobs(%+v) = %v; want %v", tt.filter, ids, tt.want)
		}
	}
}

func TestDeleteJob(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	queued := createJobAt(t, s, "owner", testNow)
	running := createJobAt(t, s, "owner", testNow)
	transitionJob(t, s, &running, job.StatusRunning, "")

	if err := s.DeleteJob(ctx, queued.ID); err != nil {
		t.Fatalf("DeleteJob(queued) error = %v", err)
	}
	if exists, err := s.JobExists(ctx, queued.ID); err != nil || exists {
		t.Fatalf("JobExists(deleted) = %v, %v", exists, err)
	}
	if err := s.DeleteJob(ctx, running.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("DeleteJob(running) error = %v; want ErrConflict", err)
	}
	if exists, err := s.JobExists(ctx, running.ID); err != nil || !exists {
		t.Fatalf("JobExists(running) = %v, %v", exists, err)
	}
	if err := s.DeleteJob(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteJob(missing) error = %v; want ErrNotFound", err)
	}
}

func TestPurgeJobs(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	createTestKey(t, s, "other", "other")
	queued := createJobAt(t, s, "owner", testNow)
	running := createJobAt(t, s, "owner", testNow)
	transitionJob(t, s, &running, job.StatusRunning, "")
	kept := createJobAt(t, s, "other", testNow)

	ids, err := s.PurgeJobs(ctx, "owner", testNow)
	sort.Strings(ids)
	want := []string{queued.ID, running.ID}
	sort.Strings(want)
	if err != nil || !reflect.DeepEqual(ids, want) {
		t.Fatalf("PurgeJobs(owner) = %v, %v; want %v", ids, err, want)
	}
	if exists, _ := s.JobExists(ctx, kept.ID); !exists {
		t.Fatal("PurgeJobs(owner) removed another owner's job")
	}
	ids, err = s.PurgeJobs(ctx, "", testNow)
	if err != nil || !reflect.DeepEqual(ids, []string{kept.ID}) {
		t.Fatalf("PurgeJobs(all) = %v, %v", ids, err)
	}
	if ids, err := s.PurgeJobs(ctx, "", testNow); err != nil || len(ids) != 0 {
		t.Fatalf("PurgeJobs(empty) = %v, %v", ids, err)
	}
}

func TestExpiredJobsAndRecovery(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	expiredDone := createJobAt(t, s, "owner", testNow)
	transitionJob(t, s, &expiredDone, job.StatusCanceled, "")
	createJobAt(t, s, "owner", testNow) // expired but still queued
	fresh := createJobAt(t, s, "owner", testNow.Add(2*time.Hour))
	transitionJob(t, s, &fresh, job.StatusCanceled, "")
	running := createJobAt(t, s, "owner", testNow.Add(2*time.Hour))
	transitionJob(t, s, &running, job.StatusRunning, "")

	later := testNow.Add(90 * time.Minute)
	ids, err := s.ExpiredJobIDs(ctx, later)
	if err != nil || !reflect.DeepEqual(ids, []string{expiredDone.ID}) {
		t.Fatalf("ExpiredJobIDs() = %v, %v", ids, err)
	}

	failed, err := s.FailRunningJobs(ctx, later)
	if err != nil || failed != 1 {
		t.Fatalf("FailRunningJobs() = %d, %v; want 1", failed, err)
	}
	got, err := s.Job(ctx, "owner", running.ID)
	if err != nil || got.Status != job.StatusFailed || got.ErrorCode != job.ErrorInternal || !got.FinishedAt.Equal(later) {
		t.Fatalf("recovered job = %#v, %v", got, err)
	}
}

func TestReplaceDownloadToken(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	createTestKey(t, s, "owner", "owner")
	j := createJobAt(t, s, "owner", testNow)
	item := job.Item{ID: "item", JobID: j.ID, FileName: "a", MediaType: "video/mp4", CreatedAt: testNow}
	if err := s.CreateItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	first := job.DownloadToken{Hash: []byte("first"), ItemID: item.ID, OwnerID: "owner", CreatedAt: testNow, ExpiresAt: testNow.Add(time.Hour)}
	second := first
	second.Hash = []byte("second")

	for _, token := range []job.DownloadToken{first, second} {
		if err := s.ReplaceDownloadToken(ctx, token); err != nil {
			t.Fatalf("ReplaceDownloadToken() error = %v", err)
		}
	}
	if _, err := s.ValidDownloadToken(ctx, first.Hash, testNow); !errors.Is(err, ErrNotFound) {
		t.Fatalf("replaced token still valid: %v", err)
	}
	if _, err := s.ValidDownloadToken(ctx, second.Hash, testNow); err != nil {
		t.Fatalf("new token invalid: %v", err)
	}
	orphan := second
	orphan.ItemID = "missing"
	if err := s.ReplaceDownloadToken(ctx, orphan); err == nil {
		t.Fatal("ReplaceDownloadToken() accepted an unknown item")
	}
}

func TestLifecycleQueriesFailAfterClose(t *testing.T) {
	s, err := Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	ctx := context.Background()
	j := job.New("owner", "https://vimeo.com/1", "vimeo", testNow, time.Hour)
	checks := map[string]error{
		"CompleteJob":          s.CompleteJob(ctx, j, nil),
		"DeleteJob":            s.DeleteJob(ctx, j.ID),
		"ReplaceDownloadToken": s.ReplaceDownloadToken(ctx, job.DownloadToken{}),
	}
	_, checks["ClaimNextJob"] = s.ClaimNextJob(ctx, testNow)
	_, checks["JobStatus"] = s.JobStatus(ctx, j.ID)
	_, checks["JobExists"] = s.JobExists(ctx, j.ID)
	_, checks["ActiveJobCount"] = s.ActiveJobCount(ctx, "owner")
	_, checks["Jobs"] = s.Jobs(ctx, JobFilter{})
	_, checks["PurgeJobs"] = s.PurgeJobs(ctx, "", testNow)
	_, checks["ExpiredJobIDs"] = s.ExpiredJobIDs(ctx, testNow)
	_, checks["FailRunningJobs"] = s.FailRunningJobs(ctx, testNow)
	_, checks["Item"] = s.Item(ctx, "item")
	for name, err := range checks {
		if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
			t.Fatalf("%s() error = %v; want database error", name, err)
		}
	}
}
