// Package readiness verifies that the service's runtime dependencies are usable.
package readiness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

const defaultTimeout = 2 * time.Second

// Check is one named dependency check. Errors are for internal use and are never shown to clients.
type Check struct {
	Name string
	Run  func(context.Context) error
}

// Checker runs checks in order under a shared timeout.
type Checker struct {
	checks  []Check
	timeout time.Duration
}

// New returns a Checker for checks.
func New(checks ...Check) *Checker {
	return &Checker{checks: checks, timeout: defaultTimeout}
}

// Ready runs every check until one fails and returns the failing check's name.
func (c *Checker) Ready(ctx context.Context) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	for _, check := range c.checks {
		if err := check.Run(ctx); err != nil {
			return check.Name, false
		}
	}

	return "", true
}

// Database checks that the database answers queries with the expected schema.
func Database(db interface{ Check(context.Context) error }) Check {
	return Check{Name: "database", Run: db.Check}
}

// Storage checks that dir is a real, writable directory with at least minFree bytes available.
func Storage(dir string, minFree int64) Check {
	return storage(dir, minFree, availableBytes)
}

func storage(dir string, minFree int64, free func(string) (uint64, error)) Check {
	return Check{Name: "storage", Run: func(context.Context) error {
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return errors.New("readiness: storage is not a real directory")
		}
		probe, err := os.CreateTemp(dir, ".readiness-*")
		if err != nil {
			return err
		}
		if err := errors.Join(probe.Close(), os.Remove(probe.Name())); err != nil {
			return err
		}
		available, err := free(dir)
		if err != nil {
			return err
		}
		if minFree > 0 && available < uint64(minFree) {
			return errors.New("readiness: insufficient free space")
		}

		return nil
	}}
}

func availableBytes(dir string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, fmt.Errorf("readiness: statfs: %w", err)
	}

	return stat.Bavail * uint64(stat.Bsize), nil //nolint:gosec // Block size is always positive.
}

// Executable checks that path is a regular file with an execute permission bit.
func Executable(name, path string) Check {
	return Check{Name: name, Run: func(context.Context) error {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return errors.New("readiness: tool is not an executable file")
		}

		return nil
	}}
}
