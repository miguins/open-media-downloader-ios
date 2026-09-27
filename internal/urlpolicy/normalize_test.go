package urlpolicy

import (
	"errors"
	"strings"
	"testing"
)

func newTestPolicy(t *testing.T, platforms ...string) *Policy {
	t.Helper()
	if len(platforms) == 0 {
		platforms = PlatformIDs()
	}
	p, err := New(platforms, 256)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	return p
}

func TestNewRejectsInvalidSettings(t *testing.T) {
	if _, err := New([]string{"youtube", "unknown"}, 256); err == nil {
		t.Fatal("New() accepted an unknown platform")
	}
	if _, err := New(nil, 256); err == nil {
		t.Fatal("New() accepted no platforms")
	}
	if _, err := New([]string{"youtube"}, 0); err == nil {
		t.Fatal("New() accepted a non-positive length")
	}
}

func TestNormalizeAcceptsSupportedURLs(t *testing.T) {
	tests := []struct {
		raw, url, platform string
	}{
		{"https://www.youtube.com/watch?v=abc&list=PL1#t=3", "https://www.youtube.com/watch?v=abc", "youtube"},
		{"https://youtube.com/watch/?v=a_b-9&vtracking=x", "https://youtube.com/watch/?v=a_b-9", "youtube"},
		{"https://youtube.com/watch?v=%61bc", "https://youtube.com/watch?v=abc", "youtube"},
		{"https://youtu.be/abc?si=tracking", "https://youtu.be/abc", "youtube"},
		{"https://youtu.be/abc/", "https://youtu.be/abc/", "youtube"},
		{"https://youtube.com/shorts/a_b-9", "https://youtube.com/shorts/a_b-9", "youtube"},
		{"https://youtube.com/live/a_b-9/?", "https://youtube.com/live/a_b-9/", "youtube"},
		{"HTTPS://M.YouTube.COM./shorts/abc", "https://m.youtube.com/shorts/abc", "youtube"},
		{"https://youtube.com:443/watch?v=abc", "https://youtube.com/watch?v=abc", "youtube"},
		{"https://www.instagram.com/p/abc/", "https://www.instagram.com/p/abc/", "instagram"},
		{"https://instagram.com/reel/Ab_9/?igsh=x", "https://instagram.com/reel/Ab_9/", "instagram"},
		{"https://instagram.com/reels/Ab_9", "https://instagram.com/reels/Ab_9", "instagram"},
		{"https://instagram.com/tv/Ab-9?", "https://instagram.com/tv/Ab-9", "instagram"},
		{"https://www.instagram.com/some.user_9/p/Ab_9/?img_index=1", "https://www.instagram.com/p/Ab_9/", "instagram"},
		{"https://instagram.com/user/reel/Ab_9", "https://instagram.com/reel/Ab_9", "instagram"},
		{"https://vm.tiktok.com/abc/", "https://vm.tiktok.com/abc/", "tiktok"},
		{"https://vt.tiktok.com/Ztoken?share=x", "https://vt.tiktok.com/Ztoken", "tiktok"},
		{"https://www.tiktok.com/t/Ztoken/", "https://www.tiktok.com/t/Ztoken/", "tiktok"},
		{"https://www.tiktok.com/@creator.name_9/video/123?lang=en", "https://www.tiktok.com/@creator.name_9/video/123", "tiktok"},
		{"https://x.com/user/status/1", "https://x.com/user/status/1", "x"},
		{"https://twitter.com/user/status/123?s=20", "https://twitter.com/user/status/123", "x"},
		{"https://x.com/i/status/123/?s=20", "https://x.com/i/status/123/", "x"},
		{"https://mobile.twitter.com/user/status/1", "https://mobile.twitter.com/user/status/1", "x"},
		{"https://old.reddit.com/r/a/comments/b/", "https://old.reddit.com/r/a/comments/b/", "reddit"},
		{"https://reddit.com/r/go/comments/abc/title/?share_id=x", "https://reddit.com/r/go/comments/abc/title/", "reddit"},
		{"https://reddit.com/comments/Ab9/title", "https://reddit.com/comments/Ab9/title", "reddit"},
		{"https://reddit.com/comments/abc", "https://reddit.com/comments/abc", "reddit"},
		{"https://reddit.com/gallery/abc/", "https://reddit.com/gallery/abc/", "reddit"},
		{"https://redd.it/abc?utm_source=x", "https://redd.it/abc", "reddit"},
		{"https://vimeo.com/123?share=copy", "https://vimeo.com/123", "vimeo"},
		{"https://vimeo.com/123/?", "https://vimeo.com/123/", "vimeo"},
		{"https://player.vimeo.com/video/123", "https://player.vimeo.com/video/123", "vimeo"},
		{"https://vimeo.com/%31%32%33?share=copy", "https://vimeo.com/123", "vimeo"},
		{"https://ＶＩＭＥＯ.com/123", "https://vimeo.com/123", "vimeo"},
	}
	p := newTestPolicy(t)
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := p.Normalize(tt.raw)
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if got.URL != tt.url || got.Platform != tt.platform {
				t.Fatalf("Normalize() = %#v; want %q on %q", got, tt.url, tt.platform)
			}
		})
	}
}

func TestNormalizeRejectsNonPostURLs(t *testing.T) {
	tests := []string{
		"https://youtube.com/watch", "https://youtube.com/watch?v=", "https://youtube.com/watch?v=a&v=b",
		"https://youtube.com/watch?v=a&%76=b", "https://youtube.com/watch?v=abc;list=PL1",
		"https://youtube.com/watch?v=abc&bad=%zz", "https://youtube.com/watch?v=abc%2fdef",
		"https://youtube.com/watch%2f?v=abc", "https://youtube.com/watch?v=abc+def",
		"https://youtube.com/watch?v=abc%00", "https://youtube.com/watch?v=é",
		"https://youtube.com/playlist?list=PL1", "https://youtube.com/channel/abc", "https://youtube.com/@name",
		"https://youtube.com/shorts/", "https://youtube.com/live/abc/extra", "https://youtu.be/abc/extra",
		"https://youtu.be/", "https://youtube.com/abc",
		"https://instagram.com/user", "https://instagram.com/explore", "https://instagram.com/p/",
		"https://instagram.com/p/abc/extra", "https://instagram.com/p/é", "https://instagram.com/p/a.b",
		"https://instagram.com/user/p/", "https://instagram.com/user/explore/abc", "https://instagram.com/a/b/p/abc",
		"https://instagram.com/../p/abc", "https://instagram.com/%2e%2e/p/abc", "https://instagram.com/.user/p/abc",
		"https://instagram.com/user%2fp/abc", "https://instagram.com/user/p/abc/extra",
		"https://instagram.com/abcdefghijklmnopqrstuvwxyz12345/p/abc",
		"https://tiktok.com/@user", "https://tiktok.com/@user/video/abc", "https://tiktok.com/@/video/123",
		"https://tiktok.com/@user/video/123/extra", "https://tiktok.com/abc", "https://vm.tiktok.com/a/b",
		"https://vt.tiktok.com/", "https://tiktok.com/t/", "https://sub.vm.tiktok.com/abc",
		"https://x.com/user", "https://twitter.com/user/status/abc", "https://x.com//status/1",
		"https://x.com/user/status/1/photo/1", "https://x.com/user.name/status/1",
		"https://reddit.com/r/go", "https://reddit.com/r/go/new", "https://reddit.com/comments/",
		"https://reddit.com/r/go/comments/a-b", "https://reddit.com/r/go/comments/abc/title/def",
		"https://reddit.com/gallery/abc/extra", "https://redd.it/abc/extra", "https://v.redd.it/abc",
		"https://vimeo.com/channels/staffpicks", "https://vimeo.com/showcase/123", "https://vimeo.com/",
		"https://vimeo.com/abc", "https://vimeo.com/123/extra", "https://vimeo.com/video/123",
		"https://player.vimeo.com/video/", "https://player.vimeo.com/video/123/extra",
		"https://vimeo.com/%2f123", "https://vimeo.com/%2e%2e/123", "https://vimeo.com/../123",
		"https://youtube.com/shorts%2Fabc", "https://instagram.com/p%2fabc", "https://tiktok.com/t%2fabc",
		"https://twitter.com/user%2fstatus/123", "https://reddit.com/r/go%2fcomments/abc",
		"https://player.vimeo.com/video%2f123", "https://reddit.com/r/go/comments/abc/%2e%2e",
		"https://vimeo.com/123\\extra", "https://vimeo.com/123%5cextra", "https://vimeo.com/123%252fextra",
		"https://vimeo.com//123", "https://vimeo.com/123//", "https://vimeo.com/１２３",
	}
	p := newTestPolicy(t)
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			_, err := p.Normalize(raw)
			if !errors.Is(err, ErrUnsupportedURL) {
				t.Fatalf("Normalize() error = %v; want ErrUnsupportedURL", err)
			}
			if strings.Contains(err.Error(), raw) {
				t.Fatal("error exposes the submitted URL")
			}
		})
	}
}

func TestNormalizeRejectsUnsupportedURLs(t *testing.T) {
	tests := map[string]string{
		"empty":               "",
		"too long":            "https://vimeo.com/" + strings.Repeat("a", 256),
		"invalid UTF-8":       "https://vimeo.com/\xff",
		"control character":   "https://vimeo.com/\x00",
		"DEL character":       "https://vimeo.com/\x7f",
		"leading whitespace":  " https://vimeo.com/1",
		"inner whitespace":    "https://vimeo.com/a b",
		"unicode space":       "https://vimeo.com/a b",
		"http":                "http://vimeo.com/1",
		"javascript":          "javascript:alert(1)",
		"file":                "file:///etc/passwd",
		"relative":            "//vimeo.com/1",
		"opaque":              "https:vimeo.com/1",
		"parse error":         "https://vimeo.com/%zz",
		"user info":           "https://user@vimeo.com/1",
		"empty user info":     "https://@vimeo.com/1",
		"custom port":         "https://vimeo.com:8443/1",
		"empty port":          "https://vimeo.com:/1",
		"no host":             "https:///path",
		"IPv4 literal":        "https://127.0.0.1/1",
		"IPv6 literal":        "https://[::1]/1",
		"unlisted host":       "https://example.com/1",
		"look-alike suffix":   "https://evilvimeo.com/1",
		"look-alike prefix":   "https://vimeo.com.evil.example/1",
		"invalid IDNA":        "https://xn--a.vimeo.com/1",
		"empty label":         "https://a..vimeo.com/1",
		"disallowed platform": "https://youtube.com/watch?v=1",
		"bare TLD":            "https://com/1",
	}
	p := newTestPolicy(t, "vimeo")
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := p.Normalize(raw)
			if !errors.Is(err, ErrUnsupportedURL) {
				t.Fatalf("Normalize() error = %v; want ErrUnsupportedURL", err)
			}
			if raw != "" && strings.Contains(err.Error(), raw) {
				t.Fatalf("error leaked input: %v", err)
			}
		})
	}
}
