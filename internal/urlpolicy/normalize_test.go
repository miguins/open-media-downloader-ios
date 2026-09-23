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
		{"https://www.youtube.com/watch?v=abc#t=10", "https://www.youtube.com/watch?v=abc", "youtube"},
		{"https://youtu.be/abc", "https://youtu.be/abc", "youtube"},
		{"HTTPS://M.YouTube.COM./shorts/abc", "https://m.youtube.com/shorts/abc", "youtube"},
		{"https://youtube.com:443/watch?v=abc", "https://youtube.com/watch?v=abc", "youtube"},
		{"https://www.instagram.com/p/abc/", "https://www.instagram.com/p/abc/", "instagram"},
		{"https://vm.tiktok.com/abc/", "https://vm.tiktok.com/abc/", "tiktok"},
		{"https://x.com/user/status/1", "https://x.com/user/status/1", "x"},
		{"https://mobile.twitter.com/user/status/1", "https://mobile.twitter.com/user/status/1", "x"},
		{"https://old.reddit.com/r/a/comments/b/", "https://old.reddit.com/r/a/comments/b/", "reddit"},
		{"https://v.redd.it/abc", "https://v.redd.it/abc", "reddit"},
		{"https://vimeo.com/123", "https://vimeo.com/123", "vimeo"},
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
