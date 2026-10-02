package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

func bundleFixture(t *testing.T, s *Store) (job.Job, []job.Item, job.Bundle) {
	t.Helper()
	createTestKey(t, s, "owner", "owner")
	j := createJobAt(t, s, "owner", testNow)
	transitionJob(t, s, &j, job.StatusRunning, "")
	if err := j.Transition(job.StatusSucceeded, "", testNow); err != nil {
		t.Fatal(err)
	}
	items := []job.Item{{ID: "one", JobID: j.ID, Position: 0, FileName: "one.mp4", MediaType: "video/mp4", SizeBytes: 3, CreatedAt: testNow}, {ID: "two", JobID: j.ID, Position: 1, FileName: "two.jpg", MediaType: "image/jpeg", SizeBytes: 4, CreatedAt: testNow}}
	return j, items, job.Bundle{JobID: j.ID, FileName: "bundle.zip", SizeBytes: 300, CreatedAt: testNow}
}

func TestCompleteJobWithBundle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	j, items, b := bundleFixture(t, s)
	if err := s.CompleteJob(ctx, j, items, &b); err != nil {
		t.Fatal(err)
	}
	got, err := s.Bundle(ctx, "owner", j.ID)
	if err != nil || !reflect.DeepEqual(got, b) {
		t.Fatalf("bundle = %#v, %v", got, err)
	}
	if _, err = s.Bundle(ctx, "other", j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner bundle: %v", err)
	}
	for _, name := range []string{"missing bundle", "single item", "wrong bundle job", "zero size", "wrong item job", "duplicate position", "wrong status"} {
		t.Run(name, func(t *testing.T) {
			st := openTestStore(t)
			done, its, bb := bundleFixture(t, st)
			bp := &bb
			switch name {
			case "missing bundle":
				bp = nil
			case "single item":
				its = its[:1]
			case "wrong bundle job":
				bb.JobID = "other"
			case "zero size":
				bb.SizeBytes = 0
			case "wrong item job":
				its[0].JobID = "other"
			case "duplicate position":
				its[1].Position = 0
			case "wrong status":
				done.Status = job.StatusFailed
			}
			if err := st.CompleteJob(ctx, done, its, bp); err == nil {
				t.Fatal("invalid completion accepted")
			}
			status, _ := st.JobStatus(ctx, done.ID)
			if status != job.StatusRunning {
				t.Fatalf("status after rollback: %s", status)
			}
			persisted, _ := st.Items(ctx, "owner", done.ID)
			if len(persisted) != 0 {
				t.Fatal("partial items persisted")
			}
			if _, err := st.Bundle(ctx, "owner", done.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("partial bundle: %v", err)
			}
		})
	}
}

func TestBundleTokenLifecycle(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	j, items, b := bundleFixture(t, s)
	createTestKey(t, s, "other", "other")
	if err := s.CompleteJob(ctx, j, items, &b); err != nil {
		t.Fatal(err)
	}
	token := job.BundleToken{Hash: []byte("first"), JobID: j.ID, OwnerID: "owner", CreatedAt: testNow, ExpiresAt: testNow.Add(time.Minute)}
	if err := s.ReplaceBundleToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	bad := token
	bad.OwnerID = "other"
	bad.Hash = []byte("bad")
	if err := s.ReplaceBundleToken(ctx, bad); err == nil {
		t.Fatal("wrong owner accepted")
	}
	got, err := s.ValidBundleToken(ctx, token.Hash, testNow)
	if err != nil || !reflect.DeepEqual(got, token) {
		t.Fatalf("valid token: %#v %v", got, err)
	}
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Go(func() {
			replacement := token
			replacement.Hash = []byte(fmt.Sprintf("replacement-%d", n))
			if err := s.ReplaceBundleToken(ctx, replacement); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM bundle_tokens WHERE job_id = ?", j.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("token count: %d %v", count, err)
	}
	if _, err := s.ValidBundleToken(ctx, []byte("first"), testNow); !errors.Is(err, ErrNotFound) {
		t.Fatal("old token remains valid")
	}
	if err := s.ReplaceBundleToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidBundleToken(ctx, token.Hash, token.ExpiresAt); !errors.Is(err, ErrNotFound) {
		t.Fatal("token valid at expiry")
	}
	if deleted, err := s.DeleteExpiredBundleTokens(ctx, token.ExpiresAt); err != nil || deleted != 1 {
		t.Fatalf("expired deletion: %d %v", deleted, err)
	}
	token.ExpiresAt = j.ExpiresAt.Add(time.Hour)
	if err := s.ReplaceBundleToken(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidBundleToken(ctx, token.Hash, j.ExpiresAt); !errors.Is(err, ErrNotFound) {
		t.Fatal("token outlived job")
	}
	if err := s.RevokeAPIKey(ctx, "owner", testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidBundleToken(ctx, token.Hash, testNow); err != nil {
		t.Fatalf("revoke changed existing token: %v", err)
	}
	if _, _, err := s.PurgeRevokedAPIKeys(ctx, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ValidBundleToken(ctx, token.Hash, testNow); !errors.Is(err, ErrNotFound) {
		t.Fatal("purged token remains")
	}
}

func TestBundleTokenInsertFailureRollsBackDeletion(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	j, items, b := bundleFixture(t, s)
	if err := s.CompleteJob(ctx, j, items, &b); err != nil {
		t.Fatal(err)
	}
	original := job.BundleToken{Hash: []byte("original"), JobID: j.ID, OwnerID: "owner", CreatedAt: testNow, ExpiresAt: testNow.Add(time.Minute)}
	if err := s.ReplaceBundleToken(ctx, original); err != nil {
		t.Fatal(err)
	}
	other := createJobAt(t, s, "owner", testNow)
	transitionJob(t, s, &other, job.StatusRunning, "")
	if err := other.Transition(job.StatusSucceeded, "", testNow); err != nil {
		t.Fatal(err)
	}
	for i := range items {
		items[i].ID += "-other"
		items[i].JobID = other.ID
	}
	b.JobID = other.ID
	if err := s.CompleteJob(ctx, other, items, &b); err != nil {
		t.Fatal(err)
	}
	occupied := original
	occupied.JobID = other.ID
	occupied.Hash = []byte("occupied")
	if err := s.ReplaceBundleToken(ctx, occupied); err != nil {
		t.Fatal(err)
	}
	replacement := original
	replacement.Hash = occupied.Hash
	if err := s.ReplaceBundleToken(ctx, replacement); err == nil {
		t.Fatal("duplicate token hash accepted")
	}
	for _, want := range []job.BundleToken{original, occupied} {
		got, err := s.ValidBundleToken(ctx, want.Hash, testNow)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("token changed after rollback: %#v %v", got, err)
		}
	}
}

func TestBundleMigrationPreservesExistingData(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sql.Open("sqlite", dataSourceName(filepath.Join(dir, databaseFile)))
	if err != nil {
		t.Fatal(err)
	}
	source := fstest.MapFS{}
	for _, name := range []string{"0001_initial.sql", "0002_job_error_detail.sql"} {
		data, e := embeddedMigrations.ReadFile("migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		source["migrations/"+name] = &fstest.MapFile{Data: data}
	}
	if _, err := migrate(ctx, db, source); err != nil {
		t.Fatal(err)
	}
	old := &Store{db: db, schemaVersion: 2}
	j, items, _ := bundleFixture(t, old)
	for _, item := range items {
		if err := old.CreateItem(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := old.UpdateJobStatus(ctx, j, job.StatusRunning); err != nil {
		t.Fatal(err)
	}
	tok := job.DownloadToken{Hash: []byte("legacy"), ItemID: items[0].ID, OwnerID: j.OwnerID, CreatedAt: testNow, ExpiresAt: testNow.Add(time.Minute)}
	if err := old.CreateDownloadToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if st.schemaVersion != 3 {
		t.Fatalf("schema version %d", st.schemaVersion)
	}
	got, err := st.Items(ctx, j.OwnerID, j.ID)
	if err != nil || !reflect.DeepEqual(got, items) {
		t.Fatalf("legacy items: %v %v", got, err)
	}
	if _, err := st.ValidDownloadToken(ctx, tok.Hash, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Bundle(ctx, j.OwnerID, j.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("legacy bundle exists")
	}
}

func TestBundleRepositoryFailures(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	_ = s.Close()
	if _, err := s.Bundle(ctx, "owner", "job"); err == nil {
		t.Fatal("closed bundle read succeeded")
	}
	if err := s.ReplaceBundleToken(ctx, job.BundleToken{}); err == nil {
		t.Fatal("closed token write succeeded")
	}
	if _, err := s.ValidBundleToken(ctx, []byte("x"), testNow); err == nil {
		t.Fatal("closed token read succeeded")
	}
	if _, err := s.DeleteExpiredBundleTokens(ctx, testNow); err == nil {
		t.Fatal("closed token cleanup succeeded")
	}
}
