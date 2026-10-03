package extractor

import (
	"context"
	"errors"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

type redditSession struct {
	testSession
	closed bool
	budget bool
	stats  urlpolicy.EgressStats
}

func (s *redditSession) Stats() urlpolicy.EgressStats { return s.stats }

func (s *redditSession) Close() error { s.closed = true; return nil }
func (s *redditSession) Err() error {
	if s.budget {
		return urlpolicy.ErrEgressTooLarge
	}
	return nil
}

func TestRealRedditResolution(t *testing.T) {
	for _, fail := range []bool{false, true} {
		session := &redditSession{}
		called := false
		r := &Real{
			beginSession: func(context.Context, int64) (proxySession, error) { return session, nil },
			resolveReddit: func(_ context.Context, raw, proxy string) (string, error) {
				if raw != testRedditShare || proxy != session.URL() {
					t.Fatal("resolver outside session")
				}
				if fail {
					return "", redditFailure(job.DetailForbidden)
				}
				return "https://www.reddit.com/comments/abc123/", nil
			},
			redditMedia: func(_ context.Context, req Request, proxy string) ([]File, error) {
				called = true
				if req.URL != "https://www.reddit.com/comments/abc123/" || proxy != session.URL() {
					t.Fatal("wrong resolved request")
				}
				return []File{{"item-000.jpg", "image/jpeg"}}, nil
			},
		}
		request := adapterRequest(t.TempDir(), "reddit", 1)
		request.URL = testRedditShare
		files, err := r.Extract(t.Context(), request)
		if fail {
			_ = requireFailure(t, err, job.DetailForbidden, "reddit")
			if called {
				t.Fatal("adapter called after resolver failure")
			}
		} else if err != nil || len(files) != 1 || !called {
			t.Fatalf("result = %v, %v", files, err)
		}
		if !session.closed || request.URL != testRedditShare {
			t.Fatal("session leaked or input mutated")
		}
	}
}

func TestRealRedditBudgetAndCancellation(t *testing.T) {
	for _, budget := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		session := &redditSession{budget: budget}
		r := &Real{beginSession: func(context.Context, int64) (proxySession, error) { return session, nil }, resolveReddit: func(context.Context, string, string) (string, error) {
			if !budget {
				cancel()
			}
			return "", redditFailure(job.DetailNetworkError)
		}}
		_, err := r.Extract(ctx, adapterRequest(t.TempDir(), "reddit", 1))
		cancel()
		want := context.Canceled
		if budget {
			want = ErrTooLarge
		}
		if !errors.Is(err, want) || !session.closed {
			t.Fatalf("error = %v; closed %v", err, session.closed)
		}
	}
}

func TestRealRedditNativeMedia(t *testing.T) {
	for _, scenario := range []string{"image", "gallery", "video", "no media", "blocked", "budget", "canceled", "denied"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			session := &redditSession{}
			req := adapterRequest(t.TempDir(), "reddit", 2)
			req.URL = testRedditShare
			calls := 0
			adapter := &Real{
				beginSession: func(context.Context, int64) (proxySession, error) { return session, nil },
				resolveReddit: func(context.Context, string, string) (string, error) {
					return "https://www.reddit.com/comments/abc123/", nil
				},
				gallery: func(context.Context, Request, string) ([]File, error) {
					t.Error("Reddit must not depend on gallery-dl REST access")
					return nil, &Failure{Detail: job.DetailBlocked, Tool: "gallery-dl"}
				},
				redditMedia: func(_ context.Context, got Request, proxy string) ([]File, error) {
					calls++
					if got.URL != "https://www.reddit.com/comments/abc123/" || proxy != session.URL() || got.MaxBytes != req.MaxBytes || got.WorkDir != req.WorkDir {
						t.Fatal("native extraction escaped the validated request/session")
					}
					switch scenario {
					case "no media":
						return nil, &Failure{Detail: job.DetailNoMedia, Tool: "yt-dlp"}
					case "blocked":
						return nil, &Failure{Detail: job.DetailBlocked, Tool: "yt-dlp"}
					case "budget":
						session.budget = true
						return nil, &Failure{Detail: job.DetailNetworkError, Tool: "yt-dlp"}
					case "canceled":
						cancel()
						return nil, &Failure{Detail: job.DetailToolError, Tool: "yt-dlp"}
					case "denied":
						session.stats.Rejected = 1
						return nil, &Failure{Detail: job.DetailNetworkError, Tool: "yt-dlp"}
					case "video":
						return []File{{"item-000.mp4", "video/mp4"}}, nil
					case "gallery":
						return []File{{"item-001.jpg", "image/jpeg"}, {"item-002.jpg", "image/jpeg"}}, nil
					default:
						return []File{{"item-000.jpg", "image/jpeg"}}, nil
					}
				},
			}
			files, err := adapter.Extract(ctx, req)
			switch scenario {
			case "image", "gallery", "video":
				want := 1
				if scenario == "gallery" {
					want = 2
				}
				if err != nil || len(files) != want {
					t.Fatalf("native result = %v, %v", files, err)
				}
			case "budget":
				if !errors.Is(err, ErrTooLarge) {
					t.Fatalf("budget = %v", err)
				}
			case "canceled":
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation = %v", err)
				}
			default:
				detail := job.DetailNoMedia
				if scenario == "blocked" {
					detail = job.DetailBlocked
				}
				if scenario == "denied" {
					detail = job.DetailEgressDenied
				}
				_ = requireFailure(t, err, detail, "yt-dlp")
			}
			if calls != 1 || !session.closed || req.URL != testRedditShare {
				t.Fatal("session leak, duplicate extraction, or source URL mutation")
			}
		})
	}
}
