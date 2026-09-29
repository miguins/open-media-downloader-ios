package extractor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

func requireFailure(t *testing.T, err error, detail job.ErrorDetail, tool string) *Failure {
	t.Helper()
	var failure *Failure
	if !errors.As(err, &failure) || failure.Detail != detail || failure.Tool != tool {
		t.Fatalf("error = %v (%+v); want %s from %s", err, failure, detail, tool)
	}

	return failure
}

func TestRunnerReportsTerminatingSignal(t *testing.T) {
	for signal, want := range map[string]string{
		"KILL": "SIGKILL", "TERM": "SIGTERM", "SEGV": "SIGSEGV", "ABRT": "SIGABRT", "USR1": "signal 10",
	} {
		result, err := NewRunner().Run(t.Context(), Command{Path: testTool(t, "kill -"+signal+" $$"), Dir: t.TempDir(), StdoutLimit: 64, StderrLimit: 64})
		if !errors.Is(err, ErrCommandExit) || result.Signal != want || result.ExitCode != -1 {
			t.Fatalf("kill -%s: result = %+v, error = %v; want signal %q", signal, result, err, want)
		}
	}
	result, err := NewRunner().Run(t.Context(), Command{Path: testTool(t, "exit 3"), Dir: t.TempDir(), StdoutLimit: 64, StderrLimit: 64})
	if !errors.Is(err, ErrCommandExit) || result.Signal != "" || result.ExitCode != 3 {
		t.Fatalf("exit 3: result = %+v, error = %v; want no signal", result, err)
	}
}

func TestYTDLPReportsFailureDetails(t *testing.T) {
	media := NewMediaTools(probeTool(t, probeJSON("mp4", "h264", "aac")), "/bin/true", NewRunner())
	blocked := NewYTDLP(testTool(t, `printf '%s\n' "ERROR: [youtube] abc123: Sign in to confirm you're not a bot. See https://example.invalid/help" >&2; exit 1`), "/bin/true", NewRunner(), media)
	_, err := blocked.Extract(t.Context(), adapterRequest(t.TempDir(), "youtube", 1), "http://127.0.0.1:8080")
	failure := requireFailure(t, err, job.DetailBlocked, "yt-dlp")
	if failure.ExitCode != 1 || len(failure.Diagnostics) != 1 || strings.Contains(failure.Diagnostics[0], "abc123") ||
		strings.Contains(failure.Diagnostics[0], "example.invalid") {
		t.Fatalf("failure = %+v; want exit 1 and one sanitized diagnostic", failure)
	}

	killed := NewYTDLP(testTool(t, "kill -KILL $$"), "/bin/true", NewRunner(), media)
	_, err = killed.Extract(t.Context(), adapterRequest(t.TempDir(), "youtube", 1), "http://127.0.0.1:8080")
	if failure := requireFailure(t, err, job.DetailOutOfMemory, "yt-dlp"); failure.Signal != "SIGKILL" {
		t.Fatalf("failure = %+v; want SIGKILL", failure)
	}
}

func TestGalleryDLReportsFailureDetails(t *testing.T) {
	media := NewMediaTools(probeTool(t, probeJSON("mp4", "h264", "aac")), "/bin/true", NewRunner())
	g := NewGalleryDL(testTool(t, `printf '%s\n' '[twitter][error] AuthorizationError: Login required' >&2; exit 4`), NewRunner(), media)
	_, err := g.Extract(t.Context(), adapterRequest(t.TempDir(), "x", 1), "http://127.0.0.1:8080")
	if failure := requireFailure(t, err, job.DetailLoginRequired, "gallery-dl"); failure.ExitCode != 4 {
		t.Fatalf("failure = %+v; want exit 4", failure)
	}
}

func TestMediaToolFailuresAreProcessingFailures(t *testing.T) {
	dir := t.TempDir()
	regular(t, dir, "media.mp4")
	probe := NewMediaTools(testTool(t, `printf '%s\n' 'error: invalid data' >&2; exit 1`), "/bin/true", NewRunner())
	_, err := probe.Finalize(t.Context(), dir, []string{"media.mp4"})
	_ = requireFailure(t, err, job.DetailProcessingFailed, "ffprobe")

	regular(t, dir, "media.ts")
	remux := NewMediaTools(probeTool(t, probeJSON("mpegts", "h264", "aac")), testTool(t, "exit 1"), NewRunner())
	_, err = remux.Finalize(t.Context(), dir, []string{"media.ts"})
	_ = requireFailure(t, err, job.DetailProcessingFailed, "ffmpeg")
}

func TestRealExplainsFailuresWithEgressStats(t *testing.T) {
	stats := urlpolicy.EgressStats{Rejected: 2}
	r := &Real{
		beginSession: func(context.Context, int64) (proxySession, error) { return scriptedSession{stats: stats}, nil },
		ytdlp: func(context.Context, Request, string) ([]File, error) {
			return nil, &Failure{Detail: job.DetailToolError, Tool: "yt-dlp"}
		},
	}
	_, err := r.Extract(t.Context(), adapterRequest(t.TempDir(), "youtube", 1))
	if failure := requireFailure(t, err, job.DetailEgressDenied, "yt-dlp"); failure.Egress != stats {
		t.Fatalf("failure = %+v; want the session stats", failure)
	}
}
