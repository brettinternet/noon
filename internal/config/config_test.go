package config_test

import (
	"fmt"
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

func TestYAMLComposition(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	path := filepath.Join(root, "noon.yaml")
	load := func(source string) config.Config {
		t.Helper()
		put(t, path, source)
		cfg, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	composed := `definitions:
  argv: &argv [echo, hello]
  commandKey: &commandKey run
  env: &env {CI: "true", MODE: fast}
  first: &first {*commandKey : *argv, timeout: 2m, env: *env}
  second: &second {timeout: 3m, overlap: queue}
  inherited: &inherited
    <<: [*first, *second]
    env: {<<: *env, MODE: slow}
jobs:
  tests:
    timeout: 4m
    <<: *inherited
`
	cfg := load(composed)
	job := cfg.Jobs["tests"]
	if len(cfg.Jobs) != 1 || job.Timeout != "4m0s" || job.Overlap != "queue" || job.Env["MODE"] != "slow" || job.Env["CI"] != "true" {
		t.Fatalf("resolved job: %+v", job)
	}
	expanded := load("jobs: {tests: {run: [echo, hello], timeout: 4m, overlap: queue, env: {CI: 'true', MODE: slow}}}")
	if job.Digest != expanded.Jobs["tests"].Digest {
		t.Fatal("composition changed approval digest")
	}
	changed := load(strings.Replace(composed, "[echo, hello]", "[echo, changed]", 1))
	if job.Digest == changed.Jobs["tests"].Digest {
		t.Fatal("inherited command change retained approval")
	}
	first := load(strings.Replace(composed, "    timeout: 4m\n", "", 1))
	if first.Jobs["tests"].Timeout != "2m0s" {
		t.Fatal("first merge source did not win")
	}
	put(t, filepath.Join(root, ".git", "noon.yaml"), "jobs: {tests: {run: local}}")
	local := load(composed).Jobs["tests"]
	if local.Run != "local" || len(local.Env) != 0 || local.Timeout != "1h" {
		t.Fatal("composition changed whole-job layer replacement")
	}
}

func TestYAMLCompositionRejectsUnsafeInput(t *testing.T) {
	t.Parallel()
	bomb := "definitions:\n  a0: &a0 [x, x]\n"
	for i := 1; i <= 17; i++ {
		bomb += fmt.Sprintf("  a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	large := "definitions:\n  text: &text " + strings.Repeat("x", 65536) + "\n  copies: [" + strings.Repeat("*text, ", 130) + "]\n"
	for _, tc := range []struct{ name, source, want string }{
		{"cycle", "definitions: {loop: &loop [*loop]}", "cyclic YAML alias"},
		{"expansion nodes", bomb, "expansion limit exceeded"},
		{"expansion bytes", large, "expansion limit exceeded"},
		{"expansion keys", "definitions:\n  base: &base\n    ? " + strings.Repeat("k", 100000) + "\n    : x\n  copies: [" + strings.Repeat("*base, ", 100) + "]\n", "expansion limit exceeded"},
		{"depth", "definitions: {deep: " + strings.Repeat("[", 102) + "x" + strings.Repeat("]", 102) + "}", "expansion limit exceeded"},
		{"inherited typo", "definitions: {base: &base {run: hi, timout: 2m}}\njobs: {tests: {<<: *base}}", "jobs.tests.timout: unknown field"},
		{"duplicate source key", "definitions: {base: &base {run: hi, run: bye}}\njobs: {tests: {<<: *base, run: override}}", "duplicate field"},
		{"duplicate merge key", "jobs: {tests: {<<: {run: hi}, <<: {run: bye}}}", "duplicate field"},
		{"bad merge", "jobs: {tests: {<<: [hi], run: hi}}", "merge requires a mapping"},
		{"alias type", "definitions: {bad: &bad 123}\njobs: {tests: {run: *bad}}", "must be a string"},
		{"custom tag", "definitions: {unused: !include other.yaml}", "unsupported YAML tag"},
		{"unknown top level", "definitions: {base: &base {typo: hi}}\n<<: *base", "unknown field"},
		{"definitions shape", "definitions: []", "definitions must be a mapping"},
		{"undefined alias", "jobs: {tests: {run: *private_secret}}", "invalid YAML"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := canonical(t, t.TempDir())
			put(t, filepath.Join(root, "noon.yaml"), tc.source)
			_, err := config.Load(config.Input{CWD: root})
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(err.Error(), "private_secret") {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadLayersAndPathOrigins(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	user, project := filepath.Join(root, "user"), filepath.Join(root, "repo")
	put(t, filepath.Join(user, "noon/config.yaml"), "timezone: UTC\nstate_dir: runs\n")
	put(t, filepath.Join(project, "noon.yaml"), "timezone: America/Chicago\n")
	put(t, filepath.Join(project, ".git", "noon.yaml"), "timezone: Europe/London\n")
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

// Private jobs skip approval, so only a file repository content cannot supply
// may be private: an in-tree .noon.yaml (committable) is never loaded.
func TestPrivateConfigLivesInCommonGitDirectory(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	main, worktree := filepath.Join(root, "main"), filepath.Join(root, "main", ".worktrees", "feature")
	put(t, filepath.Join(main, ".noon.yaml"), "timezone: Asia/Tokyo\n")
	put(t, filepath.Join(main, ".git", "noon.yaml"), "timezone: Europe/London\n")
	put(t, filepath.Join(main, ".git", "worktrees", "feature", "commondir"), "../..\n")
	put(t, filepath.Join(worktree, ".git"), "gitdir: ../../.git/worktrees/feature\n")
	put(t, filepath.Join(worktree, ".noon.yaml"), "timezone: Asia/Tokyo\n")
	for _, project := range []string{main, worktree} {
		cfg, err := config.Load(config.Input{CWD: project})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Project != project || cfg.Timezone != "Europe/London" || len(cfg.Sources) != 1 || cfg.Sources[0] != filepath.Join(main, ".git", "noon.yaml") {
			t.Fatalf("private layer from %s: %+v", project, cfg)
		}
	}

	noGit := filepath.Join(root, "plain")
	put(t, filepath.Join(noGit, "noon.yaml"), "")
	put(t, filepath.Join(noGit, ".noon.yaml"), "timezone: Asia/Tokyo\n")
	if cfg, err := config.Load(config.Input{CWD: noGit}); err != nil || cfg.Timezone != "Local" || len(cfg.Sources) != 1 {
		t.Fatalf("in-tree .noon.yaml loaded: %+v, %v", cfg, err)
	}

	broken := filepath.Join(root, "broken")
	put(t, filepath.Join(broken, ".git"), "not a pointer\n")
	if _, err := config.Load(config.Input{CWD: broken}); err == nil || !strings.Contains(err.Error(), filepath.Join(broken, ".git")) {
		t.Fatalf("malformed .git = %v", err)
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
	put(t, filepath.Join(child, "noon.yaml"), "timezone: UTC\n")
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
	put(t, filepath.Join(root, ".git", "noon.yaml"), "also: invalid\n")
	put(t, filepath.Join(root, "user/noon/config.yaml"), "timezone: UTC\njobs: {personal: {run: echo user}}\n")
	put(t, filepath.Join(root, "settings/custom.yaml"), "state_dir: runs\n")
	path := "settings/custom.yaml"
	input := config.Input{CWD: root, UserConfigDir: filepath.Join(root, "user"), File: &path}
	cfg, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sources) != 1 || cfg.StateDir != filepath.Join(root, "settings/runs") || len(cfg.Jobs) != 0 {
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
	put(t, filepath.Join(root, ".git", "noon.yaml"), "")
	if _, err := config.Load(config.Input{CWD: root}); err != nil {
		t.Fatalf("zero-byte config rejected: %v", err)
	}
}

func TestUserProjectJobsAreIsolatedAndResolveFromUserRoot(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	configHome := filepath.Join(root, "config-home")
	userProject := filepath.Join(configHome, "noon")
	repo := filepath.Join(root, "repo")
	userFile := filepath.Join(userProject, "config.yaml")
	put(t, userFile, "jobs:\n  personal:\n    run: [pwd]\n    cwd: scripts\n")
	put(t, filepath.Join(userProject, "noon.yaml"), "jobs: {tree: {run: tree}}\n")
	put(t, filepath.Join(userProject, ".git", "noon.yaml"), "jobs: {private: {run: private}}\n")
	put(t, filepath.Join(repo, "noon.yaml"), "jobs: {repository: {run: repository}}\n")
	put(t, filepath.Join(repo, ".git", "noon.yaml"), "jobs: {repo_private: {run: private}}\n")

	regular, err := config.Load(config.Input{CWD: repo, UserConfigDir: configHome})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"personal", "tree", "private"} {
		if _, leaked := regular.Jobs[name]; leaked {
			t.Fatalf("user/tree job %q leaked into repository: %+v", name, regular.Jobs)
		}
	}

	selected, err := config.Load(config.Input{CWD: root, UserConfigDir: configHome, Project: &userProject})
	if err != nil {
		t.Fatal(err)
	}
	job, ok := selected.Jobs["personal"]
	if !ok || len(selected.Jobs) != 1 || selected.Project != userProject || job.Shared || job.Source != userFile || job.CWD != filepath.Join(userProject, "scripts") {
		t.Fatalf("selected user jobs/path: config=%+v job=%+v", selected, job)
	}

	broken := "jobs: {bad: {schedule: every day, run: true}}\n"
	put(t, userFile, broken)
	if _, err := config.Load(config.Input{CWD: repo, UserConfigDir: configHome}); err == nil || !strings.Contains(err.Error(), userFile) {
		t.Fatalf("broken user job was not validated for regular project: %v", err)
	}
}

func TestUserProjectReadOnlySelectionDoesNotCreateConfig(t *testing.T) {
	t.Parallel()
	root := canonical(t, t.TempDir())
	userConfigDir := filepath.Join(root, "missing-config")
	userProject, err := config.UserProjectDir(userConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Input{CWD: root, UserConfigDir: userConfigDir, User: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project != userProject || len(cfg.Jobs) != 0 {
		t.Fatalf("empty user selection: %+v", cfg)
	}
	if _, err := os.Stat(userConfigDir); !os.IsNotExist(err) {
		t.Fatalf("read-only load created user config: %v", err)
	}
}

// Fragments are approval-free, so their names must never shadow each other and
// their content must never come from the working tree.
func TestPrivateFragments(t *testing.T) {
	home := canonical(t, t.TempDir())
	t.Setenv("HOME", home)
	put(t, filepath.Join(home, "vars"), "SCHED=every 2h\n")
	root := canonical(t, t.TempDir())
	main, worktree := filepath.Join(root, "main"), filepath.Join(root, "main", ".worktrees", "feature")
	put(t, filepath.Join(main, ".git", "worktrees", "feature", "commondir"), "../..\n")
	put(t, filepath.Join(worktree, ".git"), "gitdir: ../../.git/worktrees/feature\n")
	fragments := filepath.Join(main, ".git", "noon.d")
	put(t, filepath.Join(home, "dotfiles", "a.yaml"), "vars_file: ~/vars\njobs:\n  alpha: {schedule: '${SCHED}', cwd: '~', prompt_file: ~/a.md, env_file: [~/a.env], agent: claude}\n")
	put(t, filepath.Join(fragments, "b.yaml"), "jobs: {beta: {run: private}}\n")
	if err := os.Symlink(filepath.Join(home, "dotfiles", "a.yaml"), filepath.Join(fragments, "a.yaml")); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(fragments, ".c.yaml"), "not: [valid\n")
	put(t, filepath.Join(fragments, "d.yml"), "not: [valid\n")
	for _, project := range []string{main, worktree} {
		put(t, filepath.Join(project, "noon.yaml"), "jobs: {beta: {run: shared}, gamma: {run: hi, cwd: ~/w}}\n")
		cfg, err := config.Load(config.Input{CWD: project})
		if err != nil {
			t.Fatal(err)
		}
		alpha, beta, gamma := cfg.Jobs["alpha"], cfg.Jobs["beta"], cfg.Jobs["gamma"]
		if alpha.Shared || alpha.Source != filepath.Join(fragments, "a.yaml") || alpha.CWD != home || *alpha.PromptFile != filepath.Join(home, "a.md") || alpha.EnvFile[0] != filepath.Join(home, "a.env") || alpha.Schedule.String() != "every 2h" {
			t.Fatalf("fragment job from %s: %+v", project, alpha)
		}
		if beta.Shared || !beta.Overrides || beta.Run != "private" || gamma.CWD != filepath.Join(project, "~", "w") {
			t.Fatalf("fragment override or literal shared ~ from %s: %+v %+v", project, beta, gamma)
		}
	}

	for _, tc := range []struct {
		name  string
		files map[string]string
		links map[string]string
		check func(config.Config) bool
		want  string
	}{
		{name: "fragment collision", files: map[string]string{".git/noon.d/a.yaml": "jobs: {x: {run: a}}", ".git/noon.d/b.yaml": "jobs: {x: {run: b}}"}, want: `noon.d/b.yaml:1: jobs.x: also defined in "`},
		{name: "local collision", files: map[string]string{".git/noon.d/a.yaml": "jobs: {x: {run: a}}", ".git/noon.yaml": "jobs: {x: {run: b}}"}, want: "noon.yaml:1: jobs.x: also defined in"},
		{name: "toggle", files: map[string]string{".git/noon.d/a.yaml": "jobs: {x: {run: a}}", ".git/noon.yaml": "jobs: {x: {enabled: false}}"}, check: func(c config.Config) bool { return !c.Jobs["x"].Enabled && c.Jobs["x"].Run == "a" }},
		{name: "settings", files: map[string]string{".git/noon.d/a.yaml": "timezone: UTC"}, want: "timezone: not allowed in .git/noon.d fragments; set it in .git/noon.yaml"},
		{name: "fragment into tree", files: map[string]string{"jobs.yaml": "jobs: {x: {run: a}}"}, links: map[string]string{".git/noon.d/a.yaml": "jobs.yaml"}, want: "inside the working tree"},
		{name: "directory into tree", files: map[string]string{"ci/a.yaml": "jobs: {x: {run: a}}"}, links: map[string]string{".git/noon.d": "ci"}, want: "inside the working tree"},
		{name: "local into tree", files: map[string]string{"jobs.yaml": "jobs: {x: {run: a}}"}, links: map[string]string{".git/noon.yaml": "jobs.yaml"}, want: "inside the working tree"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := canonical(t, t.TempDir())
			put(t, filepath.Join(project, ".git", "HEAD"), "")
			for name, data := range tc.files {
				put(t, filepath.Join(project, name), data)
			}
			for link, target := range tc.links {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(project, link)), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(project, target), filepath.Join(project, link)); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := config.Load(config.Input{CWD: project})
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want %q, got %v", tc.want, err)
				}
				return
			}
			if err != nil || !tc.check(cfg) {
				t.Fatalf("%+v %v", cfg.Jobs, err)
			}
		})
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
