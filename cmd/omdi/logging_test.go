package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/id"
)

func logLines(t *testing.T, stderr string) []map[string]any {
	t.Helper()
	var lines []map[string]any
	decoder := json.NewDecoder(strings.NewReader(stderr))
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

// assertOneTrace checks that every line of one command invocation shares a valid trace.
func assertOneTrace(t *testing.T, lines []map[string]any) string {
	t.Helper()
	trace, _ := lines[0]["trace_id"].(string)
	if !id.Valid(trace) {
		t.Fatalf("trace_id = %q; want a valid identifier", trace)
	}
	for _, line := range lines {
		if line["trace_id"] != trace {
			t.Fatalf("log line = %v; want trace %q", line, trace)
		}
	}

	return trace
}

func TestServeLogsStartupUnderOneTrace(t *testing.T) {
	plaintext, key := auth.Generate("phone", time.Now().UTC())
	c := newCLI(t)
	c.env["OMDI_RESTORE_API_KEY_ON_STARTUP"] = "true"
	c.env["OMDI_API_KEY"] = plaintext
	c.env["OMDI_HTTP_ADDR"] = "192.0.2.1:1"
	c.env["OMDI_PUBLIC_URL"] = "https://omdi.example.com"

	var traces []string
	for attempt, message := range []string{"API key restored", "API key already present"} {
		_, _, stderr := c.run(context.Background(), "serve")
		if strings.Contains(stderr, plaintext[32:]) {
			t.Fatal("startup logs disclosed the API key secret")
		}
		lines := logLines(t, stderr)
		restored := findLog(t, lines, message)
		if restored["key_id"] != key.ID || restored["key_name"] != "startup-"+key.ID || restored["revoked"] != false {
			t.Fatalf("attempt %d restoration log = %v", attempt, restored)
		}
		starting := findLog(t, lines, "starting server")
		platforms, _ := starting["allowed_platforms"].([]any)
		if starting["http_addr"] != "192.0.2.1:1" || starting["public_url"] != "https://omdi.example.com" ||
			len(platforms) != 6 || starting["log_level"] != "INFO" || starting["job_timeout"] != "10m0s" ||
			starting["max_queued_jobs"] != float64(10) {
			t.Fatalf("starting server log = %v", starting)
		}
		// Startup lines share the invocation trace; worker and cleanup passes have their own.
		traces = append(traces, assertOneTrace(t, []map[string]any{restored, starting}))
	}
	if traces[0] == traces[1] {
		t.Fatal("separate invocations shared a trace")
	}
}

func TestServeWarnsWhenRestoredKeyIsRevoked(t *testing.T) {
	plaintext, _ := auth.Generate("phone", time.Now().UTC())
	c := newCLI(t)
	c.env["OMDI_RESTORE_API_KEY_ON_STARTUP"] = "true"
	c.env["OMDI_API_KEY"] = plaintext
	c.env["OMDI_HTTP_ADDR"] = "192.0.2.1:1"
	c.run(context.Background(), "serve")
	keyID, _, _ := auth.Parse(plaintext)
	if code, _, _ := c.run(context.Background(), "keys", "revoke", keyID); code != 0 {
		t.Fatalf("revoke exit = %d", code)
	}

	_, _, stderr := c.run(context.Background(), "serve")
	present := findLog(t, logLines(t, stderr), "API key already present")
	if present["revoked"] != true || present["level"] != "WARN" {
		t.Fatalf("restoration log = %v; want a revoked warning", present)
	}
}

func TestLogLevelFiltersCommandLogs(t *testing.T) {
	c := newCLI(t)
	c.env["OMDI_HTTP_ADDR"] = "192.0.2.1:1"
	c.env["OMDI_LOG_LEVEL"] = "error"
	_, _, stderr := c.run(context.Background(), "serve")
	for _, line := range logLines(t, stderr) {
		if line["level"] != "ERROR" {
			t.Fatalf("log line = %v; want only errors", line)
		}
	}
}

func TestCommandLogsHaveATrace(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{{"keys", "create", "--name", "phone"}, {"unknown"}, {"jobs", "delete", id.New()}} {
		_, _, stderr := c.run(context.Background(), args...)
		assertOneTrace(t, logLines(t, stderr))
	}
}
