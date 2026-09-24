package extractor

import (
	"context"
	"errors"

	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

type proxySession interface {
	URL() string
	Err() error
	Close() error
}

type beginSessionFunc func(context.Context, int64) (proxySession, error)
type adapterFunc func(context.Context, Request, string) ([]File, error)

// Real routes supported platforms through real extractors behind one egress session.
type Real struct {
	beginSession beginSessionFunc
	ytdlp        adapterFunc
	gallery      adapterFunc
}

// NewReal constructs production extractor routing.
func NewReal(proxy *urlpolicy.Proxy, ytdlp *YTDLP, gallery *GalleryDL) *Real {
	return &Real{
		beginSession: func(ctx context.Context, maxBytes int64) (proxySession, error) { return proxy.Begin(ctx, maxBytes) },
		ytdlp:        ytdlp.Extract, gallery: gallery.Extract,
	}
}

// Extract downloads one post with the adapter selected for its platform.
func (r *Real) Extract(ctx context.Context, request Request) ([]File, error) {
	if request.MaxBytes <= 0 || request.MaxItems <= 0 || !safeWorkDir(request.WorkDir) {
		return nil, errors.New("extractor: invalid request")
	}
	session, err := r.beginSession(ctx, request.MaxBytes)
	if err != nil {
		return nil, errors.New("extractor: egress session unavailable")
	}
	defer func() { _ = session.Close() }()
	var files []File
	switch request.Platform {
	case "youtube", "vimeo", "tiktok":
		files, err = r.ytdlp(ctx, request, session.URL())
	case "instagram", "x", "reddit":
		files, err = r.gallery(ctx, request, session.URL())
	default:
		err = errors.New("extractor: unsupported platform")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if errors.Is(session.Err(), urlpolicy.ErrEgressTooLarge) {
		return nil, ErrTooLarge
	}
	return files, err
}
