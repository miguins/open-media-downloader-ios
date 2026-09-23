package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/store"
)

type cli struct {
	env map[string]string
}

func newCLI(t *testing.T) *cli {
	t.Helper()

	return &cli{env: map[string]string{"OMDI_DATA_DIR": filepath.Join(t.TempDir(), "data")}}
}

func (c *cli) run(ctx context.Context, args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(ctx, args, &stdout, &stderr, func(name string) (string, bool) {
		value, ok := c.env[name]

		return value, ok
	})

	return code, stdout.String(), stderr.String()
}

func TestRunRejectsUsageErrors(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{
		nil, {"unknown"}, {"serve", "extra"}, {"keys"}, {"keys", "unknown"},
		{"keys", "create"}, {"keys", "create", "--name"}, {"keys", "create", "--name", "a", "extra"},
		{"keys", "list", "extra"}, {"keys", "revoke"}, {"keys", "revoke", "a", "b"},
	} {
		if code, stdout, _ := c.run(context.Background(), args...); code != 2 || stdout != "" {
			t.Fatalf("run(%q) = %d, %q; want 2 and no output", args, code, stdout)
		}
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	c := newCLI(t)
	c.env["OMDI_MAX_URL_LENGTH"] = "secret-value"
	for _, args := range [][]string{{"serve"}, {"keys", "list"}} {
		code, _, stderr := c.run(context.Background(), args...)
		if code != 1 || !strings.Contains(stderr, "invalid configuration") || strings.Contains(stderr, "secret-value") {
			t.Fatalf("run(%q) = %d, %s", args, code, stderr)
		}
	}
}

func TestRunReportsStoreFailure(t *testing.T) {
	c := newCLI(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c.env["OMDI_DATA_DIR"] = file
	for _, args := range [][]string{{"serve"}, {"keys", "list"}} {
		code, _, stderr := c.run(context.Background(), args...)
		if code != 1 || !strings.Contains(stderr, "open store") || strings.Contains(stderr, file) {
			t.Fatalf("run(%q) = %d, %s", args, code, stderr)
		}
	}
}

func TestServeRunsUntilContextIsCanceled(t *testing.T) {
	c := newCLI(t)
	c.env["OMDI_HTTP_ADDR"] = "127.0.0.1:18080"
	c.env["OMDI_MIN_FREE_BYTES"] = "0"
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		code, _, _ := c.run(ctx, "serve")
		done <- code
	}()

	client := &http.Client{Timeout: time.Second}
	var status int
	for attempt := 0; attempt < 50 && status == 0; attempt++ {
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:18080/readyz", nil)
		if response, err := client.Do(request); err == nil {
			status = response.StatusCode
			_ = response.Body.Close()
		} else {
			time.Sleep(50 * time.Millisecond)
		}
	}
	cancel()

	if status != http.StatusOK && status != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz status = %d", status)
	}
	if code := <-done; code != 0 {
		t.Fatalf("run(serve) = %d", code)
	}
}

func TestServeReportsListenFailure(t *testing.T) {
	c := newCLI(t)
	c.env["OMDI_HTTP_ADDR"] = "192.0.2.1:1"
	if code, _, stderr := c.run(context.Background(), "serve"); code != 1 || !strings.Contains(stderr, "application stopped unexpectedly") {
		t.Fatalf("run(serve) = %d, %s", code, stderr)
	}
}

func TestKeysLifecycle(t *testing.T) {
	c := newCLI(t)
	ctx := context.Background()

	code, key, stderr := c.run(ctx, "keys", "create", "--name", "phone")
	if code != 0 {
		t.Fatalf("keys create = %d, %s", code, stderr)
	}
	key = strings.TrimSuffix(key, "\n")
	keyID, _, ok := auth.Parse(key)
	if !ok {
		t.Fatal("keys create did not print a valid key")
	}
	if strings.Contains(stderr, key) {
		t.Fatal("key was written to stderr")
	}

	if code, _, _ := c.run(ctx, "keys", "create", "--name", "phone"); code != 1 {
		t.Fatalf("duplicate keys create = %d; want 1", code)
	}
	if code, _, _ := c.run(ctx, "keys", "create", "--name", "bad name"); code != 2 {
		t.Fatalf("invalid-name keys create = %d; want 2", code)
	}

	code, list, _ := c.run(ctx, "keys", "list")
	if code != 0 || !regexp.MustCompile(`(?m)^`+keyID+`\s+phone\s+\S+\s+-\s+-$`).MatchString(list) {
		t.Fatalf("keys list = %d:\n%s", code, list)
	}
	if strings.Contains(list, key[32:]) {
		t.Fatal("keys list disclosed a secret")
	}

	if code, _, stderr := c.run(ctx, "keys", "revoke", keyID); code != 0 {
		t.Fatalf("keys revoke = %d, %s", code, stderr)
	}
	code, list, _ = c.run(ctx, "keys", "list")
	if code != 0 || !regexp.MustCompile(`(?m)^`+keyID+`\s+phone\s+\S+\s+-\s+\d{4}-`).MatchString(list) {
		t.Fatalf("keys list after revoke = %d:\n%s", code, list)
	}

	for _, id := range []string{"invalid", strings.Repeat("a", 26)} {
		if code, _, stderr := c.run(ctx, "keys", "revoke", id); code != 1 || !strings.Contains(stderr, "API key not found") {
			t.Fatalf("keys revoke %s = %d, %s", id, code, stderr)
		}
	}
}

func TestJobsCommands(t *testing.T) {
	c := newCLI(t)
	ctx := context.Background()
	_, key, _ := c.run(ctx, "keys", "create", "--name", "phone")
	ownerID, _, _ := auth.Parse(strings.TrimSpace(key))

	st, err := store.Open(ctx, c.env["OMDI_DATA_DIR"])
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	queued := job.New(ownerID, "https://vimeo.com/secret-path", "vimeo", now, time.Hour)
	running := job.New(ownerID, "https://vimeo.com/2", "vimeo", now.Add(time.Second), time.Hour)
	for _, j := range []job.Job{queued, running} {
		if err := st.CreateJob(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.ClaimNextJob(ctx, now); err != nil { // claims the older, queued job
		t.Fatal(err)
	}
	_ = st.Close()
	runningID, queuedID := queued.ID, running.ID
	jobDir := filepath.Join(c.env["OMDI_DATA_DIR"], "jobs", queuedID)
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}

	code, list, _ := c.run(ctx, "jobs", "list")
	if code != 0 || !strings.Contains(list, runningID) || !strings.Contains(list, queuedID) || strings.Contains(list, "secret-path") {
		t.Fatalf("jobs list = %d:\n%s", code, list)
	}
	if code, list, _ = c.run(ctx, "jobs", "list", "--status", "running", "--owner", ownerID); code != 0 || !strings.Contains(list, runningID) || strings.Contains(list, queuedID) {
		t.Fatalf("filtered jobs list = %d:\n%s", code, list)
	}

	if code, _, stderr := c.run(ctx, "jobs", "delete", runningID); code != 1 || !strings.Contains(stderr, "job is running") {
		t.Fatalf("jobs delete running = %d, %s", code, stderr)
	}
	if code, _, stderr := c.run(ctx, "jobs", "delete", queuedID); code != 0 {
		t.Fatalf("jobs delete = %d, %s", code, stderr)
	}
	if _, err := os.Lstat(jobDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("jobs delete left files")
	}
	for _, target := range []string{queuedID, "invalid"} {
		if code, _, stderr := c.run(ctx, "jobs", "delete", target); code != 1 || !strings.Contains(stderr, "job not found") {
			t.Fatalf("jobs delete %s = %d, %s", target, code, stderr)
		}
	}

	if code, out, stderr := c.run(ctx, "jobs", "purge", "--yes", "--owner", ownerID); code != 0 || strings.TrimSpace(out) != "purged 1 jobs" {
		t.Fatalf("jobs purge = %d, %q, %s", code, out, stderr)
	}
	if code, out, _ := c.run(ctx, "jobs", "purge", "--yes"); code != 0 || strings.TrimSpace(out) != "purged 0 jobs" {
		t.Fatalf("empty jobs purge = %d, %q", code, out)
	}
}

func TestJobsUsageErrors(t *testing.T) {
	c := newCLI(t)
	for _, args := range [][]string{
		{"jobs"}, {"jobs", "unknown"}, {"jobs", "list", "extra"}, {"jobs", "list", "--status", "paused"},
		{"jobs", "list", "--owner", "bad"}, {"jobs", "delete"}, {"jobs", "delete", "a", "b"},
		{"jobs", "purge"}, {"jobs", "purge", "--yes", "--owner", "bad"}, {"jobs", "purge", "--yes", "extra"},
	} {
		if code, stdout, _ := c.run(context.Background(), args...); code != 2 || stdout != "" {
			t.Fatalf("run(%q) = %d, %q; want 2", args, code, stdout)
		}
	}
}
