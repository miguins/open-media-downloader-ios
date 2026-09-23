package storage

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/id"
)

var now = time.UnixMilli(1_800_000_000_000).UTC()

func newTestLayout(t *testing.T) (*Layout, string) {
	t.Helper()
	dir := t.TempDir()
	layout, err := New(dir)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return layout, dir
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != want {
		t.Fatalf("mode of %s = %v, %v; want %v", filepath.Base(path), info.Mode().Perm(), err, want)
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

func TestNewCreatesPrivateDirectories(t *testing.T) {
	_, dir := newTestLayout(t)
	assertMode(t, filepath.Join(dir, "jobs"), 0o700)
	assertMode(t, filepath.Join(dir, "work"), 0o700)
	if _, err := New(dir); err != nil {
		t.Fatalf("New(existing) error = %v", err)
	}
}

func TestNewRejectsUnsafeDirectories(t *testing.T) {
	t.Run("symlinked subdirectory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink(t.TempDir(), filepath.Join(dir, "jobs")); err != nil {
			t.Fatal(err)
		}
		if _, err := New(dir); err == nil {
			t.Fatal("New() error = nil")
		}
	})
	t.Run("file instead of directory", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "work"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(dir); err == nil {
			t.Fatal("New() error = nil")
		}
	})
	t.Run("unwritable data directory", func(t *testing.T) {
		dir := t.TempDir()
		readOnlyDir(t, dir)
		if _, err := New(dir); err == nil {
			t.Fatal("New() error = nil")
		}
	})
}

func TestWorkDirLifecycle(t *testing.T) {
	layout, dir := newTestLayout(t)
	jobID := id.New()

	workDir, err := layout.PrepareWorkDir(jobID)
	if err != nil || workDir != filepath.Join(dir, "work", jobID) {
		t.Fatalf("PrepareWorkDir() = %q, %v", workDir, err)
	}
	assertMode(t, workDir, 0o700)
	if err := os.WriteFile(filepath.Join(workDir, "stale"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := layout.PrepareWorkDir(jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(workDir, "stale")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("PrepareWorkDir() kept stale files")
	}

	if err := layout.RemoveWorkDir(jobID); err != nil {
		t.Fatalf("RemoveWorkDir() error = %v", err)
	}
	if _, err := os.Lstat(workDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("RemoveWorkDir() left the directory")
	}

	for _, bad := range []string{"", "../escape", "ABCDEFGHIJKLMNOPQRSTUVWXYZ"} {
		if _, err := layout.PrepareWorkDir(bad); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("PrepareWorkDir(%q) error = %v", bad, err)
		}
		if err := layout.RemoveWorkDir(bad); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("RemoveWorkDir(%q) error = %v", bad, err)
		}
		if err := layout.RemoveJob(bad); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("RemoveJob(%q) error = %v", bad, err)
		}
		if _, err := layout.OpenItem(bad, id.New()); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("OpenItem(%q, valid) error = %v", bad, err)
		}
		if _, err := layout.OpenItem(id.New(), bad); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("OpenItem(valid, %q) error = %v", bad, err)
		}
		if _, err := layout.Ingest(bad, nil, 1, now); !errors.Is(err, ErrInvalidID) {
			t.Fatalf("Ingest(%q) error = %v", bad, err)
		}
	}
}

func TestPrepareWorkDirFailsWhenWorkRootIsReadOnly(t *testing.T) {
	layout, dir := newTestLayout(t)
	existing := id.New()
	workDir, err := layout.PrepareWorkDir(existing)
	if err != nil {
		t.Fatal(err)
	}
	writeOutput(t, workDir, "leftover", "x")
	readOnlyDir(t, filepath.Join(dir, "work"))
	if _, err := layout.PrepareWorkDir(id.New()); err == nil {
		t.Fatal("PrepareWorkDir(new) error = nil")
	}
	readOnlyDir(t, workDir)
	if _, err := layout.PrepareWorkDir(existing); err == nil {
		t.Fatal("PrepareWorkDir(existing) error = nil")
	}
}

func writeOutput(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644); err != nil { //nolint:gosec // Extractor output fixture.
		t.Fatal(err)
	}
}

func TestIngestMovesValidatedFiles(t *testing.T) {
	layout, dir := newTestLayout(t)
	jobID := id.New()
	workDir, err := layout.PrepareWorkDir(jobID)
	if err != nil {
		t.Fatal(err)
	}
	writeOutput(t, workDir, "video.mp4", "abc")
	writeOutput(t, workDir, "cover", "defg")

	items, err := layout.Ingest(jobID, []Output{
		{Name: "video.mp4", MediaType: "video/mp4"},
		{Name: "cover", MediaType: "image/jpeg"},
	}, 7, now)
	if err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Ingest() returned %d items", len(items))
	}
	prefix := "omdi-" + jobID[:8] + "-"
	wantNames := []string{prefix + "1.mp4", prefix + "2.jpg"}
	for i, item := range items {
		if !id.Valid(item.ID) || item.JobID != jobID || item.Position != i || item.FileName != wantNames[i] || !item.CreatedAt.Equal(now) {
			t.Fatalf("item %d = %#v", i, item)
		}
		assertMode(t, filepath.Join(dir, "jobs", jobID, item.ID), 0o600)
	}
	if items[0].SizeBytes != 3 || items[1].MediaType != "image/jpeg" || items[1].SizeBytes != 4 {
		t.Fatalf("items = %#v", items)
	}
	assertMode(t, filepath.Join(dir, "jobs", jobID), 0o700)

	file, err := layout.OpenItem(jobID, items[1].ID)
	if err != nil {
		t.Fatalf("OpenItem() error = %v", err)
	}
	contents, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil || string(contents) != "defg" {
		t.Fatalf("OpenItem() contents = %q, %v", contents, err)
	}
}

func TestIngestRejectsUnsafeOutput(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, workDir, dataDir string)
		outputs []Output
		max     int64
		want    error
	}{
		{name: "no files", outputs: nil, max: 10, want: ErrInvalidOutput},
		{name: "unknown media type", setup: func(t *testing.T, w, _ string) { writeOutput(t, w, "a", "x") },
			outputs: []Output{{Name: "a", MediaType: "text/html"}}, max: 10, want: ErrInvalidOutput},
		{name: "traversal name", outputs: []Output{{Name: "../../omdi.db", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "absolute name", outputs: []Output{{Name: "/etc/passwd", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "dot name", outputs: []Output{{Name: "..", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "missing file", outputs: []Output{{Name: "missing", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "empty file", setup: func(t *testing.T, w, _ string) { writeOutput(t, w, "a", "") },
			outputs: []Output{{Name: "a", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "symlink", setup: func(t *testing.T, w, d string) {
			if err := os.Symlink(filepath.Join(d, "omdi.db"), filepath.Join(w, "a")); err != nil {
				t.Fatal(err)
			}
		}, outputs: []Output{{Name: "a", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "hard link", setup: func(t *testing.T, w, d string) {
			writeOutput(t, d, "omdi.db", "secret")
			if err := os.Link(filepath.Join(d, "omdi.db"), filepath.Join(w, "a")); err != nil {
				t.Fatal(err)
			}
		}, outputs: []Output{{Name: "a", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "directory", setup: func(t *testing.T, w, _ string) {
			if err := os.Mkdir(filepath.Join(w, "a"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, outputs: []Output{{Name: "a", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "fifo", setup: func(t *testing.T, w, _ string) {
			if err := syscall.Mkfifo(filepath.Join(w, "a"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, outputs: []Output{{Name: "a", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "duplicate name", setup: func(t *testing.T, w, _ string) { writeOutput(t, w, "a", "x") },
			outputs: []Output{{Name: "a", MediaType: "video/mp4"}, {Name: "a", MediaType: "video/mp4"}}, max: 10, want: ErrInvalidOutput},
		{name: "over budget", setup: func(t *testing.T, w, _ string) {
			writeOutput(t, w, "a", "12345")
			writeOutput(t, w, "b", "123456")
		}, outputs: []Output{{Name: "a", MediaType: "video/mp4"}, {Name: "b", MediaType: "video/mp4"}}, max: 10, want: ErrTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout, dataDir := newTestLayout(t)
			jobID := id.New()
			workDir, err := layout.PrepareWorkDir(jobID)
			if err != nil {
				t.Fatal(err)
			}
			if tt.setup != nil {
				tt.setup(t, workDir, dataDir)
			}
			items, err := layout.Ingest(jobID, tt.outputs, tt.max, now)
			if !errors.Is(err, tt.want) || items != nil {
				t.Fatalf("Ingest() = %v, %v; want %v", items, err, tt.want)
			}
			if strings.Contains(err.Error(), dataDir) {
				t.Fatalf("error leaked a path: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(dataDir, "jobs", jobID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("rejected ingest left a job directory")
			}
		})
	}
}

func TestIngestFailsWhenJobsRootIsReadOnly(t *testing.T) {
	layout, dir := newTestLayout(t)
	jobID := id.New()
	workDir, err := layout.PrepareWorkDir(jobID)
	if err != nil {
		t.Fatal(err)
	}
	writeOutput(t, workDir, "a", "x")
	readOnlyDir(t, filepath.Join(dir, "jobs"))
	if _, err := layout.Ingest(jobID, []Output{{Name: "a", MediaType: "video/mp4"}}, 10, now); err == nil {
		t.Fatal("Ingest() error = nil")
	}
}

func TestOpenItemRejectsUnsafeFiles(t *testing.T) {
	layout, dir := newTestLayout(t)
	jobID, itemID := id.New(), id.New()
	jobDir := filepath.Join(dir, "jobs", jobID)
	if err := os.Mkdir(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := layout.OpenItem(jobID, itemID); err == nil {
		t.Fatal("OpenItem(missing) error = nil")
	}
	writeOutput(t, dir, "secret", "x")
	if err := os.Symlink(filepath.Join(dir, "secret"), filepath.Join(jobDir, itemID)); err != nil {
		t.Fatal(err)
	}
	if _, err := layout.OpenItem(jobID, itemID); err == nil {
		t.Fatal("OpenItem(symlink) error = nil")
	}
	dirItem := id.New()
	if err := os.Mkdir(filepath.Join(jobDir, dirItem), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := layout.OpenItem(jobID, dirItem); err == nil {
		t.Fatal("OpenItem(directory) error = nil")
	}
}

func TestJobDirectoriesAndRemoval(t *testing.T) {
	layout, dir := newTestLayout(t)
	first, second := id.New(), id.New()
	for _, name := range []string{first, second, "not-an-id"} {
		if err := os.Mkdir(filepath.Join(dir, "jobs", name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeOutput(t, filepath.Join(dir, "jobs"), id.New(), "stray file")

	ids, err := layout.JobDirIDs()
	sort.Strings(ids)
	want := []string{first, second}
	sort.Strings(want)
	if err != nil || !reflect.DeepEqual(ids, want) {
		t.Fatalf("JobDirIDs() = %v, %v", ids, err)
	}
	if err := layout.RemoveJob(first); err != nil {
		t.Fatalf("RemoveJob() error = %v", err)
	}
	if err := layout.RemoveJob(first); err != nil {
		t.Fatalf("RemoveJob(missing) error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "jobs", first)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("RemoveJob() left the directory")
	}

	if err := os.RemoveAll(filepath.Join(dir, "jobs")); err != nil {
		t.Fatal(err)
	}
	if _, err := layout.JobDirIDs(); err == nil {
		t.Fatal("JobDirIDs() error = nil without jobs directory")
	}
}

func TestClearWork(t *testing.T) {
	layout, dir := newTestLayout(t)
	workDir, err := layout.PrepareWorkDir(id.New())
	if err != nil {
		t.Fatal(err)
	}
	writeOutput(t, workDir, "partial", "x")
	if err := layout.ClearWork(); err != nil {
		t.Fatalf("ClearWork() error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "work"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("work entries = %v, %v", entries, err)
	}
	assertMode(t, filepath.Join(dir, "work"), 0o700)

	readOnlyDir(t, dir)
	if err := layout.ClearWork(); err == nil {
		t.Fatal("ClearWork() error = nil in read-only data directory")
	}
}

func TestAvailableBytes(t *testing.T) {
	layout, _ := newTestLayout(t)
	if available, err := layout.AvailableBytes(); err != nil || available == 0 {
		t.Fatalf("AvailableBytes() = %d, %v", available, err)
	}
	missing := &Layout{root: filepath.Join(t.TempDir(), "missing")}
	if _, err := missing.AvailableBytes(); err == nil {
		t.Fatal("AvailableBytes(missing) error = nil")
	}
}

func TestExtension(t *testing.T) {
	for mediaType, want := range map[string]string{
		"video/mp4": "mp4", "video/webm": "webm", "video/quicktime": "mov", "image/jpeg": "jpg",
		"image/png": "png", "image/webp": "webp", "image/gif": "gif", "audio/mp4": "m4a", "audio/mpeg": "mp3",
	} {
		if got, ok := Extension(mediaType); !ok || got != want {
			t.Fatalf("Extension(%q) = %q, %v", mediaType, got, ok)
		}
	}
	if _, ok := Extension("application/octet-stream"); ok {
		t.Fatal("Extension() accepted an unlisted type")
	}
}
