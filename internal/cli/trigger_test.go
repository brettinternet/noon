package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/scheduler"
	"github.com/brettinternet/noon/internal/store"
)

func TestTriggers(t *testing.T) {
	project := sandbox(t)
	for _, key := range []string{"NOON_DEPTH", "NOON_JOB", "NOON_EVENT_FILE"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	stateDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "noon")
	manifest := `jobs:
  event:
    run: 'cat "$NOON_EVENT_FILE"'
  cooldown:
    run: 'true'
    cooldown: 10m
  probe:
    run: 'printf "%s" "$NOON_DEPTH" > job-depth'
    on_success: 'printf "%s" "$NOON_DEPTH" >> hook-depth'
  a:
    run: 'noon run b --detach'
  b:
    run: 'noon run a --detach'
`
	if err := os.WriteFile(filepath.Join(project, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	invalidCode, invalid := invoke(t, "--json", "run", "event", "--data", "not-json")
	oversized := `"` + strings.Repeat("x", store.MaxEventBytes) + `"`
	oversizedCode, oversizedResult := invoke(t, "--json", "run", "event", "--data", oversized)
	for _, result := range []struct {
		code  int
		value map[string]any
	}{{invalidCode, invalid}, {oversizedCode, oversizedResult}} {
		failure, ok := result.value["error"].(map[string]any)
		if result.code != 2 || !ok || failure["code"] != "usage" {
			t.Fatalf("invalid trigger data: code=%d result=%+v", result.code, result.value)
		}
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatalf("invalid or oversized trigger data created state: %v", err)
	}

	literal := []byte(`{"value":"$(touch trigger-payload-command)","kind":"literal"}`)
	code, stdout, stderr := triggerCLI(t, "run", "event", "--data", string(literal))
	if code != 0 {
		t.Fatalf("literal foreground run: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	literalRun := latestTriggerRun(t, stateDir, project, "event")
	literalFile := assertTriggerEvent(t, stateDir, literalRun, map[string]any{"value": "$(touch trigger-payload-command)", "kind": "literal"})
	if !bytes.Contains(stdout, literalFile) {
		t.Fatalf("foreground job did not read NOON_EVENT_FILE: stdout=%s event=%s", stdout, literalFile)
	}
	if _, err := os.Stat(filepath.Join(project, "trigger-payload-command")); !os.IsNotExist(err) {
		t.Fatalf("payload text was interpreted as a command: %v", err)
	}

	filePayload := []byte(`{"kind":"file","path":"@FILE"}`)
	if err := os.WriteFile(filepath.Join(project, "event-input.json"), filePayload, 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr = triggerCLI(t, "run", "event", "--data", "@event-input.json")
	if code != 0 {
		t.Fatalf("@FILE foreground run: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	fileRun := latestTriggerRun(t, stateDir, project, "event")
	fileEvent := assertTriggerEvent(t, stateDir, fileRun, map[string]any{"kind": "file", "path": "@FILE"})
	if !bytes.Contains(stdout, fileEvent) {
		t.Fatalf("@FILE job output does not contain its event: %s", stdout)
	}

	stdinPayload := `{"kind":"stdin","session_id":"hook-1"}`
	code, stdout, stderr = triggerCLIWithStdin(t, stdinPayload, "run", "event", "--data", "-")
	if code != 0 {
		t.Fatalf("stdin foreground run: exit=%d stdout=%s stderr=%s", code, stdout, stderr)
	}
	stdinRun := latestTriggerRun(t, stateDir, project, "event")
	stdinEvent := assertTriggerEvent(t, stateDir, stdinRun, map[string]any{"kind": "stdin", "session_id": "hook-1"})
	if !bytes.Contains(stdout, stdinEvent) {
		t.Fatalf("stdin job output does not contain its event: %s", stdout)
	}

	code, _, stderr = triggerCLI(t, "run", "probe")
	if code != 0 {
		t.Fatalf("foreground recursion-depth probe: exit=%d stderr=%s", code, stderr)
	}
	if got := mustRead(t, filepath.Join(project, "job-depth")); got != "1" {
		t.Fatalf("foreground job NOON_DEPTH=%q, want 1", got)
	}
	if got := mustRead(t, filepath.Join(project, "hook-depth")); got != "1" {
		t.Fatalf("foreground hook NOON_DEPTH=%q, want 1", got)
	}

	if code, result := invoke(t, "--json", "enable"); code != 0 {
		t.Fatalf("enable project: code=%d result=%+v", code, result)
	}
	wrapperDir := filepath.Join(project, "bin")
	if err := os.MkdirAll(wrapperDir, 0o700); err != nil {
		t.Fatal(err)
	}
	wrapper := "#!/bin/sh\nNOON_TRIGGER_HELPER=1 exec " + triggerShellQuote(os.Args[0]) + " -test.run '^TestTriggerCLIHelper$' -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(wrapperDir, "noon"), []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	server := startTriggerScheduler(t, project, stateDir)
	defer server.stop(t)

	queuedCode, queuedValue := triggerCLIJSONWithStdin(t, `{"kind":"queued","session_id":"hook-2"}`, "--json", "run", "event", "--detach", "--data", "-")
	if queuedCode != 0 || queuedValue["created"] != true {
		t.Fatalf("queue stdin event: code=%d result=%+v", queuedCode, queuedValue)
	}
	queuedID := int64(queuedValue["run"].(map[string]any)["id"].(float64))
	queuedRun := triggerRunByID(t, stateDir, queuedID)
	queuedFile := assertTriggerEvent(t, stateDir, queuedRun, map[string]any{"kind": "queued", "session_id": "hook-2"})
	if queuedRun.Trigger != "queued" {
		t.Fatalf("queued trigger=%q, want queued", queuedRun.Trigger)
	}
	server.tick(t)
	queuedRun = waitTriggerRun(t, server, stateDir, queuedID)
	queuedLog, err := os.ReadFile(filepath.Join(stateDir, queuedRun.LogPath.String))
	if err != nil || !bytes.Contains(queuedLog, queuedFile) {
		t.Fatalf("queued job did not read its persisted event file: log=%s err=%v", queuedLog, err)
	}

	os.Setenv("NOON_DEPTH", "2")
	probeCode, probeValue := invoke(t, "--json", "run", "probe", "--detach")
	_ = os.Unsetenv("NOON_DEPTH")
	if probeCode != 0 || probeValue["created"] != true {
		t.Fatalf("queue depth probe: code=%d result=%+v", probeCode, probeValue)
	}
	probeID := int64(probeValue["run"].(map[string]any)["id"].(float64))
	probeRun := waitTriggerHook(t, server, stateDir, probeID)
	if probeRun.Depth != 2 || mustRead(t, filepath.Join(project, "job-depth")) != "3" || mustRead(t, filepath.Join(project, "hook-depth")) != "13" {
		t.Fatalf("persisted depth did not reach job and hook: run=%+v job=%q hook=%q", probeRun, mustRead(t, filepath.Join(project, "job-depth")), mustRead(t, filepath.Join(project, "hook-depth")))
	}

	firstCode, firstKey := invoke(t, "--json", "run", "cooldown", "--detach", "--key", "stable")
	if firstCode != 0 || firstKey["created"] != true {
		t.Fatalf("first keyed request: code=%d result=%+v", firstCode, firstKey)
	}
	firstKeyID := int64(firstKey["run"].(map[string]any)["id"].(float64))
	otherCode, cooled := invoke(t, "--json", "run", "cooldown", "--detach", "--key", "other")
	if otherCode != 0 || cooled["created"] != false || cooled["reason"] != "cooldown" || cooled["cooldown_until"] == nil {
		t.Fatalf("cooldown response: code=%d result=%+v", otherCode, cooled)
	}
	if got := int64(cooled["run"].(map[string]any)["id"].(float64)); got != firstKeyID {
		t.Fatalf("cooldown returned run %d, want latest run %d", got, firstKeyID)
	}
	if _, err := time.Parse(store.TimestampLayout, cooled["cooldown_until"].(string)); err != nil {
		t.Fatalf("cooldown end is not a stored timestamp: %v", err)
	}
	retryCode, retry := invoke(t, "--json", "run", "cooldown", "--detach", "--key", "stable")
	if retryCode != 0 || retry["created"] != false || retry["reason"] != "duplicate" || int64(retry["run"].(map[string]any)["id"].(float64)) != firstKeyID {
		t.Fatalf("idempotency key was lost inside cooldown: code=%d result=%+v", retryCode, retry)
	}
	if len(triggerRunsForJob(t, stateDir, project, "cooldown")) != 1 {
		t.Fatalf("cooldown/key requests created more than one run: %+v", triggerRunsForJob(t, stateDir, project, "cooldown"))
	}

	beforeRecursion := len(triggerRuns(t, stateDir, project))
	t.Setenv("NOON_PROJECT", project)
	t.Setenv("NOON_JOB", "event")
	sameCode, same := invoke(t, "--json", "run", "event")
	t.Setenv("NOON_PROJECT", "")
	_ = os.Unsetenv("NOON_PROJECT")
	t.Setenv("NOON_JOB", "")
	_ = os.Unsetenv("NOON_JOB")
	if failure, ok := same["error"].(map[string]any); sameCode != 1 || !ok || failure["code"] != "recursion" {
		t.Fatalf("same-job recursion was not refused: code=%d result=%+v", sameCode, same)
	}
	t.Setenv("NOON_DEPTH", "3")
	depthCode, depth := invoke(t, "--json", "run", "event", "--detach")
	t.Setenv("NOON_DEPTH", "")
	_ = os.Unsetenv("NOON_DEPTH")
	if failure, ok := depth["error"].(map[string]any); depthCode != 1 || !ok || failure["code"] != "recursion" {
		t.Fatalf("depth-three recursion was not refused: code=%d result=%+v", depthCode, depth)
	}
	if len(triggerRuns(t, stateDir, project)) != beforeRecursion {
		t.Fatal("refused recursion created a run")
	}

	cycleCode, cycle := invoke(t, "--json", "run", "a", "--detach")
	if cycleCode != 0 || cycle["created"] != true {
		t.Fatalf("queue recursion cycle: code=%d result=%+v", cycleCode, cycle)
	}
	runs := waitTriggerCycle(t, server, stateDir, project)
	var aRuns, bRuns []store.Run
	for _, run := range runs {
		switch run.Job {
		case "a":
			aRuns = append(aRuns, run)
		case "b":
			bRuns = append(bRuns, run)
		}
	}
	if len(aRuns) != 2 || len(bRuns) != 1 || aRuns[0].Depth != 2 || aRuns[1].Depth != 0 || bRuns[0].Depth != 1 {
		t.Fatalf("detached recursion depths/runs: a=%+v b=%+v all=%+v", aRuns, bRuns, runs)
	}
	var refused bool
	for _, run := range aRuns {
		if run.Depth != 2 {
			continue
		}
		log, err := os.ReadFile(filepath.Join(stateDir, run.LogPath.String))
		if err == nil && run.Status == "failed" && bytes.Contains(log, []byte("recursion guard refused request at NOON_DEPTH=3")) {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("A -> B -> A detached chain did not stop at depth 3: %+v", aRuns)
	}
}

func TestTriggerCLIHelper(t *testing.T) {
	if os.Getenv("NOON_TRIGGER_HELPER") != "1" {
		return
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "missing CLI helper arguments")
		os.Exit(125)
	}
	code := cli.Run(context.Background(), append([]string{"noon"}, os.Args[separator+1:]...), config.BuildOpts{Version: "trigger-test"}, os.Stdout, os.Stderr)
	os.Exit(code)
}

func triggerCLI(t *testing.T, args ...string) (int, []byte, []byte) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{Version: "trigger-test"}, &stdout, &stderr)
	return code, stdout.Bytes(), stderr.Bytes()
}

func triggerCLIWithStdin(t *testing.T, input string, args ...string) (int, []byte, []byte) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(writer, input); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = reader
	defer func() {
		os.Stdin = previous
		_ = reader.Close()
	}()
	return triggerCLI(t, args...)
}

func triggerCLIJSONWithStdin(t *testing.T, input string, args ...string) (int, map[string]any) {
	t.Helper()
	code, stdout, stderr := triggerCLIWithStdin(t, input, args...)
	var value map[string]any
	if err := json.Unmarshal(stdout, &value); err != nil {
		t.Fatalf("decode CLI JSON: %v stdout=%s stderr=%s", err, stdout, stderr)
	}
	return code, value
}

func latestTriggerRun(t *testing.T, stateDir, project, job string) store.Run {
	t.Helper()
	runs := triggerRunsForJob(t, stateDir, project, job)
	if len(runs) == 0 {
		t.Fatalf("no run recorded for %s", job)
	}
	latest := runs[0]
	for _, run := range runs[1:] {
		if run.ID > latest.ID {
			latest = run
		}
	}
	return latest
}

func assertTriggerEvent(t *testing.T, stateDir string, run store.Run, wantData map[string]any) []byte {
	t.Helper()
	if !run.EventPath.Valid {
		t.Fatalf("run %d has no event path: %+v", run.ID, run)
	}
	path := filepath.Join(stateDir, filepath.FromSlash(run.EventPath.String))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("event file for run %d: %v", run.ID, err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("event file mode=%o, want 600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatalf("decode event file: %v: %s", err, data)
	}
	if event["schema_version"] != float64(1) || event["source"] != "cli" || !reflect.DeepEqual(event["data"], wantData) {
		t.Fatalf("event file shape/data: %+v", event)
	}
	if _, err := time.Parse(store.TimestampLayout, event["received_at"].(string)); err != nil {
		t.Fatalf("event received_at=%v: %v", event["received_at"], err)
	}
	return data
}

func startTriggerScheduler(t *testing.T, project, stateDir string) *triggerScheduler {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	stateHome := filepath.Dir(stateDir)
	server := &triggerScheduler{ticks: make(chan time.Time), ack: make(chan struct{}, 1), done: make(chan error, 1), cancel: cancel}
	go func() {
		server.done <- scheduler.Serve(ctx, scheduler.ServeOptions{
			StateDir: stateDir, StateDirDefault: stateDir, UserConfigDir: os.Getenv("XDG_CONFIG_HOME"), Version: "trigger-test",
			Environment: []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "XDG_CONFIG_HOME=" + os.Getenv("XDG_CONFIG_HOME"), "XDG_STATE_HOME=" + stateHome, "NOON_DEPTH=9"},
			Now:         time.Now, Ticks: server.ticks, KillGrace: 20 * time.Millisecond, Logger: io.Discard, IgnoreSignals: true,
			OnTick: func() { server.ack <- struct{}{} },
		})
	}()
	server.tick(t)
	return server
}

type triggerScheduler struct {
	ticks   chan time.Time
	ack     chan struct{}
	done    chan error
	cancel  context.CancelFunc
	stopped bool
}

func (s *triggerScheduler) tick(t *testing.T) {
	t.Helper()
	select {
	case s.ticks <- time.Now():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out sending trigger scheduler tick")
	}
	select {
	case <-s.ack:
	case err := <-s.done:
		s.stopped = true
		t.Fatalf("trigger scheduler exited before tick: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for trigger scheduler tick")
	}
}

func (s *triggerScheduler) stop(t *testing.T) {
	t.Helper()
	if s.stopped {
		return
	}
	s.cancel()
	select {
	case err := <-s.done:
		s.stopped = true
		if err != nil {
			t.Errorf("trigger scheduler shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Error("trigger scheduler did not stop")
	}
}

func waitTriggerHook(t *testing.T, server *triggerScheduler, stateDir string, id int64) store.Run {
	t.Helper()
	run := waitTriggerRun(t, server, stateDir, id)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for run.HookStatus.String == "running" {
		server.tick(t)
		run = triggerRunByID(t, stateDir, id)
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("hook for run %d did not finish: %+v", id, run)
		}
	}
	return run
}

func waitTriggerRun(t *testing.T, server *triggerScheduler, stateDir string, id int64) store.Run {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		run := triggerRunByID(t, stateDir, id)
		if run.Status != "queued" && run.Status != "running" {
			return run
		}
		server.tick(t)
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("run %d did not finish: %+v", id, run)
		}
	}
}

func waitTriggerCycle(t *testing.T, server *triggerScheduler, stateDir, project string) []store.Run {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	for {
		server.tick(t)
		runs := triggerRuns(t, stateDir, project)
		var relevant []store.Run
		complete := true
		for _, run := range runs {
			if run.Job != "a" && run.Job != "b" {
				continue
			}
			relevant = append(relevant, run)
			if run.Status == "queued" || run.Status == "running" {
				complete = false
			}
		}
		if complete && len(relevant) == 3 {
			return relevant
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatalf("detached recursion chain did not finish: %+v", relevant)
		}
	}
}

func triggerRuns(t *testing.T, stateDir, project string) []store.Run {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	runs, err := state.RunsForProject(canonical, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func triggerRunsForJob(t *testing.T, stateDir, project, job string) []store.Run {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	runs, err := state.RunsForProject(canonical, job, 50)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func triggerRunByID(t *testing.T, stateDir string, id int64) store.Run {
	t.Helper()
	state, err := store.OpenExisting(stateDir)
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

func triggerShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
