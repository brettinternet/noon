package runner

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// ProcessStartIdentity returns a boot-qualified start identity where the OS exposes one.
func ProcessStartIdentity(pid int) (string, error) { return processStartIdentity(pid) }

// ProcessOwnerAlive reports whether PID still has the expected identity and is not a zombie.
func ProcessOwnerAlive(pid int, expectedIdentity string) (bool, error) {
	return processOwnerAlive(pid, expectedIdentity)
}

// TerminateProcessGroup signals only a group whose leader still has the recorded identity.
func TerminateProcessGroup(pgid int, expectedIdentity string, grace time.Duration) error {
	if pgid <= 0 || expectedIdentity == "" {
		return nil
	}
	identity, err := ProcessStartIdentity(pgid)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if identity != expectedIdentity {
		return nil
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("send TERM to process group %d: %w", pgid, err)
	}
	if grace <= 0 {
		grace = 10 * time.Second
	}
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		alive, err := processGroupAlive(pgid)
		if err != nil {
			return err
		}
		if !alive {
			return nil
		}
		select {
		case <-deadline.C:
			identity, err := ProcessStartIdentity(pgid)
			if errors.Is(err, os.ErrNotExist) || (err == nil && identity != expectedIdentity) {
				return nil
			}
			if err != nil {
				return err
			}
			alive, err := processGroupAlive(pgid)
			if err != nil {
				return err
			}
			if !alive {
				return nil
			}
			if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
				// Another recovery may have killed the group after our liveness check.
				if errors.Is(err, syscall.EPERM) {
					deadline := time.Now().Add(100 * time.Millisecond)
					for {
						identity, identityErr := ProcessStartIdentity(pgid)
						if errors.Is(identityErr, os.ErrNotExist) || (identityErr == nil && identity != expectedIdentity) {
							return nil
						}
						if identityErr != nil {
							return identityErr
						}
						alive, aliveErr := processGroupAlive(pgid)
						if aliveErr != nil {
							return aliveErr
						}
						if !alive {
							return nil
						}
						if !time.Now().Before(deadline) {
							break
						}
						time.Sleep(10 * time.Millisecond)
					}
				}
				return fmt.Errorf("send KILL to process group %d: %w", pgid, err)
			}
			return nil
		case <-ticker.C:
		}
	}
}
