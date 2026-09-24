package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func newTestRunner() *Runner {
	runner := NewRunner()
	runner.grace = 500 * time.Millisecond
	runner.extraEnv = []string{"GO_WANT_EXTRACTOR_HELPER=1"}
	return runner
}

func helperCommand(t *testing.T, args ...string) Command {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Command{
		Path: path, Args: append([]string{"-test.run=^TestExtractorHelperProcess$", "--"}, args...),
		Dir: t.TempDir(), StdoutLimit: 16 << 10, StderrLimit: 16 << 10,
	}
}

func assertRuntimeRemoved(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, ".omdi-runtime")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime directory remains: %v", err)
	}
}

func TestRunnerRejectsInvalidCommands(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*Command)
	}{
		{"relative executable", func(c *Command) { c.Path = "tool" }},
		{"empty executable", func(c *Command) { c.Path = "" }},
		{"unclean executable", func(c *Command) { c.Path = "/usr/bin/../bin/tool" }},
		{"empty work directory", func(c *Command) { c.Dir = "" }},
		{"relative work directory", func(c *Command) { c.Dir = "work" }},
		{"unclean work directory", func(c *Command) { c.Dir += "/../work" }},
		{"zero stdout", func(c *Command) { c.StdoutLimit = 0 }},
		{"negative stdout", func(c *Command) { c.StdoutLimit = -1 }},
		{"zero stderr", func(c *Command) { c.StderrLimit = 0 }},
		{"negative stderr", func(c *Command) { c.StderrLimit = -1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := helperCommand(t, "inspect")
			test.edit(&command)
			result, err := newTestRunner().Run(context.Background(), command)
			if err == nil || result.ExitCode != -1 || len(result.Stdout)+len(result.Stderr) != 0 {
				t.Fatalf("invalid Run() = %#v, %v", result, err)
			}
		})
	}
}

func TestRunnerRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := helperCommand(t, "inspect")
	result, err := newTestRunner().Run(ctx, command)
	if !errors.Is(err, context.Canceled) || result.ExitCode != -1 {
		t.Fatalf("Run(canceled) = %#v, %v", result, err)
	}
	assertRuntimeRemoved(t, command.Dir)
}

func TestRunnerSuccessIsolatesExecution(t *testing.T) {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY", "http_proxy", "API_SECRET", "ARBITRARY_VARIABLE"} {
		t.Setenv(name, "private-parent-value")
	}
	args := []string{"spaces stay together", "'quote'", "$(touch forbidden)", "a;b", "", "https://example.invalid/?token=synthetic&x=1", "--flag=value"}
	command := helperCommand(t, append([]string{"inspect"}, args...)...)
	result, err := newTestRunner().Run(context.Background(), command)
	if err != nil || result.ExitCode != 0 || len(result.Stderr) != 0 {
		t.Fatalf("Run() = %#v, %v", result, err)
	}
	var got helperInspection
	if err := json.Unmarshal(result.Stdout, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Args, args) || got.Dir != command.Dir || got.Input != "" {
		t.Fatalf("execution = %#v", got)
	}
	wantEnv := []string{
		"HOME=" + command.Dir + "/.omdi-runtime/home",
		"XDG_CACHE_HOME=" + command.Dir + "/.omdi-runtime/cache",
		"TMPDIR=" + command.Dir + "/.omdi-runtime/tmp",
		"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "PATH=/usr/bin:/bin", "GO_WANT_EXTRACTOR_HELPER=1",
	}
	slices.Sort(got.Env)
	slices.Sort(wantEnv)
	if !reflect.DeepEqual(got.Env, wantEnv) {
		t.Fatalf("environment = %v, want %v", got.Env, wantEnv)
	}
	for name, mode := range got.Modes {
		if mode != 0o700 {
			t.Errorf("runtime directory %q mode = %o", name, mode)
		}
	}
	assertRuntimeRemoved(t, command.Dir)
}

func TestRunnerRejectsExistingRuntimeWithoutFollowingSymlinks(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		command := helperCommand(t, "inspect")
		runtimeDir := filepath.Join(command.Dir, ".omdi-runtime")
		if symlink {
			if err := os.Symlink(t.TempDir(), runtimeDir); err != nil {
				t.Fatal(err)
			}
		} else if err := os.Mkdir(runtimeDir, 0o700); err != nil {
			t.Fatal(err)
		}
		result, err := newTestRunner().Run(context.Background(), command)
		if err == nil || result.ExitCode != -1 {
			t.Fatalf("Run(existing runtime) = %#v, %v", result, err)
		}
		if _, err := os.Lstat(runtimeDir); err != nil {
			t.Fatalf("preexisting runtime entry was removed: %v", err)
		}
	}
}

func TestRunnerClassifiesExitWithoutDiagnostics(t *testing.T) {
	const diagnostic = "https://example.invalid/media?token=synthetic /private/synthetic/file.mp4"
	command := helperCommand(t, "exit", "7", diagnostic)
	result, err := newTestRunner().Run(context.Background(), command)
	if !errors.Is(err, ErrCommandExit) || result.ExitCode != 7 || string(result.Stderr) != diagnostic {
		t.Fatalf("Run(exit 7) = %#v, %v", result, err)
	}
	assertSafeRunnerError(t, err)
	assertRuntimeRemoved(t, command.Dir)
}

func assertSafeRunnerError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected sanitized error")
	}
	for _, secret := range []string{"https://", "synthetic", "/private/", "token=", "private-parent-value"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}

func TestRunnerLaunchFailureIsInternal(t *testing.T) {
	command := helperCommand(t, "inspect")
	command.Path = filepath.Join(command.Dir, "synthetic-missing-tool")
	result, err := newTestRunner().Run(context.Background(), command)
	if err == nil || errors.Is(err, ErrCommandExit) || errors.Is(err, ErrOutputLimit) || result.ExitCode != -1 {
		t.Fatalf("Run(missing executable) = %#v, %v", result, err)
	}
	assertSafeRunnerError(t, err)
	if strings.Contains(err.Error(), command.Path) || strings.Contains(err.Error(), command.Dir) {
		t.Fatalf("error contains command paths: %v", err)
	}
	assertRuntimeRemoved(t, command.Dir)
}

func TestRunnerCapsOutput(t *testing.T) {
	for _, test := range []struct {
		name           string
		stdout, stderr int
		wantErr        bool
	}{
		{"empty", 0, 0, false},
		{"exact limits", 32, 48, false},
		{"stdout one over", 33, 0, true},
		{"stderr one over", 0, 49, true},
		{"stdout huge", 1 << 20, 0, true},
		{"stderr huge", 0, 1 << 20, true},
		{"both huge", 1 << 20, 1 << 20, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := helperCommand(t, "emit", strconv.Itoa(test.stdout), strconv.Itoa(test.stderr))
			command.StdoutLimit, command.StderrLimit = 32, 48
			result, err := newTestRunner().Run(context.Background(), command)
			if errors.Is(err, ErrOutputLimit) != test.wantErr || (!test.wantErr && (err != nil || result.ExitCode != 0)) {
				t.Fatalf("Run(emit) exit = %d, error = %v, want overflow %v", result.ExitCode, err, test.wantErr)
			}
			if len(result.Stdout) > 32 || len(result.Stderr) > 48 {
				t.Fatalf("unbounded capture: stdout=%d stderr=%d", len(result.Stdout), len(result.Stderr))
			}
			if !test.wantErr && (len(result.Stdout) != test.stdout || len(result.Stderr) != test.stderr) {
				t.Fatalf("truncated valid output: stdout=%d stderr=%d", len(result.Stdout), len(result.Stderr))
			}
			if test.wantErr {
				assertSafeRunnerError(t, err)
			}
			assertRuntimeRemoved(t, command.Dir)
		})
	}
}

func TestRunnerCancelsWholeProcessTree(t *testing.T) {
	for _, reason := range []string{"cancel", "deadline", "overflow", "resist"} {
		t.Run(reason, func(t *testing.T) {
			dir := t.TempDir()
			parentFile, childFile := filepath.Join(dir, "parent.pid"), filepath.Join(dir, "child.pid")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if reason == "deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, time.Second)
				defer deadlineCancel()
			}
			runner := newTestRunner()
			if reason == "resist" {
				runner.grace = 100 * time.Millisecond
			}
			command := helperCommand(t, "tree", parentFile, childFile, reason)
			command.StdoutLimit = 64
			type outcome struct {
				result Result
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := runner.Run(ctx, command)
				done <- outcome{result, err}
			}()
			parent := waitHelperPID(t, parentFile)
			child := waitHelperPID(t, childFile)
			t.Cleanup(func() {
				_ = syscall.Kill(child, syscall.SIGKILL)
				_ = syscall.Kill(parent, syscall.SIGKILL)
			})
			start := time.Now()
			wantErr := context.Canceled
			switch reason {
			case "cancel", "resist":
				cancel()
			case "deadline":
				<-ctx.Done()
				start = time.Now()
				wantErr = context.DeadlineExceeded
			case "overflow":
				wantErr = ErrOutputLimit
			}
			select {
			case got := <-done:
				if !errors.Is(got.err, wantErr) || time.Since(start) > 500*time.Millisecond {
					t.Fatalf("Run(%s) error = %v, elapsed = %v", reason, got.err, time.Since(start))
				}
				if reason == "overflow" && len(got.result.Stdout) != 64 {
					t.Fatalf("overflow prefix length = %d", len(got.result.Stdout))
				}
			case <-time.After(750 * time.Millisecond):
				t.Fatal("runner did not finish after cancellation")
			}
			for _, pid := range []int{parent, child} {
				if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
					t.Errorf("process %d remains after runner returned: %v", pid, err)
				}
			}
			assertRuntimeRemoved(t, command.Dir)
		})
	}
}

func waitHelperPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path) //nolint:gosec // Test-owned PID file in a private temporary directory.
		if err == nil {
			pid, err := strconv.Atoi(string(data))
			if err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("helper did not publish its process ID")
	return 0
}

func TestRunnerInternalErrorsDoNotBecomeExtractionFailures(t *testing.T) {
	for _, setup := range []func(*Command){
		func(c *Command) { c.Path = filepath.Join(c.Dir, "synthetic-missing-tool") },
		func(c *Command) { c.Dir = filepath.Join(c.Dir, "synthetic-missing-directory") },
	} {
		command := helperCommand(t, "inspect")
		setup(&command)
		result, err := newTestRunner().Run(context.Background(), command)
		if err == nil || result.ExitCode != -1 || errors.Is(err, ErrTooLarge) || errors.Is(err, ErrExtractionFailed) {
			t.Fatalf("internal failure = %#v, %v", result, err)
		}
		assertSafeRunnerError(t, err)
	}
}

func TestRunnerCleanupFailureIsInternalAndSanitized(t *testing.T) {
	command := helperCommand(t, "lock-runtime")
	runtimeDir := filepath.Join(command.Dir, ".omdi-runtime")
	t.Cleanup(func() { _ = os.Chmod(runtimeDir, 0o700) }) //nolint:gosec // Restores the directory fixture for cleanup.
	result, err := newTestRunner().Run(context.Background(), command)
	if err == nil || result.ExitCode != 0 || errors.Is(err, ErrExtractionFailed) || errors.Is(err, ErrCommandExit) {
		t.Fatalf("Run(cleanup failure) = %#v, %v", result, err)
	}
	assertSafeRunnerError(t, err)
	if strings.Contains(err.Error(), command.Dir) {
		t.Fatalf("cleanup error leaked work directory: %v", err)
	}
}

func TestRunnerSignalErrorsAreSanitized(t *testing.T) {
	if err := terminateProcessGroup(1<<30, time.Millisecond); err != nil {
		t.Fatalf("absent process group = %v", err)
	}
	for _, err := range []error{syscall.EPERM, errors.New("https://example.invalid/synthetic /private/file")} {
		assertSafeRunnerError(t, sanitizeSignalError(err))
	}
}

func TestRunnerCancelsDescendantAfterLeaderExits(t *testing.T) {
	// Adopt the deliberately orphaned fixture here so the runner must reap it.
	// The assertion precedes every test-side reap; cleanup is only a failure guard.
	const prSetChildSubreaper = 36
	if _, _, errno := syscall.Syscall6(syscall.SYS_PRCTL, prSetChildSubreaper, 1, 0, 0, 0, 0); errno != 0 {
		t.Fatal(errno)
	}
	t.Cleanup(func() {
		_, _, _ = syscall.Syscall6(syscall.SYS_PRCTL, prSetChildSubreaper, 0, 0, 0, 0, 0)
	})
	dir := t.TempDir()
	parentFile, childFile := filepath.Join(dir, "parent.pid"), filepath.Join(dir, "child.pid")
	command := helperCommand(t, "tree", parentFile, childFile, "orphan")
	runner := newTestRunner()
	runner.grace = 100 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, command)
		done <- err
	}()
	parent, child := waitHelperPID(t, parentFile), waitHelperPID(t, childFile)
	t.Cleanup(func() {
		_ = syscall.Kill(child, syscall.SIGKILL)
		_, _ = syscall.Wait4(child, nil, 0, nil)
	})
	deadline := time.Now().Add(time.Second)
	for !errors.Is(syscall.Kill(parent, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			t.Fatal("leader did not exit")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run(orphan cancellation) = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("orphaned descendant held runner open")
	}
	if err := syscall.Kill(child, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("descendant remains at Run return, before test reaping: %v", err)
	}
	assertRuntimeRemoved(t, command.Dir)
}

func TestRunnerChecksCancellationImmediatelyBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := &exec.Cmd{Path: "/synthetic/nonexistent"}
	if err := runProcess(ctx, cmd, time.Millisecond); !errors.Is(err, context.Canceled) || cmd.Process != nil {
		t.Fatalf("runProcess(canceled) = %v, process = %v", err, cmd.Process)
	}
}

func TestRunnerRuntimeSetupFailureCleansPartialDirectories(t *testing.T) {
	command := helperCommand(t, "inspect")
	mkdir := func(name string, mode os.FileMode) error {
		if filepath.Base(name) == "cache" {
			return &os.PathError{Op: "mkdir", Path: "/private/synthetic", Err: syscall.ENOSPC}
		}
		return os.Mkdir(name, mode)
	}
	result, err := newTestRunner().run(context.Background(), command, mkdir)
	if err == nil || result.ExitCode != -1 || errors.Is(err, ErrExtractionFailed) || errors.Is(err, ErrTooLarge) {
		t.Fatalf("Run(runtime setup failure) = %#v, %v", result, err)
	}
	assertSafeRunnerError(t, err)
	assertRuntimeRemoved(t, command.Dir)
}

func TestRunnerProcessGroupConfirmationIsBounded(t *testing.T) {
	command := helperCommand(t, "sleep", filepath.Join(t.TempDir(), "helper.pid"))
	cmd := exec.CommandContext(context.Background(), command.Path, command.Args...) //nolint:gosec // Test-only helper path and arguments.
	cmd.Env = []string{"GO_WANT_EXTRACTOR_HELPER=1"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = cmd.Wait()
	})
	start := time.Now()
	err := confirmProcessGroupExit(cmd.Process.Pid, 5*time.Millisecond)
	if !errors.Is(err, errProcessCleanup) || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("confirmation error = %v, elapsed = %v", err, time.Since(start))
	}
	assertSafeRunnerError(t, err)
}

func TestRunnerIncompleteCleanupIsInternalEvenWhenCanceledOrOverflowed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := commandError(ctx, errProcessCleanup, true)
	if !errors.Is(err, errProcessCleanup) || errors.Is(err, context.Canceled) || errors.Is(err, ErrOutputLimit) || errors.Is(err, ErrExtractionFailed) {
		t.Fatalf("incomplete cleanup classification = %v", err)
	}
	assertSafeRunnerError(t, err)
}
