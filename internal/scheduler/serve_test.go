package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

var serveTestStart = time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)

type serveFixture struct {
	root, project, stateDir, userConfig string
	stateDirDefault                     string
	listenAddr                          string
	environment                         []string
	logger                              io.Writer
	clock                               atomic.Int64
	ticks                               chan time.Time
	ack                                 chan struct{}
	cancel                              context.CancelFunc
	done                                chan error
}

// A newly selected config must not inherit local execution permission from the
// old selection, and a restored checkout must recover without touching its
// external config file.
func TestServeConfigSelectionRecovery(t *testing.T) {
	fixture := newServeFixture(t, scheduledManifest("every 1m", "true"))
	defer fixture.stop(t)
	fixture.step(t, serveTestStart)
	explicit := filepath.Join(fixture.root, "external.yaml")
	if err := os.WriteFile(explicit, []byte("jobs: [invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if err := state.EnableProject(fixture.project, &explicit, nil, serveTestStart); err != nil {
		t.Fatal(err)
	}
	fixture.step(t, serveTestStart.Add(time.Minute))
	if runs := fixture.runs(t); len(runs) != 0 {
		t.Fatalf("old local configuration dispatched after selection changed: %+v", runs)
	}
	fixture.stop(t)
	if err := os.WriteFile(explicit, []byte(scheduledManifest("every 1m", "true")), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Input{CWD: fixture.project, Project: &fixture.project, File: &explicit, StateDirDefault: fixture.stateDir})
	if err != nil {
		t.Fatal(err)
	}
	digests := map[string]string{}
	for name, job := range cfg.Jobs {
		digests[name] = job.Digest
	}
	if err := state.EnableProject(fixture.project, &explicit, digests, serveTestStart); err != nil {
		t.Fatal(err)
	}
	moved := fixture.project + "-moved"
	if err := os.Rename(fixture.project, moved); err != nil {
		t.Fatal(err)
	}
	fixture.start(t)
	fixture.step(t, serveTestStart.Add(2*time.Minute))
	if fixture.configError(t) == "" {
		t.Fatal("missing checkout did not record config error")
	}
	if err := os.Rename(moved, fixture.project); err != nil {
		t.Fatal(err)
	}
	fixture.step(t, serveTestStart.Add(2*time.Minute+2*time.Second))
	if fixture.configError(t) != "" || len(fixture.states(t)) != 1 {
		t.Fatal("restored checkout did not reload unchanged external config")
	}
	fixture.step(t, serveTestStart.Add(3*time.Minute+2*time.Second))
	fixture.stop(t)
	if runs := fixture.runs(t); len(runs) != 1 {
		t.Fatalf("restored checkout runs: %+v", runs)
	}
}

func TestServeLifecycleHooks(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "hook.json")
	hookCommand := "cat > " + shellQuote(marker)
	manifest := fmt.Sprintf("jobs:\n  tests:\n    schedule: every 1m\n    run: 'true'\n    on_success: %s\n", strconv.Quote(hookCommand))
	fixture := newServeFixture(t, manifest)
	defer fixture.stop(t)
	fixture.step(t, serveTestStart)
	fixture.step(t, serveTestStart.Add(time.Minute))
	waitFor(t, func() bool {
		runs := fixture.runs(t)
		return len(runs) == 1 && runs[0].HookStatus.Valid && runs[0].HookStatus.String == "ok"
	})
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	var input map[string]any
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatalf("scheduled hook stdin is not JSON: %v: %s", err, data)
	}
	project := input["project"].(map[string]any)
	run := input["run"].(map[string]any)
	if input["schema_version"] != float64(1) || project["path"] != fixture.project || run["status"] != "succeeded" || run["trigger"] != "schedule" {
		t.Fatalf("scheduled hook payload: %+v", input)
	}
	if hook := run["hook"].(map[string]any); hook["event"] != "on_success" || hook["status"] != "running" {
		t.Fatalf("scheduled hook transition: %+v", run["hook"])
	}
	stored := fixture.runs(t)[0]
	if stored.Status != "succeeded" || stored.HookStatus.String != "ok" || !stored.HookExitCode.Valid || stored.HookExitCode.Int64 != 0 {
		t.Fatalf("scheduled hook result: %+v", stored)
	}
}

func TestServe(t *testing.T) {
	t.Run("(a) due job runs exactly once with schedule trigger", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		command := `printf '%s %s' "$NOON_TRIGGER" "$NOON_SCHEDULED_AT" > ` + shellQuote(marker)
		fixture := newServeFixture(t, scheduledManifest("every 1m", command))
		fixture.step(t, serveTestStart)
		fixture.step(t, serveTestStart.Add(time.Minute))
		waitFor(t, func() bool {
			data, err := os.ReadFile(marker)
			return err == nil && string(data) == "schedule 2026-10-16T00:01:00Z"
		})
		data, err := os.ReadFile(marker)
		if err != nil || string(data) != "schedule 2026-10-16T00:01:00Z" {
			t.Fatalf("scheduled environment = %q err=%v", data, err)
		}
		fixture.step(t, serveTestStart.Add(time.Minute))
		fixture.stop(t)
		runs := fixture.runs(t)
		if len(runs) != 1 || runs[0].Trigger != "schedule" {
			t.Fatalf("runs = %+v; want one schedule run", runs)
		}
	})

	t.Run("(b) downtime creates one catch_up and no burst", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		fixture := newServeFixture(t, scheduledManifest("every 1m", "touch "+shellQuote(marker)))
		fixture.step(t, serveTestStart)
		fixture.step(t, serveTestStart.Add(5*time.Minute))
		waitFor(t, func() bool { _, err := os.Stat(marker); return err == nil })
		fixture.step(t, serveTestStart.Add(5*time.Minute))
		fixture.stop(t)
		runs := fixture.runs(t)
		if len(runs) != 1 || runs[0].Trigger != "catch_up" {
			t.Fatalf("runs = %+v; want one catch_up run", runs)
		}
	})

	t.Run("(c) invalid edit keeps last valid jobs and fixing clears error", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		fixture := newServeFixture(t, scheduledManifest("every 1m", "touch "+shellQuote(marker)))
		fixture.step(t, serveTestStart)
		fixture.writeConfig(t, "jobs: [invalid\n")
		fixture.step(t, serveTestStart.Add(2*time.Second))
		if message := fixture.configError(t); !strings.Contains(message, "/.git/noon.yaml:") {
			t.Fatalf("config error = %q; want source file and line", message)
		}
		fixture.step(t, serveTestStart.Add(121*time.Second))
		waitFor(t, func() bool { _, err := os.Stat(marker); return err == nil })
		fixture.writeConfig(t, scheduledManifest("every 2m", "touch "+shellQuote(marker)))
		fixture.step(t, serveTestStart.Add(124*time.Second))
		if message := fixture.configError(t); message != "" {
			t.Fatalf("config error after repair = %q", message)
		}
		fixture.stop(t)
		runs := fixture.runs(t)
		if len(runs) != 1 || runs[0].Trigger != "catch_up" {
			t.Fatalf("last valid job did not run once: %+v", runs)
		}
		states := fixture.states(t)
		if len(states) != 1 || states[0].ScheduleKey != "every 2m|Local" {
			t.Fatalf("fixed config schedule state: %+v", states)
		}
	})

	t.Run("(d) shutdown interrupts and kills the running process group", func(t *testing.T) {
		childFile := filepath.Join(t.TempDir(), "child.pid")
		command := fmt.Sprintf(`trap "" TERM; (echo $$ > %s; trap "" TERM; while :; do :; done) & wait`, shellQuote(childFile))
		fixture := newServeFixture(t, scheduledManifest("every 1m", command))
		fixture.step(t, serveTestStart)
		fixture.step(t, serveTestStart.Add(time.Minute))
		waitFor(t, func() bool {
			data, err := os.ReadFile(childFile)
			return err == nil && strings.HasSuffix(string(data), "\n")
		})
		data, err := os.ReadFile(childFile)
		if err != nil {
			t.Fatal(err)
		}
		childPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		identity, err := runner.ProcessStartIdentity(childPID)
		if err != nil {
			t.Fatalf("identify child %d: %v", childPID, err)
		}
		fixture.stop(t)
		runs := fixture.runs(t)
		if len(runs) != 1 || runs[0].Status != "interrupted" || !runs[0].PGID.Valid {
			t.Fatalf("shutdown run = %+v; want interrupted with process group", runs)
		}
		waitFor(t, func() bool {
			alive, err := runner.ProcessOwnerAlive(childPID, identity)
			return err != nil || !alive
		})
	})

	t.Run("(e) dead previous scheduler runs are interrupted and not rerun", func(t *testing.T) {
		fixture := newServeFixture(t, scheduledManifest("every 1h", "true"))
		owner := exec.Command("sh", "-c", "sleep 60")
		if err := owner.Start(); err != nil {
			t.Fatal(err)
		}
		pid := owner.Process.Pid
		identity, err := runner.ProcessStartIdentity(pid)
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.Process.Signal(syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		_ = owner.Wait()
		fixture.stop(t)
		state, err := store.Open(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		stale, err := state.CreateRun(store.NewRun{ProjectID: fixture.projectID(t), Job: "tests", Trigger: "schedule", Digest: "old", Now: serveTestStart, OwnerPID: pid, OwnerStart: identity})
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Close(); err != nil {
			t.Fatal(err)
		}
		fixture.start(t)
		fixture.step(t, serveTestStart)
		fixture.stop(t)
		result := fixture.runByID(t, stale.ID)
		if result.Status != "interrupted" {
			t.Fatalf("stale run status = %q", result.Status)
		}
		if runs := fixture.runs(t); len(runs) != 1 {
			t.Fatalf("stale run was scheduled again: %+v", runs)
		}
	})

	t.Run("(f) disabling a project stops later scheduled runs", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "runs")
		fixture := newServeFixture(t, scheduledManifest("every 1m", "printf x >> "+shellQuote(marker)))
		fixture.step(t, serveTestStart)
		fixture.step(t, serveTestStart.Add(time.Minute))
		waitFor(t, func() bool { data, err := os.ReadFile(marker); return err == nil && len(data) == 1 })
		state, err := store.Open(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.DisableProject(fixture.project, serveTestStart.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		_ = state.Close()
		fixture.step(t, serveTestStart.Add(2*time.Minute))
		fixture.stop(t)
		if runs := fixture.runs(t); len(runs) != 1 {
			t.Fatalf("disabled project continued scheduling: %+v", runs)
		}
	})

	t.Run("(g) disable and re-enable clears overdue phase while stopped", func(t *testing.T) {
		fixture := newServeFixture(t, scheduledManifest("every 1m", "true"))
		fixture.step(t, serveTestStart)
		fixture.stop(t)
		state, err := store.Open(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.DisableProject(fixture.project, serveTestStart.Add(4*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := state.EnableProject(fixture.project, nil, map[string]string{}, serveTestStart.Add(4*time.Minute)); err != nil {
			t.Fatal(err)
		}
		_ = state.Close()
		fixture.start(t)
		fixture.step(t, serveTestStart.Add(5*time.Minute))
		fixture.stop(t)
		if runs := fixture.runs(t); len(runs) != 0 {
			t.Fatalf("re-enabled project caught up stale run: %+v", runs)
		}
		states := fixture.states(t)
		if len(states) != 1 || states[0].NextAt != store.FormatTime(serveTestStart.Add(6*time.Minute)) {
			t.Fatalf("re-enabled interval phase = %+v", states)
		}
	})

	t.Run("(h) a local config file created after startup is loaded", func(t *testing.T) {
		fixture := newServeFixture(t, "")
		fixture.step(t, serveTestStart)
		fixture.writeConfig(t, scheduledManifest("every 1m", "true"))
		fixture.step(t, serveTestStart.Add(2*time.Second))
		fixture.stop(t)
		states := fixture.states(t)
		if len(states) != 1 || states[0].ScheduleKey != "every 1m|Local" {
			t.Fatalf("created local config was not scheduled: %+v", states)
		}
	})
}

func scheduledManifest(schedule, run string) string {
	return fmt.Sprintf("jobs:\n  tests:\n    schedule: %s\n    run: '%s'\n", schedule, strings.ReplaceAll(run, "'", "''"))
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }

func newServeFixture(t *testing.T, manifest string) *serveFixture {
	t.Helper()
	fixture := &serveFixture{root: t.TempDir(), ticks: make(chan time.Time), ack: make(chan struct{}, 20), done: make(chan error, 1)}
	fixture.project = filepath.Join(fixture.root, "project")
	if err := os.MkdirAll(filepath.Join(fixture.project, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	var err error
	fixture.project, err = filepath.EvalSymlinks(fixture.project)
	if err != nil {
		t.Fatal(err)
	}
	fixture.stateDir = filepath.Join(fixture.root, "state")
	fixture.userConfig = filepath.Join(fixture.root, "user-config")
	if err := os.MkdirAll(fixture.userConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.clock.Store(serveTestStart.UnixNano())
	if manifest != "" {
		fixture.writeConfig(t, manifest)
	}
	state, err := store.Open(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.EnableProject(fixture.project, nil, map[string]string{}, serveTestStart); err != nil {
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.start(t)
	return fixture
}

func (f *serveFixture) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	logger := f.logger
	if logger == nil {
		logger = io.Discard
	}
	environment := f.environment
	if environment == nil {
		environment = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + f.root}
	}
	stateDirDefault := f.stateDirDefault
	if stateDirDefault == "" {
		stateDirDefault = f.stateDir
	}
	go func() {
		f.done <- Serve(ctx, ServeOptions{
			StateDir: f.stateDir, StateDirDefault: stateDirDefault, UserConfigDir: f.userConfig, Version: "test",
			Environment: environment, ListenAddr: f.listenAddr,
			Now: func() time.Time { return time.Unix(0, f.clock.Load()).UTC() }, Ticks: f.ticks,
			KillGrace: 20 * time.Millisecond, Logger: logger, IgnoreSignals: true, OnTick: func() { f.ack <- struct{}{} },
		})
	}()
}

func (f *serveFixture) step(t *testing.T, now time.Time) {
	t.Helper()
	f.clock.Store(now.UnixNano())
	select {
	case f.ticks <- now:
	case err := <-f.done:
		t.Fatalf("scheduler exited before tick: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting to send scheduler tick")
	}
	select {
	case <-f.ack:
	case err := <-f.done:
		t.Fatalf("scheduler exited before completing tick: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for scheduler tick completion")
	}
}

func (f *serveFixture) stop(t *testing.T) {
	t.Helper()
	if f.cancel == nil {
		return
	}
	f.cancel()
	select {
	case err := <-f.done:
		f.cancel = nil
		if err != nil {
			t.Fatalf("scheduler shutdown: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("scheduler did not stop")
	}
}

func (f *serveFixture) writeConfig(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.project, ".git", "noon.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *serveFixture) runs(t *testing.T) []store.Run {
	t.Helper()
	state, err := store.OpenExisting(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	runs, err := state.RunsForProject(f.project, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func (f *serveFixture) projectID(t *testing.T) int64 {
	t.Helper()
	state, err := store.OpenExisting(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	snapshot, err := state.ReadSchedulerSnapshot()
	if err != nil || len(snapshot.Projects) != 1 {
		t.Fatalf("project snapshot: %+v %v", snapshot, err)
	}
	return snapshot.Projects[0].ID
}

func (f *serveFixture) runByID(t *testing.T, id int64) store.Run {
	t.Helper()
	state, err := store.OpenExisting(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	run, err := state.RunByID(id)
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func (f *serveFixture) states(t *testing.T) []store.PersistedJobState {
	t.Helper()
	state, err := store.OpenExisting(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	states, err := state.ScheduleStatesForProject(f.project)
	if err != nil {
		t.Fatal(err)
	}
	return states
}

func (f *serveFixture) configError(t *testing.T) string {
	t.Helper()
	state, err := store.OpenExisting(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	message, _, err := state.ProjectConfigError(f.project)
	if err != nil {
		t.Fatal(err)
	}
	return message
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("timed out waiting for process condition")
		case <-ticker.C:
		}
	}
}
