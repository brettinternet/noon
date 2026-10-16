package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
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
				cmd.Action = func(ctx context.Context, cmd *urfave.Command) error { return doctorAt(ctx, cmd, diagnosticNow) }
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

func TestDashboard(t *testing.T) {
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
		{"recorded config error", true, true, []string{"config_error"}, "noon config check"},
		{"invalid config", false, false, []string{"config_error"}, "noon config check"},
		{"queued", true, true, []string{"queued"}, "noon run t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := diagnosticSandbox(t)
			manifest := "timezone: UTC\njobs:\n  t:\n    schedule: every 1h\n    run: [/bin/sh, -c, 'true']\n"
			if tc.name == "empty" {
				manifest = "timezone: UTC\n"
			}
			cfg := diagnosticConfig(t, root, manifest)
			if tc.enabled {
				state := diagnosticState(t, cfg, tc.fresh)
				id, err := state.RegisterProject(cfg.Project, diagnosticNow)
				if err != nil {
					t.Fatal(err)
				}
				if tc.name == "recorded config error" {
					if err := state.SetProjectConfigError(id, "noon.yaml: invalid schedule"); err != nil {
						t.Fatal(err)
					}
				}
				if tc.name == "queued" {
					if _, err := state.QueueRun(store.QueueRunInput{ProjectID: id, Job: "t", Digest: cfg.Jobs["t"].Digest, JobExists: true, JobEnabled: true, JobShared: true, Now: diagnosticNow, DueAt: diagnosticNow}); err != nil {
						t.Fatal(err)
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
			_, human := diagnosticCommand(t, false, false)
			if strings.Count(human, "Next:") != 1 {
				t.Fatal(human)
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
			cfg := diagnosticConfig(t, root, "timezone: UTC\njobs:\n  t:\n    schedule: every 1h\n    run: ["+command+"]\n")
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
