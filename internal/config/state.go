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
	cfg, err := userSettings(input)
	return cfg.StateDir, err
}

// userSettings reads only state_dir and serve from user config.
func userSettings(input Input) (Config, error) {
	stateDir := input.StateDirDefault
	if stateDir == "" {
		stateHome := os.Getenv("XDG_STATE_HOME")
		if stateHome == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return Config{}, err
			}
			stateHome = filepath.Join(home, ".local", "state")
		} else if !filepath.IsAbs(stateHome) {
			return Config{}, errors.New("XDG_STATE_HOME must be absolute")
		}
		stateDir = filepath.Join(stateHome, "noon")
	}
	cfg := Config{StateDir: stateDir}
	if input.UserConfigDir != "" {
		userFile := filepath.Join(input.UserConfigDir, "noon", "config.yaml")
		if err := cfg.applyFile(userFile, true, "user", userFile, true, nil, false); err != nil {
			return Config{}, err
		}
	}
	if input.StateDir != nil {
		if strings.TrimSpace(*input.StateDir) == "" {
			return Config{}, errors.New("state-dir must not be empty")
		}
		cfg.StateDir = resolvePath(input.CWD, *input.StateDir)
	}
	if !filepath.IsAbs(cfg.StateDir) {
		return Config{}, fmt.Errorf("state directory must be absolute: %q", cfg.StateDir)
	}
	cfg.StateDir = filepath.Clean(cfg.StateDir)
	return cfg, nil
}
