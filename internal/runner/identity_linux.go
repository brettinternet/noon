//go:build linux

package runner

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

var bootID = sync.OnceValues(func() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("read boot ID: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", errors.New("read boot ID: empty value")
	}
	return value, nil
})

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
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", false, err
	}
	stat := string(data)
	closeParen := strings.LastIndexByte(stat, ')')
	if closeParen < 0 || closeParen+1 >= len(stat) {
		return "", false, fmt.Errorf("parse process stat for PID %d", pid)
	}
	fields := strings.Fields(stat[closeParen+1:])
	const startTimeField = 19 // Suffix begins at field 3; starttime is field 22.
	if len(fields) <= startTimeField {
		return "", false, fmt.Errorf("process stat for PID %d has no start time", pid)
	}
	start, err := strconv.ParseUint(fields[startTimeField], 10, 64)
	if err != nil || start == 0 {
		return "", false, fmt.Errorf("invalid process start time for PID %d", pid)
	}
	boot, err := bootID()
	if err != nil {
		return "", false, err
	}
	identity := "procfs:" + boot + ":" + strconv.FormatUint(start, 10)
	return identity, fields[0] != "Z" && fields[0] != "X", nil
}
