package extractor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"

	"github.com/miguins/open-media-downloader-ios/internal/job"
)

var galleryTooLarge = []byte("File size larger than allowed maximum")

var galleryName = regexp.MustCompile(`^item-([0-9]{3})\.[A-Za-z0-9]+$`)

// GalleryDL extracts bounded posts and carousels with gallery-dl.
type GalleryDL struct {
	path   string
	runner *Runner
	media  *MediaTools
}

// NewGalleryDL constructs the gallery-dl adapter.
func NewGalleryDL(path string, runner *Runner, media *MediaTools) *GalleryDL {
	return &GalleryDL{path: path, runner: runner, media: media}
}

// Extract downloads and validates one gallery-backed public post.
func (g *GalleryDL) Extract(ctx context.Context, request Request, proxyURL string) ([]File, error) {
	return g.extract(ctx, request, proxyURL)
}

func (g *GalleryDL) extract(ctx context.Context, request Request, proxyURL string) ([]File, error) {
	if !validAdapterRequest(request, "x", "reddit") || !validProxyURL(proxyURL) {
		return nil, errors.New("extractor: invalid adapter request")
	}
	args := []string{
		"--config-ignore", "--no-input", "--no-colors", "--no-postprocessors", "--no-mtime",
		"--proxy", proxyURL, "--directory", request.WorkDir, "--filename", "item-{num:03}.{extension}",
		"--range", "1-" + strconv.Itoa(request.MaxItems+1), "--filesize-max", strconv.FormatInt(request.MaxBytes, 10),
		"--retries", "3", "--http-timeout", "30", "-o", "cache.file=:memory:",
		// An empty whitelist stops child extractors, such as external links in Reddit posts.
		"-o", "extractor.whitelist=[]",
	}
	if request.Platform == "reddit" {
		args = append(args, redditGalleryOptions(request, proxyURL, g.media.ffmpegPath)...)
	}
	args = append(args, "--", request.URL)
	result, err := g.runner.Run(ctx, Command{Path: g.path, Args: args, Dir: request.WorkDir, StdoutLimit: 256 << 10, StderrLimit: 64 << 10})
	// A downloader may emit the size-limit marker before returning a nonzero exit.
	if ctx.Err() == nil && (err == nil || errors.Is(err, ErrCommandExit)) && result.Signal == "" &&
		(bytes.Contains(result.Stderr, galleryTooLarge) || bytes.Contains(result.Stderr, ytdlpTooLarge) || bytes.Contains(result.Stdout, ytdlpTooLarge)) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, toolFailure(ctx, "gallery-dl", job.DetailToolError, result, err)
	}
	entries, err := numberedEntries(request.WorkDir)
	if err != nil {
		return nil, err
	}
	if request.Platform == "reddit" && len(entries) == 0 {
		return nil, &Failure{Detail: job.DetailNoMedia, Tool: "gallery-dl"}
	}
	names, err := orderedNames(entries, request.MaxItems)
	if err != nil {
		return nil, err
	}
	return g.media.Finalize(ctx, request.WorkDir, names)
}

type numberedEntry struct {
	name   string
	number int
}

func discoverGallery(dir string, maxItems int) ([]string, error) {
	items, err := numberedEntries(dir)
	if err != nil {
		return nil, err
	}
	return orderedNames(items, maxItems)
}

// numberedEntries enumerates a work directory in which every entry must be a
// top-level regular file named item-NNN.extension.
func numberedEntries(dir string) ([]numberedEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.New("extractor: enumerate output")
	}
	items := make([]numberedEntry, 0, len(entries))
	for _, entry := range entries {
		match := galleryName.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil || !secureRegular(filepath.Join(dir, entry.Name())) {
			return nil, ErrExtractionFailed
		}
		n, _ := strconv.Atoi(match[1]) // The anchored expression permits exactly three decimal digits.
		items = append(items, numberedEntry{entry.Name(), n})
	}
	return items, nil
}

// orderedNames returns item names in source order, rejecting gaps and posts over the item limit.
func orderedNames(items []numberedEntry, maxItems int) ([]string, error) {
	if len(items) == 0 {
		return nil, ErrExtractionFailed
	}
	if len(items) > maxItems {
		return nil, ErrTooLarge
	}
	// Extractors number a single-media post 0 and carousel entries from 1.
	if len(items) == 1 && items[0].number == 0 {
		return []string{items[0].name}, nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].number < items[j].number })
	names := make([]string, len(items))
	for i, item := range items {
		if item.number != i+1 {
			return nil, ErrExtractionFailed
		}
		names[i] = item.name
	}
	return names, nil
}
