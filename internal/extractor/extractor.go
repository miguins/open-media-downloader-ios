// Package extractor defines extractor requests and results and provides extractor implementations.
package extractor

import (
	"context"
	"errors"
	"fmt"
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

// Fake produces one synthetic file without network access. It stands in for real extractors
// until they are implemented.
type Fake struct{}

// Extract writes the synthetic payload to the work directory.
func (Fake) Extract(ctx context.Context, request Request) ([]File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if int64(len(fakeMedia)) > request.MaxBytes {
		return nil, errors.New("extractor: output exceeds size budget")
	}
	const name = "fake.mp4"
	if err := os.WriteFile(filepath.Join(request.WorkDir, name), fakeMedia, 0o600); err != nil {
		return nil, fmt.Errorf("extractor: write fake media: %w", err)
	}

	return []File{{Name: name, MediaType: "video/mp4"}}, nil
}
