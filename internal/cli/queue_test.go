package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

func TestQueueRuns(t *testing.T) {
	root := sandbox(t)
	manifest := `timezone: America/Denver
jobs:
  tests: {run: 'true'}
  keyed: {run: 'true'}
  racing: {run: 'true'}
  queued-cancel: {run: 'true'}
  shared: {run: 'true'}
`
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	code, refusal := invoke(t, "run", "tests", "--detach", "--json")
	if code != 1 || refusal["error"].(map[string]any)["code"] != "not_enabled" || refusal["error"].(map[string]any)["hint"] != "Run noon enable" {
		t.Fatalf("not-enabled queue: exit=%d result=%+v", code, refusal)
	}
	if code, result := invoke(t, "enable", "--json"); code != 0 {
		t.Fatalf("enable: exit=%d result=%+v", code, result)
	}

	stateDir := filepath.Join(root, "state", "noon")
	projectID := func() int64 {
		t.Helper()
		state, err := store.Open(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		defer state.Close()
		id, err := state.RegisterProject(root, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return id
	}()
	queued := func(id int64) store.Run {
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
	checkDue := func(run store.Run, want time.Time) {
		t.Helper()
		if run.Status != "queued" || run.Trigger != "queued" || !run.ScheduledAt.Valid || !run.DueAt.Valid || run.ScheduledAt.String != run.DueAt.String || run.RequestedBy.String != "cli" {
			t.Fatalf("queued run fields: %+v", run)
		}
		got, err := time.Parse(store.TimestampLayout, run.DueAt.String)
		if err != nil || absDuration(got.Sub(want)) > time.Second {
			t.Fatalf("queued due=%s err=%v, want near %s", run.DueAt.String, err, want)
		}
	}
	decodeRunID := func(result map[string]any) int64 {
		t.Helper()
		return int64(result["run"].(map[string]any)["id"].(float64))
	}

	before := time.Now().UTC()
	code, detached := invoke(t, "run", "tests", "--detach", "--json")
	if code != 0 || detached["created"] != true || detached["scheduler"] != "offline" {
		t.Fatalf("detach: exit=%d result=%+v", code, detached)
	}
	detachedID := decodeRunID(detached)
	checkDue(queued(detachedID), before)
	if _, exists := detached["reason"]; exists {
		t.Fatalf("created queue result contains reason: %+v", detached)
	}

	before = time.Now().UTC()
	code, inResult := invoke(t, "run", "tests", "--in", "5s", "--json")
	if code != 0 || inResult["created"] != true {
		t.Fatalf("--in: exit=%d result=%+v", code, inResult)
	}
	checkDue(queued(decodeRunID(inResult)), before.Add(5*time.Second))

	before = time.Now().UTC()
	location, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Fatal(err)
	}
	targetClock := before.In(location).Add(2 * time.Hour)
	clockDue := time.Date(targetClock.Year(), targetClock.Month(), targetClock.Day(), targetClock.Hour(), targetClock.Minute(), 0, 0, location)
	code, clockResult := invoke(t, "run", "tests", "--at", targetClock.Format("15:04"), "--json")
	if code != 0 || clockResult["created"] != true {
		t.Fatalf("--at HH:MM: exit=%d result=%+v", code, clockResult)
	}
	checkDue(queued(decodeRunID(clockResult)), clockDue)

	localTarget := before.Add(48 * time.Hour).In(location).Truncate(time.Minute)
	code, localResult := invoke(t, "run", "tests", "--at", localTarget.Format("2006-01-02 15:04"), "--json")
	if code != 0 || localResult["created"] != true {
		t.Fatalf("--at local datetime: exit=%d result=%+v", code, localResult)
	}
	checkDue(queued(decodeRunID(localResult)), localTarget)

	rfcTarget := before.Add(72 * time.Hour).UTC().Truncate(time.Second)
	code, rfcResult := invoke(t, "run", "tests", "--at", rfcTarget.Format(time.RFC3339), "--json")
	if code != 0 || rfcResult["created"] != true {
		t.Fatalf("--at RFC3339: exit=%d result=%+v", code, rfcResult)
	}
	checkDue(queued(decodeRunID(rfcResult)), rfcTarget)

	code, firstKey := invoke(t, "run", "keyed", "--detach", "--key", "nightly:42", "--json")
	if code != 0 || firstKey["created"] != true {
		t.Fatalf("first keyed run: exit=%d result=%+v", code, firstKey)
	}
	keyRunID := decodeRunID(firstKey)
	code, duplicate := invoke(t, "run", "keyed", "--detach", "--key", "nightly:42", "--json")
	if code != 0 || duplicate["created"] != false || duplicate["reason"] != "duplicate" || decodeRunID(duplicate) != keyRunID {
		t.Fatalf("idempotent retry: exit=%d result=%+v", code, duplicate)
	}

	var dashboardOut, dashboardErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon"}, config.BuildOpts{}, &dashboardOut, &dashboardErr); code != 0 || !strings.Contains(dashboardOut.String(), "queued: tests due") {
		t.Fatalf("queued dashboard notice: exit=%d stdout=%s stderr=%s", code, &dashboardOut, &dashboardErr)
	}
	var cancelListOut, cancelListErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "cancel"}, config.BuildOpts{}, &cancelListOut, &cancelListErr); code != 0 || !strings.Contains(cancelListOut.String(), "Next: noon cancel "+fmt.Sprint(detachedID)) {
		t.Fatalf("active run inspection: exit=%d stdout=%s stderr=%s", code, &cancelListOut, &cancelListErr)
	}
	if run := queued(detachedID); run.Status != "queued" {
		t.Fatalf("bare cancel changed run status: %+v", run)
	}

	code, coalesced := invoke(t, "run", "tests", "--detach", "--key", "different-key", "--json")
	if code != 0 || coalesced["created"] != false || coalesced["reason"] != "queued" || decodeRunID(coalesced) != detachedID {
		t.Fatalf("due-run coalescing: exit=%d result=%+v", code, coalesced)
	}
	code, independent := invoke(t, "run", "tests", "--in", "10s", "--json")
	if code != 0 || independent["created"] != true || decodeRunID(independent) == detachedID {
		t.Fatalf("future one-shot was coalesced: exit=%d result=%+v", code, independent)
	}

	var out, stderr bytes.Buffer
	code = cli.Run(context.Background(), []string{"noon", "run", "tests", "--detach"}, config.BuildOpts{}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), "The scheduler isn't running, so nothing runs until it starts.") || !(strings.Contains(out.String(), "noon service install") || strings.Contains(out.String(), "noon serve")) {
		t.Fatalf("offline guidance: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}

	past := time.Now().Add(-24 * time.Hour).UTC().Format("2006-01-02 15:04")
	code, pastResult := invoke(t, "run", "tests", "--at", past, "--json")
	pastError, ok := pastResult["error"].(map[string]any)
	if code != 2 || !ok || pastError["code"] != "usage" || !strings.Contains(pastError["message"].(string), "past") {
		t.Fatalf("past --at: exit=%d result=%+v", code, pastResult)
	}
	for _, args := range [][]string{
		{"run", "tests", "--detach", "--in", "1s"},
		{"run", "tests", "--key", "missing-mode"},
		{"run", "tests", "--in", "500ms"},
		{"run", "tests", "--detach", "--key", "bad key"},
	} {
		code, result := invoke(t, append(args, "--json")...)
		failure, ok := result["error"].(map[string]any)
		if code != 2 || !ok || failure["code"] != "usage" {
			t.Fatalf("invalid queue args %v: exit=%d result=%+v", args, code, result)
		}
	}

	var helpOut, helpErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "run", "--help"}, config.BuildOpts{}, &helpOut, &helpErr); code != 0 || !strings.Contains(helpOut.String(), "newest 50 terminal runs per job") {
		t.Fatalf("run help: exit=%d stdout=%s stderr=%s", code, &helpOut, &helpErr)
	}

	// Different keys must not defeat due-now coalescing under concurrent writers.
	const requests = 10
	var group sync.WaitGroup
	errs := make(chan error, requests)
	for i := range requests {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			var requestOut, requestErr bytes.Buffer
			args := []string{"noon", "run", "racing", "--detach", "--key", fmt.Sprintf("parallel-%d", i), "--json"}
			if code := cli.Run(context.Background(), args, config.BuildOpts{}, &requestOut, &requestErr); code != 0 {
				errs <- fmt.Errorf("request %d exit=%d stdout=%s stderr=%s", i, code, &requestOut, &requestErr)
				return
			}
			var value map[string]any
			if err := json.Unmarshal(requestOut.Bytes(), &value); err != nil {
				errs <- err
				return
			}
		}(i)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	racingRuns, err := state.RunsForJob(projectID, "racing")
	_ = state.Close()
	if err != nil || len(racingRuns) != 1 || racingRuns[0].Status != "queued" {
		t.Fatalf("concurrent due requests created %d rows: %+v err=%v", len(racingRuns), racingRuns, err)
	}

	// A changed shared digest must be refused after the project's earlier approval.
	changed := strings.Replace(manifest, "shared: {run: 'true'}", "shared: {run: 'false'}", 1)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	code, approval := invoke(t, "run", "shared", "--detach", "--json")
	if code != 1 || approval["error"].(map[string]any)["code"] != "needs_approval" || approval["error"].(map[string]any)["hint"] != "Run noon enable" {
		t.Fatalf("unapproved queue: exit=%d result=%+v", code, approval)
	}
	if code, _ := invoke(t, "disable", "--json"); code != 0 {
		t.Fatalf("disable: exit=%d", code)
	}
	if code, duplicate := invoke(t, "run", "keyed", "--detach", "--key", "nightly:42", "--json"); code != 0 || duplicate["reason"] != "duplicate" {
		t.Fatalf("key lookup did not precede project requirement: exit=%d result=%+v", code, duplicate)
	}
	state, err = store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	canceledRace, err := state.RunByID(racingRuns[0].ID)
	_ = state.Close()
	if err != nil || canceledRace.Status != "cancelled" {
		t.Fatalf("disable did not cancel queued work: %+v err=%v", canceledRace, err)
	}

	t.Run("queued cancellation skips unrelated orphan recovery", func(t *testing.T) {
		root := sandbox(t)
		manifest := `jobs:
  queued-cancel: {run: 'true'}
`
		if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		if code, _ := invoke(t, "enable", "--json"); code != 0 {
			t.Fatal("enable failed")
		}
		code, queued := invoke(t, "run", "queued-cancel", "--in", "1h", "--json")
		if code != 0 || queued["created"] != true {
			t.Fatalf("queue run: exit=%d result=%+v", code, queued)
		}
		queuedID := int64(queued["run"].(map[string]any)["id"].(float64))

		ready := filepath.Join(root, "orphan-ready")
		cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf(`trap '' TERM; printf ready > %s; while :; do sleep 30; done`, ready))
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() }()
		readyDeadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(readyDeadline) {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, err := os.Stat(ready); err != nil {
			t.Fatal("unrelated orphan did not become ready")
		}
		leaderStart, err := runner.ProcessStartIdentity(cmd.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		project, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		stateDir := filepath.Join(root, "state", "noon")
		state, err := store.Open(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		projectID, err := state.RegisterProject(project, time.Now())
		if err != nil {
			_ = state.Close()
			t.Fatal(err)
		}
		orphan, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: "unrelated", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: os.Getpid(), OwnerStart: "stale-owner"})
		if err != nil {
			_ = state.Close()
			t.Fatal(err)
		}
		if err := state.SetProcessIdentity(orphan.ID, cmd.Process.Pid, leaderStart); err != nil {
			_ = state.Close()
			t.Fatal(err)
		}
		if err := state.Close(); err != nil {
			t.Fatal(err)
		}

		started := time.Now()
		var out, stderr bytes.Buffer
		if code := cli.Run(context.Background(), []string{"noon", "cancel", "queued-cancel"}, config.BuildOpts{}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "Cancelled queued run") {
			t.Fatalf("cancel queued job: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
		}
		if elapsed := time.Since(started); elapsed > 2*time.Second {
			t.Fatalf("queued cancellation spent %s recovering an unrelated orphan", elapsed)
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("queued cancellation terminated unrelated orphan: %v", err)
		}
		state, err = store.OpenExisting(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		defer state.Close()
		orphanAfter, err := state.RunByID(orphan.ID)
		if err != nil || orphanAfter.Status != "running" {
			t.Fatalf("queued cancellation recovered unrelated run: %+v err=%v", orphanAfter, err)
		}
		queuedAfter, err := state.RunByID(queuedID)
		if err != nil || queuedAfter.Status != "cancelled" {
			t.Fatalf("queued run not cancelled: %+v err=%v", queuedAfter, err)
		}
	})
}

func TestCancelQueued(t *testing.T) {
	t.Run("retention clears cancelled run payload path", func(t *testing.T) {
		root := sandbox(t)
		if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte("jobs:\n  tests: {run: 'true'}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if code, _ := invoke(t, "enable", "--json"); code != 0 {
			t.Fatal("enable failed")
		}
		code, queued := invoke(t, "run", "tests", "--in", "24h", "--data", `{"source":"retention"}`, "--json")
		if code != 0 {
			t.Fatalf("queue: %v", queued)
		}
		run := queued["run"].(map[string]any)
		id := fmt.Sprint(run["id"])
		eventPath := run["event_file"].(string)
		if _, err := os.Stat(eventPath); err != nil {
			t.Fatal(err)
		}
		for range 50 {
			var out, stderr bytes.Buffer
			if code := cli.Run(context.Background(), []string{"noon", "run", "tests"}, config.BuildOpts{}, &out, &stderr); code != 0 {
				t.Fatalf("foreground run: exit=%d stderr=%s", code, &stderr)
			}
		}
		code, cancelled := invoke(t, "cancel", id, "--json")
		if code != 0 {
			t.Fatalf("cancel: %v", cancelled)
		}
		result := cancelled["runs"].([]any)[0].(map[string]any)
		log := result["log"].(map[string]any)
		if result["status"] != "cancelled" || result["event_file"] != nil || log["path"] != nil || log["available"] != false {
			t.Fatalf("pruned cancellation metadata: %v", result)
		}
		if _, err := os.Stat(eventPath); !os.IsNotExist(err) {
			t.Fatalf("event file not pruned: %v", err)
		}
	})
	root := sandbox(t)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte("timezone: UTC\njobs:\n  tests: {run: 'true'}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _ := invoke(t, "enable", "--json"); code != 0 {
		t.Fatal("enable failed")
	}
	stateDir := filepath.Join(root, "state", "noon")
	project, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, args := range [][]string{{"run", "tests", "--detach"}, {"run", "tests", "--in", "1h"}, {"run", "tests", "--in", "2h"}} {
		var out, stderr bytes.Buffer
		if code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr); code != 0 {
			t.Fatalf("queue %v: exit=%d stdout=%s stderr=%s", args, code, &out, &stderr)
		}
		state, err := store.OpenExisting(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		runs, err := state.RunsForProject(project, "tests", 5)
		_ = state.Close()
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, runs[0].ID)
	}
	var listOut, listErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "cancel"}, config.BuildOpts{}, &listOut, &listErr); code != 0 || !strings.Contains(listOut.String(), "Next: noon cancel") {
		t.Fatalf("cancel list: exit=%d stdout=%s stderr=%s", code, &listOut, &listErr)
	}
	var cancelOut, cancelErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "cancel", "tests"}, config.BuildOpts{}, &cancelOut, &cancelErr); code != 0 || strings.Count(cancelOut.String(), "Cancelled queued run") != 3 {
		t.Fatalf("cancel job: exit=%d stdout=%s stderr=%s", code, &cancelOut, &cancelErr)
	}
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		run, err := state.RunByID(id)
		if err != nil || run.Status != "cancelled" || run.StartedAt != "" {
			t.Fatalf("job cancellation run %d: %+v err=%v", id, run, err)
		}
	}
	_ = state.Close()
	var finishedOut, finishedErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "cancel", fmt.Sprint(ids[0])}, config.BuildOpts{}, &finishedOut, &finishedErr); code != 0 || !strings.Contains(finishedOut.String(), "already cancelled") {
		t.Fatalf("finished-run no-op: exit=%d stdout=%s stderr=%s", code, &finishedOut, &finishedErr)
	}
	var disableOut, disableErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "run", "tests", "--in", "1h"}, config.BuildOpts{}, &disableOut, &disableErr); code != 0 {
		t.Fatalf("queue before disable: exit=%d stdout=%s stderr=%s", code, &disableOut, &disableErr)
	}
	state, err = store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := state.RunsForProject(project, "tests", 1)
	_ = state.Close()
	if err != nil {
		t.Fatal(err)
	}
	queuedID := runs[0].ID
	if code, _ := invoke(t, "disable", "--json"); code != 0 {
		t.Fatalf("disable: exit=%d", code)
	}
	state, err = store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	run, err := state.RunByID(queuedID)
	_ = state.Close()
	if err != nil || run.Status != "cancelled" {
		t.Fatalf("disable did not cancel queue: %+v err=%v", run, err)
	}
}

func TestCancelForeground(t *testing.T) {
	root := sandbox(t)
	startedMarker := filepath.Join(root, "started")
	termMarker := filepath.Join(root, "term")
	manifest := fmt.Sprintf("jobs:\n  tests:\n    run: 'printf started > %s; trap \"printf term > %s; trap '''' TERM\" TERM; while :; do sleep 30; done'\n", startedMarker, termMarker)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	type commandResult struct {
		code int
		out  string
		err  string
	}
	done := make(chan commandResult, 1)
	go func() {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "run", "tests"}, config.BuildOpts{}, &out, &stderr)
		done <- commandResult{code: code, out: out.String(), err: stderr.String()}
	}()

	stateDir := filepath.Join(root, "state", "noon")
	project, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var run store.Run
	for time.Now().Before(deadline) {
		state, err := store.OpenExisting(stateDir)
		if err == nil {
			runs, readErr := state.RunsForProject(project, "tests", 5)
			_ = state.Close()
			if readErr == nil && len(runs) > 0 && runs[0].Status == "running" {
				run = runs[0]
				break
			}
		}
		select {
		case result := <-done:
			t.Fatalf("foreground run exited before cancellation: %+v", result)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.ID == 0 {
		t.Fatal("foreground run did not become active")
	}
	childDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(childDeadline) {
		if _, err := os.Stat(startedMarker); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(startedMarker); err != nil {
		t.Fatal("foreground child did not start")
	}
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	run, err = state.RunByID(run.ID)
	_ = state.Close()
	if err != nil || !run.PGID.Valid {
		t.Fatalf("foreground process group was not recorded: %+v err=%v", run, err)
	}

	cancelDone := make(chan commandResult, 1)
	go func() {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "cancel", fmt.Sprint(run.ID)}, config.BuildOpts{}, &out, &stderr)
		cancelDone <- commandResult{code: code, out: out.String(), err: stderr.String()}
	}()
	foregroundFinished, cancelFinished := false, false
	defer func() {
		if !foregroundFinished {
			_ = syscall.Kill(-int(run.PGID.Int64), syscall.SIGKILL)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
			}
		}
		if !cancelFinished {
			select {
			case <-cancelDone:
			case <-time.After(3 * time.Second):
			}
		}
	}()

	requestDeadline := time.Now().Add(5 * time.Second)
	requested := false
	for time.Now().Before(requestDeadline) {
		state, err := store.OpenExisting(stateDir)
		if err == nil {
			current, readErr := state.RunByID(run.ID)
			_ = state.Close()
			if readErr == nil && current.CancelRequestedAt.Valid {
				requested = true
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !requested {
		t.Fatal("cancel command did not persist its request")
	}
	// The TERM marker proves the durable request was observed before testing
	// that the signal reader remains alive for force-kill signals.
	termDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(termDeadline) {
		if _, err := os.Stat(termMarker); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(termMarker); err != nil {
		t.Fatal("foreground run did not receive TERM for the durable cancellation")
	}
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		foregroundFinished = true
		if result.code != 130 {
			t.Fatalf("foreground exit=%d stdout=%s stderr=%s", result.code, result.out, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second OS signal did not force-kill the cancelled foreground run")
	}
	select {
	case result := <-cancelDone:
		cancelFinished = true
		if result.code != 0 || !strings.Contains(result.out, "Cancelled run") {
			t.Fatalf("cancel command: exit=%d stdout=%s stderr=%s", result.code, result.out, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel command did not observe the run's terminal status")
	}
	state, err = store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	finished, err := state.RunByID(run.ID)
	if err != nil || finished.Status != "cancelled" || !finished.ExitCode.Valid || finished.ExitCode.Int64 != 130 {
		t.Fatalf("cancelled foreground run: %+v err=%v", finished, err)
	}
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
