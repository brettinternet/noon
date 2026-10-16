//go:build darwin

package runner

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func processStartIdentity(pid int) (string, error) {
	identity, _, err := processInfo(pid)
	return identity, err
}

func processOwnerAlive(pid int, expectedIdentity string) (bool, error) {
	identity, alive, err := processInfo(pid)
	return identity == expectedIdentity && alive, err
}

func processInfo(pid int) (string, bool, error) {
	if pid <= 0 {
		return "", false, fmt.Errorf("invalid process ID %d", pid)
	}
	if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
		return "", false, os.ErrNotExist
	} else if err != nil && !errors.Is(err, syscall.EPERM) {
		return "", false, fmt.Errorf("check process ID %d: %w", pid, err)
	}
	proc, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			return "", false, os.ErrNotExist
		}
		return "", false, fmt.Errorf("read process table for PID %d: %w", pid, err)
	}
	if proc == nil || proc.Proc.P_pid != int32(pid) {
		return "", false, fmt.Errorf("process identity unavailable for PID %d", pid)
	}
	start := proc.Proc.P_starttime
	if start.Sec == 0 && start.Usec == 0 {
		return "", false, fmt.Errorf("process start time unavailable for PID %d", pid)
	}
	return fmt.Sprintf("kinfo:%d:%d", start.Sec, start.Usec), proc.Proc.P_stat != darwinZombieState, nil
}
