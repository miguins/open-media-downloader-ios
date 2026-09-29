package extractor

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

func TestClassifyStderr(t *testing.T) {
	for stderr, want := range map[string]job.ErrorDetail{
		"ERROR: [youtube] abc: Sign in to confirm your age. This video may be inappropriate for some users.": job.DetailAgeRestricted,
		"ERROR: [youtube] abc: Sign in to confirm you're not a bot. Use --cookies-from-browser":              job.DetailBlocked,
		"ERROR: [vimeo] 1: Unable to download JSON metadata: HTTP Error 403: Forbidden":                      job.DetailForbidden,
		"ERROR: unable to download video data: HTTP Error 403: Forbidden":                                    job.DetailForbidden,
		"ERROR: [youtube] abc: The uploader has not made this video available in your country":               job.DetailGeoRestricted,
		"ERROR: [instagram] abc: Requested content is not available, rate-limit reached or login required":   job.DetailLoginRequired,
		"[twitter][error] AuthorizationError: Login required to access this Tweet":                           job.DetailLoginRequired,
		"ERROR: [tiktok] 1: Unable to download webpage: HTTP Error 429: Too Many Requests":                   job.DetailRateLimited,
		"ERROR: [reddit] abc: Requested format is not available. Use --list-formats":                         job.DetailNoMedia,
		"ERROR: [youtube] abc: Private video. Sign in if you've been granted access":                         job.DetailUnavailable,
		"ERROR: [youtube] abc: Video unavailable. This video has been removed by the uploader":               job.DetailUnavailable,
		"ERROR: [vimeo] 1: Unable to download webpage: HTTP Error 404: Not Found":                            job.DetailUnavailable,
		"ERROR: [tiktok] 1: Unable to download webpage: <urlopen error [Errno 111] Connection refused>":      job.DetailNetworkError,
		"ERROR: [youtube] abc: Unable to download API page: The read operation timed out":                    job.DetailNetworkError,
		"ERROR: [youtube] abc: Unable to extract initial player response":                                    job.DetailToolError,
		"WARNING: [youtube] Sign in to confirm you're not a bot\n":                                           job.DetailToolError,
		"": job.DetailToolError,
	} {
		if got := classifyStderr([]byte(stderr)); got != want {
			t.Errorf("classifyStderr(%q) = %s; want %s", stderr, got, want)
		}
	}
}

func TestSanitizeDiagnostics(t *testing.T) {
	stderr := strings.Join([]string{
		"[debug] https://example.invalid/private?sig=secret",
		"WARNING: [youtube] ignored warning",
		"ERROR: [youtube] dQw4w9WgXcQ: Unable to download https://example.invalid/watch?v=dQw4w9WgXcQ&sig=secret to /data/work/job/media.mp4",
		"[twitter][error] HttpError: '404 Not Found' for 'https://example.invalid/i/api'\x1b[0m",
		"ERROR: " + strings.Repeat("x", 400),
		"ERROR: 4", "ERROR: 5", "ERROR: 6",
	}, "\n")
	got := sanitizeDiagnostics([]byte(stderr))
	want := []string{
		"ERROR: [youtube] Unable to download <url> to <path>",
		"[twitter][error] HttpError: '404 Not Found' for '<url>'?[0m",
		"ERROR: " + strings.Repeat("x", maxDiagnosticLength-len("ERROR: ")),
		"ERROR: 4", "ERROR: 5",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sanitizeDiagnostics() =\n%q\nwant\n%q", got, want)
	}
	for _, line := range got {
		if strings.Contains(line, "secret") || strings.Contains(line, "dQw4w9WgXcQ") || strings.Contains(line, "/data") {
			t.Fatalf("diagnostic leaked a URL, post ID, or path: %q", line)
		}
	}
	if got := sanitizeDiagnostics([]byte("WARNING: nothing\n")); got != nil {
		t.Fatalf("sanitizeDiagnostics() = %q; want none without error lines", got)
	}
}

func TestToolFailure(t *testing.T) {
	loginRequired := []byte("ERROR: [instagram] abc: login required\n")
	for name, test := range map[string]struct {
		err    error
		result Result
		detail job.ErrorDetail
	}{
		"classified exit": {ErrCommandExit, Result{ExitCode: 1, Stderr: loginRequired}, job.DetailLoginRequired},
		"killed":          {ErrCommandExit, Result{ExitCode: -1, Signal: "SIGKILL", Stderr: loginRequired}, job.DetailOutOfMemory},
		"other signal":    {ErrCommandExit, Result{ExitCode: -1, Signal: "SIGSEGV"}, job.DetailToolError},
		"output limit":    {ErrOutputLimit, Result{ExitCode: -1}, job.DetailToolError},
	} {
		t.Run(name, func(t *testing.T) {
			err := toolFailure(context.Background(), "yt-dlp", job.DetailToolError, test.result, test.err)
			var failure *Failure
			if !errors.As(err, &failure) || !errors.Is(err, ErrExtractionFailed) {
				t.Fatalf("toolFailure() = %v; want a Failure", err)
			}
			if failure.Detail != test.detail || failure.Tool != "yt-dlp" || failure.ExitCode != test.result.ExitCode ||
				failure.Signal != test.result.Signal {
				t.Fatalf("failure = %+v; want detail %s", failure, test.detail)
			}
		})
	}

	processing := toolFailure(context.Background(), "ffmpeg", job.DetailProcessingFailed, Result{ExitCode: 1, Stderr: loginRequired}, ErrCommandExit)
	var failure *Failure
	if !errors.As(processing, &failure) || failure.Detail != job.DetailProcessingFailed || failure.Diagnostics == nil {
		t.Fatalf("media tool failure = %+v; want processing_failed with diagnostics", failure)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := toolFailure(ctx, "yt-dlp", job.DetailToolError, Result{}, errors.New("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled toolFailure() = %v", err)
	}
	if err := toolFailure(context.Background(), "yt-dlp", job.DetailToolError, Result{}, context.DeadlineExceeded); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline toolFailure() = %v", err)
	}
	internal := errors.New("internal")
	if err := toolFailure(context.Background(), "yt-dlp", job.DetailToolError, Result{}, internal); !errors.Is(err, internal) {
		t.Fatalf("internal toolFailure() = %v", err)
	}
}

func TestFailureError(t *testing.T) {
	err := error(&Failure{Detail: job.DetailBlocked, Diagnostics: []string{"ERROR: private text"}})
	if err.Error() != "extractor: extraction failed: blocked" || !errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("Failure = %q; want a fixed message that wraps ErrExtractionFailed", err.Error())
	}
}

func TestExplainFailure(t *testing.T) {
	for name, test := range map[string]struct {
		err   error
		stats urlpolicy.EgressStats
		want  job.ErrorDetail
	}{
		"plain failure":              {ErrExtractionFailed, urlpolicy.EgressStats{}, job.DetailInvalidOutput},
		"tool error with rejection":  {&Failure{Detail: job.DetailToolError}, urlpolicy.EgressStats{Rejected: 1}, job.DetailEgressDenied},
		"network error with denial":  {&Failure{Detail: job.DetailNetworkError}, urlpolicy.EgressStats{Rejected: 1, UpstreamFailures: 1}, job.DetailEgressDenied},
		"tool error with failure":    {&Failure{Detail: job.DetailToolError}, urlpolicy.EgressStats{UpstreamFailures: 2}, job.DetailNetworkError},
		"specific reason is kept":    {&Failure{Detail: job.DetailLoginRequired}, urlpolicy.EgressStats{Rejected: 1}, job.DetailLoginRequired},
		"tool error without egress":  {&Failure{Detail: job.DetailToolError}, urlpolicy.EgressStats{}, job.DetailToolError},
		"invalid output with egress": {ErrExtractionFailed, urlpolicy.EgressStats{Rejected: 1}, job.DetailInvalidOutput},
	} {
		t.Run(name, func(t *testing.T) {
			var failure *Failure
			if err := explainFailure(test.err, test.stats); !errors.As(err, &failure) || failure.Detail != test.want || failure.Egress != test.stats {
				t.Fatalf("explainFailure() = %+v; want %s with egress stats", failure, test.want)
			}
		})
	}
	for _, err := range []error{nil, ErrTooLarge, context.Canceled} {
		if got := explainFailure(err, urlpolicy.EgressStats{Rejected: 1}); got != err { //nolint:errorlint // Identity is the contract.
			t.Fatalf("explainFailure(%v) = %v; want it unchanged", err, got)
		}
	}
}
