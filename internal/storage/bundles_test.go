package storage

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/job"
)

func bundleItems(t *testing.T) (*Layout, string, []job.Item) {
	t.Helper()
	l, dir := newTestLayout(t)
	jid := id.New()
	w, err := l.PrepareWorkDir(jid)
	if err != nil {
		t.Fatal(err)
	}
	writeOutput(t, w, "video", "abc")
	writeOutput(t, w, "photo", "defg")
	items, err := l.Ingest(context.Background(), jid, []Output{{Name: "video", MediaType: "video/mp4"}, {Name: "photo", MediaType: "image/jpeg"}}, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	return l, dir, items
}
func TestBuildBundleMixedMedia(t *testing.T) {
	l, dir, items := bundleItems(t)
	items[0], items[1] = items[1], items[0]
	b, err := l.BuildBundle(context.Background(), items[0].JobID, items, 20, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	if b.FileName != "omdi-"+b.JobID[:8]+".zip" || b.SizeBytes <= 7 || b.SizeBytes > 3079 {
		t.Fatalf("bundle metadata: %#v", b)
	}
	assertMode(t, filepath.Join(dir, "jobs", b.JobID, "bundle.zip"), 0o600)
	f, err := l.OpenBundle(b.JobID, b.SizeBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zr, err := zip.NewReader(f, b.SizeBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != 2 {
		t.Fatalf("entries: %d", len(zr.File))
	}
	for i, want := range []string{"abc", "defg"} {
		entry := zr.File[i]
		if entry.Method != zip.Store || !strings.HasSuffix(entry.Name, []string{"-1.mp4", "-2.jpg"}[i]) {
			t.Fatalf("entry: %#v", entry.FileHeader)
		}
		r, e := entry.Open()
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(r)
		_ = r.Close()
		if e != nil || string(body) != want {
			t.Fatalf("entry bytes: %q %v", body, e)
		}
	}
	if _, err := l.BuildBundle(context.Background(), b.JobID, items, 20, 7, now); err == nil {
		t.Fatal("existing bundle overwritten")
	}
	if _, err := l.OpenBundle(b.JobID, b.SizeBytes); err != nil {
		t.Fatalf("failed rebuild removed existing bundle: %v", err)
	}
}

func TestBuildBundleRejectsUnsafeInputs(t *testing.T) {
	for _, name := range []string{"one item", "too many", "byte budget", "foreign job", "bad id", "duplicate id", "duplicate position", "duplicate name", "traversal", "backslash", "control", "long name", "missing", "symlink", "hardlink", "directory", "truncated", "grown", "zero", "wrong extension"} {
		t.Run(name, func(t *testing.T) {
			l, dir, items := bundleItems(t)
			maxItems := 20
			maxBytes := int64(7)
			jid := items[0].JobID
			p := filepath.Join(dir, "jobs", jid, items[0].ID)
			switch name {
			case "one item":
				items = items[:1]
			case "too many":
				maxItems = 1
			case "byte budget":
				maxBytes = 6
			case "foreign job":
				items[0].JobID = id.New()
			case "bad id":
				items[0].ID = "../escape"
			case "duplicate id":
				items[1].ID = items[0].ID
			case "duplicate position":
				items[1].Position = 0
			case "duplicate name":
				items[1].FileName = items[0].FileName
			case "traversal":
				items[0].FileName = "../x.mp4"
			case "backslash":
				items[0].FileName = "a\\b.mp4"
			case "control":
				items[0].FileName = "a\n.mp4"
			case "long name":
				items[0].FileName = strings.Repeat("a", 129) + ".mp4"
			case "missing":
				_ = os.Remove(p)
			case "symlink":
				_ = os.Remove(p)
				if err := os.Symlink(filepath.Join(dir, "jobs", jid, items[1].ID), p); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(p, filepath.Join(dir, "link")); err != nil {
					t.Fatal(err)
				}
			case "directory":
				_ = os.Remove(p)
				if err := os.Mkdir(p, 0o700); err != nil {
					t.Fatal(err)
				}
			case "truncated":
				if err := os.Truncate(p, 2); err != nil {
					t.Fatal(err)
				}
			case "grown":
				if err := os.Truncate(p, 4); err != nil {
					t.Fatal(err)
				}
			case "zero":
				items[0].SizeBytes = 0
			case "wrong extension":
				items[0].MediaType = "text/html"
			}
			if _, err := l.BuildBundle(context.Background(), jid, items, maxItems, maxBytes, now); err == nil {
				t.Fatal("unsafe input accepted")
			} else if strings.Contains(err.Error(), dir) {
				t.Fatal("error leaked internal path")
			}
			entries, err := os.ReadDir(filepath.Join(dir, "jobs", jid))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() == "bundle.zip" || strings.HasPrefix(e.Name(), ".bundle-") {
					t.Fatal("partial bundle remains")
				}
			}
		})
	}
}

func TestBuildBundleCancellation(t *testing.T) {
	l, dir, items := bundleItems(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.BuildBundle(ctx, items[0].JobID, items, 20, 7, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "jobs", items[0].JobID, "bundle.zip")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published canceled bundle")
	}
}
func TestIngestCancellation(t *testing.T) {
	l, _ := newTestLayout(t)
	jid := id.New()
	w, _ := l.PrepareWorkDir(jid)
	writeOutput(t, w, "a", "abc")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Ingest(ctx, jid, []Output{{Name: "a", MediaType: "video/mp4"}}, 3, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("ingest cancellation: %v", err)
	}
}
func TestOpenBundleRejectsUnsafeFiles(t *testing.T) {
	for _, name := range []string{"bad id", "bad size", "missing", "symlink", "hardlink", "directory", "size mismatch"} {
		t.Run(name, func(t *testing.T) {
			l, dir, items := bundleItems(t)
			b, err := l.BuildBundle(context.Background(), items[0].JobID, items, 20, 7, now)
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(dir, "jobs", b.JobID, "bundle.zip")
			switch name {
			case "bad id":
				b.JobID = "../escape"
			case "bad size":
				b.SizeBytes = 0
			case "missing":
				_ = os.Remove(p)
			case "symlink":
				_ = os.Remove(p)
				if err := os.Symlink(filepath.Join(dir, "jobs", b.JobID, items[0].ID), p); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(p, filepath.Join(dir, "link")); err != nil {
					t.Fatal(err)
				}
			case "directory":
				_ = os.Remove(p)
				if err := os.Mkdir(p, 0o700); err != nil {
					t.Fatal(err)
				}
			case "size mismatch":
				b.SizeBytes++
			}
			if f, err := l.OpenBundle(b.JobID, b.SizeBytes); err == nil {
				_ = f.Close()
				t.Fatal("unsafe bundle opened")
			}
		})
	}
}

func TestBuildBundleFailureRemovesTemporaryOutput(t *testing.T) {
	for _, name := range []string{"bad job", "create", "encode", "stat", "size", "close", "cancel after close", "install", "remove"} {
		t.Run(name, func(t *testing.T) {
			l, dir, items := bundleItems(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			jid := items[0].JobID
			boom := errors.New("injected I/O failure")
			switch name {
			case "bad job":
				jid = "../escape"
			case "create":
				l.bundleIO.create = func(string, string) (*os.File, error) { return nil, boom }
			case "encode":
				l.bundleIO.encode = func(context.Context, io.Writer, []job.Item, func(job.Item) (*os.File, error)) error { return boom }
			case "stat":
				l.bundleIO.create = func(a, b string) (*os.File, error) {
					f, e := os.CreateTemp(a, b)
					if e == nil {
						_ = f.Close()
					}
					return f, e
				}
				l.bundleIO.encode = func(context.Context, io.Writer, []job.Item, func(job.Item) (*os.File, error)) error { return nil }
			case "size":
				l.bundleIO.encode = func(context.Context, io.Writer, []job.Item, func(job.Item) (*os.File, error)) error { return nil }
			case "close":
				l.bundleIO.close = func(f *os.File) error { _ = f.Close(); return boom }
			case "cancel after close":
				l.bundleIO.close = func(f *os.File) error { cancel(); return f.Close() }
			case "install":
				l.bundleIO.install = func(string, string) error { return boom }
			case "remove":
				calls := 0
				l.bundleIO.remove = func(p string) error {
					calls++
					if calls == 1 {
						return boom
					}
					return os.Remove(p)
				}
			}
			if _, err := l.BuildBundle(ctx, jid, items, 20, 7, now); err == nil {
				t.Fatal("failed construction succeeded")
			}
			entries, err := os.ReadDir(filepath.Join(dir, "jobs", items[0].JobID))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.Name() == "bundle.zip" || strings.HasPrefix(e.Name(), ".bundle-") {
					t.Fatal("partial bundle remains")
				}
			}
		})
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestBundleWriterBudget(t *testing.T) {
	var out bytes.Buffer
	w := &bundleWriter{writer: &out, remaining: 3}
	if n, err := w.Write([]byte("abc")); n != 3 || err != nil {
		t.Fatalf("exact budget %d %v", n, err)
	}
	if n, err := w.Write([]byte("d")); n != 0 || !errors.Is(err, ErrTooLarge) {
		t.Fatalf("excess %d %v", n, err)
	}
	w = &bundleWriter{writer: writerFunc(func([]byte) (int, error) { return 1, nil }), remaining: 3}
	if _, err := w.Write([]byte("abc")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write %v", err)
	}
}

func TestWriteBundleFailures(t *testing.T) {
	for _, name := range []string{"open", "header", "finalize", "context before", "context during", "context after"} {
		t.Run(name, func(t *testing.T) {
			l, _, items := bundleItems(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			open := func(item job.Item) (*os.File, error) { return l.OpenItem(item.JobID, item.ID) }
			out := io.Discard
			boom := errors.New("write failure")
			switch name {
			case "open":
				open = func(job.Item) (*os.File, error) { return nil, boom }
			case "header":
				items[0].FileName = strings.Repeat("x", 65536)
			case "finalize":
				out = writerFunc(func([]byte) (int, error) { return 0, boom })
			case "context before":
				cancel()
			case "context during":
				open = func(item job.Item) (*os.File, error) { cancel(); return l.OpenItem(item.JobID, item.ID) }
			case "context after":
				items[1].SizeBytes = 0
				open = func(item job.Item) (*os.File, error) {
					if item.Position == 1 {
						cancel()
					}
					return l.OpenItem(item.JobID, item.ID)
				}
			}
			if err := writeBundle(ctx, out, items, open); err == nil {
				t.Fatal("writer ignored failure")
			}
		})
	}
}

func TestCopyBundleItemFailures(t *testing.T) {
	for _, name := range []string{"stat", "size", "read", "zero read", "write", "short write", "cancel at finish", "changed size"} {
		t.Run(name, func(t *testing.T) {
			l, dir, items := bundleItems(t)
			file, err := l.OpenItem(items[0].JobID, items[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = file.Close() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			out := io.Discard
			buffer := make([]byte, 32*1024)
			size := int64(3)
			switch name {
			case "stat":
				_ = file.Close()
			case "size":
				size = 4
			case "read":
				_, err = file.Seek(3, io.SeekStart)
				if err != nil {
					t.Fatal(err)
				}
			case "zero read":
				buffer = buffer[:0]
			case "write":
				out = writerFunc(func([]byte) (int, error) { return 0, errors.New("write failure") })
			case "short write":
				out = writerFunc(func([]byte) (int, error) { return 1, nil })
			case "cancel at finish":
				out = writerFunc(func(p []byte) (int, error) { cancel(); return len(p), nil })
			case "changed size":
				out = writerFunc(func(p []byte) (int, error) {
					err := os.Truncate(filepath.Join(dir, "jobs", items[0].JobID, items[0].ID), 4)
					return len(p), err
				})
			}
			if err := copyBundleItem(ctx, out, file, size, buffer); err == nil {
				t.Fatal("copy ignored failure")
			}
		})
	}
}

func TestBundleLongestNamesStayWithinBudget(t *testing.T) {
	l, _, items := bundleItems(t)
	for i := range items {
		items[i].FileName = fmt.Sprintf("%s%d", strings.Repeat("x", 127), i)
	}
	b, err := l.BuildBundle(context.Background(), items[0].JobID, items, 20, 7, now)
	if err != nil || b.SizeBytes > 3079 {
		t.Fatalf("bounded metadata: %#v %v", b, err)
	}
}

// cancelAfterChecks coordinates cancellation at a bounded-work boundary without timing sleeps.
type cancelAfterChecks struct {
	context.Context
	cancel     context.CancelFunc
	checks, at int
}

func (c *cancelAfterChecks) Err() error {
	c.checks++
	if c.checks == c.at {
		c.cancel()
	}
	return c.Context.Err()
}
func TestCancellationAtFinalizationBoundary(t *testing.T) {
	l, _, items := bundleItems(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &cancelAfterChecks{Context: ctx, cancel: cancel, at: 7}
	err := writeBundle(controlled, io.Discard, items, func(item job.Item) (*os.File, error) { return l.OpenItem(item.JobID, item.ID) })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("finalization cancellation: %v", err)
	}
}
func TestIngestCancellationBetweenEntries(t *testing.T) {
	l, _ := newTestLayout(t)
	jid := id.New()
	w, _ := l.PrepareWorkDir(jid)
	writeOutput(t, w, "a", "abc")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &cancelAfterChecks{Context: ctx, cancel: cancel, at: 2}
	if _, err := l.Ingest(controlled, jid, []Output{{Name: "a", MediaType: "video/mp4"}}, 3, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("ingest boundary cancellation: %v", err)
	}
}
