package extractor

import (
	"context"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

type testSession struct{}

func (testSession) URL() string                  { return "http://127.0.0.1:1" }
func (testSession) Err() error                   { return nil }
func (testSession) Stats() urlpolicy.EgressStats { return urlpolicy.EgressStats{} }
func (testSession) Close() error                 { return nil }

func TestRealRoutesYouTube(t *testing.T) {
	called := false
	r := &Real{beginSession: func(context.Context, int64) (proxySession, error) { return testSession{}, nil }, ytdlp: func(context.Context, Request, string) ([]File, error) { called = true; return nil, nil }}
	_, err := r.Extract(t.Context(), Request{Platform: "youtube", WorkDir: t.TempDir(), MaxBytes: 1, MaxItems: 1})
	if err != nil || !called {
		t.Fatalf("Extract = %v, called %v", err, called)
	}
}

func TestRealRoutesEveryPlatform(t *testing.T) {
	for platform, want := range map[string]string{
		"youtube": "yt-dlp", "vimeo": "yt-dlp", "tiktok": "yt-dlp", "instagram": "yt-dlp", "reddit": "yt-dlp",
		"x": "gallery-dl",
	} {
		var got []string
		r := &Real{
			beginSession: func(context.Context, int64) (proxySession, error) { return testSession{}, nil },
			ytdlp:        func(context.Context, Request, string) ([]File, error) { got = append(got, "yt-dlp"); return nil, nil },
			gallery: func(context.Context, Request, string) ([]File, error) {
				got = append(got, "gallery-dl")
				return nil, nil
			},
		}
		if _, err := r.Extract(t.Context(), Request{Platform: platform, WorkDir: t.TempDir(), MaxBytes: 1, MaxItems: 1}); err != nil || len(got) != 1 || got[0] != want {
			t.Errorf("%s routed to %v, error %v; want %s", platform, got, err, want)
		}
	}
}
