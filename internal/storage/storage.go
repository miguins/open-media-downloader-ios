// Package storage manages the private on-disk layout for job work areas and media files.
// Every path is derived from validated identifiers; extractor output is treated as untrusted.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/job"
)

const (
	jobsDir = "jobs"
	workDir = "work"
)

var (
	// ErrInvalidID reports an identifier that cannot name a directory or file.
	ErrInvalidID = errors.New("storage: invalid identifier")
	// ErrInvalidOutput reports extractor output that is missing, unsafe, or of an unsupported type.
	ErrInvalidOutput = errors.New("storage: invalid extractor output")
	// ErrTooLarge reports extractor output above the job size budget.
	ErrTooLarge = errors.New("storage: output exceeds size budget")
)

// extensions maps each accepted media type to the extension of its download name.
var extensions = map[string]string{
	"video/mp4":       "mp4",
	"video/webm":      "webm",
	"video/quicktime": "mov",
	"image/jpeg":      "jpg",
	"image/png":       "png",
	"image/webp":      "webp",
	"image/gif":       "gif",
	"audio/mp4":       "m4a",
	"audio/mpeg":      "mp3",
}

// Extension returns the download-name extension for an accepted media type.
func Extension(mediaType string) (string, bool) {
	extension, ok := extensions[mediaType]

	return extension, ok
}

// Output is one file an extractor reports inside its work directory.
type Output struct {
	Name      string
	MediaType string
}

// Layout is the storage layout rooted at the data directory.
type Layout struct {
	root     string
	bundleIO bundleIO
}

// New ensures the private jobs and work directories exist under dataDir.
func New(dataDir string) (*Layout, error) {
	layout := &Layout{root: dataDir, bundleIO: defaultBundleIO()}
	for _, name := range []string{jobsDir, workDir} {
		if err := ensurePrivateDir(filepath.Join(dataDir, name)); err != nil {
			return nil, err
		}
	}

	return layout, nil
}

func ensurePrivateDir(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return pathless("storage: create directory", err)
	}
	if info, err := os.Lstat(path); err != nil || !info.IsDir() {
		return errors.New("storage: expected a real directory")
	}

	return nil
}

// PrepareWorkDir returns an empty private work directory for jobID, removing leftovers from earlier runs.
func (l *Layout) PrepareWorkDir(jobID string) (string, error) {
	if !id.Valid(jobID) {
		return "", ErrInvalidID
	}
	path := l.workPath(jobID)
	if err := os.RemoveAll(path); err != nil {
		return "", pathless("storage: reset work directory", err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		return "", pathless("storage: create work directory", err)
	}

	return path, nil
}

// RemoveWorkDir deletes the work directory of jobID.
func (l *Layout) RemoveWorkDir(jobID string) error {
	if !id.Valid(jobID) {
		return ErrInvalidID
	}

	return pathless("storage: remove work directory", os.RemoveAll(l.workPath(jobID)))
}

// ClearWork deletes every work directory. It is used on startup, when no job can be running.
func (l *Layout) ClearWork() error {
	path := filepath.Join(l.root, workDir)
	if err := os.RemoveAll(path); err != nil {
		return pathless("storage: clear work directories", err)
	}

	return ensurePrivateDir(path)
}

// Ingest moves the reported outputs from the work directory of jobID into its private job directory
// under server-generated names and returns the resulting items. Any unsafe output, unsupported media
// type, or total size above maxBytes rejects the whole job and removes its job directory.
func (l *Layout) Ingest(ctx context.Context, jobID string, outputs []Output, maxBytes int64, now time.Time) ([]job.Item, error) {
	if !id.Valid(jobID) {
		return nil, ErrInvalidID
	}
	items, err := l.ingest(ctx, jobID, outputs, maxBytes, now)
	if err != nil {
		return nil, errors.Join(err, pathless("storage: remove rejected job", os.RemoveAll(l.jobPath(jobID))))
	}

	return items, nil
}

func (l *Layout) ingest(ctx context.Context, jobID string, outputs []Output, maxBytes int64, now time.Time) ([]job.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(outputs) == 0 {
		return nil, ErrInvalidOutput
	}
	if err := os.Mkdir(l.jobPath(jobID), 0o700); err != nil {
		return nil, pathless("storage: create job directory", err)
	}

	items := make([]job.Item, 0, len(outputs))
	var total int64
	for position, output := range outputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		extension, ok := Extension(output.MediaType)
		if !ok || !validName(output.Name) {
			return nil, ErrInvalidOutput
		}
		itemID := id.New()
		destination := filepath.Join(l.jobPath(jobID), itemID)
		// Move first and validate the destination, which only this process can reach, so the
		// extractor cannot swap the file between the check and its use.
		if err := os.Rename(filepath.Join(l.workPath(jobID), output.Name), destination); err != nil {
			return nil, ErrInvalidOutput
		}
		size, err := validateMovedFile(destination)
		if err != nil {
			return nil, err
		}
		total += size
		if total > maxBytes {
			return nil, ErrTooLarge
		}
		items = append(items, job.Item{
			ID:        itemID,
			JobID:     jobID,
			Position:  position,
			FileName:  fmt.Sprintf("omdi-%s-%d.%s", jobID[:8], position+1, extension),
			MediaType: output.MediaType,
			SizeBytes: size,
			CreatedAt: now,
		})
	}

	return items, nil
}

// validName accepts only a plain, non-special entry name inside the work directory.
func validName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name
}

// validateMovedFile requires a non-empty regular file with a single link, which rules out symlinks,
// special files, and hard links to files outside the work directory, and makes it private.
func validateMovedFile(path string) (int64, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return 0, ErrInvalidOutput
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return 0, ErrInvalidOutput
	}

	return info.Size(), pathless("storage: restrict output permissions", os.Chmod(path, 0o600))
}

// OpenItem opens a stored media file for reading without following symlinks.
func (l *Layout) OpenItem(jobID, itemID string) (*os.File, error) {
	if !id.Valid(jobID) || !id.Valid(itemID) {
		return nil, ErrInvalidID
	}
	// Both path components are validated identifiers, so the path stays inside the jobs root.
	file, err := os.OpenFile(filepath.Join(l.jobPath(jobID), itemID), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) //nolint:gosec // See above.
	if err != nil {
		return nil, pathless("storage: open item", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(ErrInvalidOutput, file.Close())
	}

	return file, nil
}

// RemoveJob deletes the job directory of jobID and everything in it. Missing directories are not an error.
func (l *Layout) RemoveJob(jobID string) error {
	if !id.Valid(jobID) {
		return ErrInvalidID
	}

	return pathless("storage: remove job directory", os.RemoveAll(l.jobPath(jobID)))
}

// JobDirIDs lists the job IDs that have a directory under the jobs root.
func (l *Layout) JobDirIDs() ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(l.root, jobsDir))
	if err != nil {
		return nil, pathless("storage: list job directories", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() && id.Valid(entry.Name()) {
			ids = append(ids, entry.Name())
		}
	}

	return ids, nil
}

// AvailableBytes returns the free space available to this process in the data directory.
func (l *Layout) AvailableBytes() (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(l.root, &stat); err != nil {
		return 0, fmt.Errorf("storage: statfs: %w", err)
	}

	return stat.Bavail * uint64(stat.Bsize), nil //nolint:gosec // Block size is always positive.
}

func (l *Layout) jobPath(jobID string) string {
	return filepath.Join(l.root, jobsDir, jobID)
}

func (l *Layout) workPath(jobID string) string {
	return filepath.Join(l.root, workDir, jobID)
}

// pathless wraps err under message after removing any filesystem path it carries.
func pathless(message string, err error) error {
	if err == nil {
		return nil
	}
	var pathError *fs.PathError
	if errors.As(err, &pathError) {
		err = pathError.Err
	}

	return fmt.Errorf("%s: %w", message, err)
}
