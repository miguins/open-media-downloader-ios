package extractor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

const testRedditShare = "https://www.reddit.com/r/example/s/Ab12Cd34Ef/"

type redditRoundTrip func(*http.Request) (*http.Response, error)

func (f redditRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type redditBody struct{ closed bool }

func (*redditBody) Read([]byte) (int, error) { panic("redirect body must not be read") }
func (b *redditBody) Close() error           { b.closed = true; return nil }

func redditPolicy(t *testing.T) *urlpolicy.Policy {
	t.Helper()
	p, err := urlpolicy.New(urlpolicy.PlatformIDs(), 256)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRedditRedirectDestination(t *testing.T) {
	for _, code := range []int{301, 302, 303, 307, 308} {
		for _, location := range []string{"/comments/abc123/?utm_source=test#frag", "https://old.reddit.com/gallery/abc123/", "/r/s/comments/abc123/", "/comments/abc123/s/"} {
			body := &redditBody{}
			calls := 0
			client := &http.Client{Transport: redditRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.String() != testRedditShare || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatal("unsafe redirect request")
				}
				return &http.Response{StatusCode: code, Header: http.Header{"Location": {location}}, Body: body}, nil
			})}
			got, err := resolveRedditRedirects(t.Context(), testRedditShare, redditPolicy(t), client)
			want := "https://www.reddit.com/comments/abc123/"
			if strings.Contains(location, "gallery") {
				want = location
			} else if strings.Contains(location, "/s/") {
				want = "https://www.reddit.com" + location
			}
			if err != nil || got != want || calls != 1 || !body.closed {
				t.Fatalf("redirect = %q, %v, calls %d, closed %v", got, err, calls, body.closed)
			}
		}
	}
}

func TestRedditRedirectRejectsUnsafeLocation(t *testing.T) {
	for _, location := range []string{
		"", "%zz", "http://www.reddit.com/comments/abc123/", "https://user:secret@www.reddit.com/comments/abc123/",
		"https://www.reddit.com:444/comments/abc123/", "https://vimeo.com/123", "https://evil.example/comments/abc123/",
		"https://127.0.0.1/comments/abc123/", "/r/example/", "/r/example/comments/abc123/title/comment",
		"/comments/" + strings.Repeat("x", 256), testRedditShare + "?utm_source=cycle",
	} {
		t.Run(location, func(t *testing.T) {
			calls := 0
			body := &redditBody{}
			client := &http.Client{Transport: redditRoundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {location}}, Body: body}, nil
			})}
			_, err := resolveRedditRedirects(t.Context(), testRedditShare, redditPolicy(t), client)
			if requireFailure(t, err, job.DetailToolError, "reddit").Detail != job.DetailToolError || calls != 1 || !body.closed {
				t.Fatal("unsafe destination contacted or body leaked")
			}
		})
	}
}

func TestRedditRedirectHopLimit(t *testing.T) {
	for _, finish := range []bool{true, false} {
		calls := 0
		client := &http.Client{Transport: redditRoundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			location := "https://" + strings.Repeat("a", calls) + ".reddit.com/r/example/s/Ab12Cd34Ef/"
			if calls == 2 {
				location = "https://redd.it/abc123"
			}
			if calls == 5 && finish {
				location = "/comments/abc123/"
			}
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {location}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		got, err := resolveRedditRedirects(t.Context(), testRedditShare, redditPolicy(t), client)
		if finish && (err != nil || got != "https://aaaa.reddit.com/comments/abc123/") {
			t.Fatalf("five-hop result = %q, %v", got, err)
		}
		if !finish {
			_ = requireFailure(t, err, job.DetailToolError, "reddit")
		}
		if calls != 5 {
			t.Fatalf("requests = %d", calls)
		}
	}
}

func TestRedditRedirectErrors(t *testing.T) {
	for code, detail := range map[int]job.ErrorDetail{200: job.DetailToolError, 500: job.DetailToolError, 403: job.DetailForbidden, 404: job.DetailUnavailable, 429: job.DetailRateLimited} {
		body := &redditBody{}
		client := &http.Client{Transport: redditRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Body: body, Header: make(http.Header)}, nil
		})}
		_, err := resolveRedditRedirects(t.Context(), testRedditShare, redditPolicy(t), client)
		_ = requireFailure(t, err, detail, "reddit")
		if !body.closed {
			t.Fatal("body leaked")
		}
	}
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		client := &http.Client{Transport: redditRoundTrip(func(*http.Request) (*http.Response, error) {
			if canceled {
				cancel()
			}
			return nil, errors.New("private transport diagnostics")
		})}
		_, err := resolveRedditRedirects(ctx, testRedditShare, redditPolicy(t), client)
		cancel()
		if canceled {
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v", err)
			}
		} else {
			_ = requireFailure(t, err, job.DetailNetworkError, "reddit")
		}
		if strings.Contains(err.Error(), "private") {
			t.Fatal("transport diagnostics leaked")
		}
	}
	client := &http.Client{Timeout: time.Millisecond, Transport: redditRoundTrip(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	_, err := resolveRedditRedirects(t.Context(), testRedditShare, redditPolicy(t), client)
	_ = requireFailure(t, err, job.DetailNetworkError, "reddit")
}

func TestResolveRedditURL(t *testing.T) {
	p := redditPolicy(t)
	for _, raw := range []string{"https://www.reddit.com/comments/abc123/", "https://redd.it/abc123", "https://www.reddit.com/gallery/abc123/", "https://www.reddit.com/r/s/comments/abc123/", "https://www.reddit.com/comments/abc123/s/"} {
		got, err := resolveRedditURL(t.Context(), raw, "http://127.0.0.1:1", p)
		if err != nil || got != raw {
			t.Fatalf("direct result = %q, %v", got, err)
		}
	}
	for _, raw := range []string{"%zz", "https://vimeo.com/123"} {
		_, err := resolveRedditURL(t.Context(), raw, "http://127.0.0.1:1", p)
		_ = requireFailure(t, err, job.DetailToolError, "reddit")
	}
	if _, err := resolveRedditURL(t.Context(), testRedditShare, "http://proxy.example:80", p); err == nil {
		t.Fatal("accepted external proxy")
	}
	_, err := resolveRedditURL(t.Context(), testRedditShare, "http://127.0.0.1:1", p)
	_ = requireFailure(t, err, job.DetailNetworkError, "reddit")
}

func TestRedditCanonicalExtractionURL(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"https://www.reddit.com/comments/AbC123/Title/", "https://www.reddit.com/comments/abc123/Title/"},
		{"https://www.reddit.com/r/Example/comments/ABC123/s/", "https://www.reddit.com/r/Example/comments/abc123/s/"},
		{"https://www.reddit.com/gallery/ABC123/", "https://www.reddit.com/gallery/abc123/"},
		{"https://redd.it/ABC123", "https://redd.it/abc123"},
	} {
		got, err := resolveRedditURL(t.Context(), tc.raw, "http://127.0.0.1:1", redditPolicy(t))
		if err != nil || got != tc.want {
			t.Fatalf("canonical = %q, %v; want %q", got, err, tc.want)
		}
		client := &http.Client{Transport: redditRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {tc.raw}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		if !strings.Contains(tc.raw, "redd.it/") {
			got, err = resolveRedditRedirects(t.Context(), testRedditShare, redditPolicy(t), client)
			if err != nil || got != tc.want {
				t.Fatalf("destination = %q, %v", got, err)
			}
		}
	}
}
