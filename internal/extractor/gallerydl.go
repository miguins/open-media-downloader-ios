package extractor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

var galleryName = regexp.MustCompile(`^item-([0-9]{3})\.[A-Za-z0-9]+$`)

type GalleryDL struct {
	path   string
	runner *Runner
	media  *MediaTools
}

func NewGalleryDL(path string, runner *Runner, media *MediaTools) *GalleryDL {
	return &GalleryDL{path: path, runner: runner, media: media}
}

func (g *GalleryDL) Extract(ctx context.Context, request Request, proxyURL string) ([]File, error) {
	if !validAdapterRequest(request, "instagram", "x", "reddit") || !validProxyURL(proxyURL) {
		return nil, errors.New("extractor: invalid adapter request")
	}
	args := []string{
		"--config-ignore", "--no-input", "--no-colors", "--no-postprocessors", "--no-mtime",
		"--proxy", proxyURL, "--directory", request.WorkDir, "--filename", "item-{num:03}.{extension}",
		"--range", "1-" + strconv.Itoa(request.MaxItems+1), "--filesize-max", strconv.FormatInt(request.MaxBytes, 10),
		"--retries", "3", "--http-timeout", "30", "-o", "cache.file=:memory:", "--", request.URL,
	}
	_, err := g.runner.Run(ctx, Command{Path: g.path, Args: args, Dir: request.WorkDir, StdoutLimit: 256 << 10, StderrLimit: 64 << 10})
	if err != nil {
		return nil, adapterCommandError(ctx, err)
	}
	names, err := discoverGallery(request.WorkDir, request.MaxItems)
	if err != nil {
		return nil, err
	}
	files, err := g.media.Finalize(ctx, request.WorkDir, names)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, ErrExtractionFailed) {
			return nil, ErrExtractionFailed
		}
		return nil, err
	}
	return files, nil
}

func discoverGallery(dir string, maxItems int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.New("extractor: enumerate output")
	}
	type numbered struct {
		name   string
		number int
	}
	items := make([]numbered, 0, len(entries))
	for _, entry := range entries {
		match := galleryName.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil || !secureRegular(filepath.Join(dir, entry.Name())) {
			return nil, ErrExtractionFailed
		}
		n, _ := strconv.Atoi(match[1]) // The anchored expression permits exactly three decimal digits.
		items = append(items, numbered{entry.Name(), n})
	}
	if len(items) == 0 {
		return nil, ErrExtractionFailed
	}
	if len(items) > maxItems {
		return nil, ErrTooLarge
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
