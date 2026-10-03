//go:build omdi_testextractor

package main

import (
	"context"

	"github.com/miguins/open-media-downloader-ios/internal/config"
	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
	"github.com/miguins/open-media-downloader-ios/internal/worker"
)

type extractorComponents struct {
	extractor worker.Extractor
	run       func(context.Context) error
}

func newExtractorComponents(config.Config, *urlpolicy.Policy) (extractorComponents, error) {
	return extractorComponents{extractor: extractor.Fake{}, run: func(ctx context.Context) error { <-ctx.Done(); return nil }}, nil
}
