package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
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
		code, invalidLimit := invoke(t, "history", "--limit", "51", "--json")
		if code != 2 || invalidLimit["error"].(map[string]any)["code"] != "usage" {
			t.Fatalf("invalid limit: %d %+v", code, invalidLimit)
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
	project := sandbox(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	t.Setenv("NOON_STATE_DIR", stateDir)
	manifest := `jobs:
  tests:
    run: 'printf "first\\n"; sleep 0.5; printf "later\\n"'
  large:
    run: "head -c 1048576 /dev/zero | tr '\\000' x; sleep 0.3; head -c 100000 /dev/zero | tr '\\000' y; printf 'TAIL-MARKER\\n'"
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

	runAndFollow := func(name string) (string, time.Time) {
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
		followStarted := time.Now()
		followDone := make(chan result, 1)
		go func() {
			var out, stderr bytes.Buffer
			code := cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(runID), "--follow"}, config.BuildOpts{}, &out, &stderr)
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

	followed, _ := runAndFollow("tests")
	if !strings.Contains(followed, "first\nlater\n") {
		t.Fatalf("follow omitted output written after it began: %q", followed)
	}
	followed, _ = runAndFollow("large")
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
