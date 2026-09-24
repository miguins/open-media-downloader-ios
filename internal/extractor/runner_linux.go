package extractor

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

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
