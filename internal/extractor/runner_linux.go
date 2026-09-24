package extractor

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

var errProcessCleanup = errors.New("extractor: process cleanup incomplete")

func configureProcessGroup(cmd *exec.Cmd, grace time.Duration) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Also bound pipe draining if a tool exits with a descendant holding a pipe.
	cmd.WaitDelay = grace
}

func terminateProcessGroup(pid int, grace time.Duration) error {
	err := syscall.Kill(-pid, syscall.SIGTERM)
	if err != nil {
		return sanitizeSignalError(err)
	}
	timer := time.NewTimer(grace)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			return sanitizeSignalError(syscall.Kill(-pid, syscall.SIGKILL))
		case <-ticker.C:
			if errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) {
				return nil
			}
		}
	}
}

func sanitizeSignalError(err error) error {
	if err == nil || errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return errors.New("extractor: signal process group")
}

// confirmProcessGroupExit runs only after Cmd.Wait has reaped the group leader.
// It reaps adopted descendants in this group without consuming other commands'
// children, and allows the container init to reap descendants it adopted.
func confirmProcessGroupExit(pid int, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		// ECHILD is normal when init adopted the descendants. Other transient
		// wait errors cannot establish completion; the group check stays authoritative.
		_, _ = syscall.Wait4(-pid, nil, syscall.WNOHANG, nil)
		if errors.Is(syscall.Kill(-pid, 0), syscall.ESRCH) {
			return nil
		}
		select {
		case <-timer.C:
			return errProcessCleanup
		case <-ticker.C:
		}
	}
}
