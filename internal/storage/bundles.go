package storage

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/job"
)

const bundleName = "bundle.zip"

// BundleOverhead bounds ZIP metadata for validated entry counts and names.
func BundleOverhead(itemCount int) int64 { return 1024 * (int64(itemCount) + 1) }

// bundleIO isolates the filesystem and encoding operations that can fail independently.
type bundleIO struct {
	create  func(string, string) (*os.File, error)
	encode  func(context.Context, io.Writer, []job.Item, func(job.Item) (*os.File, error)) error
	close   func(*os.File) error
	install func(string, string) error
	remove  func(string) error
}

func defaultBundleIO() bundleIO {
	return bundleIO{create: os.CreateTemp, encode: writeBundle, close: (*os.File).Close, install: os.Link, remove: os.Remove}
}

// BuildBundle installs one complete bounded ZIP without overwriting an existing bundle.
func (l *Layout) BuildBundle(ctx context.Context, jobID string, items []job.Item, maxItems int, maxBytes int64, now time.Time) (job.Bundle, error) {
	if !id.Valid(jobID) {
		return job.Bundle{}, ErrInvalidID
	}
	if err := ctx.Err(); err != nil {
		return job.Bundle{}, err
	}
	ordered, total, err := validateBundleItems(jobID, items, maxItems, maxBytes)
	if err != nil {
		return job.Bundle{}, err
	}
	ops := l.bundleIO
	file, err := ops.create(l.jobPath(jobID), ".bundle-*")
	if err != nil {
		return job.Bundle{}, pathless("storage: create bundle", err)
	}
	temp := file.Name()
	installed := false
	createdFinal := false
	defer func() {
		_ = file.Close()
		_ = ops.remove(temp)
		if createdFinal && !installed {
			_ = ops.remove(filepath.Join(l.jobPath(jobID), bundleName))
		}
	}()
	limited := &bundleWriter{writer: file, remaining: total + BundleOverhead(len(items))}
	err = ops.encode(ctx, limited, ordered, func(item job.Item) (*os.File, error) { return l.OpenItem(jobID, item.ID) })
	if err != nil {
		return job.Bundle{}, pathless("storage: write bundle", err)
	}
	info, err := file.Stat()
	if err != nil {
		return job.Bundle{}, pathless("storage: inspect bundle", err)
	}
	if info.Size() != limited.written || info.Size() <= 0 {
		return job.Bundle{}, ErrInvalidOutput
	}
	if err = ops.close(file); err != nil {
		return job.Bundle{}, pathless("storage: close bundle", err)
	}
	if err = ctx.Err(); err != nil {
		return job.Bundle{}, err
	}
	// Link is atomic and refuses an existing destination. Remove the temporary link
	// before publishing metadata so the installed file has exactly one link.
	final := filepath.Join(l.jobPath(jobID), bundleName)
	if err = ops.install(temp, final); err != nil {
		return job.Bundle{}, pathless("storage: install bundle", err)
	}
	createdFinal = true
	if err = ops.remove(temp); err != nil {
		return job.Bundle{}, pathless("storage: finish bundle", err)
	}
	installed = true
	return job.Bundle{JobID: jobID, FileName: fmt.Sprintf("omdi-%s.zip", jobID[:8]), SizeBytes: info.Size(), CreatedAt: now}, nil
}

func validateBundleItems(jobID string, items []job.Item, maxItems int, maxBytes int64) ([]job.Item, int64, error) {
	if len(items) < 2 {
		return nil, 0, ErrInvalidOutput
	}
	if len(items) > maxItems {
		return nil, 0, ErrTooLarge
	}
	ordered := slices.Clone(items)
	slices.SortFunc(ordered, func(a, b job.Item) int {
		if a.Position < b.Position {
			return -1
		}
		if a.Position > b.Position {
			return 1
		}
		return 0
	})
	names := map[string]bool{}
	ids := map[string]bool{}
	var total int64
	for position, item := range ordered {
		_, ok := Extension(item.MediaType)
		if !ok || !id.Valid(item.ID) || item.JobID != jobID || item.Position != position || item.SizeBytes <= 0 || !validBundleName(item.FileName) || names[item.FileName] || ids[item.ID] {
			return nil, 0, ErrInvalidOutput
		}
		if item.SizeBytes > maxBytes-total {
			return nil, 0, ErrTooLarge
		}
		total += item.SizeBytes
		names[item.FileName] = true
		ids[item.ID] = true
	}
	return ordered, total, nil
}

func validBundleName(name string) bool {
	return len(name) <= 128 && validName(name) && !strings.ContainsAny(name, "/\\") && !strings.ContainsFunc(name, unicode.IsControl)
}

type bundleWriter struct {
	writer             io.Writer
	remaining, written int64
}

func (w *bundleWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, ErrTooLarge
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	w.written += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

func writeBundle(ctx context.Context, out io.Writer, items []job.Item, open func(job.Item) (*os.File, error)) error {
	zw := zip.NewWriter(out)
	buffer := make([]byte, 32*1024)
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		header := &zip.FileHeader{Name: item.FileName, Method: zip.Store, Modified: item.CreatedAt}
		entry, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		file, err := open(item)
		if err != nil {
			return err
		}
		err = copyBundleItem(ctx, entry, file, item.SizeBytes, buffer)
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return zw.Close()
}

func copyBundleItem(ctx context.Context, out io.Writer, file *os.File, size int64, buffer []byte) error {
	if err := validateBundleFile(file, size); err != nil {
		return err
	}
	remaining := size
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := file.Read(buffer[:min(int64(len(buffer)), remaining)])
		if err != nil {
			return ErrInvalidOutput
		}
		if n == 0 {
			return ErrInvalidOutput
		}
		written, err := out.Write(buffer[:n])
		if err != nil {
			return err
		}
		if written != n {
			return io.ErrShortWrite
		}
		remaining -= int64(n)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return validateBundleFile(file, size)
}

func validateBundleFile(file *os.File, size int64) error {
	info, err := file.Stat()
	if err != nil {
		return pathless("storage: inspect bundle file", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Size() != size || size <= 0 || stat.Nlink != 1 {
		return ErrInvalidOutput
	}
	return nil
}

// OpenBundle opens only the installed, regular, single-link ZIP of the recorded size.
func (l *Layout) OpenBundle(jobID string, expectedSize int64) (*os.File, error) {
	if !id.Valid(jobID) {
		return nil, ErrInvalidID
	}
	file, err := os.OpenFile(filepath.Join(l.jobPath(jobID), bundleName), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // Validated ID and fixed basename.
	if err != nil {
		return nil, pathless("storage: open bundle", err)
	}
	if err := validateBundleFile(file, expectedSize); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}
