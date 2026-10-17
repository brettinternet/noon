package tui_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
	"github.com/brettinternet/noon/internal/tui"
)

func TestTUIModel(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "user-config"))
	for _, name := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE", "NOON_STATE_DIR", "NOON_DEPTH"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(project)
	input := config.Input{
		CWD: project, UserConfigDir: filepath.Join(root, "user-config"),
		StateDirDefault: filepath.Join(root, "state", "noon"),
	}
	shared := filepath.Join(project, "noon.yaml")
	if err := os.WriteFile(shared, []byte("jobs:\n  review:\n    run: \"printf original\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gate := filepath.Join(root, "release-live")
	uniqueGate := filepath.Join(root, "release-live-after-unique")
	capOpenGate := filepath.Join(root, "release-cap-open")
	capFollowStartGate := filepath.Join(root, "release-cap-follow-start")
	capFollowReleaseGate := filepath.Join(root, "release-cap-follow-end")
	var local strings.Builder
	fmt.Fprint(&local, `jobs:
  hooked:
    run: "printf job-output; exit 3"
    on_failure: [sh, -c, "printf hook-output"]
  long:
    run: |
      i=1
      while [ "$i" -le 1200 ]; do
        printf 'line-%04d-' "$i"
        head -c 100 /dev/zero | tr '\000' x
        printf '\n'
        i=$((i + 1))
      done
  orphan:
    run: ["true"]
  tests:
    run: [sh, -c, "printf queued"]
  zlive:
    run: |
      printf 'before\n'
      while [ ! -f `)
	fmt.Fprint(&local, gate)
	fmt.Fprint(&local, ` ]; do sleep 0.02; done
      printf 'unique-after-G\n'
      while [ ! -f `)
	fmt.Fprint(&local, uniqueGate)
	fmt.Fprint(&local, ` ]; do sleep 0.02; done
      printf 'after\n'
  zzcapopen:
    run: |
      head -c 1048577 /dev/zero | tr '\000' x
      while [ ! -f `)
	fmt.Fprint(&local, capOpenGate)
	fmt.Fprint(&local, ` ]; do sleep 0.02; done
      printf '\n'
  zzcapfollow:
    run: |
      printf 'cap-follow-start\n'
      while [ ! -f `)
	fmt.Fprint(&local, capFollowStartGate)
	fmt.Fprint(&local, ` ]; do sleep 0.02; done
      head -c 1048577 /dev/zero | tr '\000' x
      while [ ! -f `)
	fmt.Fprint(&local, capFollowReleaseGate)
	fmt.Fprint(&local, ` ]; do sleep 0.02; done
      printf '\n'
`)
	if err := os.WriteFile(filepath.Join(project, ".git", "noon.yaml"), []byte(local.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	initialConfig, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	project = initialConfig.Project
	if _, _, err := app.Enable(initialConfig, time.Now()); err != nil {
		t.Fatalf("enable fixture project: %v", err)
	}
	// Changing the shared job after approval creates a real needs-approval
	// notice while leaving the private test job safe to queue.
	if err := os.WriteFile(shared, []byte("jobs:\n  review:\n    run: \"printf changed\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		want int
	}{{"long", 0}, {"hooked", 3}} {
		var stdout, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "run", tc.name}, config.BuildOpts{}, &stdout, &stderr)
		if code != tc.want {
			t.Fatalf("run %s: exit=%d want=%d stdout=%q stderr=%q", tc.name, code, tc.want, stdout.String(), stderr.String())
		}
	}

	model, err := tui.NewModel(input)
	if err != nil {
		t.Fatal(err)
	}
	content := model.View().Content
	for _, expected := range []string{"hooked", "long", "orphan", "review", "tests", "zlive", "needs approval", "noon enable", "scheduler offline"} {
		if !strings.Contains(content, expected) {
			t.Fatalf("initial view missing %q:\n%s", expected, content)
		}
	}

	stateDir := input.StateDirDefault
	beforeRuns := readRuns(t, stateDir, project, "")
	beforePaused := readPaused(t, stateDir, project)

	// Selection, rendering, history filtering, and log reads must be read-only.
	model = press(t, model, "enter")
	if !strings.Contains(model.View().Content, "Runs for hooked") || !strings.Contains(model.View().Content, "status: all") {
		t.Fatalf("all-status run view:\n%s", model.View().Content)
	}
	model = press(t, model, "f")
	if !strings.Contains(model.View().Content, "failed,timed_out,interrupted") {
		t.Fatalf("failure status filter view:\n%s", model.View().Content)
	}
	model = press(t, model, "f")
	if !strings.Contains(model.View().Content, "status: running,queued") {
		t.Fatalf("queued/running status filter not shown:\n%s", model.View().Content)
	}
	model = press(t, model, "f")
	if !strings.Contains(model.View().Content, "status: all") {
		t.Fatalf("all status filter not restored:\n%s", model.View().Content)
	}
	model = press(t, model, "esc")
	model = press(t, model, "j")
	model = press(t, model, "down")
	model = press(t, model, "k")
	model = press(t, model, "enter")
	if !strings.Contains(model.View().Content, "Runs for long") {
		t.Fatalf("enter did not open selected job history:\n%s", model.View().Content)
	}
	model = press(t, model, "esc")
	model = press(t, model, "l")
	content = model.View().Content
	if !strings.Contains(content, "Run 1 · succeeded") || !strings.Contains(content, "line-1200") || strings.Contains(content, "line-0001") {
		t.Fatalf("log did not open on the last screenful:\n%s", content)
	}
	model = press(t, model, "g")
	if !strings.Contains(model.View().Content, "line-0001") {
		t.Fatalf("g did not jump to the beginning of the log:\n%s", model.View().Content)
	}
	model = refreshOnce(t, model)
	model = refreshOnce(t, model)
	if content := model.View().Content; !strings.Contains(content, "line-0001") || strings.Contains(content, "line-1200") {
		t.Fatalf("refresh after g autoscrolled away from the first page:\n%s", content)
	}
	for range 1400 {
		model = press(t, model, "j")
	}
	if !strings.Contains(model.View().Content, "line-1200") {
		t.Fatalf("scrolling after g could not reach the completed log's later page:\n%s", model.View().Content)
	}
	model = press(t, model, "G")
	if !strings.Contains(model.View().Content, "line-1200") {
		t.Fatalf("G did not jump to the end of the log:\n%s", model.View().Content)
	}
	model = press(t, model, "/")
	model = typeText(t, model, "line-0042")
	model = press(t, model, "enter")
	if !strings.Contains(model.View().Content, "42:line-0042") {
		t.Fatalf("log search did not preserve the file line number:\n%s", model.View().Content)
	}
	model = press(t, model, "esc")
	if !strings.Contains(model.View().Content, "line-1200") || strings.Contains(model.View().Content, "42:line-0042") {
		t.Fatalf("escape did not return to the log tail:\n%s", model.View().Content)
	}
	model = press(t, model, "?")
	if !strings.Contains(model.View().Content, "never adds, edits, enables, or approves") {
		t.Fatalf("help view missing safety contract:\n%s", model.View().Content)
	}
	model = press(t, model, "esc")
	model = press(t, model, "esc")

	// A failed run with hook output opens on job output, then cycles job → hook → all.
	model = press(t, model, "up")
	model = press(t, model, "l")
	content = model.View().Content
	if !strings.Contains(content, "section: job") || !strings.Contains(content, "job-output") || strings.Contains(content, "hook-output") {
		t.Fatalf("failed run did not open on job output:\n%s", content)
	}
	model = press(t, model, "s")
	content = model.View().Content
	if !strings.Contains(content, "section: hook") || strings.Contains(content, "job-output") || !strings.Contains(content, "hook-output") {
		t.Fatalf("hook section did not isolate hook output:\n%s", content)
	}
	model = press(t, model, "s")
	content = model.View().Content
	if !strings.Contains(content, "section: all") || !strings.Contains(content, "job-output") || !strings.Contains(content, "hook-output") {
		t.Fatalf("all section did not include both outputs:\n%s", content)
	}
	model = press(t, model, "s")
	if strings.Contains(model.View().Content, "hook-output") {
		t.Fatalf("cycling back to job section retained hook output:\n%s", model.View().Content)
	}
	model = press(t, model, "esc")
	if afterRuns := readRuns(t, stateDir, project, ""); !reflect.DeepEqual(runIDs(beforeRuns), runIDs(afterRuns)) {
		t.Fatalf("selection, views, or log reads changed run history: before=%+v after=%+v", beforeRuns, afterRuns)
	}
	if afterPaused := readPaused(t, stateDir, project); !reflect.DeepEqual(beforePaused, afterPaused) {
		t.Fatalf("selection or rendering changed pause state: before=%v after=%v", beforePaused, afterPaused)
	}

	// r queues with the same authorization rules as `noon run --detach`.
	model = press(t, model, "j")
	model = press(t, model, "j")
	model = press(t, model, "j")
	model = press(t, model, "j")
	if !strings.Contains(model.View().Content, "> tests") {
		t.Fatalf("test job was not selected:\n%s", model.View().Content)
	}
	model = press(t, model, "r")
	testsRuns := readRuns(t, stateDir, project, "tests")
	if len(testsRuns) == 0 || testsRuns[0].Status != "queued" || !strings.Contains(model.View().Content, "Follow it: noon logs") {
		t.Fatalf("r did not queue the selected job: runs=%+v\n%s", testsRuns, model.View().Content)
	}
	model = press(t, model, "p")
	if !strings.Contains(model.View().Content, "paused") || !readPaused(t, stateDir, project)["tests"] {
		t.Fatalf("p did not pause the selected job:\n%s", model.View().Content)
	}
	model = press(t, model, "p")
	if readPaused(t, stateDir, project)["tests"] {
		t.Fatalf("p did not resume the selected job:\n%s", model.View().Content)
	}
	// Pausing intentionally cancels queued work. Queue a fresh run after resume
	// so the confirmation keys exercise both the no-op and confirmed paths.
	model = press(t, model, "r")
	testsRuns = readRuns(t, stateDir, project, "tests")
	if len(testsRuns) == 0 || testsRuns[0].Status != "queued" {
		t.Fatalf("run after resume was not queued: %+v", testsRuns)
	}
	queuedID := testsRuns[0].ID

	model = press(t, model, "enter")
	if !strings.Contains(model.View().Content, "Runs for tests") {
		t.Fatalf("enter did not open queued job history:\n%s", model.View().Content)
	}
	model = press(t, model, "c")
	if !strings.Contains(model.View().Content, "Press y to confirm") {
		t.Fatalf("c did not ask for cancellation confirmation:\n%s", model.View().Content)
	}
	model = press(t, model, "n")
	testsRuns = readRuns(t, stateDir, project, "tests")
	if len(testsRuns) == 0 || testsRuns[0].ID != queuedID || testsRuns[0].Status != "queued" {
		t.Fatalf("n changed the queued run: %+v", testsRuns)
	}
	model = press(t, model, "c")
	model = press(t, model, "y")
	testsRuns = readRuns(t, stateDir, project, "tests")
	if len(testsRuns) == 0 || testsRuns[0].ID != queuedID || testsRuns[0].Status != "cancelled" {
		t.Fatalf("y did not cancel the selected queued run: %+v", testsRuns)
	}

	// A queued log shows the shared waiting notice until its owner starts.
	model = press(t, model, "esc")
	model = press(t, model, "j")
	if !strings.Contains(model.View().Content, "> zlive") {
		t.Fatalf("live job was not selected:\n%s", model.View().Content)
	}
	model = press(t, model, "r")
	liveRuns := readRuns(t, stateDir, project, "zlive")
	if len(liveRuns) == 0 || liveRuns[0].Status != "queued" {
		t.Fatalf("r did not queue zlive: %+v", liveRuns)
	}
	queuedLiveID := liveRuns[0].ID
	model = press(t, model, "l")
	if !strings.Contains(model.View().Content, fmt.Sprintf("noon: run %d is queued; waiting for it to start", queuedLiveID)) {
		t.Fatalf("queued log omitted waiting notice:\n%s", model.View().Content)
	}
	model = press(t, model, "esc")
	model = press(t, model, "enter")
	model = press(t, model, "c")
	press(t, model, "y")
	liveRuns = readRuns(t, stateDir, project, "zlive")
	if len(liveRuns) == 0 || liveRuns[0].Status != "cancelled" {
		t.Fatalf("confirmed cancellation did not cancel the queued live run: %+v", liveRuns)
	}

	// A running log follows only bytes after its initial cursor.
	liveCtx, cancelLive := context.WithCancel(context.Background())
	liveDone := make(chan int, 1)
	liveFinished := false
	go func() {
		var stdout, stderr bytes.Buffer
		liveDone <- cli.Run(liveCtx, []string{"noon", "run", "zlive"}, config.BuildOpts{}, &stdout, &stderr)
	}()
	defer func() {
		cancelLive()
		if liveFinished {
			return
		}
		select {
		case <-liveDone:
		case <-time.After(3 * time.Second):
			t.Error("live job did not stop during cleanup")
		}
	}()
	liveRun := waitForLogText(t, stateDir, project, "zlive", "before")
	followModel, err := tui.NewModel(input)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		followModel = press(t, followModel, "j")
	}
	followModel = press(t, followModel, "l")
	if !strings.Contains(followModel.View().Content, "before") || !strings.Contains(followModel.View().Content, "running") {
		t.Fatalf("active log did not open at its current tail: run=%+v\n%s", liveRun, followModel.View().Content)
	}
	followModel = refreshOnce(t, followModel)
	if strings.Contains(followModel.View().Content, "after") {
		t.Fatalf("live job passed its gate before the test released it:\n%s", followModel.View().Content)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitForLogText(t, stateDir, project, "zlive", "unique-after-G")
	followModel = press(t, followModel, "G")
	if strings.Count(followModel.View().Content, "unique-after-G") != 1 {
		t.Fatalf("G snapshot did not contain the new unique output once:\n%s", followModel.View().Content)
	}
	followModel = refreshOnce(t, followModel)
	if strings.Count(followModel.View().Content, "unique-after-G") != 1 {
		t.Fatalf("refresh duplicated output already included in the G snapshot:\n%s", followModel.View().Content)
	}
	if err := os.WriteFile(uniqueGate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-liveDone:
		liveFinished = true
		if code != 0 {
			t.Fatalf("live job exit=%d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("live job did not finish after its gate opened")
	}
	followModel = refreshOnce(t, followModel)
	if content := followModel.View().Content; strings.Count(content, "before") != 1 || strings.Count(content, "unique-after-G") != 1 || !strings.Contains(content, "\nafter\n") {
		t.Fatalf("log follow repeated or missed output after its cursor:\n%s", content)
	}

	capOpenDone := startTUIRun(t, input, "zzcapopen")
	waitForRunLogSize(t, stateDir, project, "zzcapopen", 1<<20)
	capOpenModel := openLatestLog(t, input, "zzcapopen")
	if content := capOpenModel.View().Content; !strings.Contains(content, "running") || !strings.Contains(content, "output past 1 MiB") {
		t.Fatalf("opening an active unfinished log at the persisted cap omitted its notice:\n%s", content)
	}
	if err := os.WriteFile(capOpenGate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitTUIRun(t, capOpenDone, "zzcapopen")

	capFollowDone := startTUIRun(t, input, "zzcapfollow")
	waitForLogText(t, stateDir, project, "zzcapfollow", "cap-follow-start")
	capFollowModel := openLatestLog(t, input, "zzcapfollow")
	if content := capFollowModel.View().Content; !strings.Contains(content, "cap-follow-start") || !strings.Contains(content, "running") {
		t.Fatalf("cap-follow log did not open before output reached the cap:\n%s", content)
	}
	if err := os.WriteFile(capFollowStartGate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitForRunLogSize(t, stateDir, project, "zzcapfollow", 1<<20)
	capFollowModel = refreshOnce(t, capFollowModel)
	if content := capFollowModel.View().Content; !strings.Contains(content, "output past 1 MiB") {
		t.Fatalf("following an active unfinished log at the persisted cap omitted its notice:\n%s", content)
	}
	if err := os.WriteFile(capFollowReleaseGate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitTUIRun(t, capFollowDone, "zzcapfollow")

	orphanID := seedOrphanRun(t, input)
	orphanModel, err := tui.NewModel(input)
	if err != nil {
		t.Fatal(err)
	}
	orphanModel = press(t, orphanModel, "j")
	orphanModel = press(t, orphanModel, "j")
	orphanModel = press(t, orphanModel, "l")
	content = orphanModel.View().Content
	if !strings.Contains(content, fmt.Sprintf("run %d's owner process exited without recording a result", orphanID)) || !strings.Contains(content, fmt.Sprintf("noon cancel %d", orphanID)) {
		t.Fatalf("orphan log did not show its diagnostic and cancel hint:\n%s", content)
	}
}

func refreshOnce(t *testing.T, model *tui.Model) *tui.Model {
	t.Helper()
	command := model.Init()
	if command == nil {
		t.Fatal("model has no refresh tick")
	}
	message := command()
	if message == nil {
		t.Fatal("refresh tick returned no message")
	}
	updated, _ := model.Update(message)
	result, ok := updated.(*tui.Model)
	if !ok {
		t.Fatalf("Update returned %T, want *tui.Model", updated)
	}
	return result
}

func waitForLogText(t *testing.T, stateDir, project, job, text string) app.Run {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		runs := readRuns(t, stateDir, project, job)
		if len(runs) > 0 && runs[0].Status == "running" {
			limit := 64 << 10
			detail, err := app.GetRun(context.Background(), stateDir, runs[0].ID, 0, app.LogQuery{LimitBytes: &limit})
			if err == nil && strings.Contains(detail.Log, text) {
				return runs[0]
			}
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("run %s did not write %q before the deadline", job, text)
		}
	}
}

func startTUIRun(t *testing.T, input config.Input, job string) <-chan int {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		done <- cli.Run(ctx, []string{"noon", "run", job}, config.BuildOpts{}, &stdout, &stderr)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("run %s did not stop during cleanup", job)
		}
	})
	return done
}

func waitTUIRun(t *testing.T, done <-chan int, job string) {
	t.Helper()
	select {
	case code, ok := <-done:
		if !ok {
			t.Fatalf("run %s finished without an exit code", job)
		}
		if code != 0 {
			t.Fatalf("run %s exit=%d", job, code)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("run %s did not finish before the deadline", job)
	}
}

func waitForRunLogSize(t *testing.T, stateDir, project, job string, size int64) app.Run {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	limit := 64 << 10
	for {
		runs := readRuns(t, stateDir, project, job)
		if len(runs) > 0 && runs[0].Status == "running" {
			detail, err := app.GetRun(context.Background(), stateDir, runs[0].ID, 0, app.LogQuery{LimitBytes: &limit})
			if err == nil && detail.Size >= size {
				return runs[0]
			}
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("run %s did not persist %d log bytes before the deadline", job, size)
		}
	}
}

func openLatestLog(t *testing.T, input config.Input, job string) *tui.Model {
	t.Helper()
	model, err := tui.NewModel(input)
	if err != nil {
		t.Fatal(err)
	}
	for range 25 {
		if strings.Contains(model.View().Content, "> "+job+" ") {
			model = press(t, model, "enter")
			return press(t, model, "l")
		}
		model = press(t, model, "j")
	}
	t.Fatalf("job %s was not selectable in the TUI:\n%s", job, model.View().Content)
	return nil
}

func seedOrphanRun(t *testing.T, input config.Input) int64 {
	t.Helper()
	cfg, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	projectID, err := state.RegisterProject(cfg.Project, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	job := cfg.Jobs["orphan"]
	run, err := state.CreateRun(store.NewRun{
		ProjectID: projectID, Job: "orphan", Trigger: "manual", Digest: job.Digest,
		Now: time.Now(), OwnerPID: 2147483647, OwnerStart: "not-real",
	})
	if err != nil {
		t.Fatal(err)
	}
	log, err := state.RunLog(projectID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SetRunLog(run.ID, log.Path()); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("orphaned-output\n")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := log.Close(); err != nil {
		t.Fatal(err)
	}
	return run.ID
}

func press(t *testing.T, model *tui.Model, key string) *tui.Model {
	t.Helper()
	var message tea.KeyPressMsg
	switch key {
	case "enter":
		message = tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		message = tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape})
	case "up":
		message = tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		message = tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "G":
		message = tea.KeyPressMsg(tea.Key{Code: 'g', Text: "G", ShiftedCode: 'G'})
	default:
		runes := []rune(key)
		if len(runes) != 1 {
			t.Fatalf("unsupported test key %q", key)
		}
		message = tea.KeyPressMsg(tea.Key{Code: runes[0], Text: key})
	}
	updated, _ := model.Update(message)
	result, ok := updated.(*tui.Model)
	if !ok {
		t.Fatalf("Update returned %T, want *tui.Model", updated)
	}
	return result
}

func typeText(t *testing.T, model *tui.Model, text string) *tui.Model {
	t.Helper()
	for _, r := range text {
		model = press(t, model, string(r))
	}
	return model
}

func readRuns(t *testing.T, stateDir, project, job string) []app.Run {
	t.Helper()
	runs, err := app.RunsWithStatuses(stateDir, project, job, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func readPaused(t *testing.T, stateDir, project string) map[string]bool {
	t.Helper()
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	paused, err := state.PausedJobs(project)
	if err != nil {
		t.Fatal(err)
	}
	return paused
}

func runIDs(runs []app.Run) []string {
	ids := make([]string, len(runs))
	for i, run := range runs {
		ids[i] = fmt.Sprintf("%d:%s:%s", run.ID, run.Job, run.Status)
	}
	return ids
}
