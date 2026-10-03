package extractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFakeWritesSyntheticMedia(t *testing.T) {
	dir := t.TempDir()
	files, err := Fake{}.Extract(context.Background(), Request{URL: "https://vimeo.com/1", Platform: "vimeo", WorkDir: dir, MaxBytes: 1 << 20, MaxItems: 1})
	if err != nil || len(files) != 1 || files[0].MediaType != "video/mp4" {
		t.Fatalf("Extract() = %#v, %v", files, err)
	}
	info, err := os.Lstat(filepath.Join(dir, files[0].Name))
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(fakeMedia)) || info.Mode().Perm() != 0o600 {
		t.Fatalf("output = %v, %v", info, err)
	}
}

func TestFakeRedditFixtures(t *testing.T) {
	for raw, count := range map[string]int{
		"https://www.reddit.com/comments/abc123/":        1,
		"https://www.reddit.com/r/example/s/Ab12Cd34Ef/": 1,
		"https://www.reddit.com/gallery/def456/":         2,
	} {
		request := Request{URL: raw, Platform: "reddit", WorkDir: t.TempDir(), MaxBytes: 1 << 20, MaxItems: 2}
		files, err := (Fake{}).Extract(t.Context(), request)
		if err != nil || len(files) != count {
			t.Fatalf("fixture count = %d, %v", len(files), err)
		}
		for _, file := range files {
			if file.MediaType != "image/jpeg" {
				t.Fatalf("media type = %s", file.MediaType)
			}
		}
		request.MaxBytes = 1
		if _, err := (Fake{}).Extract(t.Context(), request); err == nil {
			t.Fatal("byte limit ignored")
		}
		if count == 2 {
			request.MaxBytes = 1 << 20
			request.MaxItems = 1
			if _, err := (Fake{}).Extract(t.Context(), request); err == nil {
				t.Fatal("item limit ignored")
			}
		}
	}
}

func TestFakeHonorsLimits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Fake{}).Extract(ctx, Request{WorkDir: t.TempDir(), MaxBytes: 1 << 20, MaxItems: 1}); err == nil {
		t.Fatal("Extract(canceled) error = nil")
	}
	if _, err := (Fake{}).Extract(context.Background(), Request{WorkDir: t.TempDir(), MaxBytes: 1, MaxItems: 1}); err == nil {
		t.Fatal("Extract(tiny budget) error = nil")
	}
	if _, err := (Fake{}).Extract(context.Background(), Request{WorkDir: filepath.Join(t.TempDir(), "missing"), MaxBytes: 1 << 20, MaxItems: 1}); err == nil {
		t.Fatal("Extract(missing dir) error = nil")
	}
}

func TestFakeWritesMixedBundleFixture(t *testing.T) {
	request := Request{URL: "https://www.instagram.com/p/DduKfFmDxsG/", Platform: "instagram", WorkDir: t.TempDir(), MaxBytes: 1 << 20, MaxItems: 20}
	files, err := (Fake{}).Extract(context.Background(), request)
	if err != nil || len(files) != 2 || files[0].MediaType != "video/mp4" || files[1].MediaType != "image/jpeg" {
		t.Fatalf("mixed fixture: %#v %v", files, err)
	}
	request.MaxItems = 1
	if _, err := (Fake{}).Extract(context.Background(), request); err == nil {
		t.Fatal("item limit ignored")
	}
	request.MaxItems = 20
	request.MaxBytes = int64(len(fakeMedia))
	if _, err := (Fake{}).Extract(context.Background(), request); err == nil {
		t.Fatal("aggregate byte limit ignored")
	}
}

func TestFakeMixedFixtureWriteFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "fake.jpg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := (Fake{}).Extract(context.Background(), Request{URL: "https://www.instagram.com/p/DduKfFmDxsG/", WorkDir: dir, MaxBytes: 1 << 20, MaxItems: 20}); err == nil {
		t.Fatal("second write failure ignored")
	}
}

type fakeCanceledDuringWork struct {
	context.Context
	cancel context.CancelFunc
	checks int
}

func (c *fakeCanceledDuringWork) Err() error {
	c.checks++
	if c.checks == 2 {
		c.cancel()
	}
	return c.Context.Err()
}
func TestFakeCancellationBetweenWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &fakeCanceledDuringWork{Context: ctx, cancel: cancel}
	if _, err := (Fake{}).Extract(controlled, Request{WorkDir: t.TempDir(), MaxBytes: 1 << 20, MaxItems: 20}); err == nil {
		t.Fatal("fake ignored work cancellation")
	}
}
