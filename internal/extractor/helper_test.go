package extractor

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

type helperInspection struct {
	Args  []string
	Env   []string
	Dir   string
	Input string
	Modes map[string]uint32
}

// TestExtractorHelperProcess runs only in a child test binary explicitly started
// by the test factory. No application input can activate it in production.
func TestExtractorHelperProcess(_ *testing.T) {
	if os.Getenv("GO_WANT_EXTRACTOR_HELPER") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		syscall.Exit(90)
	}
	switch args[0] {
	case "inspect":
		dir, err := os.Getwd()
		if err != nil {
			syscall.Exit(91)
		}
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			syscall.Exit(92)
		}
		modes := make(map[string]uint32)
		for _, name := range []string{"", "home", "cache", "tmp"} {
			info, err := os.Lstat(filepath.Join(dir, ".omdi-runtime", name))
			if err != nil {
				syscall.Exit(93)
			}
			modes[name] = uint32(info.Mode().Perm())
		}
		if err := json.NewEncoder(os.Stdout).Encode(helperInspection{args[1:], os.Environ(), dir, string(input), modes}); err != nil {
			syscall.Exit(94)
		}
	case "emit":
		stdout, err := strconv.Atoi(args[1])
		if err != nil {
			syscall.Exit(95)
		}
		stderr, err := strconv.Atoi(args[2])
		if err != nil {
			syscall.Exit(95)
		}
		_, _ = io.CopyN(os.Stdout, strings.NewReader(strings.Repeat("o", stdout)), int64(stdout))
		_, _ = io.CopyN(os.Stderr, strings.NewReader(strings.Repeat("e", stderr)), int64(stderr))
	case "exit":
		code, err := strconv.Atoi(args[1])
		if err != nil {
			syscall.Exit(95)
		}
		_, _ = io.WriteString(os.Stderr, args[2])
		syscall.Exit(code)
	case "sleep":
		helperSleep(args[1:])
	case "tree":
		helperTree(args[1:])
	case "lock-runtime":
		if err := os.WriteFile(".omdi-runtime/locked", []byte("fixture"), 0o600); err != nil {
			syscall.Exit(97)
		}
		if err := os.Chmod(".omdi-runtime", 0o500); err != nil { //nolint:gosec // Read-only directory fixture.
			syscall.Exit(97)
		}
	default:
		syscall.Exit(96)
	}
	// A raw exit keeps race/coverage runtime diagnostics out of fixture output.
	syscall.Exit(0)
}

func helperSleep(args []string) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	if err := os.WriteFile(args[0], []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil { //nolint:gosec // PID file selected by the parent test under its private temporary directory.
		syscall.Exit(97)
	}
	if len(args) > 1 && args[1] == "resist" {
		for {
			<-signals
		}
	}
	<-signals
	// Avoid the race detector's process-exit sleep in signal timing tests.
	syscall.Exit(0)
}

func helperTree(args []string) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	childArgs := []string{"-test.run=^TestExtractorHelperProcess$", "--", "sleep", args[1]}
	if args[2] == "orphan" {
		childArgs = append(childArgs, "resist")
	}
	child := exec.CommandContext(context.Background(), os.Args[0], childArgs...) //nolint:gosec // Reexecutes only this test binary with test-selected arguments.
	child.Env = os.Environ()
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		syscall.Exit(98)
	}
	childDone := make(chan struct{})
	go func() {
		_ = child.Wait()
		close(childDone)
	}()
	for {
		if _, err := os.Stat(args[1]); err == nil { //nolint:gosec // PID file selected by the parent test.
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err := os.WriteFile(args[0], []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil { //nolint:gosec // PID file selected by the parent test under its private temporary directory.
		syscall.Exit(97)
	}
	if args[2] == "overflow" {
		_, _ = io.WriteString(os.Stdout, strings.Repeat("o", 65))
	}
	if args[2] == "orphan" {
		syscall.Exit(0)
	}
	if args[2] == "resist" {
		for {
			<-signals
		}
	}
	<-signals
	<-childDone
	syscall.Exit(0)
}
