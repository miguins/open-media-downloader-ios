package job

import (
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/id"
)

var now = time.UnixMilli(1_800_000_000_000).UTC()

func newTestJob(t *testing.T) Job {
	t.Helper()
	j := New("owner", "https://vimeo.com/1", "vimeo", now, time.Hour)

	return j
}

func TestNew(t *testing.T) {
	j := newTestJob(t)
	if !id.Valid(j.ID) || j.OwnerID != "owner" || j.SourceURL != "https://vimeo.com/1" || j.Platform != "vimeo" {
		t.Fatalf("New() = %#v", j)
	}
	if j.Status != StatusQueued || j.ErrorCode != "" {
		t.Fatalf("New() status = %q code = %q", j.Status, j.ErrorCode)
	}
	if !j.CreatedAt.Equal(now) || !j.UpdatedAt.Equal(now) || !j.ExpiresAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("New() timestamps = %#v", j)
	}
	if !j.StartedAt.IsZero() || !j.FinishedAt.IsZero() {
		t.Fatalf("New() set lifecycle timestamps: %#v", j)
	}
}

func TestTransitions(t *testing.T) {
	all := []Status{StatusQueued, StatusRunning, StatusSucceeded, StatusFailed, StatusCanceled}
	allowed := map[Status][]Status{
		StatusQueued:  {StatusRunning, StatusCanceled},
		StatusRunning: {StatusSucceeded, StatusFailed, StatusCanceled},
	}
	for _, from := range all {
		for _, to := range all {
			want := false
			for _, candidate := range allowed[from] {
				want = want || candidate == to
			}
			if got := from.CanTransitionTo(to); got != want {
				t.Fatalf("%s.CanTransitionTo(%s) = %v; want %v", from, to, got, want)
			}
		}
		if got, want := from.Terminal(), len(allowed[from]) == 0; got != want {
			t.Fatalf("%s.Terminal() = %v; want %v", from, got, want)
		}
	}
	if Status("unknown").CanTransitionTo(StatusRunning) || Status("unknown").Valid() {
		t.Fatal("unknown status accepted")
	}
	for _, status := range all {
		if !status.Valid() {
			t.Fatalf("%s.Valid() = false", status)
		}
	}
}

func TestTransitionLifecycle(t *testing.T) {
	j := newTestJob(t)
	start := now.Add(time.Second)
	if err := j.Transition(StatusRunning, "", start); err != nil {
		t.Fatalf("Transition(running) error = %v", err)
	}
	if !j.StartedAt.Equal(start) || !j.UpdatedAt.Equal(start) || !j.FinishedAt.IsZero() {
		t.Fatalf("after running: %#v", j)
	}
	finish := now.Add(2 * time.Second)
	if err := j.Transition(StatusFailed, ErrorTimeout, finish); err != nil {
		t.Fatalf("Transition(failed) error = %v", err)
	}
	if j.Status != StatusFailed || j.ErrorCode != ErrorTimeout || !j.FinishedAt.Equal(finish) || !j.StartedAt.Equal(start) {
		t.Fatalf("after failed: %#v", j)
	}
}

func TestTransitionCancelFromQueued(t *testing.T) {
	j := newTestJob(t)
	if err := j.Transition(StatusCanceled, "", now); err != nil {
		t.Fatal(err)
	}
	if !j.StartedAt.IsZero() || !j.FinishedAt.Equal(now) {
		t.Fatalf("after cancel: %#v", j)
	}
}

func TestTransitionRejectsInvalidChanges(t *testing.T) {
	tests := []struct {
		name string
		to   Status
		code ErrorCode
	}{
		{name: "skip running", to: StatusSucceeded},
		{name: "failed from queued", to: StatusFailed, code: ErrorInternal},
		{name: "unknown status", to: Status("paused")},
		{name: "code on non-failure", to: StatusRunning, code: ErrorInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := newTestJob(t)
			before := j
			if err := j.Transition(tt.to, tt.code, now.Add(time.Second)); err == nil {
				t.Fatal("Transition() error = nil")
			}
			if j != before {
				t.Fatalf("rejected transition mutated job: %#v", j)
			}
		})
	}

	j := newTestJob(t)
	if err := j.Transition(StatusRunning, "", now); err != nil {
		t.Fatal(err)
	}
	for _, code := range []ErrorCode{"", ErrorCode("raw extractor output")} {
		if err := j.Transition(StatusFailed, code, now); err == nil {
			t.Fatalf("Transition(failed, %q) error = nil", code)
		}
	}
}

func TestErrorCodes(t *testing.T) {
	for _, code := range []ErrorCode{ErrorUnsupportedURL, ErrorExtractionFailed, ErrorTooLarge, ErrorTimeout, ErrorInternal} {
		if !code.Valid() {
			t.Fatalf("%s.Valid() = false", code)
		}
	}
	if ErrorCode("").Valid() || ErrorCode("other").Valid() {
		t.Fatal("unknown error code accepted")
	}
}

func TestDownloadTokenExpired(t *testing.T) {
	token := DownloadToken{ExpiresAt: now}
	if token.Expired(now.Add(-time.Millisecond)) || !token.Expired(now) || !token.Expired(now.Add(time.Second)) {
		t.Fatal("Expired() boundary is wrong")
	}
}
