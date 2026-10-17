package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StateDirectory resolves the per-user state location without discovering or
// loading a project. Long-running per-user commands use the same config as CLI
// commands, independent of their current working directory.
func StateDirectory(input Input) (string, error) {
	stateDir := input.StateDirDefault
	if stateDir == "" {
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			stateHome = filepath.Join(home, ".local", "state")
		} else if !filepath.IsAbs(stateHome) {
			return "", errors.New("XDG_STATE_HOME must be absolute")
		}
		stateDir = filepath.Join(stateHome, "noon")
	}
	if input.UserConfigDir != "" {
		userFile := filepath.Join(input.UserConfigDir, "noon", "config.yaml")
		cfg := Config{StateDir: stateDir}
		if err := cfg.applyFile(userFile, true, "user", userFile, true, nil, false); err != nil {
			return "", err
		}
		stateDir = cfg.StateDir
	}
	if input.StateDir != nil {
		if strings.TrimSpace(*input.StateDir) == "" {
			return "", errors.New("state-dir must not be empty")
		}
		stateDir = resolvePath(input.CWD, *input.StateDir)
	}
	if !filepath.IsAbs(stateDir) {
		return "", fmt.Errorf("state directory must be absolute: %q", stateDir)
	}
	return filepath.Clean(stateDir), nil
}
