//go:build darwin

package runner

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const darwinZombieState = 5

func processGroupAlive(pgid int) (bool, error) {
	members, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) || errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("read process group %d: %w", pgid, err)
	}
	for _, member := range members {
		if member.Eproc.Pgid == int32(pgid) && member.Proc.P_stat != darwinZombieState {
			return true, nil
		}
	}
	return false, nil
}
