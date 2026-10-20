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

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

var serveTestStart = time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)

type serveFixture struct {
	root, project, stateDir, userConfig string
	stateDirDefault                     string
	listenAddr                          string
	platform                            *config.Platform
	environment                         []string
	logger                              io.Writer
	clock                               atomic.Int64
	ticks                               chan time.Time
	ack                                 chan struct{}
	cancel                              context.CancelFunc
	done                                chan error
}

// IDs for scheduled and queued prompt executions must stay null until dispatch,
// then match the child argv, environment, shared run JSON, and hook payload.
func TestServeAgentSessionIDs(t *testing.T) {
	fixture := newServeFixture(t, "")
	fixture.stop(t)

	recordDir := filepath.Join(fixture.root, "records")
	if err := os.Mkdir(recordDir, 0o700); err != nil {
		t.Fatal(err)
	}
	agent := filepath.Join(fixture.root, "record-agent")
	agentScript := "#!/bin/sh\nprintf '%s\\0' \"$@\" > \"$RECORD_DIR/argv-$NOON_AGENT_SESSION_ID\"\nprintf '%s' \"$NOON_AGENT_SESSION_ID\" > \"$RECORD_DIR/env-$NOON_AGENT_SESSION_ID\"\n"
	if err := os.WriteFile(agent, []byte(agentScript), 0o700); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(fixture.root, "record-hook")
	hookScript := "#!/bin/sh\nprintf '%s' \"$NOON_AGENT_SESSION_ID\" > \"$RECORD_DIR/hook-env-$NOON_RUN_ID\"\ncat > \"$RECORD_DIR/hook-json-$NOON_RUN_ID\"\n"
	if err := os.WriteFile(hook, []byte(hookScript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(fixture.userConfig, "noon"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeUserConfig := func(sessionIDFlag string) {
		t.Helper()
		userConfig := fmt.Sprintf("agents:\n  session:\n    command: [%s]\n    prompt: argument\n    session_id_flag: %s\non_success: [%s]\non_failure: [%s]\n", strconv.Quote(agent), sessionIDFlag, strconv.Quote(hook), strconv.Quote(hook))
		if err := os.WriteFile(filepath.Join(fixture.userConfig, "noon", "config.yaml"), []byte(userConfig), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeUserConfig("--old-session-id")
	fixture.writeConfig(t, `jobs:
  scheduled:
    schedule: every 1m
    prompt: scheduled prompt
    agent: session
  queued:
    prompt: queued prompt
    agent: session
  cancelled:
    prompt: cancelled prompt
    agent: session
`)
	fixture.environment = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + fixture.root, "RECORD_DIR=" + recordDir, "NOON_AGENT_SESSION_ID=inherited-session-leak"}
	fixture.start(t)
	defer fixture.stop(t)
	fixture.step(t, serveTestStart)

	queued := createQueuedRun(t, fixture, "queued", serveTestStart, "")
	cancelled := createQueuedRun(t, fixture, "cancelled", serveTestStart, "")
	if queued.AgentSessionID.Valid || cancelled.AgentSessionID.Valid {
		t.Fatalf("queued runs acquired session IDs before dispatch: queued=%+v cancelled=%+v", queued.AgentSessionID, cancelled.AgentSessionID)
	}
	state, err := store.Open(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	cancelledRun, err := state.RequestCancelRun(cancelled.ID, serveTestStart)
	if err != nil || cancelledRun.Status != "cancelled" || cancelledRun.AgentSessionID.Valid {
		_ = state.Close()
		t.Fatalf("cancel before dispatch = %+v err=%v, want null session ID", cancelledRun, err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}

	writeUserConfig("--session-id")
	assertExecution := func(run store.Run, prompt string) {
		t.Helper()
		if !run.AgentSessionID.Valid {
			t.Fatalf("run %d has no session ID: %+v", run.ID, run)
		}
		sessionID := run.AgentSessionID.String
		if len(sessionID) != 36 || sessionID[14] != '4' || !strings.ContainsRune("89ab", rune(sessionID[19])) {
			t.Fatalf("run %d session ID is not UUID v4: %q", run.ID, sessionID)
		}
		argvData, err := os.ReadFile(filepath.Join(recordDir, "argv-"+sessionID))
		if err != nil {
			t.Fatal(err)
		}
		argv := strings.Split(strings.TrimSuffix(string(argvData), "\x00"), "\x00")
		if fmt.Sprint(argv) != fmt.Sprint([]string{"--session-id", sessionID, prompt}) {
			t.Fatalf("child argv = %q, want session flag and ID before prompt %q", argv, prompt)
		}
		if got, err := os.ReadFile(filepath.Join(recordDir, "env-"+sessionID)); err != nil || string(got) != sessionID {
			t.Fatalf("child session environment = %q err=%v, want %s", got, err, sessionID)
		}
		if got, err := os.ReadFile(filepath.Join(recordDir, fmt.Sprintf("hook-env-%d", run.ID))); err != nil || string(got) != sessionID {
			t.Fatalf("hook session environment = %q err=%v, want %s", got, err, sessionID)
		}
		var payload map[string]any
		hookData, err := os.ReadFile(filepath.Join(recordDir, fmt.Sprintf("hook-json-%d", run.ID)))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(hookData, &payload); err != nil {
			t.Fatalf("hook JSON: %v", err)
		}
		hookRun := payload["run"].(map[string]any)
		if hookRun["agent_session_id"] != sessionID {
			t.Fatalf("hook run session ID = %v, want %s", hookRun["agent_session_id"], sessionID)
		}
		inspection, err := store.OpenExisting(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		defer inspection.Close()
		shared, err := json.Marshal(app.RunFromStore(inspection, run))
		if err != nil {
			t.Fatal(err)
		}
		var sharedRun map[string]any
		if err := json.Unmarshal(shared, &sharedRun); err != nil || sharedRun["agent_session_id"] != sessionID {
			t.Fatalf("shared run JSON session ID = %v err=%v, want %s", sharedRun["agent_session_id"], err, sessionID)
		}
	}

	fixture.step(t, serveTestStart.Add(2*time.Second))
	waitFor(t, func() bool {
		run := fixture.runByID(t, queued.ID)
		return run.Status == "succeeded" && run.HookStatus.String == "ok"
	})
	queuedRun := fixture.runByID(t, queued.ID)
	assertExecution(queuedRun, "queued prompt")
	fixture.step(t, serveTestStart.Add(time.Minute))
	waitFor(t, func() bool {
		for _, run := range fixture.runs(t) {
			if run.Job == "scheduled" && run.Status == "succeeded" && run.HookStatus.String == "ok" {
				return true
			}
		}
		return false
	})
	var scheduledRun store.Run
	for _, run := range fixture.runs(t) {
		if run.Job == "scheduled" {
			scheduledRun = run
			break
		}
	}
	if scheduledRun.ID == 0 || scheduledRun.Trigger != "schedule" {
		t.Fatalf("scheduled prompt run = %+v", scheduledRun)
	}
	assertExecution(scheduledRun, "scheduled prompt")
	if run := fixture.runByID(t, cancelled.ID); run.AgentSessionID.Valid || run.Status != "cancelled" {
		t.Fatalf("cancelled pre-dispatch run = %+v", run)
	}
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
	// Explicit-config loads still validate the user config, so editing only it must reload.
	userFile := filepath.Join(fixture.userConfig, "noon", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(userFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte("timezone: [invalid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.step(t, serveTestStart.Add(3*time.Minute+4*time.Second))
	if message := fixture.configError(t); !strings.Contains(message, "config.yaml") || len(fixture.states(t)) != 1 {
		t.Fatalf("user config edit under explicit config: error=%q states=%d", message, len(fixture.states(t)))
	}
	fixture.step(t, serveTestStart.Add(4*time.Minute+2*time.Second))
	fixture.stop(t)
	if runs := fixture.runs(t); len(runs) != 2 {
		t.Fatalf("restored checkout runs with last-good config: %+v", runs)
	}
}

func TestServeUserProjectIsolationAndLastGoodReload(t *testing.T) {
	fixture := newServeFixture(t, "jobs: {regular: {run: 'true'}}\n")
	fixture.stop(t)

	userProject, err := config.UserProjectDir(fixture.userConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(userProject, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(fixture.root, "user-cwd")
	command := "pwd >> " + shellQuote(marker)
	userConfig := filepath.Join(userProject, "config.yaml")
	userManifest := fmt.Sprintf("jobs:\n  personal:\n    schedule: every 1m\n    run: %s\n", strconv.Quote(command))
	if err := os.WriteFile(userConfig, []byte(userManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userProject, "noon.yaml"), []byte("jobs: {tree: {schedule: every 1m, run: 'touch tree-ran'}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userProject, ".git", "noon.yaml"), []byte("jobs: {private: {schedule: every 1m, run: 'touch private-ran'}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := config.Input{CWD: fixture.root, UserConfigDir: fixture.userConfig, Project: &userProject, StateDirDefault: fixture.stateDir}
	userConfigSnapshot, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.Enable(userConfigSnapshot, serveTestStart); err != nil {
		t.Fatalf("enable user project: %v", err)
	}
	fixture.start(t)
	fixture.step(t, serveTestStart)

	regular, err := app.ListJobs(config.Input{CWD: fixture.project, UserConfigDir: fixture.userConfig, StateDirDefault: fixture.stateDir}, serveTestStart)
	if err != nil {
		t.Fatal(err)
	}
	for _, job := range regular.Jobs {
		if job.Name == "personal" || job.Name == "tree" || job.Name == "private" {
			t.Fatalf("user/repository job appeared in regular list: %+v", regular.Jobs)
		}
	}

	waitForUserRuns := func(count int) {
		t.Helper()
		state, err := store.OpenExisting(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		defer state.Close()
		waitFor(t, func() bool {
			runs, err := state.RunsForProject(userProject, "", 50)
			return err == nil && len(runs) == count && runs[0].Status == "succeeded"
		})
	}

	fixture.step(t, serveTestStart.Add(time.Minute))
	waitFor(t, func() bool {
		data, err := os.ReadFile(marker)
		return err == nil && strings.TrimSpace(string(data)) == userProject
	})
	// Output can arrive before completion is persisted. Advancing the clock then
	// would skip the next occurrence under the default overlap policy.
	waitForUserRuns(1)
	broken := "jobs: {personal: {schedule: every day, run: 'true'}}\n"
	if err := os.WriteFile(userConfig, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	fixture.step(t, serveTestStart.Add(time.Minute+2*time.Second))
	state, err := store.OpenExisting(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := state.ProjectConfigError(userProject)
	_ = state.Close()
	if err != nil || !strings.Contains(message, "config.yaml") || !strings.Contains(message, "every day") {
		t.Fatalf("broken user config error=%q err=%v", message, err)
	}
	fixture.step(t, serveTestStart.Add(2*time.Minute))
	waitFor(t, func() bool {
		data, err := os.ReadFile(marker)
		return err == nil && strings.Count(string(data), "\n") == 2
	})
	data, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(data)) != userProject+"\n"+userProject {
		t.Fatalf("last-good user schedule output=%q err=%v", data, err)
	}
	waitForUserRuns(2)
	fixture.stop(t)

	state, err = store.OpenExisting(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	userRuns, err := state.RunsForProject(userProject, "", 50)
	if err != nil {
		_ = state.Close()
		t.Fatal(err)
	}
	regularRuns, err := state.RunsForProject(fixture.project, "", 50)
	_ = state.Close()
	if err != nil || len(userRuns) != 2 || len(regularRuns) != 0 || userRuns[0].Job != "personal" || userRuns[1].Job != "personal" {
		t.Fatalf("user/regular runs: user=%+v regular=%+v err=%v", userRuns, regularRuns, err)
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

func TestServeNoopLifecycle(t *testing.T) {
	fixtureDir := t.TempDir()
	failedOnce := filepath.Join(fixtureDir, "failed-once")
	noopedOnce := filepath.Join(fixtureDir, "nooped-once")
	run := fmt.Sprintf("if [ ! -e %s ]; then touch %s; exit 3; fi; if [ ! -e %s ]; then touch %s; exit 78; fi; exit 0",
		shellQuote(failedOnce), shellQuote(failedOnce), shellQuote(noopedOnce), shellQuote(noopedOnce))
	failureHook := filepath.Join(fixtureDir, "failure-hook")
	successHook := filepath.Join(fixtureDir, "success-hook")
	manifest := fmt.Sprintf("jobs:\n  poll:\n    schedule: every 1m\n    run: %s\n    noop_exit_code: 78\n    on_failure: %s\n    on_success: %s\n",
		strconv.Quote(run), strconv.Quote("echo $NOON_STATUS >> "+shellQuote(failureHook)), strconv.Quote("echo $NOON_PREVIOUS_STATUS >> "+shellQuote(successHook)))
	fixture := newServeFixture(t, manifest)
	defer fixture.stop(t)
	fixture.step(t, serveTestStart)

	for i, expected := range []string{"failed", "noop", "succeeded"} {
		fixture.step(t, serveTestStart.Add(time.Duration(i+1)*time.Minute))
		wantRuns := i + 1
		waitFor(t, func() bool {
			runs := fixture.runs(t)
			if len(runs) != wantRuns || runs[0].Status != expected {
				return false
			}
			return expected == "noop" || (runs[0].HookStatus.Valid && runs[0].HookStatus.String == "ok")
		})
	}

	failureData, err := os.ReadFile(failureHook)
	if err != nil || string(failureData) != "failed\n" {
		t.Fatalf("failure hook ran for noop: output=%q err=%v", failureData, err)
	}
	successData, err := os.ReadFile(successHook)
	if err != nil || string(successData) != "failed\n" {
		t.Fatalf("success hook did not receive last result, or ran for noop: output=%q err=%v", successData, err)
	}
	runs := fixture.runs(t)
	if len(runs) != 3 || runs[0].Status != "succeeded" || runs[1].Status != "noop" || !runs[1].ExitCode.Valid || runs[1].ExitCode.Int64 != 78 || runs[2].Status != "failed" {
		t.Fatalf("failed/noop/succeeded sequence = %+v", runs)
	}
}

func TestServeNoopDoesNotOverrideStops(t *testing.T) {
	command := "trap 'exit 78' TERM; while :; do sleep 0.01; done"
	t.Run("timeout", func(t *testing.T) {
		manifest := fmt.Sprintf("jobs:\n  poll:\n    schedule: every 1m\n    run: %s\n    timeout: 1s\n    noop_exit_code: 78\n", strconv.Quote(command))
		fixture := newServeFixture(t, manifest)
		defer fixture.stop(t)
		fixture.step(t, serveTestStart)
		fixture.step(t, serveTestStart.Add(time.Minute))
		waitFor(t, func() bool {
			runs := fixture.runs(t)
			return len(runs) == 1 && runs[0].Status == "timed_out"
		})
	})

	t.Run("cancellation", func(t *testing.T) {
		manifest := fmt.Sprintf("jobs:\n  poll:\n    schedule: every 1m\n    run: %s\n    noop_exit_code: 78\n", strconv.Quote(command))
		fixture := newServeFixture(t, manifest)
		defer fixture.stop(t)
		fixture.step(t, serveTestStart)
		fixture.step(t, serveTestStart.Add(time.Minute))
		waitFor(t, func() bool {
			runs := fixture.runs(t)
			return len(runs) == 1 && runs[0].Status == "running"
		})
		runID := fixture.runs(t)[0].ID
		state, err := store.Open(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := state.RequestCancelRun(runID, serveTestStart.Add(time.Minute)); err != nil {
			_ = state.Close()
			t.Fatal(err)
		}
		if err := state.Close(); err != nil {
			t.Fatal(err)
		}
		fixture.step(t, serveTestStart.Add(time.Minute+time.Second))
		waitFor(t, func() bool {
			runs := fixture.runs(t)
			return len(runs) == 1 && runs[0].Status == "cancelled"
		})
	})
}

func TestServePlatformEligibility(t *testing.T) {
	mismatchMarker := filepath.Join(t.TempDir(), "unsupported-ran")
	matchMarker := filepath.Join(t.TempDir(), "supported-ran")
	manifest := fmt.Sprintf("jobs:\n  elsewhere:\n    os: [darwin]\n    schedule: every 1m\n    run: [touch, %q]\n  here:\n    os: [linux]\n    arch: [amd64]\n    schedule: every 1m\n    run: [touch, %q]\n", mismatchMarker, matchMarker)
	fixture := newServeFixture(t, manifest)
	defer fixture.stop(t)
	fixture.stop(t)
	fixture.platform = &config.Platform{OS: "linux", Arch: "amd64"}
	fixture.start(t)

	fixture.step(t, serveTestStart)
	states := fixture.states(t)
	if len(states) != 1 || states[0].Job != "here" {
		t.Fatalf("platform-ineligible job got schedule state: %+v", states)
	}

	state, err := store.Open(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	queued, err := state.QueueRun(store.QueueRunInput{
		ProjectID: fixture.projectID(t), Job: "elsewhere", Digest: "test", RequestedBy: "test",
		DueAt: serveTestStart, Now: serveTestStart, JobExists: true, JobEnabled: true,
	})
	_ = state.Close()
	if err != nil || !queued.Created {
		t.Fatalf("seed previously queued nonmatching run: %+v err=%v", queued, err)
	}

	fixture.step(t, serveTestStart.Add(5*time.Minute))
	waitFor(t, func() bool {
		for _, run := range fixture.runs(t) {
			if run.Job == "here" && run.Status == "succeeded" {
				return true
			}
		}
		return false
	})
	fixture.stop(t)

	if _, err := os.Stat(mismatchMarker); !os.IsNotExist(err) {
		t.Fatalf("platform-ineligible job ran: %v", err)
	}
	if _, err := os.Stat(matchMarker); err != nil {
		t.Fatalf("matching job did not run: %v", err)
	}
	runs := fixture.runs(t)
	if len(runs) != 2 {
		t.Fatalf("runs = %+v; want one cancelled queued run and one catch-up", runs)
	}
	statuses := map[string]store.Run{}
	for _, run := range runs {
		statuses[run.Job] = run
	}
	if statuses["elsewhere"].Status != "cancelled" {
		t.Fatalf("queued nonmatching job was not cancelled: %+v", statuses["elsewhere"])
	}
	if statuses["here"].Status != "succeeded" || statuses["here"].Trigger != "catch_up" {
		t.Fatalf("matching job failed to catch up normally: %+v", statuses["here"])
	}
	states = fixture.states(t)
	if len(states) != 1 || states[0].Job != "here" {
		t.Fatalf("post-dispatch schedule state = %+v", states)
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
			Environment: environment, ListenAddr: f.listenAddr, Platform: f.platform,
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
