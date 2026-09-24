//go:build !omdi_testextractor

package main

import (
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/config"
	"github.com/miguins/open-media-downloader-ios/internal/extractor"
)

func TestNewExtractorComponentsUsesReal(t *testing.T) {
	c, err := newExtractorComponents(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.extractor.(*extractor.Real); !ok {
		t.Fatalf("extractor = %T", c.extractor)
	}
}
