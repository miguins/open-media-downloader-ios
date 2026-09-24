package extractor

import (
	"context"
	"testing"
)

type testSession struct{}

func (testSession) URL() string  { return "http://127.0.0.1:1" }
func (testSession) Err() error   { return nil }
func (testSession) Close() error { return nil }

func TestRealRoutesYouTube(t *testing.T) {
	called := false
	r := &Real{beginSession: func(context.Context, int64) (proxySession, error) { return testSession{}, nil }, ytdlp: func(context.Context, Request, string) ([]File, error) { called = true; return nil, nil }}
	_, err := r.Extract(t.Context(), Request{Platform: "youtube", WorkDir: t.TempDir(), MaxBytes: 1, MaxItems: 1})
	if err != nil || !called {
		t.Fatalf("Extract = %v, called %v", err, called)
	}
}
