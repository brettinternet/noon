package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/noon/internal/config"
)

func put(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func canonical(t *testing.T, path string) string {
	t.Helper()
	value, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestLoadLayersAndPathOrigins(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	user, project := filepath.Join(root, "user"), filepath.Join(root, "repo")
	put(t, filepath.Join(user, "noon/config.yaml"), "timezone: UTC\nstate_dir: runs\n")
	put(t, filepath.Join(project, "noon.yaml"), "timezone: America/Chicago\n")
	put(t, filepath.Join(project, ".noon.yaml"), "timezone: Europe/London\n")
	input := config.Input{CWD: project, UserConfigDir: user}
	cfg, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timezone != "Europe/London" || cfg.StateDir != filepath.Join(user, "noon/runs") || len(cfg.Sources) != 3 {
		t.Fatalf("unexpected layer resolution: %+v", cfg)
	}
	zone, state := "UTC", "private-runs"
	input.Timezone, input.StateDir = &zone, &state
	cfg, err = config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Timezone != zone || cfg.StateDir != filepath.Join(project, state) {
		t.Fatalf("overrides: %+v", cfg)
	}
	if _, err := os.Stat(cfg.StateDir); !os.IsNotExist(err) {
		t.Fatalf("load must not create state: %v", err)
	}
}

func TestStateDirectoryScopeAndOrigins(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	project := filepath.Join(root, "repo")
	userDir := filepath.Join(root, "user")
	put(t, filepath.Join(project, "noon.yaml"), "state_dir: project-state\n")
	_, err := config.Load(config.Input{CWD: project, UserConfigDir: userDir, StateDirDefault: filepath.Join(root, "state", "noon")})
	if err == nil || !strings.Contains(err.Error(), filepath.Join(userDir, "noon", "config.yaml")) {
		t.Fatalf("project state_dir should name the allowed user config path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "project-state")); !os.IsNotExist(err) {
		t.Fatalf("project state path was created: %v", err)
	}
	put(t, filepath.Join(project, "noon.yaml"), "timezone: UTC\n")
	launch := filepath.Join(root, "launch")
	if err := os.Mkdir(launch, 0o700); err != nil {
		t.Fatal(err)
	}
	state := "relative-state"
	cfg, err := config.Load(config.Input{CWD: launch, UserConfigDir: userDir, Project: &project, StateDir: &state, StateDirDefault: filepath.Join(root, "state", "noon")})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateDir != filepath.Join(launch, state) {
		t.Fatalf("flag path must be invocation-relative: %s", cfg.StateDir)
	}
}

func TestStateDirectoryResolvesWithoutProjectDiscovery(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	userDir := filepath.Join(root, "user")
	put(t, filepath.Join(userDir, "noon/config.yaml"), "state_dir: private-state\n")
	cwd := filepath.Join(root, "not-a-project")
	defaultDir := filepath.Join(root, "default")
	resolved, err := config.StateDirectory(config.Input{CWD: cwd, UserConfigDir: userDir, StateDirDefault: defaultDir})
	if err != nil || resolved != filepath.Join(userDir, "noon/private-state") {
		t.Fatalf("user state directory = %q, err=%v", resolved, err)
	}
	override := "override"
	resolved, err = config.StateDirectory(config.Input{CWD: cwd, UserConfigDir: userDir, StateDirDefault: defaultDir, StateDir: &override})
	if err != nil || resolved != filepath.Join(cwd, override) {
		t.Fatalf("explicit state directory = %q, err=%v", resolved, err)
	}
}

func TestMalformedYAMLLineIsReportedWithoutDecoderText(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	path := filepath.Join(root, "noon.yaml")
	put(t, path, "jobs:\n  tests:\n    run: 'private-secret'\n    env: [broken\n")
	_, err := config.Load(config.Input{CWD: root})
	if err == nil || !strings.Contains(err.Error(), path+":3:") || strings.Contains(err.Error(), "private-secret") || strings.Contains(err.Error(), "did not find") {
		t.Fatalf("malformed YAML diagnostic = %v", err)
	}
}

func TestLoadDiscoveryStopsAtGitBoundary(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	put(t, filepath.Join(root, "noon.yaml"), "timezone: Not/AZone\n")
	project := filepath.Join(root, "worktree")
	put(t, filepath.Join(project, ".git"), "gitdir: elsewhere\n")
	child := filepath.Join(project, "nested")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Input{CWD: child})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != project || len(cfg.Sources) != 0 {
		t.Fatalf("escaped Git boundary: %+v", cfg)
	}
	put(t, filepath.Join(child, ".noon.yaml"), "timezone: UTC\n")
	cfg, err = config.Load(config.Input{CWD: child})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != child || cfg.Timezone != "UTC" {
		t.Fatalf("nearest config: %+v", cfg)
	}
}

func TestLoadExplicitFileReplacesDiscovery(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	put(t, filepath.Join(root, "noon.yaml"), "invalid: configuration\n")
	put(t, filepath.Join(root, ".noon.yaml"), "also: invalid\n")
	put(t, filepath.Join(root, "user/noon/config.yaml"), "invalid: user\n")
	put(t, filepath.Join(root, "settings/custom.yaml"), "state_dir: runs\n")
	path := "settings/custom.yaml"
	input := config.Input{CWD: root, UserConfigDir: filepath.Join(root, "user"), File: &path}
	cfg, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sources) != 1 || cfg.StateDir != filepath.Join(root, "settings/runs") {
		t.Fatalf("explicit file: %+v", cfg)
	}
	path = "missing.yaml"
	if _, err := config.Load(input); err == nil {
		t.Fatal("explicit missing config accepted")
	}
}

func TestLoadRejectsAmbiguousOrInvalidYAML(t *testing.T) {
	t.Parallel()
	for _, data := range []string{
		"null", "[]", "timezone: UTC\ntimezone: Local\n", "timezone: null\n",
		"timezone: 42\n", "timezone: ''\n", "timezone: Invalid/Zone\n",
		"timezone: &zone UTC\nstate_dir: *zone\n", "timezon: UTC\n", "jobs: []\n",
		"version: 2\n", "version: '1'\n", "state_dir: ''\n", "{}\n---\n{}\n",
	} {
		t.Run(data, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "noon.yaml")
			put(t, path, data)
			_, err := config.Load(config.Input{CWD: root})
			if err == nil || !strings.Contains(err.Error(), "noon.yaml") {
				t.Fatalf("wanted source-specific error; got %v", err)
			}
		})
	}
}

func TestLoadAcceptsEmptyFileForJobEditing(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, ".noon.yaml"), "")
	if _, err := config.Load(config.Input{CWD: root}); err != nil {
		t.Fatalf("zero-byte config rejected: %v", err)
	}
}

func TestLoadDoesNotIgnoreBrokenConfigLink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "noon.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(config.Input{CWD: root}); err == nil {
		t.Fatal("broken configuration link silently ignored")
	}
}
