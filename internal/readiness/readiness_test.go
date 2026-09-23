package readiness

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func check(name string, err error) Check {
	return Check{Name: name, Run: func(context.Context) error { return err }}
}

func TestCheckerReportsFirstFailure(t *testing.T) {
	checker := New(check("database", nil), check("storage", errors.New("full")), check("ffmpeg", errors.New("missing")))
	if failed, ok := checker.Ready(context.Background()); ok || failed != "storage" {
		t.Fatalf("Ready() = %q, %v; want storage, false", failed, ok)
	}
	if failed, ok := New(check("database", nil)).Ready(context.Background()); !ok || failed != "" {
		t.Fatalf("Ready() = %q, %v; want ready", failed, ok)
	}
}

func TestCheckerBoundsChecks(t *testing.T) {
	var deadline time.Time
	checker := New(Check{Name: "slow", Run: func(ctx context.Context) error {
		deadline, _ = ctx.Deadline()
		<-ctx.Done()

		return ctx.Err()
	}})
	checker.timeout = 10 * time.Millisecond
	if failed, ok := checker.Ready(context.Background()); ok || failed != "slow" {
		t.Fatalf("Ready() = %q, %v", failed, ok)
	}
	if deadline.IsZero() {
		t.Fatal("check ran without a deadline")
	}
	if New().timeout != defaultTimeout {
		t.Fatal("New() did not set the default timeout")
	}
}

type fakeDatabase struct{ err error }

func (f fakeDatabase) Check(context.Context) error { return f.err }

func TestDatabase(t *testing.T) {
	if c := Database(fakeDatabase{}); c.Name != "database" || c.Run(context.Background()) != nil {
		t.Fatalf("Database(healthy) = %q, %v", c.Name, c.Run(context.Background()))
	}
	if err := Database(fakeDatabase{err: errors.New("closed")}).Run(context.Background()); err == nil {
		t.Fatal("Database(failing) error = nil")
	}
}

func TestStorage(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := Storage(dir, 0).Run(ctx); err != nil {
		t.Fatalf("Storage(ok) error = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("Storage() left files behind: %v, %v", entries, err)
	}
	if err := Storage(dir, math.MaxInt64).Run(ctx); err == nil {
		t.Fatal("Storage(insufficient space) error = nil")
	}
	if Storage(dir, 0).Name != "storage" {
		t.Fatal("unexpected check name")
	}

	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(dir, "missing"), "symlink": link, "file": file} {
		if err := Storage(path, 0).Run(ctx); err == nil {
			t.Fatalf("Storage(%s) error = nil", name)
		}
	}

	failing := storage(dir, 0, func(string) (uint64, error) { return 0, errors.New("statfs failed") })
	if err := failing.Run(ctx); err == nil {
		t.Fatal("Storage(statfs failure) error = nil")
	}

	if os.Geteuid() != 0 {
		readOnly := t.TempDir()
		if err := os.Chmod(readOnly, 0o500); err != nil { //nolint:gosec // Read-only directory fixture.
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) }) //nolint:gosec // Restores the fixture for cleanup.
		if err := Storage(readOnly, 0).Run(ctx); err == nil {
			t.Fatal("Storage(read-only) error = nil")
		}
	}
}

func TestExecutable(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // Executable tool fixture.
		t.Fatal(err)
	}
	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(tool, link); err != nil {
		t.Fatal(err)
	}

	if c := Executable("yt-dlp", tool); c.Name != "yt-dlp" || c.Run(ctx) != nil {
		t.Fatalf("Executable(tool) = %q, %v", c.Name, c.Run(ctx))
	}
	if err := Executable("yt-dlp", link).Run(ctx); err != nil {
		t.Fatalf("Executable(symlink to tool) error = %v", err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(dir, "missing"), "not executable": plain, "directory": dir} {
		if err := Executable("tool", path).Run(ctx); err == nil {
			t.Fatalf("Executable(%s) error = nil", name)
		}
	}
}
