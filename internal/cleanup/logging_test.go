package cleanup

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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
	f.logs.Reset()

	return lines
}

func TestSweepLogsSummaryUnderItsOwnTrace(t *testing.T) {
	f := newFixture(t)
	ctx := logging.NewTrace(context.Background())
	f.addJob(t, now.Add(-2*time.Hour), job.StatusSucceeded)
	f.mkdir(t, "jobs", id.New())

	f.cleaner.sweep(ctx)
	f.cleaner.sweep(ctx)

	lines := f.logLines(t)
	if len(lines) != 2 {
		t.Fatalf("lines = %v; want one summary per sweep", lines)
	}
	first, second := lines[0], lines[1]
	if first["msg"] != "cleanup sweep completed" || first["level"] != "INFO" || first["expired_jobs"] != float64(1) ||
		first["orphaned_directories"] != float64(1) || first["expired_tokens"] != float64(0) {
		t.Fatalf("first sweep log = %v", first)
	}
	if second["level"] != "DEBUG" || second["expired_jobs"] != float64(0) || second["orphaned_directories"] != float64(0) {
		t.Fatalf("idle sweep log = %v; want a debug summary", second)
	}
	for _, line := range lines {
		trace, _ := line["trace_id"].(string)
		if !id.Valid(trace) || trace == logging.TraceID(ctx) {
			t.Fatalf("sweep trace = %q; want a fresh trace per sweep", trace)
		}
	}
	if first["trace_id"] == second["trace_id"] {
		t.Fatal("sweeps shared a trace")
	}
}

func TestRecoverLogsSummary(t *testing.T) {
	f := newFixture(t)
	f.addJob(t, now, job.StatusRunning)
	f.mkdir(t, "jobs", id.New())
	ctx := logging.NewTrace(context.Background())

	if err := f.cleaner.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	lines := f.logLines(t)
	var found bool
	for _, line := range lines {
		if line["trace_id"] != logging.TraceID(ctx) {
			t.Fatalf("recovery log = %v; want the caller's trace", line)
		}
		if line["msg"] == "recovery completed" {
			found = line["interrupted_jobs"] == float64(1) && line["orphaned_directories"] == float64(1)
		}
	}
	if !found {
		t.Fatalf("lines = %v; want a recovery summary", lines)
	}
}
