// Package extractor defines extractor requests and results and provides extractor implementations.
package extractor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Stable extraction failures contain no untrusted diagnostics.
var (
	ErrTooLarge         = errors.New("extractor: resource limit exceeded")
	ErrExtractionFailed = errors.New("extractor: extraction failed")
)

// Request describes one extraction. WorkDir is a private, empty directory the extractor may write to.
type Request struct {
	URL      string
	Platform string
	WorkDir  string
	MaxBytes int64
	MaxItems int
}

// File is one output the extractor wrote inside WorkDir. Both fields are untrusted.
type File struct {
	Name      string
	MediaType string
}

// fakeMedia is the synthetic payload written by Fake.
var fakeMedia = []byte(strings.Repeat("OMDI synthetic media for development and tests.\n", 32))

// Fake produces synthetic files without network access for development and tests.
type Fake struct{}

// Extract writes the synthetic payload to the work directory.
func (Fake) Extract(ctx context.Context, request Request) ([]File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files := []File{{Name: "fake.mp4", MediaType: "video/mp4"}}
	if request.URL == "https://www.instagram.com/p/DduKfFmDxsG/" {
		files = append(files, File{Name: "fake.jpg", MediaType: "image/jpeg"})
	}
	if len(files) > request.MaxItems || int64(len(fakeMedia))*int64(len(files)) > request.MaxBytes {
		return nil, ErrTooLarge
	}
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(request.WorkDir, file.Name), fakeMedia, 0o600); err != nil {
			return nil, errors.New("extractor: write fake media failed")
		}
	}
	return files, nil
}
