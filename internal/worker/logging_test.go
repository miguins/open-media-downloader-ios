package worker

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/logging"
)

func (f *fixture) logLines(t *testing.T) []map[string]any {
	t.Helper()
	var lines []map[string]any
	decoder := json.NewDecoder(strings.NewReader(f.logs.String()))
	for decoder.More() {
		var line map[string]any
		if err := decoder.Decode(&line); err != nil {
			t.Fatalf("decode log line: %v", err)
		}
		lines = append(lines, line)
	}

	return lines
}

func findLog(t *testing.T, lines []map[string]any, message string) map[string]any {
	t.Helper()
	for _, line := range lines {
		if line["msg"] == message {
			return line
		}
	}
	t.Fatalf("no %q log in %v", message, lines)

	return nil
}

// assertJobScope checks that every line shares one fresh trace and identifies the job and its key.
func assertJobScope(t *testing.T, lines []map[string]any, j job.Job, parentTrace string) {
	t.Helper()
	trace, _ := lines[0]["trace_id"].(string)
	if !id.Valid(trace) || trace == parentTrace {
		t.Fatalf("trace_id = %q; want a fresh trace per job", trace)
	}
	for _, line := range lines {
		if line["trace_id"] != trace || line["job_id"] != j.ID || line["platform"] != j.Platform ||
			line["key_id"] != j.OwnerID || line["key_name"] != "owner" {
			t.Fatalf("log line = %v; want the job scope", line)
		}
	}
}

func TestJobLogsShareATrace(t *testing.T) {
	f := newFixture(t, writeFile("out.mp4", "video/mp4", "media"), defaultSettings())
	j := f.enqueue(t)
	parent := logging.NewTrace(context.Background())

	f.worker.runOnce(parent)

	lines := f.logLines(t)
	assertJobScope(t, lines, j, logging.TraceID(parent))
	findLog(t, lines, "job started")
	succeeded := findLog(t, lines, "job succeeded")
	if succeeded["items"] != float64(1) || succeeded["bytes"] != float64(5) {
		t.Fatalf("job succeeded log = %v", succeeded)
	}
	if _, ok := succeeded["duration_ms"].(float64); !ok {
		t.Fatalf("job succeeded log = %v; want duration_ms", succeeded)
	}
	if strings.Contains(f.logs.String(), "vimeo.com") || strings.Contains(f.logs.String(), f.dataDir) {
		t.Fatalf("logs disclosed the source URL or an internal path: %s", f.logs.String())
	}
}

func TestFailedJobLogsDuration(t *testing.T) {
	fail := extractFunc(func(context.Context, extractor.Request) ([]extractor.File, error) {
		return nil, extractor.ErrExtractionFailed
	})
	f := newFixture(t, fail, defaultSettings())
	j := f.enqueue(t)

	f.worker.runOnce(context.Background())

	lines := f.logLines(t)
	assertJobScope(t, lines, j, "")
	failed := findLog(t, lines, "job failed")
	if failed["error_code"] != "extraction_failed" || failed["level"] != "WARN" {
		t.Fatalf("job failed log = %v", failed)
	}
	if _, ok := failed["duration_ms"].(float64); !ok {
		t.Fatalf("job failed log = %v; want duration_ms", failed)
	}
}

func TestCanceledJobIsLogged(t *testing.T) {
	started := make(chan struct{})
	f := newFixture(t, blockingExtractor(started), defaultSettings())
	j := f.enqueue(t)
	done := make(chan struct{})
	go func() {
		f.worker.runOnce(context.Background())
		close(done)
	}()
	<-started
	running := f.job(t, j.ID)
	if err := running.Transition(job.StatusCanceled, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateJobStatus(context.Background(), running, job.StatusRunning); err != nil {
		t.Fatal(err)
	}
	<-done

	lines := f.logLines(t)
	assertJobScope(t, lines, j, "")
	findLog(t, lines, "job canceled while running")
}

func TestJobLogsWithoutKeyName(t *testing.T) {
	f := newFixture(t, writeFile("out.mp4", "video/mp4", "media"), defaultSettings())
	f.enqueue(t)
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.worker.runOnce(context.Background())
	for _, line := range f.logLines(t) {
		if _, ok := line["trace_id"].(string); !ok {
			t.Fatalf("log line = %v; want a trace even when the store fails", line)
		}
	}
}

func TestShutdownDoesNotLogClaimFailure(t *testing.T) {
	f := newFixture(t, writeFile("out.mp4", "video/mp4", "media"), defaultSettings())
	f.enqueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if f.worker.runOnce(ctx) {
		t.Fatal("runOnce() = true with a canceled context")
	}
	if got := f.logs.String(); got != "" {
		t.Fatalf("logs = %s; want no claim failure during shutdown", got)
	}
}
