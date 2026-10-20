package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/config"
)

func TestVarsFileFieldsAcrossSources(t *testing.T) {
	for _, field := range []struct {
		name, variable, value, yaml, want string
	}{
		{"schedule", "VALUE", "every 5m", "schedule: ${VALUE}", "every 5m"},
		{"cwd", "VALUE", "work", "cwd: ${VALUE}", "cwd"},
		{"env_file", "VALUE", ".runtime.env", "env_file: ${VALUE}", "env_file"},
		{"env_file list", "VALUE", ".runtime.env", "env_file: [\"${VALUE}\", .second.env]", "env_file_list"},
		{"prompt_file", "VALUE", "prompts/review.md", "prompt_file: ${VALUE}\n    agent: codex", "prompt_file"},
		{"timeout", "VALUE", "30s", "timeout: ${VALUE}", "timeout"},
		{"cooldown", "VALUE", "5s", "cooldown: ${VALUE}", "cooldown"},
		{"splay", "VALUE", "5m", "splay: ${VALUE}", "splay"},
		{"jitter", "VALUE", "5s", "jitter: ${VALUE}", "jitter"},
	} {
		t.Run(field.name, func(t *testing.T) {
			for _, source := range []string{"shared", "private", "user", "explicit"} {
				t.Run(source, func(t *testing.T) {
					root, err := filepath.EvalSymlinks(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					project := root
					configHome := filepath.Join(root, "config-home")
					if err := os.MkdirAll(project, 0o700); err != nil {
						t.Fatal(err)
					}
					if source == "user" {
						project = filepath.Join(configHome, "noon")
					}
					if err := os.MkdirAll(project, 0o700); err != nil {
						t.Fatal(err)
					}
					vars := fmt.Sprintf("%s=%s\n", field.variable, field.value)
					put(t, filepath.Join(project, "vars.env"), vars)
					manifest := "version: 1\nvars_file: vars.env\njobs:\n  probe:\n"
					if field.name != "schedule" {
						schedule := "every 1h"
						if field.name == "splay" || field.name == "jitter" {
							schedule = "daily"
						}
						manifest += "    schedule: " + schedule + "\n"
					}
					manifest += "    " + field.yaml + "\n"
					if field.name != "prompt_file" {
						manifest += "    run: \"true\"\n"
					}

					var input config.Input
					switch source {
					case "shared":
						put(t, filepath.Join(project, "noon.yaml"), manifest)
						input = config.Input{CWD: project, UserConfigDir: configHome}
					case "private":
						if err := os.MkdirAll(filepath.Join(project, ".git"), 0o700); err != nil {
							t.Fatal(err)
						}
						put(t, filepath.Join(project, ".git", "noon.yaml"), manifest)
						input = config.Input{CWD: project, UserConfigDir: configHome}
					case "user":
						put(t, filepath.Join(project, "config.yaml"), manifest)
						input = config.Input{CWD: root, UserConfigDir: configHome, User: true}
					case "explicit":
						path := filepath.Join(project, "settings", "custom.yaml")
						put(t, path, manifest)
						input = config.Input{CWD: root, Project: &project, File: &path, UserConfigDir: configHome}
					}
					loaded, err := config.Load(input)
					if err != nil {
						t.Fatalf("Load: %v\n%s", err, manifest)
					}
					job := loaded.Jobs["probe"]
					switch field.want {
					case "schedule":
						if job.Schedule == nil || job.Schedule.String() != field.value {
							t.Fatalf("schedule = %v", job.Schedule)
						}
					case "cwd":
						if job.CWD != filepath.Join(project, field.value) {
							t.Fatalf("cwd = %q", job.CWD)
						}
					case "env_file":
						if len(job.EnvFile) != 1 || job.EnvFile[0] != field.value {
							t.Fatalf("env_file = %v", job.EnvFile)
						}
					case "env_file_list":
						if len(job.EnvFile) != 2 || job.EnvFile[0] != field.value || job.EnvFile[1] != ".second.env" {
							t.Fatalf("env_file list = %v", job.EnvFile)
						}
					case "prompt_file":
						if job.PromptFile == nil || *job.PromptFile != field.value {
							t.Fatalf("prompt_file = %v", job.PromptFile)
						}
					case "timeout":
						if job.Timeout != field.value {
							t.Fatalf("timeout = %q", job.Timeout)
						}
					case "cooldown":
						if job.Cooldown.String() != field.value {
							t.Fatalf("cooldown = %v", job.Cooldown)
						}
					case "splay":
						if job.Splay != 5*time.Minute {
							t.Fatalf("splay = %v", job.Splay)
						}
					case "jitter":
						if job.Jitter.String() != field.value {
							t.Fatalf("jitter = %v", job.Jitter)
						}
					}
				})
			}
		})
	}
}

func TestRepositoryLoadResolvesUserVarsFileFromUserProject(t *testing.T) {
	root := t.TempDir()
	configHome := filepath.Join(root, "config-home")
	userProject, err := config.UserProjectDir(configHome)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(userProject, 0o700); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(userProject, "config.yaml"), "vars_file: user.vars\njobs:\n  user_only:\n    schedule: ${USER_SCHEDULE}\n    run: \"true\"\n")
	put(t, filepath.Join(userProject, "user.vars"), "USER_SCHEDULE=every 2h\n")
	put(t, filepath.Join(root, "user.vars"), "SECRET_SENTINEL=\"broken\n")
	put(t, filepath.Join(root, "noon.yaml"), "version: 1\n")

	loaded, err := config.Load(config.Input{CWD: root, UserConfigDir: configHome})
	if err != nil {
		t.Fatalf("repository load read user vars_file relative to project root: %v", err)
	}
	if _, exists := loaded.Jobs["user_only"]; exists {
		t.Fatal("repository config inherited user-project jobs")
	}
	if len(loaded.VarsFiles) != 1 || !loaded.VarsFiles[0].Present || loaded.VarsFiles[0].Configured != "user.vars" {
		t.Fatalf("user vars_file metadata = %+v", loaded.VarsFiles)
	}

	custom := filepath.Join(root, "custom.yaml")
	put(t, custom, "vars_file: custom.vars\n")
	put(t, filepath.Join(root, "custom.vars"), "CUSTOM=value\n")
	explicit, err := config.Load(config.Input{CWD: root, Project: &root, File: &custom, UserConfigDir: configHome})
	if err != nil {
		t.Fatalf("explicit config did not validate the user vars_file: %v", err)
	}
	if len(explicit.VarsFiles) != 2 || explicit.VarsFiles[0].Configured != "user.vars" || !explicit.VarsFiles[0].Present || explicit.VarsFiles[1].Configured != "custom.vars" || !explicit.VarsFiles[1].Present {
		t.Fatalf("explicit config vars_file metadata = %+v", explicit.VarsFiles)
	}
}

func TestVarsFileInterpolationSyntaxAndValidation(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "noon.yaml")
	load := func(manifest, vars string) (config.Config, error) {
		t.Helper()
		put(t, manifestPath, manifest)
		if vars != "" {
			put(t, filepath.Join(root, "vars.env"), vars)
		}
		return config.Load(config.Input{CWD: root})
	}
	for _, tc := range []struct {
		name, value, vars, want string
	}{
		{"required value", "${SCHEDULE}", "SCHEDULE=every 2h\n", "every 2h"},
		{"empty defaulted value", "${EMPTY:-every 3h}", "EMPTY=\n", "every 3h"},
		{"absent default", "${ABSENT:-every 4h}", "SCHEDULE=every 1h\n", "every 4h"},
		{"default without vars file", "${ABSENT:-every 5h}", "", "every 5h"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			varsLine := ""
			if tc.vars != "" {
				varsLine = "vars_file: vars.env\n"
			}
			cfg, err := load("version: 1\n"+varsLine+"jobs:\n  probe:\n    schedule: "+tc.value+"\n    run: \"true\"\n", tc.vars)
			if err != nil || cfg.Jobs["probe"].Schedule == nil || cfg.Jobs["probe"].Schedule.String() != tc.want {
				t.Fatalf("schedule=%v err=%v", cfg.Jobs["probe"].Schedule, err)
			}
		})
	}

	cfg, err := load("vars_file: vars.env\njobs:\n  probe:\n    cwd: '$$literal/$NAME'\n    run: \"true\"\n", "")
	if err != nil || cfg.Jobs["probe"].CWD != filepath.Join(root, "$literal", "$NAME") {
		t.Fatalf("literal dollars in cwd = %q, err=%v", cfg.Jobs["probe"].CWD, err)
	}
	cfg, err = load("vars_file: vars.env\njobs:\n  probe:\n    cwd: ${PATH_VALUE}\n    run: \"true\"\n", "PATH_VALUE='${INNER}/$$literal'\n")
	if err != nil || cfg.Jobs["probe"].CWD != filepath.Join(root, "${INNER}", "$$literal") {
		t.Fatalf("single-pass cwd = %q, err=%v", cfg.Jobs["probe"].CWD, err)
	}
	cfg, err = load("vars_file: vars.env\njobs:\n  probe:\n    cwd: ${PATH_VALUE}\n    run: \"true\"\n", "PATH_VALUE=one: [two]\n")
	if err != nil || cfg.Jobs["probe"].CWD != filepath.Join(root, "one: [two]") {
		t.Fatalf("YAML punctuation changed scalar: cwd=%q err=%v", cfg.Jobs["probe"].CWD, err)
	}

	for _, expression := range []string{"${BROKEN", "${}", "${1BAD}", "${A-B}", "${A:?error}", "${A:+value}", "${A:-${B}}"} {
		t.Run(expression, func(t *testing.T) {
			_, err := load("vars_file: vars.env\njobs:\n  probe:\n    cwd: "+expression+"\n    run: \"true\"\n", "A=value\nB=value\n")
			if err == nil || !strings.Contains(err.Error(), "jobs.probe.cwd") || strings.Contains(err.Error(), "value") {
				t.Fatalf("invalid expression accepted or leaked values: %v", err)
			}
		})
	}

	_, err = load("vars_file: vars.env\njobs:\n  probe:\n    schedule: ${MISSING}\n    run: \"true\"\n", "")
	for _, part := range []string{manifestPath + ":4:", "jobs.probe.schedule", "MISSING", `vars_file "vars.env"`} {
		if err == nil || !strings.Contains(err.Error(), part) {
			t.Fatalf("missing variable diagnostic lacks %q: %v", part, err)
		}
	}
	_, err = load("jobs:\n  probe:\n    schedule: ${MISSING}\n    run: \"true\"\n", "")
	if err == nil || !strings.Contains(err.Error(), "no vars_file is configured") || !strings.Contains(err.Error(), "MISSING") {
		t.Fatalf("missing variable without vars_file diagnostic = %v", err)
	}
	_, err = load("vars_file: vars.env\njobs:\n  probe:\n    timeout: ${EMPTY}\n    run: \"true\"\n", "EMPTY=\n")
	if err == nil || !strings.Contains(err.Error(), "jobs.probe.timeout") || strings.Contains(err.Error(), "variable ") {
		t.Fatalf("empty value did not reach timeout validation: %v", err)
	}
}

func TestVarsFileIsDocumentLocalAndNeverUsesOtherEnvironments(t *testing.T) {
	root := t.TempDir()
	projectFile := filepath.Join(root, "noon.yaml")
	put(t, projectFile, "vars_file: shared.vars\njobs:\n  shared:\n    schedule: ${SCHEDULE}\n    run: \"true\"\n  replacement:\n    schedule: ${SCHEDULE}\n    run: \"true\"\n")
	put(t, filepath.Join(root, "shared.vars"), "SCHEDULE=every 10m\nSHARED_ONLY=shared-value\n")
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	put(t, filepath.Join(root, ".git", "noon.yaml"), "vars_file: private.vars\njobs:\n  shared:\n    enabled: false\n  replacement:\n    schedule: ${PRIVATE_SCHEDULE}\n    run: \"true\"\n  private:\n    schedule: ${PRIVATE_SCHEDULE}\n    run: \"true\"\n")
	put(t, filepath.Join(root, "private.vars"), "PRIVATE_SCHEDULE=every 2h\n")
	cfg, err := config.Load(config.Input{CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Jobs["shared"].Schedule.String(); got != "every 10m" || cfg.Jobs["shared"].Enabled {
		t.Fatalf("enabled-only override changed shared resolution: %+v", cfg.Jobs["shared"])
	}
	if got := cfg.Jobs["private"].Schedule.String(); got != "every 2h" {
		t.Fatalf("private vars_file did not resolve: %s", got)
	}
	if got := cfg.Jobs["replacement"].Schedule.String(); got != "every 2h" {
		t.Fatalf("private replacement used shared vars_file: %s", got)
	}

	put(t, filepath.Join(root, ".git", "noon.yaml"), "vars_file: private.vars\njobs:\n  leaked:\n    schedule: ${SHARED_ONLY}\n    run: \"true\"\n")
	if _, err := config.Load(config.Input{CWD: root}); err == nil || !strings.Contains(err.Error(), `vars_file "private.vars"`) || !strings.Contains(err.Error(), "SHARED_ONLY") {
		t.Fatalf("shared vars leaked into private document: %v", err)
	}

	t.Setenv("SHELL_ONLY", "shell-value")
	put(t, filepath.Join(root, "runtime.env"), "RUNTIME_ONLY=runtime-value\n")
	put(t, projectFile, "vars_file: shared.vars\njobs:\n  absent:\n    schedule: ${SHELL_ONLY}\n    env_file: runtime.env\n    env: {JOB_ONLY: job-value}\n    run: \"true\"\n")
	if _, err := config.Load(config.Input{CWD: root}); err == nil || !strings.Contains(err.Error(), "SHELL_ONLY") {
		t.Fatalf("process environment supplied an interpolation value: %v", err)
	}
	put(t, projectFile, "vars_file: shared.vars\njobs:\n  absent:\n    schedule: ${RUNTIME_ONLY}\n    env_file: runtime.env\n    run: \"true\"\n")
	if _, err := config.Load(config.Input{CWD: root}); err == nil || !strings.Contains(err.Error(), "RUNTIME_ONLY") {
		t.Fatalf("runtime env_file supplied an interpolation value: %v", err)
	}
	put(t, projectFile, "vars_file: shared.vars\njobs:\n  absent:\n    schedule: ${JOB_ONLY}\n    env: {JOB_ONLY: job-value}\n    run: \"true\"\n")
	if _, err := config.Load(config.Input{CWD: root}); err == nil || !strings.Contains(err.Error(), "JOB_ONLY") {
		t.Fatalf("job env supplied an interpolation value: %v", err)
	}
}

func TestVarsFileDoesNotExpandOtherFieldsOrNames(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "vars.env"), "VALUE=expanded\n")
	custom := filepath.Join(root, "custom.yaml")
	put(t, custom, `vars_file: vars.env
on_success: 'echo ${VALUE} $VALUE'
jobs:
  literal:
    run: 'echo ${VALUE} $VALUE'
    on_failure: 'echo ${VALUE} $VALUE'
    env: {VALUE: '${VALUE}', OTHER: '$VALUE'}
  prompt:
    prompt: '${VALUE} $VALUE'
    agent: codex
    agent_args: ['${VALUE}', '$VALUE']
`)
	input := config.Input{CWD: root, Project: &root, File: &custom}
	cfg, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	literal, prompt := cfg.Jobs["literal"], cfg.Jobs["prompt"]
	if literal.Run != "echo ${VALUE} $VALUE" || literal.OnFailure != "echo ${VALUE} $VALUE" || literal.Env["VALUE"] != "${VALUE}" || literal.Env["OTHER"] != "$VALUE" || *prompt.Prompt != "${VALUE} $VALUE" || strings.Join(prompt.AgentArgs, "|") != "${VALUE}|$VALUE" || cfg.OnSuccess != "echo ${VALUE} $VALUE" {
		t.Fatalf("non-allowlisted fields changed: literal=%+v prompt=%+v on_success=%v", literal, prompt, cfg.OnSuccess)
	}

	put(t, custom, "vars_file: vars.env\njobs:\n  ${VALUE}: {run: \"true\"}\n")
	if _, err := config.Load(input); err == nil || !strings.Contains(err.Error(), "name must match") {
		t.Fatalf("job name was interpolated: %v", err)
	}
}

func TestVarsFileReadRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(*testing.T, string)
		line bool
	}{
		{"directory", func(t *testing.T, path string) {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"fifo", func(t *testing.T, path string) {
			if err := syscall.Mkfifo(path, 0o600); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"oversized", func(t *testing.T, path string) {
			put(t, path, "KEY="+strings.Repeat("x", config.MaxEnvFileBytes))
		}, false},
		{"malformed without value leak", func(t *testing.T, path string) {
			put(t, path, "OK=value\nBROKEN=\"secret-sentinel\n")
		}, true},
		{"unreadable dangling symlink", func(t *testing.T, path string) {
			if err := os.Symlink(path+".missing", path); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"unreadable permissions", func(t *testing.T, path string) {
			put(t, path, "KEY=value\n")
			if err := os.Chmod(path, 0); err != nil {
				t.Fatal(err)
			}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			vars := filepath.Join(root, "vars.env")
			tc.make(t, vars)
			put(t, filepath.Join(root, "noon.yaml"), "vars_file: vars.env\njobs:\n  probe:\n    schedule: ${MISSING:-every 1h}\n    run: \"true\"\n")
			_, err := config.Load(config.Input{CWD: root})
			if tc.name == "unreadable permissions" && os.Geteuid() == 0 {
				if err != nil {
					t.Fatalf("root could not read mode-000 vars file: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), `vars_file "vars.env"`) || strings.Contains(err.Error(), "secret-sentinel") {
				t.Fatalf("vars file error = %v", err)
			}
			if tc.line && !strings.Contains(err.Error(), "line 2") {
				t.Fatalf("missing parser line: %v", err)
			}
		})
	}
}

// Discovery must not parse oversized manifests before the full loader rejects
// them, otherwise every scheduler poll can allocate an unbounded document.
func TestVarsFileDiscoveryRejectsOversizedManifest(t *testing.T) {
	root := t.TempDir()
	put(t, filepath.Join(root, "noon.yaml"), "vars_file: vars.env\n#"+strings.Repeat("x", 1024*1024))
	input := config.Input{CWD: root}
	if paths := config.VarsFilePaths(input); len(paths) != 0 {
		t.Fatalf("discovered vars files in oversized manifest: %v", paths)
	}
	if _, err := config.Load(input); err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("oversized manifest error = %v", err)
	}
}

func TestVarsFileDigestParityAcrossLoadOverrideAndJobSource(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "noon.yaml")
	varsPath := filepath.Join(root, "vars.env")
	source := []byte("vars_file: vars.env\njobs:\n  probe:\n    schedule: ${SCHEDULE}\n    run: \"true\"\n")
	put(t, inputPath, string(source))
	put(t, varsPath, "SCHEDULE=every 1h\nUNUSED=one\n")
	input := config.Input{CWD: root}
	load, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	job, err := config.JobFromSource(source, "probe", config.BuiltInAgents(), "", nil, nil, false, root)
	if err != nil || job.Digest != load.Jobs["probe"].Digest {
		t.Fatalf("JobFromSource parity: job=%+v err=%v load=%+v", job, err, load.Jobs["probe"])
	}

	rewritten := []byte(strings.Replace(string(source), "${SCHEDULE}", "${SCHEDULE:-every 1h}", 1))
	same, err := config.LoadWithFileOverride(input, inputPath, rewritten)
	if err != nil {
		t.Fatal(err)
	}
	job, err = config.JobFromSource(rewritten, "probe", config.BuiltInAgents(), "", nil, nil, false, root)
	if err != nil || job.Digest != same.Jobs["probe"].Digest || job.Digest != load.Jobs["probe"].Digest {
		t.Fatalf("expression-only rewrite changed resolution: job=%+v err=%v", job, err)
	}
	put(t, varsPath, "SCHEDULE=every 2h\nUNUSED=two\n")
	changed, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := config.LoadWithFileOverride(input, inputPath, rewritten)
	if err != nil {
		t.Fatal(err)
	}
	job, err = config.JobFromSource(rewritten, "probe", config.BuiltInAgents(), "", nil, nil, false, root)
	if err != nil || job.Digest != changed.Jobs["probe"].Digest || job.Digest != candidate.Jobs["probe"].Digest || job.Digest == load.Jobs["probe"].Digest {
		t.Fatalf("changed vars-file resolution diverged: job=%+v err=%v", job, err)
	}
}
