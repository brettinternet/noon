// Package config resolves settings without starting processes or creating state.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	_ "time/tzdata" // Named zones work in minimal containers too.

	"go.yaml.in/yaml/v3"
)

// BuildOpts contains metadata injected at link time.
type BuildOpts struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

// Input contains invocation context and already-resolved flag/environment values.
// Nil means absent; an explicitly empty value is an error. Paths are not expanded
// through a shell. UserConfigDir is the directory containing the noon directory.
type Input struct {
	CWD             string
	UserConfigDir   string
	Project         *string
	File            *string
	Timezone        *string
	StateDir        *string
	StateDirDefault string
}

// Config is a read-only snapshot. Sources are ordered from lowest to highest priority.
type Config struct {
	Project      string                  `json:"project"`
	ConfigFile   *string                 `json:"-"`
	Timezone     string                  `json:"timezone"`
	StateDir     string                  `json:"state_dir"`
	Sources      []string                `json:"sources"`
	Jobs         map[string]Job          `json:"-"`
	Agents       map[string]AgentProfile `json:"-"`
	DefaultAgent string                  `json:"default_agent"`
}

// Load applies defaults, user settings, project settings, and invocation overrides.
// An explicit File replaces all automatic file loading, not invocation overrides.
func Load(input Input) (Config, error) { return load(input, false) }

// LoadForDisable resolves only the checkout and state location. Project manifests
// cannot set state_dir, so broken or missing jobs must never prevent revocation.
// An explicit state-dir also permits recovery from a broken explicit/user file.
func LoadForDisable(input Input) (Config, error) { return load(input, true) }

func load(input Input, stateOnly bool) (Config, error) {
	project, err := ProjectDir(input)
	if err != nil {
		return Config{}, err
	}
	stateDefault := input.StateDirDefault
	if stateDefault == "" {
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
		stateDefault = filepath.Join(stateHome, "noon")
	}
	cfg := Config{Project: project, Timezone: "Local", StateDir: stateDefault, Sources: []string{}, Jobs: make(map[string]Job), Agents: BuiltInAgents()}
	if stateOnly && input.StateDir != nil {
		// The state location is explicit; no configuration file is needed.
	} else if input.File != nil {
		if *input.File == "" {
			return Config{}, errors.New("config path must not be empty")
		}
		file, err := filepath.Abs(resolvePath(input.CWD, *input.File))
		if err != nil {
			return Config{}, err
		}
		cfg.ConfigFile = &file
		if err := cfg.applyFile(file, false, "explicit", filepath.Join(input.UserConfigDir, "noon", "config.yaml"), stateOnly); err != nil {
			return Config{}, err
		}
	} else {
		var paths []string
		if input.UserConfigDir != "" {
			paths = append(paths, filepath.Join(input.UserConfigDir, "noon", "config.yaml"))
		}
		paths = append(paths, filepath.Join(project, "noon.yaml"), filepath.Join(project, ".noon.yaml"))
		for i, path := range paths {
			kind := "shared"
			if input.UserConfigDir != "" && i == 0 {
				kind = "user"
			} else if i == len(paths)-1 {
				kind = "local"
			}
			if stateOnly && kind != "user" {
				continue
			}
			if err := cfg.applyFile(path, true, kind, filepath.Join(input.UserConfigDir, "noon", "config.yaml"), stateOnly); err != nil {
				return Config{}, err
			}
		}
	}
	if err := cfg.resolveAgents(); err != nil {
		return Config{}, err
	}
	if input.Timezone != nil {
		if err := validTimezone(*input.Timezone); err != nil {
			return Config{}, err
		}
		cfg.Timezone = *input.Timezone
	}
	if input.StateDir != nil {
		if strings.TrimSpace(*input.StateDir) == "" {
			return Config{}, errors.New("state-dir must not be empty")
		}
		cfg.StateDir = resolvePath(input.CWD, *input.StateDir)
	}
	return cfg, nil
}

// ProjectDir selects an explicit directory or discovers the nearest config/Git
// boundary. A Git boundary can be a worktree .git file. No config is executed.
func ProjectDir(input Input) (string, error) {
	start := input.CWD
	if input.Project != nil {
		if *input.Project == "" {
			return "", errors.New("project directory must not be empty")
		}
		start = resolvePath(input.CWD, *input.Project)
	}
	start, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("project: %w", err)
	}
	start, err = filepath.EvalSymlinks(start)
	if err != nil {
		return "", fmt.Errorf("project: %w", err)
	}
	info, err := os.Stat(start)
	if err != nil {
		return "", fmt.Errorf("project: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project %s is not a directory", start)
	}
	if input.Project != nil {
		return start, nil
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		for _, name := range []string{"noon.yaml", ".noon.yaml", ".git"} {
			_, err := os.Lstat(filepath.Join(dir, name))
			if err == nil {
				return dir, nil
			}
			if !errors.Is(err, os.ErrNotExist) {
				return "", fmt.Errorf("discover project: %w", err)
			}
		}
		if filepath.Dir(dir) == dir {
			return start, nil
		}
	}
}

func (cfg *Config) applyFile(path string, optional bool, kind, userConfigPath string, stateOnly bool) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if optional && errors.Is(err, os.ErrNotExist) {
		// A dangling symlink is a broken config, not an absent optional file.
		if _, statErr := os.Lstat(path); errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	defer file.Close()
	const maxSize = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	if len(data) > maxSize {
		return fmt.Errorf("config %s: exceeds 1 MiB", path)
	}
	values, err := parse(data)
	if err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	if stateOnly {
		for key := range values {
			if key != "state_dir" {
				delete(values, key)
			}
		}
	}
	if node, ok := values["timezone"]; ok {
		value := node.Value
		if err := validTimezone(value); err != nil {
			return fmt.Errorf("config %s: %w", path, err)
		}
		cfg.Timezone = value
	}
	if node, ok := values["state_dir"]; ok {
		if kind != "user" && kind != "explicit" {
			return fmt.Errorf("config %s: state_dir is allowed only in user config %s or an explicit --config file", path, userConfigPath)
		}
		value := node.Value
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("config %s: state_dir must not be empty", path)
		}
		cfg.StateDir = resolvePath(filepath.Dir(path), value)
	}
	if node, ok := values["default_agent"]; ok {
		if kind != "user" && kind != "local" && kind != "explicit" {
			return jobError(path, node, "default_agent", "allowed only in user config, .noon.yaml, or --config files")
		}
		value := node.Value
		if strings.TrimSpace(value) == "" {
			return jobError(path, node, "default_agent", "must not be empty")
		}
		cfg.DefaultAgent = value
	}
	if node, ok := values["agents"]; ok {
		if kind != "user" && kind != "explicit" {
			return jobError(path, node, "agents", "allowed only in user config or --config files")
		}
		if err := cfg.applyAgents(path, node); err != nil {
			return err
		}
	}
	if jobs, ok := values["jobs"]; ok {
		if kind == "user" {
			return jobError(path, jobs, "jobs", "allowed only in noon.yaml, .noon.yaml, or --config files")
		}
		if err := cfg.applyJobs(path, kind, jobs); err != nil {
			return err
		}
	}
	cfg.Sources = append(cfg.Sources, path)
	return nil
}

// parse accepts only the implemented settings and job schema. Unknown fields,
// aliases, duplicate keys, nulls, and trailing documents cannot silently hide errors.
func parse(data []byte) (map[string]*yaml.Node, error) {
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	var doc yaml.Node
	if err := decoder.Decode(&doc); err != nil {
		// Decoder diagnostics can quote raw environment values (for example an
		// unknown anchor). Never forward them to human or JSON output.
		return nil, errors.New("invalid YAML: expected one mapping; check syntax and do not use aliases")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("expected a YAML mapping")
	}
	values := make(map[string]*yaml.Node)
	root := doc.Content[0]
	for i := 0; i < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, errors.New("configuration keys must be strings")
		}
		if _, exists := values[key.Value]; exists {
			return nil, fmt.Errorf("duplicate field %q", key.Value)
		}
		switch key.Value {
		case "version":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!int" || value.Value != "1" {
				return nil, errors.New("version must be 1")
			}
		case "agents":
			// The profile schema is validated with source and YAML-path diagnostics below.
		case "default_agent":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return nil, errors.New("default_agent must be a string")
			}
		case "jobs":
			// Nested job validation retains source lines and YAML paths.
		case "timezone", "state_dir":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return nil, fmt.Errorf("%s must be a string", key.Value)
			}
		default:
			return nil, fmt.Errorf("unknown field %q", key.Value)
		}
		values[key.Value] = value
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("expected exactly one YAML document")
	}
	return values, nil
}

func validTimezone(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("timezone must not be empty")
	}
	if _, err := time.LoadLocation(value); err != nil {
		return fmt.Errorf("timezone %q: %w", value, err)
	}
	return nil
}

func resolvePath(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
