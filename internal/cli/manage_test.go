package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/scheduler"
	"github.com/brettinternet/noon/internal/store"
)

func TestAddJobs(t *testing.T) {
	t.Run("each action form serializes and validates", func(t *testing.T) {
		for _, tc := range []struct {
			name               string
			args               []string
			want               string
			job                string
			run                any
			prompt, promptFile *string
			schedule           string
		}{
			{"argv", []string{"add", "tests", "every 30m", "--", "go", "test", "./..."}, "run:", "tests", []string{"go", "test", "./..."}, nil, nil, "every 30m"},
			{"schedule-like argv", []string{"add", "tests", "--", "daily", "--json"}, "run:", "tests", []string{"daily", "--json"}, nil, nil, ""},
			{"delimiter as flag value", []string{"add", "tests", "--description", "--", "--", "daily"}, "run:", "tests", []string{"daily"}, nil, nil, ""},
			{"shell", []string{"add", "lint", "every 30m", "--sh", "golangci-lint run ./..."}, "run:", "lint", "golangci-lint run ./...", nil, nil, "every 30m"},
			{"prompt", []string{"add", "review", "weekdays at 9am", "--agent", "claude", "--prompt", "Review open PRs"}, "prompt:", "review", nil, ptr("Review open PRs"), nil, "weekdays at 09:00"},
			{"prompt file", []string{"add", "notes", "--agent", "claude", "--prompt-file", "prompts/notes.md"}, "prompt_file:", "notes", nil, nil, ptr("prompts/notes.md"), ""},
		} {
			t.Run(tc.name, func(t *testing.T) {
				root := sandbox(t)
				code, out, stderr := invokeText(t, tc.args...)
				if code != 0 {
					t.Fatalf("add: %d %s", code, stderr)
				}
				data, err := os.ReadFile(filepath.Join(root, ".noon.yaml"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(data), tc.want) {
					t.Fatalf("expected %q in config: %s", tc.want, data)
				}
				loaded, err := config.Load(config.Input{CWD: root})
				if err != nil {
					t.Fatalf("written config invalid: %v", err)
				}
				got := loaded.Jobs[tc.job]
				if !reflect.DeepEqual(got.Run, tc.run) || !reflect.DeepEqual(got.Prompt, tc.prompt) || !reflect.DeepEqual(got.PromptFile, tc.promptFile) {
					t.Fatalf("action fields: %+v", got)
				}
				if tc.schedule == "" && got.Schedule != nil || tc.schedule != "" && (got.Schedule == nil || got.Schedule.String() != tc.schedule) {
					t.Fatalf("schedule: %+v", got.Schedule)
				}
				if tc.name == "argv" && !strings.Contains(out, "every 30m") {
					t.Fatalf("normalized schedule absent: %s", out)
				}
			})
		}
	})

	t.Run("invalid schedule preserves bytes and suggests a correction", func(t *testing.T) {
		root := sandbox(t)
		path := filepath.Join(root, ".noon.yaml")
		original := []byte("# preserve exact bytes\nversion: 1\n")
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		for _, invalid := range []string{"every day", "nonsense"} {
			code, _, stderr := invokeText(t, "add", "bad", invalid, "--", "true")
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if code != 2 || !strings.Contains(stderr, "every day at 9am") || string(got) != string(original) {
				t.Fatalf("invalid schedule: exit=%d stderr=%q bytes=%q", code, stderr, got)
			}
		}
	})

	t.Run("collision and explicit config are refused", func(t *testing.T) {
		root := sandbox(t)
		path := filepath.Join(root, ".noon.yaml")
		original := []byte("jobs:\n  tests:\n    run: 'true'\n")
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		code, _, stderr := invokeText(t, "add", "tests", "--", "true")
		if code != 1 || !strings.Contains(stderr, "exists") || !strings.Contains(stderr, "noon remove tests") {
			t.Fatalf("collision: %d %s", code, stderr)
		}
		custom := filepath.Join(root, "custom.yaml")
		customBytes := []byte("version: 1\n")
		if err := os.WriteFile(custom, customBytes, 0600); err != nil {
			t.Fatal(err)
		}
		code, result := invoke(t, "--json", "--config", custom, "add", "other", "--", "true")
		got, err := os.ReadFile(custom)
		if err != nil {
			t.Fatal(err)
		}
		failure := result["error"].(map[string]any)
		if code != 2 || failure["code"] != "explicit_config" || string(got) != string(customBytes) {
			t.Fatalf("explicit config: %d %+v %q", code, result, got)
		}
		got, err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(original) {
			t.Fatal("collision changed target")
		}
	})

	t.Run("new file modes and scheduled enable", func(t *testing.T) {
		root := sandbox(t)
		if code, _, stderr := invokeText(t, "add", "local", "every 1h", "--sh", "true"); code != 0 {
			t.Fatalf("local add: %d %s", code, stderr)
		}
		localInfo, err := os.Stat(filepath.Join(root, ".noon.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if localInfo.Mode().Perm() != 0600 {
			t.Fatalf("private mode=%o", localInfo.Mode().Perm())
		}
		localPath := filepath.Join(root, ".noon.yaml")
		if err := os.Chmod(localPath, 0640); err != nil {
			t.Fatal(err)
		}
		if code, _, stderr := invokeText(t, "add", "second", "--sh", "true"); code != 0 {
			t.Fatalf("second add: %d %s", code, stderr)
		}
		localInfo, err = os.Stat(localPath)
		if err != nil {
			t.Fatal(err)
		}
		if localInfo.Mode().Perm() != 0640 {
			t.Fatalf("existing private mode changed to %o", localInfo.Mode().Perm())
		}
		cfg, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatal(err)
		}
		state, err := store.OpenExisting(cfg.StateDir)
		if err != nil {
			t.Fatal(err)
		}
		approval, err := state.ApprovalForProject(cfg.Project)
		_ = state.Close()
		if err != nil || !approval.Enabled {
			t.Fatalf("scheduled add did not enable project: %+v %v", approval, err)
		}
		root = sandbox(t)
		if code, _, stderr := invokeText(t, "add", "shared", "every 1h", "--sh", "true", "--shared"); code != 0 {
			t.Fatalf("shared add: %d %s", code, stderr)
		}
		sharedInfo, err := os.Stat(filepath.Join(root, "noon.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		if sharedInfo.Mode().Perm() != 0644 {
			t.Fatalf("shared mode=%o", sharedInfo.Mode().Perm())
		}
	})

	t.Run("local ignore warning respects git check-ignore", func(t *testing.T) {
		root := sandbox(t)
		if err := exec.Command("git", "-C", root, "init", "-q").Run(); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := invokeText(t, "add", "local", "--sh", "true")
		if code != 0 {
			t.Fatalf("add: %d %s", code, stderr)
		}
		if !strings.Contains(out, ".gitignore: .noon.yaml") {
			t.Fatalf("missing ignore reminder: %s", out)
		}
		root = sandbox(t)
		if err := exec.Command("git", "-C", root, "init", "-q").Run(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".noon.yaml\n"), 0644); err != nil {
			t.Fatal(err)
		}
		code, out, stderr = invokeText(t, "add", "local", "--sh", "true")
		if code != 0 {
			t.Fatalf("ignored add: %d %s", code, stderr)
		}
		if strings.Contains(out, ".gitignore") {
			t.Fatalf("warned for ignored file: %s", out)
		}
	})

	t.Run("only the added shared job is approved", func(t *testing.T) {
		root := sandbox(t)
		manifest := []byte("jobs:\n  first:\n    run: first\n  changed:\n    run: old\n")
		path := filepath.Join(root, "noon.yaml")
		if err := os.WriteFile(path, manifest, 0644); err != nil {
			t.Fatal(err)
		}
		if code, _, stderr := invokeText(t, "enable"); code != 0 {
			t.Fatalf("enable: %d %s", code, stderr)
		}
		modified := strings.Replace(string(manifest), "run: old", "run: new", 1)
		if err := os.WriteFile(path, []byte(modified), 0644); err != nil {
			t.Fatal(err)
		}
		if code, _, stderr := invokeText(t, "add", "newjob", "every 1h", "--sh", "new command", "--shared"); code != 0 {
			t.Fatalf("shared add: %d %s", code, stderr)
		}
		cfg, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatal(err)
		}
		view, err := app.Inspect(config.Input{CWD: root}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		view, err = app.WithApprovals(view)
		if err != nil {
			t.Fatal(err)
		}
		approvals := map[string]string{}
		for _, job := range view.Jobs {
			approvals[job.Name] = job.Approval
		}
		if approvals["newjob"] != "approved" || approvals["changed"] != "needs_approval" || approvals["first"] != "approved" {
			t.Fatalf("selective approvals: %+v", approvals)
		}
		_ = cfg
	})

	t.Run("local duplicate overrides shared and is disclosed", func(t *testing.T) {
		root := sandbox(t)
		shared := []byte("jobs:\n  tests:\n    run: shared\n")
		if err := os.WriteFile(filepath.Join(root, "noon.yaml"), shared, 0644); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := invokeText(t, "add", "tests", "--sh", "local command")
		if code != 0 || !strings.Contains(out, "overrides the shared definition") {
			t.Fatalf("local override note: %d %q %q", code, out, stderr)
		}
		cfg, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Jobs["tests"].Run != "local command" {
			t.Fatalf("local override ineffective: %+v", cfg.Jobs["tests"])
		}
	})

	// These regressions guard approval of the written shared definition rather
	// than its local override, and preservation of hidden raw scalar values.
	t.Run("shared duplicate approves raw job and discloses local override", func(t *testing.T) {
		root := sandbox(t)
		if err := os.WriteFile(filepath.Join(root, ".noon.yaml"), []byte("jobs:\n  tests:\n    run: local\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte("jobs:\n  other:\n    run: other\n"), 0644); err != nil {
			t.Fatal(err)
		}
		code, out, stderr := invokeText(t, "add", "tests", "every 1h", "--shared", "--sh", "shared")
		if code != 0 || !strings.Contains(out, "overrides the shared definition") {
			t.Fatalf("shared override: %d %q %q", code, out, stderr)
		}
		if code, _, stderr := invokeText(t, "remove", "tests", "--local"); code != 0 {
			t.Fatalf("remove override: %d %s", code, stderr)
		}
		code, result := invoke(t, "list", "--json")
		if code != 0 {
			t.Fatalf("list: %d %+v", code, result)
		}
		approvals := map[string]string{}
		for _, item := range result["jobs"].([]any) {
			job := item.(map[string]any)
			approvals[job["name"].(string)] = job["approval"].(string)
		}
		if approvals["tests"] != "approved" || approvals["other"] != "needs_approval" {
			t.Fatalf("raw shared approval: %+v", approvals)
		}
	})

	t.Run("shadowed raw scalar is preserved or edit fails before writing", func(t *testing.T) {
		for _, tc := range []struct {
			name, source string
			wantCode     int
		}{
			{"trailing blanks", "jobs:\n  tests:\n    run: |+\n      echo keep\n\n\n", 0},
			{"unterminated action", "jobs:\n  tests:\n    run: |+\n      echo keep", 1},
			{"unterminated description", "jobs:\n  tests:\n    run: echo\n    description: |+\n      keep", 1},
		} {
			t.Run(tc.name, func(t *testing.T) {
				root := sandbox(t)
				sharedPath := filepath.Join(root, "noon.yaml")
				original := []byte(tc.source)
				if err := os.WriteFile(sharedPath, original, 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, ".noon.yaml"), []byte("jobs:\n  tests:\n    run: local\n"), 0600); err != nil {
					t.Fatal(err)
				}
				code, result := invoke(t, "--json", "add", "added", "--shared", "--sh", "true")
				if code != tc.wantCode {
					t.Fatalf("edit: %d %+v", code, result)
				}
				got, err := os.ReadFile(sharedPath)
				if err != nil {
					t.Fatal(err)
				}
				if code != 0 {
					if result["error"].(map[string]any)["code"] != "config_edit" || !bytes.Equal(got, original) {
						t.Fatalf("unsafe edit not refused: %+v %q", result, got)
					}
					return
				}
				before, err := config.JobFromSource(original, "tests", nil, "")
				if err != nil {
					t.Fatal(err)
				}
				after, err := config.JobFromSource(got, "tests", nil, "")
				if err != nil || before.Digest != after.Digest {
					t.Fatalf("hidden raw action changed: before=%+v after=%+v err=%v", before, after, err)
				}
			})
		}
	})

	t.Run("finite management JSON is one object", func(t *testing.T) {
		sandbox(t)
		for _, args := range [][]string{
			{"add"}, {"remove"}, {"pause"}, {"resume"},
			{"add", "tests", "every 1h", "--sh", "true"},
			{"remove"}, {"pause"}, {"resume"},
			{"pause", "tests"}, {"resume", "tests"}, {"remove", "tests"},
		} {
			code, out, stderr := invokeText(t, append([]string{"--json"}, args...)...)
			var result map[string]any
			if code != 0 || stderr != "" || json.Unmarshal([]byte(out), &result) != nil || result["schema_version"] != float64(1) {
				t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, out, stderr)
			}
		}
	})

	t.Run("--in queues the new job", func(t *testing.T) {
		root := sandbox(t)
		if code, _, stderr := invokeText(t, "add", "later", "--in", "2h", "--", "echo", "hi"); code != 0 {
			t.Fatalf("queued add: %d %s", code, stderr)
		}
		cfg, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatal(err)
		}
		queued, err := app.QueuedRuns(cfg.StateDir, cfg.Project)
		if err != nil {
			t.Fatal(err)
		}
		if len(queued) != 1 || queued[0].Job != "later" {
			t.Fatalf("queued runs: %+v", queued)
		}
	})
}

func TestRemoveJobs(t *testing.T) {
	root := sandbox(t)
	sharedPath, localPath := filepath.Join(root, "noon.yaml"), filepath.Join(root, ".noon.yaml")
	shared := []byte("jobs:\n  tests:\n    run: shared\n")
	local := []byte("jobs:\n  tests:\n    run: local\n")
	if err := os.WriteFile(sharedPath, shared, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localPath, local, 0600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := invokeText(t, "remove", "tests")
	if code != 2 || !strings.Contains(stderr, "both") || !strings.Contains(stderr, "--shared or --local") {
		t.Fatalf("ambiguous removal: %d %s", code, stderr)
	}
	if code, _, stderr = invokeText(t, "remove", "--local", "tests"); code != 0 {
		t.Fatalf("local removal: %d %s", code, stderr)
	}
	if got, err := os.ReadFile(sharedPath); err != nil || !bytes.Equal(got, shared) {
		t.Fatalf("shared file changed: %q %v", got, err)
	}
	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("local file removed: %v", err)
	}
	if code, _, stderr = invokeText(t, "rm", "--shared", "tests"); code != 0 {
		t.Fatalf("shared alias removal: %d %s", code, stderr)
	}
	remaining, err := os.ReadFile(sharedPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(remaining), "tests:") {
		t.Fatalf("shared job remained: %s", remaining)
	}

	custom := filepath.Join(root, "custom.yaml")
	customBytes := []byte("version: 1\n")
	if err := os.WriteFile(custom, customBytes, 0600); err != nil {
		t.Fatal(err)
	}
	code, result := invoke(t, "--json", "--config", custom, "remove", "tests")
	failure := result["error"].(map[string]any)
	if code != 2 || failure["code"] != "explicit_config" {
		t.Fatalf("explicit remove: %d %+v", code, result)
	}
}

func TestPauseResume(t *testing.T) {
	t.Run("paused jobs are not dispatched and resume starts from now", func(t *testing.T) {
		f := newPauseFixture(t)
		defer f.stop(t)
		f.step(t, fixtureStart)
		if err := app.SetPaused(f.cfg.StateDir, f.cfg.Project, "tests", true, fixtureStart); err != nil {
			t.Fatal(err)
		}
		f.step(t, fixtureStart.Add(time.Minute))
		if _, err := os.Stat(f.marker); !os.IsNotExist(err) {
			t.Fatalf("paused job dispatched: %v", err)
		}
		listed, err := app.Inspect(config.Input{CWD: f.cfg.Project}, fixtureStart.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		listed, err = app.WithApprovals(listed)
		if err != nil {
			t.Fatal(err)
		}
		if !listed.Jobs[0].Paused {
			t.Fatal("paused state not exposed to list")
		}
		code, dashboard, dashboardErr := invokeText(t)
		if code != 0 || !strings.Contains(dashboard, "paused") {
			t.Fatalf("bare dashboard paused state: %d %s %s", code, dashboard, dashboardErr)
		}
		code, jsonResult := invoke(t, "--json", "list")
		if code != 0 {
			t.Fatalf("JSON list: %d %+v", code, jsonResult)
		}
		jsonJobs := jsonResult["jobs"].([]any)
		if jsonJobs[0].(map[string]any)["paused"] != true {
			t.Fatalf("JSON paused flag missing: %+v", jsonJobs)
		}
		resumeAt := fixtureStart.Add(5 * time.Minute)
		if err := app.SetPaused(f.cfg.StateDir, f.cfg.Project, "tests", false, resumeAt); err != nil {
			t.Fatal(err)
		}
		f.step(t, resumeAt)
		if _, err := os.Stat(f.marker); !os.IsNotExist(err) {
			t.Fatalf("resume caught up immediately: %v", err)
		}
		f.step(t, resumeAt.Add(time.Minute))
		waitForMarker(t, f.marker)
	})

	t.Run("pause and resume while stopped does not catch up", func(t *testing.T) {
		f := newPauseFixture(t)
		f.step(t, fixtureStart)
		f.stop(t)
		resumeAt := fixtureStart.Add(5 * time.Minute)
		if err := app.SetPaused(f.cfg.StateDir, f.cfg.Project, "tests", true, resumeAt); err != nil {
			t.Fatal(err)
		}
		if err := app.SetPaused(f.cfg.StateDir, f.cfg.Project, "tests", false, resumeAt); err != nil {
			t.Fatal(err)
		}
		f.clock.Store(resumeAt.UnixNano())
		f.start(t)
		defer f.stop(t)
		f.step(t, resumeAt)
		if _, err := os.Stat(f.marker); !os.IsNotExist(err) {
			t.Fatalf("stopped-loop resume caught up: %v", err)
		}
		f.step(t, resumeAt.Add(time.Minute))
		waitForMarker(t, f.marker)
	})
}

var fixtureStart = time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)

type pauseFixture struct {
	t      *testing.T
	cfg    config.Config
	marker string
	clock  atomic.Int64
	ticks  chan time.Time
	ack    chan struct{}
	cancel context.CancelFunc
	done   chan error
}

func newPauseFixture(t *testing.T) *pauseFixture {
	t.Helper()
	root := sandbox(t)
	marker := filepath.Join(root, "ran")
	manifest := []byte("jobs:\n  tests:\n    schedule: every 1m\n    run: [touch, " + strconv.Quote(marker) + "]\n")
	if err := os.WriteFile(filepath.Join(root, ".noon.yaml"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Input{CWD: root})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.Enable(cfg, fixtureStart); err != nil {
		t.Fatal(err)
	}
	f := &pauseFixture{t: t, cfg: cfg, marker: marker, ticks: make(chan time.Time), ack: make(chan struct{}, 8), done: make(chan error, 1)}
	f.clock.Store(fixtureStart.UnixNano())
	f.start(t)
	return f
}
func (f *pauseFixture) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	go func() {
		f.done <- scheduler.Serve(ctx, scheduler.ServeOptions{StateDir: f.cfg.StateDir, StateDirDefault: f.cfg.StateDir, UserConfigDir: os.Getenv("XDG_CONFIG_HOME"), Version: "test", Environment: os.Environ(), Now: func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }, Ticks: f.ticks, KillGrace: 20 * time.Millisecond, Logger: io.Discard, IgnoreSignals: true, OnTick: func() { f.ack <- struct{}{} }})
	}()
}
func (f *pauseFixture) step(t *testing.T, now time.Time) {
	t.Helper()
	f.clock.Store(now.UnixNano())
	f.ticks <- now
	select {
	case <-f.ack:
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not acknowledge injected tick")
	}
}
func (f *pauseFixture) stop(t *testing.T) {
	t.Helper()
	if f.cancel == nil {
		return
	}
	f.cancel()
	select {
	case err := <-f.done:
		if err != nil {
			t.Fatalf("scheduler stopped: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not stop")
	}
	f.cancel = nil
}
func waitForMarker(t *testing.T, path string) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("run marker %s not created", path)
		}
	}
}

func ptr(value string) *string { return &value }

func invokeText(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, stderr strings.Builder
	code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr)
	return code, out.String(), stderr.String()
}
