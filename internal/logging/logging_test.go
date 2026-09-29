package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/id"
)

func decode(t *testing.T, buffer *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	decoder := json.NewDecoder(buffer)
	for decoder.More() {
		var line map[string]any
		if err := decoder.Decode(&line); err != nil {
			t.Fatalf("decode log line: %v", err)
		}
		lines = append(lines, line)
	}

	return lines
}

func TestHandlerAddsScopeAttributes(t *testing.T) {
	var buffer bytes.Buffer
	logger := New(&buffer, slog.LevelInfo)

	ctx := With(context.Background(), "key_id", "k1")
	Add(ctx, "key_name", "phone")
	logger.InfoContext(ctx, "event", "items", 2)
	logger.InfoContext(context.Background(), "plain")

	lines := decode(t, &buffer)
	if len(lines) != 2 {
		t.Fatalf("lines = %d; want 2", len(lines))
	}
	if lines[0]["key_id"] != "k1" || lines[0]["key_name"] != "phone" || lines[0]["items"] != float64(2) {
		t.Fatalf("scoped line = %v", lines[0])
	}
	if _, ok := lines[1]["key_id"]; ok {
		t.Fatalf("unscoped line = %v; want no scope attributes", lines[1])
	}
}

func TestAddIsVisibleThroughAncestorContexts(t *testing.T) {
	outer := NewTrace(context.Background())
	inner, cancel := context.WithCancel(outer)
	defer cancel()
	Add(inner, "job_id", "j1")

	var buffer bytes.Buffer
	New(&buffer, slog.LevelInfo).InfoContext(outer, "done")
	if line := decode(t, &buffer)[0]; line["job_id"] != "j1" {
		t.Fatalf("line = %v; want job_id added through a descendant context", line)
	}
}

func TestWithCopiesParentAndReplacesKeys(t *testing.T) {
	parent := With(context.Background(), TraceKey, "parent", "key_id", "k1")
	child := With(parent, TraceKey, "child")
	Add(child, "key_id", "k2")

	if got := TraceID(parent); got != "parent" {
		t.Fatalf("parent trace = %q; want unchanged", got)
	}
	if got := TraceID(child); got != "child" {
		t.Fatalf("child trace = %q; want child", got)
	}

	var buffer bytes.Buffer
	logger := New(&buffer, slog.LevelInfo)
	logger.InfoContext(parent, "parent")
	logger.InfoContext(child, "child")
	lines := decode(t, &buffer)
	if lines[0]["key_id"] != "k1" || lines[1]["key_id"] != "k2" {
		t.Fatalf("lines = %v; want the child to replace key_id without affecting the parent", lines)
	}
}

func TestAddWithoutScopeIsIgnored(t *testing.T) {
	ctx := context.Background()
	Add(ctx, "key_id", "k1")
	if got := TraceID(ctx); got != "" {
		t.Fatalf("TraceID() = %q; want empty", got)
	}
}

func TestNewTraceAssignsValidIdentifier(t *testing.T) {
	first := TraceID(NewTrace(context.Background()))
	second := TraceID(NewTrace(context.Background()))
	if !id.Valid(first) || !id.Valid(second) || first == second {
		t.Fatalf("traces = %q, %q; want distinct valid identifiers", first, second)
	}
}

func TestTraceIDIgnoresNonStringValues(t *testing.T) {
	if got := TraceID(With(context.Background(), TraceKey, 7)); got != "" {
		t.Fatalf("TraceID() = %q; want empty for a non-string value", got)
	}
}

func TestLevelFiltersRecords(t *testing.T) {
	var buffer bytes.Buffer
	level := new(slog.LevelVar)
	level.Set(slog.LevelWarn)
	logger := New(&buffer, level)
	logger.Info("hidden")
	logger.Warn("shown")
	if lines := decode(t, &buffer); len(lines) != 1 || lines[0]["msg"] != "shown" {
		t.Fatalf("lines = %v; want only the warning", lines)
	}
}

func TestHandlerKeepsScopeWithAttrsAndGroups(t *testing.T) {
	var buffer bytes.Buffer
	logger := New(&buffer, slog.LevelInfo).With("component", "worker").WithGroup("detail")
	logger.InfoContext(With(context.Background(), "job_id", "j1"), "event", "items", 1)

	line := decode(t, &buffer)[0]
	if line["component"] != "worker" {
		t.Fatalf("line = %v; want logger attributes", line)
	}
	detail, ok := line["detail"].(map[string]any)
	if !ok || detail["items"] != float64(1) || detail["job_id"] != "j1" {
		t.Fatalf("line = %v; want grouped record and scope attributes", line)
	}
}
