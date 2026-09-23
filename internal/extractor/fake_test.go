package extractor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFakeWritesSyntheticMedia(t *testing.T) {
	dir := t.TempDir()
	files, err := Fake{}.Extract(context.Background(), Request{URL: "https://vimeo.com/1", Platform: "vimeo", WorkDir: dir, MaxBytes: 1 << 20})
	if err != nil || len(files) != 1 || files[0].MediaType != "video/mp4" {
		t.Fatalf("Extract() = %#v, %v", files, err)
	}
	info, err := os.Lstat(filepath.Join(dir, files[0].Name))
	if err != nil || !info.Mode().IsRegular() || info.Size() != int64(len(fakeMedia)) || info.Mode().Perm() != 0o600 {
		t.Fatalf("output = %v, %v", info, err)
	}
}

func TestFakeHonorsLimits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Fake{}).Extract(ctx, Request{WorkDir: t.TempDir(), MaxBytes: 1 << 20}); err == nil {
		t.Fatal("Extract(canceled) error = nil")
	}
	if _, err := (Fake{}).Extract(context.Background(), Request{WorkDir: t.TempDir(), MaxBytes: 1}); err == nil {
		t.Fatal("Extract(tiny budget) error = nil")
	}
	if _, err := (Fake{}).Extract(context.Background(), Request{WorkDir: filepath.Join(t.TempDir(), "missing"), MaxBytes: 1 << 20}); err == nil {
		t.Fatal("Extract(missing dir) error = nil")
	}
}
