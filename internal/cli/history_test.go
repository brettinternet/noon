package cli_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/scheduler"
	"github.com/brettinternet/noon/internal/store"
)

func TestHistoryAndLogs(t *testing.T) {
	t.Run("history, target selection, retention and shared JSON", func(t *testing.T) {
		project := sandbox(t)
		stateDir := filepath.Join(t.TempDir(), "state")
		t.Setenv("NOON_STATE_DIR", stateDir)
		manifest := `jobs:
  tests:
    run: "printf 'tests-log\\377'"
  bad:
    run: "printf bad-log; exit 7"
  prune:
    run: "printf old-log"
`
		if err := os.WriteFile(filepath.Join(project, "noon.yaml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		runJSON := func(name string, wantExit int) int64 {
			t.Helper()
			var out, stderr bytes.Buffer
			code := cli.Run(context.Background(), []string{"noon", "run", name, "--json"}, config.BuildOpts{}, &out, &stderr)
			if code != wantExit {
				t.Fatalf("run %s: exit=%d want=%d stdout=%s stderr=%s", name, code, wantExit, out.String(), stderr.String())
			}
			var result map[string]any
			if err := json.Unmarshal(out.Bytes(), &result); err != nil {
				t.Fatalf("run JSON: %v: %s", err, out.String())
			}
			run := result["run"].(map[string]any)
			if _, ok := run["scheduled_at"]; !ok || run["scheduled_at"] != nil {
				t.Fatalf("shared run JSON lacks nullable scheduled_at: %+v", run)
			}
			if _, ok := run["log"].(map[string]any); !ok {
				t.Fatalf("shared run JSON lacks log metadata: %+v", run)
			}
			return int64(run["id"].(float64))
		}
		testsID := runJSON("tests", 0)
		badID := runJSON("bad", 7)

		code, history := invoke(t, "history", "--json")
		if code != 0 {
			t.Fatalf("history failed: %+v", history)
		}
		runs := history["runs"].([]any)
		if len(runs) != 2 {
			t.Fatalf("history count=%d: %+v", len(runs), runs)
		}
		newest, previous := runs[0].(map[string]any), runs[1].(map[string]any)
		if int64(newest["id"].(float64)) != badID || newest["status"] != "failed" || newest["exit_code"] != float64(7) || int64(previous["id"].(float64)) != testsID || previous["status"] != "succeeded" {
			t.Fatalf("history order/status/exit = %+v", runs)
		}
		for _, key := range []string{"id", "project", "job", "status", "trigger", "digest", "scheduled_at", "created_at", "started_at", "finished_at", "duration_ms", "exit_code", "signal", "error", "requested_by", "idempotency_key", "due_at", "cancel_requested_at", "event_file", "depth", "log"} {
			if _, ok := newest[key]; !ok {
				t.Fatalf("run JSON missing %q: %+v", key, newest)
			}
		}
		if newest["scheduled_at"] != nil || newest["signal"] != nil || newest["error"] != nil {
			t.Fatalf("nullable run values were not null: %+v", newest)
		}
		canonicalProject, err := filepath.EvalSymlinks(project)
		if err != nil {
			t.Fatal(err)
		}
		digest, digestIsString := newest["digest"].(string)
		if newest["project"] != canonicalProject || !digestIsString || digest == "" || newest["depth"] != float64(0) || newest["event_file"] != nil || newest["requested_by"] != nil || newest["idempotency_key"] != nil || newest["due_at"] != nil || newest["cancel_requested_at"] != nil {
			t.Fatalf("run project/request context: %+v", newest)
		}
		newestLog := newest["log"].(map[string]any)
		if newestLog["available"] != true || newestLog["bytes"] == float64(0) {
			t.Fatalf("run log metadata: %+v", newestLog)
		}
		newestPath, ok := newestLog["path"].(string)
		if !ok || !filepath.IsAbs(newestPath) {
			t.Fatalf("history log path is not absolute: %+v", newestLog)
		}
		if _, err := os.Stat(newestPath); err != nil {
			t.Fatalf("history log path does not exist: %q: %v", newestPath, err)
		}
		code, listJSON := invoke(t, "list", "--json")
		if code != 0 {
			t.Fatalf("list JSON failed: %+v", listJSON)
		}
		jobs := listJSON["jobs"].([]any)
		for _, raw := range jobs {
			job := raw.(map[string]any)
			if job["name"] == "bad" {
				last := job["last_run"].(map[string]any)
				if int64(last["id"].(float64)) != badID {
					t.Fatalf("list last_run mismatch: %+v", job)
				}
			}
		}
		for _, args := range [][]string{{"list"}, {}} {
			var out, stderr bytes.Buffer
			if code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "LAST") || !strings.Contains(out.String(), "✗ exit 7") {
				t.Fatalf("LAST column missing for %v: code=%d stdout=%s stderr=%s", args, code, out.String(), stderr.String())
			}
		}

		code, limited := invoke(t, "history", "--limit", "1", "--json")
		if code != 0 || len(limited["runs"].([]any)) != 1 || int64(limited["runs"].([]any)[0].(map[string]any)["id"].(float64)) != badID {
			t.Fatalf("history limit: %d %+v", code, limited)
		}
		code, statusBeforeLimit := invoke(t, "history", "--status", "succeeded", "--limit", "1", "--json")
		if code != 0 || len(statusBeforeLimit["runs"].([]any)) != 1 || int64(statusBeforeLimit["runs"].([]any)[0].(map[string]any)["id"].(float64)) != testsID {
			t.Fatalf("status filter was applied after limit: exit=%d history=%+v", code, statusBeforeLimit)
		}
		code, commaStatuses := invoke(t, "history", "--status", "failed,succeeded", "--json")
		if code != 0 || len(commaStatuses["runs"].([]any)) != 2 {
			t.Fatalf("comma-separated status filter: exit=%d history=%+v", code, commaStatuses)
		}
		code, repeatedStatuses := invoke(t, "history", "--status", "failed", "--status", "succeeded", "--json")
		if code != 0 || len(repeatedStatuses["runs"].([]any)) != 2 {
			t.Fatalf("repeated status filter: exit=%d history=%+v", code, repeatedStatuses)
		}
		code, invalidStatus := invoke(t, "history", "--status", "bogus", "--json")
		if code != 2 || invalidStatus["error"].(map[string]any)["code"] != "usage" || !strings.Contains(invalidStatus["error"].(map[string]any)["hint"].(string), "queued, running, succeeded, failed") || !strings.Contains(invalidStatus["error"].(map[string]any)["hint"].(string), "expired") {
			t.Fatalf("invalid status did not list valid values: exit=%d error=%+v", code, invalidStatus)
		}
		code, invalidLimit := invoke(t, "history", "--limit", "51", "--json")
		if code != 2 || invalidLimit["error"].(map[string]any)["code"] != "usage" {
			t.Fatalf("invalid limit: %d %+v", code, invalidLimit)
		}
		code, emptyJob := invoke(t, "history", "", "--json")
		if code != 1 || emptyJob["error"].(map[string]any)["code"] != "not_found" {
			t.Fatalf("empty job matched all history: %d %+v", code, emptyJob)
		}
		code, unknown := invoke(t, "history", "tset", "--json")
		if code != 1 || unknown["error"].(map[string]any)["code"] != "not_found" || !strings.Contains(unknown["error"].(map[string]any)["hint"].(string), "tests") {
			t.Fatalf("unknown job: %d %+v", code, unknown)
		}

		code, bareLog := invoke(t, "logs", "--json")
		if code != 0 || int64(bareLog["run"].(map[string]any)["id"].(float64)) != badID || bareLog["log"] != "bad-log" {
			t.Fatalf("bare logs selection: %d %+v", code, bareLog)
		}
		logRun := bareLog["run"].(map[string]any)
		logPath, ok := logRun["log"].(map[string]any)["path"].(string)
		if !ok || logPath != newestPath {
			t.Fatalf("logs and history paths differ: logs=%+v history=%q", logRun["log"], newestPath)
		}
		var pathOut, pathErr bytes.Buffer
		code = cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(badID), "--path"}, config.BuildOpts{}, &pathOut, &pathErr)
		if code != 0 || pathErr.Len() != 0 || pathOut.String() != newestPath+"\n" {
			t.Fatalf("logs --path: exit=%d stdout=%q stderr=%q want=%q", code, pathOut.String(), pathErr.String(), newestPath+"\n")
		}
		for _, args := range [][]string{{"logs", fmt.Sprint(badID), "--path", "--json"}, {"logs", fmt.Sprint(badID), "--path", "--follow"}} {
			var conflictOut, conflictErr bytes.Buffer
			code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &conflictOut, &conflictErr)
			if code != 2 || !strings.Contains(conflictOut.String()+conflictErr.String(), "log.path") {
				t.Fatalf("logs --path conflict %v: exit=%d stdout=%q stderr=%q", args, code, conflictOut.String(), conflictErr.String())
			}
			if args[len(args)-1] == "--json" {
				var errorOutput map[string]any
				if err := json.Unmarshal(conflictOut.Bytes(), &errorOutput); err != nil {
					t.Fatalf("logs --path --json did not return JSON error: %v stdout=%q", err, conflictOut.String())
				}
				failure := errorOutput["error"].(map[string]any)
				if failure["code"] != "usage" || !strings.Contains(failure["hint"].(string), "log.path") || conflictErr.Len() != 0 {
					t.Fatalf("logs --path --json error = %+v stderr=%q", failure, conflictErr.String())
				}
			}
		}
		code, namedLog := invoke(t, "logs", "tests", "--json")
		if code != 0 || int64(namedLog["run"].(map[string]any)["id"].(float64)) != testsID || namedLog["log"] != "tests-log�" {
			t.Fatalf("job logs selection or UTF-8 replacement: %d %+v", code, namedLog)
		}
		code, repairedPage := invoke(t, "logs", fmt.Sprint(testsID), "--json", "--after-cursor", "0", "--limit-bytes", "10")
		if code != 0 || repairedPage["log"] != "tests-log" || repairedPage["next"] != float64(9) || len(repairedPage["log"].(string)) > 10 {
			t.Fatalf("repaired forward page exceeded its byte cap or used repaired cursor: %d %+v", code, repairedPage)
		}
		code, repairedNext := invoke(t, "logs", fmt.Sprint(testsID), "--json", "--after-cursor", "9", "--limit-bytes", "3")
		if code != 0 || repairedNext["log"] != "�" || repairedNext["next"] != float64(10) || len(repairedNext["log"].(string)) > 3 {
			t.Fatalf("repaired next page did not resume at raw byte offset 9: %d %+v", code, repairedNext)
		}
		code, runLog := invoke(t, "logs", fmt.Sprint(testsID), "--json")
		if code != 0 || int64(runLog["run"].(map[string]any)["id"].(float64)) != testsID {
			t.Fatalf("run ID logs selection: %d %+v", code, runLog)
		}
		var rawOut, rawErr bytes.Buffer
		if code := cli.Run(context.Background(), []string{"noon", "logs", "tests"}, config.BuildOpts{}, &rawOut, &rawErr); code != 0 || !bytes.Equal(rawOut.Bytes(), []byte{'t', 'e', 's', 't', 's', '-', 'l', 'o', 'g', 0xff}) {
			t.Fatalf("plain logs did not preserve stored bytes: %d %v %q", code, rawOut.Bytes(), rawErr.String())
		}

		state, err := store.Open(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		projectID, err := state.RegisterProject(project, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		var oldRun int64
		for i := range 22 {
			now := time.Now().Add(time.Duration(i) * time.Millisecond)
			run, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: "prune", Trigger: "manual", Digest: "test", Now: now, OwnerPID: os.Getpid(), OwnerStart: "test", EventData: []byte(`{"pruned":true}`), EventSource: "cli"})
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				oldRun = run.ID
			}
			log, err := state.RunLog(projectID, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := log.Write([]byte("old-log")); err != nil {
				t.Fatal(err)
			}
			logBytes, truncated, err := log.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err := state.SetRunLog(run.ID, log.Path()); err != nil {
				t.Fatal(err)
			}
			exit := 0
			if err := state.FinishRun(run.ID, projectID, "prune", store.Outcome{Status: "succeeded", FinishedAt: now, ExitCode: &exit, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated}); err != nil {
				t.Fatal(err)
			}
		}
		if err := state.Close(); err != nil {
			t.Fatal(err)
		}
		missingLog := filepath.Join(stateDir, "logs", fmt.Sprint(projectID), fmt.Sprintf("%d.log", testsID))
		if err := os.Remove(missingLog); err != nil {
			t.Fatal(err)
		}
		var missingOut, missingErr bytes.Buffer
		code = cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(testsID), "--follow"}, config.BuildOpts{}, &missingOut, &missingErr)
		if code != 1 || !strings.Contains(missingErr.String(), "Logs are kept for the newest 10 runs per job") {
			t.Fatalf("missing terminal log follow did not explain retention: exit=%d stdout=%s stderr=%s", code, missingOut.String(), missingErr.String())
		}
		missingOut.Reset()
		missingErr.Reset()
		code = cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(testsID), "--path"}, config.BuildOpts{}, &missingOut, &missingErr)
		if code != 1 || missingOut.Len() != 0 || !strings.Contains(missingErr.String(), "Logs are kept for the newest 10 runs per job") {
			t.Fatalf("missing terminal log path did not explain retention: exit=%d stdout=%s stderr=%s", code, missingOut.String(), missingErr.String())
		}
		code, defaultHistory := invoke(t, "history", "--json")
		if code != 0 || len(defaultHistory["runs"].([]any)) != 20 {
			t.Fatalf("default history limit: %d %+v", code, defaultHistory)
		}
		code, maximumHistory := invoke(t, "history", "--limit", "50", "--json")
		if code != 0 || len(maximumHistory["runs"].([]any)) != 24 {
			t.Fatalf("maximum history limit: %d %+v", code, maximumHistory)
		}
		code, prunedHistory := invoke(t, "history", "prune", "--limit", "50", "--json")
		var prunedRun map[string]any
		for _, raw := range prunedHistory["runs"].([]any) {
			run := raw.(map[string]any)
			if int64(run["id"].(float64)) == oldRun {
				prunedRun = run
				break
			}
		}
		if code != 0 || prunedRun == nil {
			t.Fatalf("pruned run missing from history: exit=%d history=%+v", code, prunedHistory)
		}
		prunedLog := prunedRun["log"].(map[string]any)
		if prunedLog["available"] != false || prunedLog["path"] != nil || prunedRun["event_file"] != nil {
			t.Fatalf("pruned run paths were not null: run=%+v", prunedRun)
		}
		var out, stderr bytes.Buffer
		code = cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(oldRun), "--path"}, config.BuildOpts{}, &out, &stderr)
		if code != 1 || !strings.Contains(stderr.String(), "Logs are kept for the newest 10 runs per job") {
			t.Fatalf("pruned log path error: exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
		}
	})

	t.Run("logs skip newer queued runs without an explicit status", func(t *testing.T) {
		project := sandbox(t)
		manifest := "jobs:\n  tests:\n    run: 'printf started-log'\n"
		if err := os.WriteFile(filepath.Join(project, "noon.yaml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if code := cli.Run(context.Background(), []string{"noon", "run", "tests"}, config.BuildOpts{}, &out, &stderr); code != 0 {
			t.Fatalf("run tests: exit=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
		}
		out.Reset()
		stderr.Reset()
		if code := cli.Run(context.Background(), []string{"noon", "enable"}, config.BuildOpts{}, &out, &stderr); code != 0 {
			t.Fatalf("enable project for queued fixture: exit=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
		}
		out.Reset()
		stderr.Reset()
		if code := cli.Run(context.Background(), []string{"noon", "run", "tests", "--detach"}, config.BuildOpts{}, &out, &stderr); code != 0 {
			t.Fatalf("queue tests: exit=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
		}
		code, queuedHistory := invoke(t, "history", "tests", "--status", "queued", "--json")
		if code != 0 || len(queuedHistory["runs"].([]any)) != 1 {
			t.Fatalf("queued run fixture missing: exit=%d history=%+v", code, queuedHistory)
		}
		queuedID := int64(queuedHistory["runs"].([]any)[0].(map[string]any)["id"].(float64))
		code, latestStarted := invoke(t, "logs", "tests", "--json")
		if code != 0 || int64(latestStarted["run"].(map[string]any)["id"].(float64)) == queuedID || latestStarted["log"] != "started-log" {
			t.Fatalf("logs selected newer queued run instead of latest started: exit=%d result=%+v", code, latestStarted)
		}
		code, explicitlyQueued := invoke(t, "logs", "tests", "--status", "queued", "--json")
		if code != 1 || explicitlyQueued["error"].(map[string]any)["code"] != "not_found" || explicitlyQueued["error"].(map[string]any)["message"] != fmt.Sprintf("run %d is queued and has not started", queuedID) || explicitlyQueued["error"].(map[string]any)["hint"] != fmt.Sprintf("Follow it: noon logs %d --follow", queuedID) {
			t.Fatalf("explicit queued selection did not select the queued run: exit=%d result=%+v", code, explicitlyQueued)
		}
		code, cancelled := invoke(t, "cancel", fmt.Sprint(queuedID), "--json")
		if code != 0 {
			t.Fatalf("cancel queued: %d %+v", code, cancelled)
		}
		code, neverStarted := invoke(t, "logs", fmt.Sprint(queuedID), "--json")
		failure := neverStarted["error"].(map[string]any)
		if code != 1 || failure["code"] != "not_found" || failure["message"] != fmt.Sprintf("run %d was cancelled and never started, so it has no log", queuedID) || failure["hint"] != nil {
			t.Fatalf("never-started log: %d %+v", code, neverStarted)
		}
	})

	t.Run("removed jobs remain readable from retained history", func(t *testing.T) {
		project := sandbox(t)
		manifest := "jobs:\n  gone:\n    run: 'printf retained-log'\n  keep:\n    run: 'true'\n"
		path := filepath.Join(project, "noon.yaml")
		if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		var out, stderr bytes.Buffer
		if code := cli.Run(context.Background(), []string{"noon", "run", "gone"}, config.BuildOpts{}, &out, &stderr); code != 0 {
			t.Fatalf("run gone: exit=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
		}
		if err := os.WriteFile(path, []byte("jobs:\n  keep:\n    run: 'true'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, history := invoke(t, "history", "gone", "--json")
		if code != 0 || len(history["runs"].([]any)) != 1 || history["runs"].([]any)[0].(map[string]any)["job"] != "gone" {
			t.Fatalf("removed job history: exit=%d result=%+v", code, history)
		}
		code, logs := invoke(t, "logs", "gone", "--json")
		if code != 0 || logs["log"] != "retained-log" {
			t.Fatalf("removed job logs: exit=%d result=%+v", code, logs)
		}
		code, outText, errText := invokeText(t, "logs", "gone", "--status", "failed")
		if code != 0 || outText != "No runs of gone match --status failed.\n" || errText != "" {
			t.Fatalf("empty logs status selection message: exit=%d stdout=%q stderr=%q", code, outText, errText)
		}
		code, outText, errText = invokeText(t, "history", "gone", "--status", "failed")
		if code != 0 || outText != "No runs of gone match --status failed.\n" || errText != "" {
			t.Fatalf("empty history status selection message: exit=%d stdout=%q stderr=%q", code, outText, errText)
		}
		for _, command := range []string{"history", "logs"} {
			code, unknown := invoke(t, command, "never-seen", "--json")
			if code != 1 || unknown["error"].(map[string]any)["code"] != "not_found" {
				t.Fatalf("%s accepted a never-seen job: exit=%d result=%+v", command, code, unknown)
			}
		}
	})

	t.Run("tail selection and query usage errors", func(t *testing.T) {
		project := sandbox(t)
		stateDir := filepath.Join(t.TempDir(), "state")
		t.Setenv("NOON_STATE_DIR", stateDir)
		manifest := "jobs:\n  tests:\n    run: \"printf 'one\\ntwo\\nthree\\n'\"\n"
		if err := os.WriteFile(filepath.Join(project, "noon.yaml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		var runOut, runErr bytes.Buffer
		if code := cli.Run(context.Background(), []string{"noon", "run", "tests"}, config.BuildOpts{}, &runOut, &runErr); code != 0 {
			t.Fatalf("run tests: exit=%d stdout=%q stderr=%q", code, runOut.String(), runErr.String())
		}
		var out, stderr bytes.Buffer
		if code := cli.Run(context.Background(), []string{"noon", "logs", "tests", "--tail", "2"}, config.BuildOpts{}, &out, &stderr); code != 0 {
			t.Fatalf("logs --tail 2: exit=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
		}
		if out.String() != "two\nthree\n" || stderr.String() != fmt.Sprintf("next: noon logs --state-dir %s 1 --after-cursor 14\n", stateDir) {
			t.Fatalf("tail output/cursor = stdout %q stderr %q", out.String(), stderr.String())
		}
		for _, args := range [][]string{
			{"logs", "1", "--status", "failed"},
			{"logs", "tests", "--context", "2"},
			{"logs", "tests", "--match", "("},
			{"logs", "tests", "--follow", "--match", "boom"},
			{"logs", "tests", "--path", "--tail", "1"},
			{"logs", "tests", "--section", "nope"},
			{"logs", "tests", "--section="},
			{"logs", "tests", "--section", "job", "--follow"},
		} {
			out.Reset()
			stderr.Reset()
			code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr)
			if code != 2 || out.Len() != 0 || !strings.Contains(stderr.String(), "noon:") {
				t.Errorf("usage case %v: exit=%d stdout=%q stderr=%q", args, code, out.String(), stderr.String())
			}
		}
	})

	t.Run("read-only inspection with no state", func(t *testing.T) {
		root := sandbox(t)
		manifest := "jobs:\n  tests:\n    run: 'printf not-run'\n"
		if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"history"}, {"logs"}, {"list"}} {
			var out, stderr bytes.Buffer
			code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr)
			if code != 0 || stderr.Len() != 0 {
				t.Fatalf("%v: exit=%d stdout=%s stderr=%s", args, code, out.String(), stderr.String())
			}
			if args[0] != "list" && !strings.Contains(out.String(), "No runs") {
				t.Fatalf("%v did not report no runs: %q", args, out.String())
			}
		}
		if _, err := os.Stat(filepath.Join(root, "state", "noon")); !os.IsNotExist(err) {
			t.Fatalf("inspection created state: %v", err)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 1 || entries[0].Name() != "noon.yaml" {
			t.Fatalf("inspection changed project files: %v err=%v", entries, err)
		}
	})
}

func TestLogsFollow(t *testing.T) {
	t.Run("queued transitions", func(t *testing.T) {
		for _, cancelled := range []bool{true, false} {
			t.Run(fmt.Sprintf("cancelled=%t", cancelled), func(t *testing.T) {
				project := sandbox(t)
				stateDir := filepath.Join(project, "state", "noon")
				if err := os.WriteFile(filepath.Join(project, "noon.yaml"), []byte("jobs:\n  queued:\n    run: \"printf 'queued-output\\\\n'\"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				for _, args := range [][]string{{"enable"}, {"run", "queued", "--detach"}} {
					var out, stderr bytes.Buffer
					if code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr); code != 0 {
						t.Fatalf("%v: exit=%d %s", args, code, &stderr)
					}
				}
				ctx, stop := context.WithCancel(context.Background())
				defer stop()
				notices, writer := io.Pipe()
				defer notices.Close()
				var output bytes.Buffer
				done := make(chan int, 1)
				go func() {
					done <- cli.Run(ctx, []string{"noon", "logs", "1", "--follow"}, config.BuildOpts{}, &output, writer)
					_ = writer.Close()
				}()
				reader := bufio.NewReader(notices)
				line, err := reader.ReadString('\n')
				if err != nil || line != "noon: run 1 is queued; waiting for it to start\n" {
					t.Fatalf("queued notice=%q err=%v", line, err)
				}
				if cancelled {
					var out, stderr bytes.Buffer
					if code := cli.Run(ctx, []string{"noon", "cancel", "1"}, config.BuildOpts{}, &out, &stderr); code != 0 {
						t.Fatalf("cancel: exit=%d %s", code, &stderr)
					}
				} else {
					schedulerCtx, stopScheduler := context.WithCancel(ctx)
					ticks := time.NewTicker(10 * time.Millisecond)
					defer ticks.Stop()
					schedulerDone := make(chan error, 1)
					go func() {
						schedulerDone <- scheduler.Serve(schedulerCtx, scheduler.ServeOptions{
							StateDir: stateDir, StateDirDefault: stateDir, UserConfigDir: filepath.Join(project, "config"),
							Version: "test", Environment: os.Environ(), Logger: io.Discard, IgnoreSignals: true, Ticks: ticks.C,
						})
					}()
					defer func() {
						stopScheduler()
						if err := <-schedulerDone; err != nil {
							t.Errorf("scheduler: %v", err)
						}
					}()
				}
				stderrDone := make(chan string, 1)
				go func() {
					data, _ := io.ReadAll(reader)
					stderrDone <- string(data)
				}()
				select {
				case code := <-done:
					stderr := <-stderrDone
					if cancelled {
						if code != 1 || stderr != "noon: run 1 was cancelled and never started, so it has no log\n" || output.Len() != 0 {
							t.Fatalf("cancelled follow: exit=%d out=%q err=%q", code, &output, stderr)
						}
					} else if code != 0 || output.String() != "queued-output\n" || stderr != "" {
						t.Fatalf("started follow: exit=%d out=%q err=%q", code, &output, stderr)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("queued follow did not finish within 2s")
				}
			})
		}
	})
	t.Run("owner exits during follow", func(t *testing.T) {
		project := sandbox(t)
		stateDir := filepath.Join(project, "state", "noon")
		state, err := store.Open(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		defer state.Close()
		projectID, err := state.RegisterProject(project, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		owner := exec.Command("sh", "-c", "printf 'before-owner-exit\\n'; exec sleep 30")
		output, err := owner.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := owner.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = owner.Process.Kill(); _ = owner.Wait() }()
		identity, err := runner.ProcessStartIdentity(owner.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		run, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: "orphan", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: owner.Process.Pid, OwnerStart: identity})
		if err != nil {
			t.Fatal(err)
		}
		log, err := state.RunLog(projectID, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(output).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
		if _, _, err := log.Close(); err != nil {
			t.Fatal(err)
		}
		if err := state.SetRunLog(run.ID, log.Path()); err != nil {
			t.Fatal(err)
		}
		ctx, stop := context.WithCancel(context.Background())
		defer stop()
		followOut, writer := io.Pipe()
		defer followOut.Close()
		var stderr bytes.Buffer
		done := make(chan int, 1)
		go func() {
			done <- cli.Run(ctx, []string{"noon", "logs", fmt.Sprint(run.ID), "--follow"}, config.BuildOpts{}, writer, &stderr)
			_ = writer.Close()
		}()
		got, err := bufio.NewReader(followOut).ReadString('\n')
		if err != nil || got != line {
			t.Fatalf("live output=%q err=%v", got, err)
		}
		if err := owner.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = owner.Wait()
		select {
		case code := <-done:
			if code != 1 || !strings.Contains(stderr.String(), "owner process exited without recording a result") || !strings.Contains(stderr.String(), fmt.Sprintf("Record it: noon cancel %d", run.ID)) {
				t.Fatalf("orphan follow: exit=%d stderr=%q", code, &stderr)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("orphan follow did not stop within 3s")
		}
		err = app.FollowLog(ctx, stateDir, run.ID, nil, nil, io.Discard, io.Discard)
		if failure, ok := err.(*app.Error); !ok || failure.Code != "orphaned" {
			t.Fatalf("orphan error=%v", err)
		}
		unchanged, err := state.RunByID(run.ID)
		if err != nil || unchanged.Status != "running" || unchanged.FinishedAt.Valid {
			t.Fatalf("follow mutated orphan: %+v err=%v", unchanged, err)
		}
	})
	project := sandbox(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	t.Setenv("NOON_STATE_DIR", stateDir)
	manifest := `jobs:
  tests:
    run: 'printf "earlier\\n"; sleep 0.2; printf "first\\n"; sleep 0.5; printf "later\\n"'
  tail:
    run: "printf 'earlier\\n'; sleep 0.2; printf 'latest\\n'; sleep 0.5; printf 'later\\n'"
  large:
    run: "head -c 1048576 /dev/zero | tr '\\000' x; sleep 0.3; head -c 100000 /dev/zero | tr '\\000' y; printf 'TAIL-MARKER\\n'"
  partial:
    run: "printf 'complete\\npartial'; sleep 1; printf '\\nfinished\\n'"
`
	if err := os.WriteFile(filepath.Join(project, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := state.RegisterProject(project, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()

	runAndFollow := func(name string, followArgs []string, readyLine string) (string, time.Time) {
		t.Helper()
		type result struct {
			code int
			out  string
			err  string
			at   time.Time
		}
		runDone := make(chan result, 1)
		go func() {
			var out, stderr bytes.Buffer
			code := cli.Run(context.Background(), []string{"noon", "run", name}, config.BuildOpts{}, &out, &stderr)
			runDone <- result{code: code, out: out.String(), err: stderr.String(), at: time.Now()}
		}()
		runID, deadline := int64(0), time.Now().Add(3*time.Second)
		for time.Now().Before(deadline) {
			runs, err := state.RunsForJob(projectID, name)
			if err != nil {
				t.Fatal(err)
			}
			if len(runs) > 0 && runs[0].Status == "running" {
				runID = runs[0].ID
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if runID == 0 {
			t.Fatalf("%s run did not become active", name)
		}
		if readyLine != "" {
			ready := false
			readyDeadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(readyDeadline) {
				runs, err := state.RunsForJob(projectID, name)
				if err != nil {
					t.Fatal(err)
				}
				if len(runs) > 0 && runs[0].LogPath.Valid {
					data, readErr := state.ReadRunLog(runs[0])
					if readErr == nil && bytes.Contains(data, []byte(readyLine)) {
						ready = true
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !ready {
				t.Fatalf("%s log did not contain %q before follow", name, readyLine)
			}
		}
		followStarted := time.Now()
		followDone := make(chan result, 1)
		go func() {
			var out, stderr bytes.Buffer
			args := append([]string{"noon", "logs", fmt.Sprint(runID)}, followArgs...)
			code := cli.Run(context.Background(), args, config.BuildOpts{}, &out, &stderr)
			followDone <- result{code: code, out: out.String(), err: stderr.String(), at: time.Now()}
		}()
		var runResult result
		select {
		case runResult = <-runDone:
			if runResult.code != 0 {
				t.Fatalf("%s run failed: %d %s", name, runResult.code, runResult.err)
			}
		case <-time.After(4 * time.Second):
			t.Fatalf("%s run did not finish", name)
		}
		select {
		case followed := <-followDone:
			if followed.code != 0 {
				t.Fatalf("%s follow failed: %d stdout=%q stderr=%q", name, followed.code, followed.out, followed.err)
			}
			if followed.at.Sub(runResult.at) > 2*time.Second || followed.at.Before(followStarted) {
				t.Fatalf("%s follow returned too late: run=%s follow=%s", name, runResult.at, followed.at)
			}
			return followed.out, runResult.at
		case <-time.After(3 * time.Second):
			t.Fatalf("%s follow did not return after the run finished", name)
			return "", time.Time{}
		}
	}

	type partialRunResult struct {
		code int
		err  string
	}
	partialDone := make(chan partialRunResult, 1)
	go func() {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "run", "partial"}, config.BuildOpts{}, &out, &stderr)
		partialDone <- partialRunResult{code: code, err: stderr.String()}
	}()
	partialRunID := int64(0)
	partialDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(partialDeadline) {
		runs, err := state.RunsForJob(projectID, "partial")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) > 0 && runs[0].Status == "running" && runs[0].LogPath.Valid {
			data, readErr := state.ReadRunLog(runs[0])
			if readErr == nil && bytes.Equal(data, []byte("complete\npartial")) {
				partialRunID = runs[0].ID
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if partialRunID == 0 {
		t.Fatal("partial run did not expose its unterminated log line while active")
	}
	var plainOut, plainErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(partialRunID)}, config.BuildOpts{}, &plainOut, &plainErr); code != 0 || plainErr.Len() != 0 || plainOut.String() != "complete\npartial" {
		t.Fatalf("unflagged active log = exit %d stdout=%q stderr=%q; want all raw bytes", code, plainOut.String(), plainErr.String())
	}
	var jsonOut, jsonErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(partialRunID), "--json"}, config.BuildOpts{}, &jsonOut, &jsonErr); code != 0 || jsonErr.Len() != 0 {
		t.Fatalf("unflagged active JSON log = exit %d stdout=%q stderr=%q", code, jsonOut.String(), jsonErr.String())
	}
	var jsonLog struct {
		Log string `json:"log"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &jsonLog); err != nil || jsonLog.Log != "complete\npartial" {
		t.Fatalf("unflagged active JSON log=%q err=%v; want complete and partial lines", jsonLog.Log, err)
	}
	var selectedOut, selectedErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(partialRunID), "--after-cursor", "0"}, config.BuildOpts{}, &selectedOut, &selectedErr); code != 0 || selectedOut.String() != "complete\n" {
		t.Fatalf("flagged active log = exit %d stdout=%q stderr=%q; want only complete lines", code, selectedOut.String(), selectedErr.String())
	}
	select {
	case result := <-partialDone:
		if result.code != 0 {
			t.Fatalf("partial run failed: exit=%d stderr=%q", result.code, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("partial run did not finish")
	}

	followed, _ := runAndFollow("tests", []string{"--follow"}, "")
	if !strings.Contains(followed, "earlier\nfirst\nlater\n") {
		t.Fatalf("follow omitted output written after it began: %q", followed)
	}
	followed, _ = runAndFollow("tail", []string{"--tail", "1", "--follow"}, "latest\n")
	if strings.Contains(followed, "earlier\n") || !strings.Contains(followed, "latest\nlater\n") {
		t.Fatalf("tail follow output = %q; want latest and later but not earlier", followed)
	}
	followed, _ = runAndFollow("large", []string{"--follow"}, "")
	notice := "[noon: output past 1 MiB is shown when the run ends]"
	noticeAt := strings.Index(followed, notice)
	if noticeAt < 0 || noticeAt > (1<<20)+1 || !strings.Contains(followed, "TAIL-MARKER") || strings.Index(followed, "TAIL-MARKER") < noticeAt {
		t.Fatalf("follow exposed overflow tail before notice or omitted terminal tail: notice=%d len=%d tail=%q", noticeAt, len(followed), followed[len(followed)-min(len(followed), 100):])
	}
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"noon", "logs", "--follow", "--json"}, config.BuildOpts{}, &out, &stderr)
	if code != 2 || !strings.Contains(out.String(), `"code":"usage"`) || !strings.Contains(out.String(), "cannot be used with --json") || stderr.Len() != 0 {
		t.Fatalf("follow JSON exit=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
}
