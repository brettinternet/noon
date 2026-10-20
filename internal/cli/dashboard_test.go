package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/service"
	"github.com/brettinternet/noon/internal/store"
	urfave "github.com/urfave/cli/v3"
)

var diagnosticNow = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

func diagnosticSandbox(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, key := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE", "NOON_STATE_DIR"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "user"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Chdir(root)
	return root
}

func diagnosticCommand(t *testing.T, doctor bool, jsonMode bool) (int, string) {
	t.Helper()
	var out, stderr bytes.Buffer
	root := NewRootCommand(config.BuildOpts{}, &out, &stderr)
	// Inject time at the command boundary, without changing production clocks.
	root.Action = func(_ context.Context, cmd *urfave.Command) error { return dashboardAt(cmd, diagnosticNow) }
	args := []string{"noon"}
	if doctor {
		for _, cmd := range root.Commands {
			if cmd.Name == "doctor" {
				cmd.Action = func(ctx context.Context, cmd *urfave.Command) error {
					return doctorAt(ctx, cmd, diagnosticNow, config.BuildOpts{})
				}
			}
		}
		args = append(args, "doctor")
	}
	if jsonMode {
		args = append(args, "--json")
	}
	code := 0
	if err := root.Run(context.Background(), args); err != nil {
		var exit runExit
		if !errors.As(err, &exit) {
			t.Fatalf("command failed: %v %s", err, &stderr)
		}
		code = int(exit)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %s", &stderr)
	}
	return code, out.String()
}

func diagnosticConfig(t *testing.T, root, manifest string) config.Config {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Input{CWD: root, UserConfigDir: filepath.Join(root, "user"), StateDirDefault: filepath.Join(root, "state", "noon")})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func diagnosticState(t *testing.T, cfg config.Config, fresh bool) *store.Store {
	t.Helper()
	if _, _, err := app.Enable(cfg, diagnosticNow); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		if err := state.SetScheduler(store.SchedulerRecord{PID: 123, StartIdentity: "test", Version: "test", StartedAt: store.FormatTime(diagnosticNow), HeartbeatAt: store.FormatTime(diagnosticNow)}); err != nil {
			t.Fatal(err)
		}
	}
	return state
}

// Build mismatch must be visible alongside genuine config errors, without changing state.
func TestDoctorSchedulerBuild(t *testing.T) {
	root := diagnosticSandbox(t)
	cfg := diagnosticConfig(t, root, "version: 1\njobs: {}\n")
	state := diagnosticState(t, cfg, false)
	identity, err := runner.ProcessStartIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	record := store.SchedulerRecord{PID: os.Getpid(), StartIdentity: identity, Version: "1.0.0", Commit: "oldcommit", StartedAt: store.FormatTime(diagnosticNow), HeartbeatAt: store.FormatTime(diagnosticNow)}
	if err := state.SetScheduler(record); err != nil {
		t.Fatal(err)
	}
	projects, err := state.ProjectRecords()
	if err != nil || len(projects) != 1 {
		t.Fatalf("projects=%v err=%v", projects, err)
	}
	if err := state.SetProjectConfigError(projects[0].ID, "old scheduler rejected config"); err != nil {
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(cfg.StateDir, "noon.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, jsonMode := range []bool{false, true} {
		var out, stderr bytes.Buffer
		command := NewRootCommand(config.BuildOpts{}, &out, &stderr)
		for _, cmd := range command.Commands {
			if cmd.Name == "doctor" {
				cmd.Action = func(ctx context.Context, cmd *urfave.Command) error {
					return doctorAt(ctx, cmd, diagnosticNow, config.BuildOpts{Version: "1.1.0", Commit: "newcommit"})
				}
			}
		}
		args := []string{"noon", "doctor"}
		if jsonMode {
			args = append(args, "--json")
		}
		_ = command.Run(t.Context(), args)
		for _, want := range []string{"scheduler_version_mismatch", "scheduler_config", "oldcommit", "newcommit", "service install"} {
			if !strings.Contains(out.String(), want) {
				t.Fatalf("missing %q: %s %s", want, &out, &stderr)
			}
		}
		_, dashboard := diagnosticCommand(t, false, jsonMode)
		if !strings.Contains(dashboard, "oldcommit") || !strings.Contains(dashboard, "1.0.0") {
			t.Fatalf("dashboard missing build: %s", dashboard)
		}
	}
	after, err := os.ReadFile(filepath.Join(cfg.StateDir, "noon.db"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("read-only diagnostics changed state: %v", err)
	}
}

func TestDashboard(t *testing.T) {
	t.Run("copy guidance across directories", func(t *testing.T) {
		root := diagnosticSandbox(t)
		project := filepath.Join(root, "project '$(touch injected); space")
		state := filepath.Join(root, "state '$(touch injected); space")
		file := filepath.Join(root, "config '$(touch injected); space.yaml")
		if err := os.MkdirAll(project, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("jobs:\n  check:\n    run: [printf, 'keep-one\\nignore\\nkeep-two\\n']\n"), 0600); err != nil {
			t.Fatal(err)
		}
		invoke := func(args ...string) (string, string) {
			t.Helper()
			var out, stderr bytes.Buffer
			if code := Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr); code != 0 {
				t.Fatalf("exit %d: %s %s", code, &out, &stderr)
			}
			return out.String(), stderr.String()
		}
		selectors := []string{"-C", filepath.Base(project), "--config", filepath.Base(file), "--state-dir", filepath.Base(state), "--timezone", "UTC"}
		invoke(append(selectors, "run", "check")...)
		_, hint := invoke(append(selectors, "logs", "check", "--section", "job", "--match", "^keep|noon run", "--context", "0", "--after-cursor", "0", "--limit-bytes", "11")...)
		command := strings.TrimSuffix(strings.TrimPrefix(hint, "Continue: "), "\n")
		other := filepath.Join(root, "other")
		if err := os.Mkdir(other, 0700); err != nil {
			t.Fatal(err)
		}
		t.Chdir(other)
		// A shell function captures the actual argv after shell expansion, then
		// the public CLI executes those exact arguments from the new cwd.
		shell := exec.Command("sh", "-c", "noon() { printf '%s\\000' \"$@\"; }; "+command)
		argv, err := shell.Output()
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Split(strings.TrimSuffix(string(argv), "\x00"), "\x00")
		out, _ := invoke(args...)
		if out != "3:keep-two\n" {
			t.Fatalf("resumed wrong selection: %q; command %s", out, command)
		}
		for _, dir := range []string{root, other, project} {
			if _, err := os.Stat(filepath.Join(dir, "injected")); !os.IsNotExist(err) {
				t.Fatalf("shell metacharacters executed in %s: %v", dir, err)
			}
		}
		t.Chdir(root)
		for _, pair := range [][2]string{{"NOON_PROJECT", project}, {"NOON_CONFIG", file}, {"NOON_STATE_DIR", state}, {"NOON_TIMEZONE", "UTC"}} {
			t.Setenv(pair[0], pair[1])
		}
		invoke("enable")
		out, _ = invoke("--json")
		var dashboard struct{ Next string }
		if err := json.Unmarshal([]byte(out), &dashboard); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(dashboard.Next, "env -u NOON_PROJECT -u NOON_CONFIG -u NOON_TIMEZONE noon service install --state-dir ") || strings.Contains(dashboard.Next, "--project") || strings.Contains(dashboard.Next, "--config") || strings.Contains(dashboard.Next, "--timezone") {
			t.Fatalf("unsupported service selectors: %s", dashboard.Next)
		}
		// Verify the environment-clearing command through the same shell, but
		// replace noon with a harmless executable that records argv.
		bin := filepath.Join(root, "bin")
		if err := os.Mkdir(bin, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(bin, "noon"), []byte("#!/bin/sh\nprintf '%s\\000' \"$@\"\n"), 0700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		argv, err = exec.Command("sh", "-c", dashboard.Next+" --print").Output()
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE"} {
			if err := os.Unsetenv(key); err != nil {
				t.Fatal(err)
			}
		}
		out, _ = invoke(strings.Split(strings.TrimSuffix(string(argv), "\x00"), "\x00")...)
		if !strings.Contains(out, "serve") {
			t.Fatalf("not a printed service unit: %s", out)
		}
	})
	for _, tc := range []struct {
		name           string
		enabled, fresh bool
		notices        []string
		next           string
	}{
		{"empty", false, false, []string{}, "noon add"},
		{"not enabled", false, false, []string{"not_enabled"}, "noon enable"},
		{"needs approval", true, true, []string{"needs_approval"}, "noon enable"},
		{"offline", true, false, []string{"scheduler_offline"}, "noon service install"},
		{"installed offline", true, false, []string{"scheduler_offline"}, "noon serve"},
		{"fresh", true, true, []string{}, "noon run t"},
		{"paused first", true, true, []string{}, "noon run u"},
		{"all paused", true, true, []string{}, "noon add"},
		{"recorded config error", true, true, []string{"config_error"}, "noon config check"},
		{"invalid config", false, false, []string{"config_error"}, "noon config check"},
		{"queued", true, true, []string{"queued"}, "noon run t"},
		{"orphaned", true, true, []string{"orphaned"}, "noon cancel 1"},
		{"offline orphan", true, false, []string{"scheduler_offline", "orphaned", "queued"}, "noon service install"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := diagnosticSandbox(t)
			manifest := "timezone: UTC\njobs:\n  t:\n    schedule: every 1h\n    run: [/bin/sh, -c, 'true']\n"
			if tc.name == "empty" {
				manifest = "timezone: UTC\n"
			}
			if tc.name == "paused first" {
				manifest += "  u:\n    schedule: every 1h\n    run: [/bin/sh, -c, 'true']\n"
			}
			cfg := diagnosticConfig(t, root, manifest)
			if tc.enabled {
				state := diagnosticState(t, cfg, tc.fresh)
				id, err := state.RegisterProject(cfg.Project, diagnosticNow)
				if err != nil {
					t.Fatal(err)
				}
				if tc.name == "paused first" || tc.name == "all paused" {
					if err := state.SetPaused(cfg.Project, "t", true, diagnosticNow); err != nil {
						t.Fatal(err)
					}
				}
				if tc.name == "recorded config error" {
					if err := state.SetProjectConfigError(id, "noon.yaml: invalid schedule"); err != nil {
						t.Fatal(err)
					}
				}
				if tc.name == "queued" || tc.name == "offline orphan" {
					if _, err := state.QueueRun(store.QueueRunInput{ProjectID: id, Job: "t", Digest: cfg.Jobs["t"].Digest, JobExists: true, JobEnabled: true, JobShared: true, Now: diagnosticNow, DueAt: diagnosticNow}); err != nil {
						t.Fatal(err)
					}
				}
				if tc.name == "orphaned" || tc.name == "offline orphan" {
					for _, name := range []string{"removed-low", "removed-high"} {
						if _, err := state.CreateRun(store.NewRun{ProjectID: id, Job: name, Trigger: "manual", Digest: "test", Now: diagnosticNow, OwnerPID: os.Getpid(), OwnerStart: "stale-owner-identity"}); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := state.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "needs approval" {
				diagnosticConfig(t, root, strings.ReplaceAll(manifest, "true", "false"))
			}
			if tc.name == "invalid config" {
				if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte("jobs: [invalid]"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "installed offline" {
				options, err := service.CurrentConfig(cfg.StateDir)
				if err != nil {
					t.Fatal(err)
				}
				unit, err := service.CurrentUnit(options)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(unit.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(unit.Path, unit.Contents, 0600); err != nil {
					t.Fatal(err)
				}
			}
			code, out := diagnosticCommand(t, false, true)
			var result struct {
				app.DashboardView
				SchemaVersion int `json:"schema_version"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			codes := []string{}
			for _, notice := range result.Notices {
				codes = append(codes, notice.Code)
			}
			if code != 0 || result.SchemaVersion != 1 || !reflect.DeepEqual(codes, tc.notices) || result.Next != tc.next {
				t.Fatalf("exit %d: %s", code, out)
			}
			if tc.name == "orphaned" {
				message := result.Notices[0].Message
				for _, part := range []string{"#1 removed-low", "#2 removed-high", "noon cancel 1", "block background runs in this project"} {
					if !strings.Contains(message, part) {
						t.Fatalf("orphan notice omitted %q: %s", part, message)
					}
				}
			}
			_, human := diagnosticCommand(t, false, false)
			if strings.Count(human, "Suggested action:") != 1 {
				t.Fatal(human)
			}
			if tc.name == "orphaned" {
				if !strings.Contains(human, "removed-low") || !strings.Contains(human, "removed-high") || !strings.Contains(human, "Suggested action: noon cancel 1") {
					t.Fatalf("orphaned dashboard omitted removed jobs or cancel guidance:\n%s", human)
				}
				// Copyable cancel commands in the notice must target the same state as Next.
				t.Setenv("NOON_STATE_DIR", cfg.StateDir)
				_, out := diagnosticCommand(t, false, true)
				var selected app.DashboardView
				if err := json.Unmarshal([]byte(out), &selected); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(selected.Next, "--state-dir") || len(selected.Notices) == 0 || !strings.Contains(selected.Notices[0].Message, "("+selected.Next+")") {
					t.Fatalf("orphan notice dropped state selector: %s", out)
				}
			}
			for _, line := range strings.Split(human, "\n") {
				if len([]rune(line)) > 80 {
					t.Fatalf("wide dashboard line: %q", line)
				}
			}
		})
	}
}

func diagnosticFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			files[path] = "directory"
			return nil
		}
		// SQLite coordination is the sole operator-approved filesystem exception.
		if path == filepath.Join(root, "state", "noon", "noon.db-wal") || path == filepath.Join(root, "state", "noon", "noon.db-shm") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[path] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestDoctor(t *testing.T) {
	for _, healthy := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing command", true: "healthy"}[healthy], func(t *testing.T) {
			root := diagnosticSandbox(t)
			command := "definitely-not-a-command-xyz"
			if healthy {
				command = "/bin/sh"
			}
			// A disabled job cannot run, so its missing command must not fail doctor.
			cfg := diagnosticConfig(t, root, "timezone: UTC\njobs:\n  t:\n    schedule: every 1h\n    run: ["+command+"]\n  off:\n    enabled: false\n    run: [definitely-not-a-command-xyz]\n")
			if healthy {
				state := diagnosticState(t, cfg, true)
				if err := state.Close(); err != nil {
					t.Fatal(err)
				}
			}
			before := diagnosticFiles(t, root)
			code, out := diagnosticCommand(t, true, true)
			var result app.DoctorView
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			if result.OK != healthy || code != map[bool]int{false: 1, true: 0}[healthy] {
				t.Fatalf("exit %d: %s", code, out)
			}
			found := false
			for _, check := range result.Checks {
				if check.Name == "job t command" {
					found = true
					if !healthy && (check.Status != "fail" || !strings.Contains(check.Message, command)) {
						t.Fatal(check)
					}
				}
			}
			if !found {
				t.Fatal(out)
			}
			if after := diagnosticFiles(t, root); !reflect.DeepEqual(before, after) {
				t.Fatalf("doctor changed files: before keys=%v after keys=%v", fileNames(before), fileNames(after))
			}
		})
	}
	t.Run("job PATH cwd and prompt", func(t *testing.T) {
		root := diagnosticSandbox(t)
		if err := os.Mkdir(filepath.Join(root, "work"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "work", "tool"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
		diagnosticConfig(t, root, "jobs:\n  t:\n    cwd: work\n    env: {PATH: .}\n    run: [tool]\n  prompt:\n    agent: codex\n    prompt_file: absent.txt\n")
		_, out := diagnosticCommand(t, true, true)
		var result app.DoctorView
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		seen := map[string]string{}
		for _, check := range result.Checks {
			seen[check.Name] = check.Status
		}
		if seen["job t command"] != "ok" || seen["job prompt prompt_file"] != "fail" {
			t.Fatal(out)
		}
	})
}

func fileNames(files map[string]string) []string {
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	return names
}
