//go:build !omdi_testextractor

package main

import (
	"context"
	"net"

	"github.com/miguins/open-media-downloader-ios/internal/config"
	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
	"github.com/miguins/open-media-downloader-ios/internal/worker"
)

type extractorComponents struct {
	extractor worker.Extractor
	run       func(context.Context) error
}

func newExtractorComponents(cfg config.Config) (extractorComponents, error) {
	runner := extractor.NewRunner()
	media := extractor.NewMediaTools(cfg.Tools.FFprobe, cfg.Tools.FFmpeg, runner)
	proxy, err := urlpolicy.NewProxy(net.DefaultResolver, &net.Dialer{Control: urlpolicy.DialControl})
	if err != nil {
		return extractorComponents{}, err
	}
	ytdlp := extractor.NewYTDLP(cfg.Tools.YTDLP, cfg.Tools.FFmpeg, runner, media)
	gallery := extractor.NewGalleryDL(cfg.Tools.GalleryDL, runner, media)
	return extractorComponents{extractor: extractor.NewReal(proxy, ytdlp, gallery), run: proxy.Run}, nil
}
