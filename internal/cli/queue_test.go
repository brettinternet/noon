package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

// Temporary requests cannot bypass approval, lifecycle restrictions, or bounded parsing.
func TestTemporaryScheduleCLI(t *testing.T) {
	root := sandbox(t)
	manifest := "timezone: UTC\njobs:\n  ping: {run: ['true']}\n  disabled: {run: ['true'], enabled: false}\n  ended: {run: ['true'], until: '2000-01-01 00:00'}\n  other: {run: ['true'], os: [linux], arch: [arm64]}\n"
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	if code, result := invoke(t, "enable", "--json"); code != 0 {
		t.Fatal(result)
	}
	request := []string{"run", "ping", "--schedule", "*/10 * * * *", "--for", "2h", "--json"}
	code, first := invoke(t, request...)
	if code != 0 || first["created"] != true {
		t.Fatalf("create: %d %+v", code, first)
	}
	value := first["temporary_schedule"].(map[string]any)
	if value["requested_by"] != "cli" || value["schedule"] != "*/10 * * * *" {
		t.Fatal(value)
	}
	_, list := invoke(t, "list", "--json")
	for _, item := range list["jobs"].([]any) {
		job := item.(map[string]any)
		if job["name"] == "ping" && (job["next_at"] != value["next_at"] || job["temporary_schedule"].(map[string]any)["until"] != value["until"]) {
			t.Fatal(job)
		}
	}
	if code, out, errOut := invokeText(t); code != 0 || !strings.Contains(out, "1 temporary schedule: ping") {
		t.Fatalf("notice: %d %s %s", code, out, errOut)
	}
	if code, result := invoke(t, request...); code != 1 || result["error"].(map[string]any)["code"] != "already_scheduled" {
		t.Fatalf("conflict: %d %+v", code, result)
	}
	if code, out, errOut := invokeText(t, "cancel", "ping"); code != 0 {
		t.Fatalf("cancel: %d %s %s", code, out, errOut)
	}
	keyed := []string{"run", "ping", "--schedule", "every 10m", "--for", "1h", "--key", "k1", "--json"}
	_, first = invoke(t, keyed...)
	if code, result := invoke(t, keyed...); code != 0 || result["created"] != false || result["reason"] != "duplicate" || result["temporary_schedule"].(map[string]any)["created_at"] != first["temporary_schedule"].(map[string]any)["created_at"] {
		t.Fatalf("dedupe: %d %+v", code, result)
	}
	for _, flags := range [][]string{
		{"--schedule", "every 10m"}, {"--schedule", "every 10m", "--for", "8d"},
		{"--schedule", "every 10m", "--for", "1h", "--until", "17:00"},
		{"--schedule", "0 9 1 1 *", "--for", "1h"}, {"--schedule", "every 10m", "--for", "1h", "--detach"},
		{"--schedule", "every day"}, {"--for", "1h"}, {"--schedule", "every 10m", "--for", "1h", "--in", "2h"},
	} {
		args := append([]string{"run", "ping"}, flags...)
		args = append(args, "--json")
		if code, result := invoke(t, args...); code != 2 || result["error"].(map[string]any)["code"] != "usage" {
			t.Fatalf("syntax %v: %d %+v", flags, code, result)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest+"  shared: {run: ['true']}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"shared": "needs_approval", "disabled": "disabled", "ended": "ended"} {
		if code, result := invoke(t, "run", name, "--schedule", "every 10m", "--for", "1h", "--json"); code != 1 || result["error"].(map[string]any)["code"] != want {
			t.Fatalf("eligibility %s: %d %+v", name, code, result)
		}
	}
	if code, result := invoke(t, "cancel", "ping", "--json"); code != 0 {
		t.Fatal(result)
	}
	if code, result := invoke(t, "pause", "ping", "--json"); code != 0 {
		t.Fatal(result)
	}
	if code, result := invoke(t, keyed...); code != 1 || result["error"].(map[string]any)["code"] != "paused" {
		t.Fatalf("paused: %d %+v", code, result)
	}
}

// Ended jobs and requests due at the exclusive end must not enter the queue;
// an explicit foreground run remains authorized.
func TestUntilQueueEligibility(t *testing.T) {
	root := sandbox(t)
	manifest := "timezone: America/Denver\njobs:\n  future: {schedule: every 1h, until: '2099-01-01 00:00', run: ['true']}\n  past: {schedule: every 1h, until: '2000-01-01 00:00', run: ['true']}\n"
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, result := invoke(t, "enable", "--json"); code != 0 {
		t.Fatalf("enable: %d %+v", code, result)
	}
	for _, args := range [][]string{
		{"run", "past", "--detach", "--json"},
		{"run", "future", "--at", "2099-01-01 00:00", "--json"},
		{"run", "future", "--at", "2099-01-01T00:00:01-07:00", "--json"},
	} {
		code, result := invoke(t, args...)
		if code != 1 || result["error"].(map[string]any)["code"] != "ended" || !strings.Contains(result["error"].(map[string]any)["hint"].(string), "noon.yaml") {
			t.Fatalf("end refusal: %d %+v", code, result)
		}
	}
	if code, result := invoke(t, "history", "--json"); code != 0 || len(result["runs"].([]any)) != 0 {
		t.Fatalf("refused request created work: %d %+v", code, result)
	}
	if code, result := invoke(t, "run", "future", "--detach", "--json"); code != 0 {
		t.Fatalf("before end: %d %+v", code, result)
	}
	if code, _, stderr := invokeText(t, "run", "past"); code != 0 {
		t.Fatalf("foreground ended: %d %s", code, stderr)
	}
	code, result := invoke(t, "list", "--json")
	if code != 0 {
		t.Fatal(result)
	}
	jobs := result["jobs"].([]any)
	future, past := jobs[0].(map[string]any), jobs[1].(map[string]any)
	if future["until"] != "2099-01-01T00:00:00-07:00" || future["ended"] != false || future["next_at"] == nil || past["ended"] != true {
		t.Fatalf("end inspection: %+v", jobs)
	}
	if _, exists := past["next_at"]; exists {
		t.Fatal("ended job exposed next_at")
	}
	// A shell entry can cross expiry while loading config or waiting on SQLite.
	end := time.Date(2099, 1, 1, 7, 0, 0, 0, time.UTC)
	_, err := app.EnterProject(config.Input{CWD: root, UserConfigDir: os.Getenv("XDG_CONFIG_HOME"), StateDirDefault: filepath.Join(root, "state", "noon")},
		app.ShellEntry{Shell: "bash", Job: "future", Directory: root}, end.Add(-time.Second), func() time.Time { return end })
	var queueError *app.QueueError
	if !errors.As(err, &queueError) || queueError.Code != "ended" {
		t.Fatalf("shell entry did not refresh expiry: %v", err)
	}
	// A valid explicit zero-time end must remain distinguishable from unset.
	zeroManifest := manifest + "  zero: {until: 0001-01-01T00:00:00Z, run: ['true']}\n"
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(zeroManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, result := invoke(t, "run", "zero", "--detach", "--json"); code != 1 || result["error"].(map[string]any)["code"] != "ended" {
		t.Fatalf("zero-time end allowed queue: %d %+v", code, result)
	}
	// Description is cosmetic; extending the end requires a fresh approval.
	manifest = strings.Replace(manifest, "schedule: every 1h, until:", "description: changed, schedule: every 1h, until:", 1)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	_, result = invoke(t, "list", "--json")
	if result["jobs"].([]any)[0].(map[string]any)["approval"] != "approved" {
		t.Fatalf("description revoked approval: %+v", result)
	}
	manifest = strings.Replace(manifest, "2099-01-01", "2099-01-02", 1)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	_, result = invoke(t, "list", "--json")
	if result["jobs"].([]any)[0].(map[string]any)["approval"] != "needs_approval" {
		t.Fatalf("end edit retained approval: %+v", result)
	}
}

// Guards duplicate side effects, preflight validation, and mixed cancellation selectors.
func TestMultipleQueuedRunsAndCancellation(t *testing.T) {
	root := sandbox(t)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte("jobs:\n  a: {run: 'true'}\n  b: {run: 'true'}\n  disabled: {run: 'true', enabled: false}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, result := invoke(t, "enable", "--json"); code != 0 {
		t.Fatalf("enable: %d %+v", code, result)
	}
	if code, result := invoke(t, "run", "a", "b", "--json"); code != 2 {
		t.Fatalf("foreground batch: %d %+v", code, result)
	}
	if code, result := invoke(t, "run", "a", "missing", "--detach", "--json"); code != 1 {
		t.Fatalf("invalid queue batch: %d %+v", code, result)
	}
	if code, result := invoke(t, "history", "--json"); code != 0 || len(result["runs"].([]any)) != 0 {
		t.Fatalf("invalid queue created work: %d %+v", code, result)
	}
	for i, mode := range [][]string{{"--detach"}, {"--in", "2h"}, {"--at", "2099-01-01T00:00:00Z"}} {
		key := fmt.Sprintf("batch-%d", i)
		args := append([]string{"run", "a", "b", "a", "--key", key, "--data", `{"batch":true}`, "--json"}, mode...)
		code, result := invoke(t, args...)
		if code != 0 || len(result["results"].([]any)) != 2 || len(result["errors"].([]any)) != 0 {
			t.Fatalf("queue batch: %d %+v", code, result)
		}
		queued := result["results"].([]any)
		firstID := queued[0].(map[string]any)["run"].(map[string]any)["id"].(float64)
		secondID := queued[1].(map[string]any)["run"].(map[string]any)["id"].(float64)
		if firstID == secondID {
			t.Fatalf("key was not job-scoped: %+v", queued)
		}
		code, retry := invoke(t, args...)
		if code != 0 {
			t.Fatalf("retry: %d %+v", code, retry)
		}
		for j, item := range retry["results"].([]any) {
			r := item.(map[string]any)
			previous := queued[j].(map[string]any)["run"].(map[string]any)
			run := r["run"].(map[string]any)
			if r["created"] != false || run["id"] != previous["id"] || run["event_file"] == nil {
				t.Fatalf("retry identity/payload: %+v", r)
			}
		}
		id := fmt.Sprintf("%.0f", firstID)
		for _, invalid := range []string{"999999", "missing", ""} {
			if code, result := invoke(t, "cancel", "a", invalid, "--json"); code == 0 {
				t.Fatalf("invalid cancellation: %d %+v", code, result)
			}
		}
		if code, result := invoke(t, "history", "a", "b", "--status", "queued", "--json"); code != 0 || len(result["runs"].([]any)) != 2 {
			t.Fatalf("invalid cancellation mutated runs: %d %+v", code, result)
		}
		code, result = invoke(t, "cancel", "a", id, "b", "a", "--json")
		if code != 0 || len(result["runs"].([]any)) != 2 || len(result["errors"].([]any)) != 0 {
			t.Fatalf("overlapping cancellation: %d %+v", code, result)
		}
		for _, item := range result["runs"].([]any) {
			if item.(map[string]any)["status"] != "cancelled" {
				t.Fatalf("not cancelled: %+v", item)
			}
		}
	}
	// Runtime eligibility failures report completed requests in the same JSON object.
	code, result := invoke(t, "run", "a", "disabled", "b", "--detach", "--json")
	if code != 1 || len(result["results"].([]any)) != 2 || len(result["errors"].([]any)) != 1 || result["errors"].([]any)[0].(map[string]any)["target"] != "disabled" {
		t.Fatalf("partial queue results lost: %d %+v", code, result)
	}
}

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
	if code := cli.Run(context.Background(), []string{"noon", "cancel"}, config.BuildOpts{}, &cancelListOut, &cancelListErr); code != 0 || !strings.Contains(cancelListOut.String(), "Suggested action: noon cancel "+fmt.Sprint(detachedID)) {
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

// Removing and re-adding identical config while offline must not revive its old authority.
func TestTemporaryScheduleOfflineRemoval(t *testing.T) {
	root := sandbox(t)
	if code, result := invoke(t, "add", "probe", "--json", "--", "true"); code != 0 {
		t.Fatal(result)
	}
	if code, result := invoke(t, "enable", "--json"); code != 0 {
		t.Fatal(result)
	}
	if code, result := invoke(t, "run", "probe", "--schedule", "every 1m", "--for", "1h", "--json"); code != 0 {
		t.Fatal(result)
	}
	if code, result := invoke(t, "remove", "probe", "--json"); code != 0 {
		t.Fatal(result)
	}
	state, err := store.OpenExisting(filepath.Join(root, "state", "noon"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	project, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	values, err := state.TemporarySchedules(project)
	if err != nil || len(values) != 0 {
		t.Fatalf("offline removal retained old request: %+v %v", values, err)
	}
	if code, result := invoke(t, "add", "probe", "--json", "--", "true"); code != 0 {
		t.Fatal(result)
	}
	if code, result := invoke(t, "list", "--json"); code != 0 || result["jobs"].([]any)[0].(map[string]any)["temporary_schedule"] != nil {
		t.Fatalf("readd revived schedule: %+v", result)
	}
}

// A slow owner must not delay durable cancellation of any later job/ID selector.
func TestCancelRecordsAllTargetsBeforeWaiting(t *testing.T) {
	root := sandbox(t)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte("jobs:\n  slow: {run: ['true']}\n  fast: {run: ['true']}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if code, result := invoke(t, "enable", "--json"); code != 0 {
		t.Fatal(result)
	}
	for _, job := range []string{"slow", "fast"} {
		if code, result := invoke(t, "run", job, "--schedule", "every 1m", "--for", "1h", "--json"); code != 0 {
			t.Fatal(result)
		}
	}
	_, queued := invoke(t, "run", "fast", "--in", "1h", "--json")
	id := int64(queued["run"].(map[string]any)["id"].(float64))
	state, err := store.Open(filepath.Join(root, "state", "noon"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	project, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := state.RegisterProject(project, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := runner.ProcessStartIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	slow, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: "slow", Trigger: "manual", Digest: "d", Now: time.Now(), OwnerPID: os.Getpid(), OwnerStart: identity})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- cli.Run(context.Background(), []string{"noon", "cancel", "slow", "fast", fmt.Sprint(id), "--json"}, config.BuildOpts{}, &stdout, &stderr)
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	requested := false
poll:
	for {
		select {
		case <-deadline.C:
			break poll
		case <-ticker.C:
			run, err := state.RunByID(id)
			if err != nil {
				t.Fatal(err)
			}
			values, err := state.TemporarySchedules(project)
			if err != nil {
				t.Fatal(err)
			}
			pending, err := state.CancellationRequested(slow.ID)
			if err != nil {
				t.Fatal(err)
			}
			if pending && run.Status == "cancelled" && len(values) == 0 {
				requested = true
				break poll
			}
		}
	}
	// Release the owner only after observing the other targets, also on failure.
	if err := state.FinishRun(slow.ID, projectID, "slow", store.Outcome{Status: "cancelled", FinishedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("cancel exit %d stdout=%s stderr=%s", code, &stdout, &stderr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not finish after owner released")
	}
	if !requested {
		t.Fatal("later targets were not cancelled while first owner was still running")
	}
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
	if code := cli.Run(context.Background(), []string{"noon", "cancel"}, config.BuildOpts{}, &listOut, &listErr); code != 0 || !strings.Contains(listOut.String(), "Suggested action: noon cancel") {
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
	// A queued target after a slow-stopping run must be cancelled before waiting.
	if code, result := invoke(t, "enable", "--json"); code != 0 {
		t.Fatalf("enable: %d %+v", code, result)
	}
	code, queued := invoke(t, "run", "tests", "--in", "1h", "--json")
	if code != 0 {
		t.Fatalf("queue: %d %+v", code, queued)
	}
	queuedID := int64(queued["run"].(map[string]any)["id"].(float64))
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
		code := cli.Run(context.Background(), []string{"noon", "cancel", fmt.Sprint(run.ID), fmt.Sprint(queuedID)}, config.BuildOpts{}, &out, &stderr)
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
			later, laterErr := state.RunByID(queuedID)
			_ = state.Close()
			if readErr == nil && current.CancelRequestedAt.Valid && current.Status == "running" && laterErr == nil && later.Status == "cancelled" {
				requested = true
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !requested {
		t.Fatal("cancel command did not persist every request before waiting for the active run")
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
