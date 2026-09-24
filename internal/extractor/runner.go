package extractor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Stable runner errors never contain captured tool diagnostics.
var (
	ErrCommandExit = errors.New("extractor: command exited unsuccessfully")
	ErrOutputLimit = errors.New("extractor: command output limit exceeded")
)

// Command describes a shell-free invocation in a private job work directory.
type Command struct {
	Path        string
	Args        []string
	Dir         string
	StdoutLimit int64
	StderrLimit int64
}

// Result contains bounded, untrusted tool output. It must never be logged.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner executes tools with an isolated environment and bounded output.
type Runner struct {
	grace    time.Duration
	extraEnv []string // Set only by the test factory; nil in production.
}

// NewRunner creates a runner with a short process termination grace period.
func NewRunner() *Runner {
	return &Runner{grace: 500 * time.Millisecond}
}

// Run executes a command without exposing process diagnostics in errors.
func (r *Runner) Run(ctx context.Context, command Command) (Result, error) {
	return r.run(ctx, command, os.Mkdir)
}

func (r *Runner) run(ctx context.Context, command Command, mkdir func(string, os.FileMode) error) (result Result, err error) {
	result.ExitCode = -1
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !filepath.IsAbs(command.Path) || filepath.Clean(command.Path) != command.Path ||
		!filepath.IsAbs(command.Dir) || filepath.Clean(command.Dir) != command.Dir ||
		command.StdoutLimit <= 0 || command.StderrLimit <= 0 {
		return result, errors.New("extractor: invalid command")
	}
	runtimeDir := filepath.Join(command.Dir, ".omdi-runtime")
	if err := mkdir(runtimeDir, 0o700); err != nil {
		return result, errors.New("extractor: create runtime directory")
	}
	defer func() {
		if removeErr := os.RemoveAll(runtimeDir); removeErr != nil {
			err = errors.New("extractor: remove runtime directory")
		}
	}()
	for _, name := range []string{"home", "cache", "tmp"} {
		if err := mkdir(filepath.Join(runtimeDir, name), 0o700); err != nil {
			return result, errors.New("extractor: create runtime directory")
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := &exec.Cmd{Path: command.Path, Args: append([]string{command.Path}, command.Args...)}
	configureProcessGroup(cmd, r.grace)
	cmd.Dir = command.Dir
	cmd.Env = append([]string{
		"HOME=" + filepath.Join(runtimeDir, "home"),
		"XDG_CACHE_HOME=" + filepath.Join(runtimeDir, "cache"),
		"TMPDIR=" + filepath.Join(runtimeDir, "tmp"),
		"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "PATH=/usr/bin:/bin",
	}, r.extraEnv...)
	cmd.Stdin = strings.NewReader("")
	stdout := cappedCapture{limit: command.StdoutLimit, cancel: cancel}
	stderr := cappedCapture{limit: command.StderrLimit, cancel: cancel}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = runProcess(runCtx, cmd, r.grace)
	result.Stdout, result.Stderr = stdout.buffer.Bytes(), stderr.buffer.Bytes()
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if stdout.overflow || stderr.overflow {
		return result, ErrOutputLimit
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return result, ErrCommandExit
		}
		return result, errors.New("extractor: execute command")
	}
	return result, nil
}

func runProcess(ctx context.Context, cmd *exec.Cmd, grace time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return errors.Join(err, terminateProcessGroup(cmd.Process.Pid, grace))
	case <-ctx.Done():
		signalErr := terminateProcessGroup(cmd.Process.Pid, grace)
		return errors.Join(<-done, signalErr)
	}
}

type cappedCapture struct {
	buffer   bytes.Buffer
	limit    int64
	cancel   context.CancelFunc
	overflow bool
}

func (w *cappedCapture) Write(p []byte) (int, error) {
	remaining := w.limit - int64(w.buffer.Len())
	if int64(len(p)) > remaining {
		_, _ = w.buffer.Write(p[:remaining])
		w.overflow = true
		w.cancel()
	} else {
		_, _ = w.buffer.Write(p)
	}
	return len(p), nil
}
