package app

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

// A notification must link to its own run, and a stale or inherited link must
// never reach a hook or job when no public URL is configured.
func TestHookRunURL(t *testing.T) {
	state, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	project := t.TempDir()
	projectID, err := state.RegisterProject(project, now)
	if err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(t.TempDir(), "env")
	job := config.Job{Name: "fail", CWD: project, Hooks: config.Hooks{OnFailure: "env > " + strconv.Quote(envFile)}}
	inherited := []string{"PATH=" + os.Getenv("PATH"), "NOON_RUN_URL=https://stale.example.com/runs/1"}
	if slices.ContainsFunc(JobEnvironment(project, job, inherited), func(entry string) bool { return strings.HasPrefix(entry, "NOON_RUN_URL=") }) {
		t.Fatal("job environment must not carry NOON_RUN_URL")
	}
	for _, tc := range []struct{ trigger, publicURL string }{
		{"schedule", "https://noon.example.com"},
		{"manual", "https://noon.example.com/base"},
		{"manual", ""},
	} {
		run, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: job.Name, Trigger: tc.trigger, Digest: "test", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		exit := 1
		if _, err := FinishRunWithHook(state, run, project, job, JobEnvironment(project, job, inherited), tc.publicURL, store.Outcome{Status: "failed", FinishedAt: now, ExitCode: &exit}); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(envFile)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for line := range strings.Lines(string(data)) {
			if strings.HasPrefix(line, "NOON_RUN_URL=") {
				got = append(got, strings.TrimSpace(line))
			}
		}
		var want []string
		if tc.publicURL != "" {
			want = []string{"NOON_RUN_URL=" + tc.publicURL + "/runs/" + strconv.FormatInt(run.ID, 10)}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s run with %q: hook NOON_RUN_URL = %q, want %q", tc.trigger, tc.publicURL, got, want)
		}
	}
}

// Heartbeat monitors need a ping for every correct run, and a noop must not
// inflate or reset the failure streak that pause-after thresholds read.
func TestNoopHook(t *testing.T) {
	state, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	project := t.TempDir()
	projectID, err := state.RegisterProject(project, now)
	if err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(t.TempDir(), "env")
	record := "env > " + strconv.Quote(envFile)
	job := config.Job{Name: "check", CWD: project, NoopExitCode: 3, Hooks: config.Hooks{OnSuccess: "exit 9", OnFailure: "true", OnNoop: record}}
	finish := func(status string, code int) (store.Run, *HookResult) {
		t.Helper()
		run, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: job.Name, Trigger: "manual", Digest: "test", Now: now})
		if err != nil {
			t.Fatal(err)
		}
		result, err := FinishRunWithHook(state, run, project, job, JobEnvironment(project, job, []string{"PATH=" + os.Getenv("PATH")}), "", store.Outcome{Status: status, FinishedAt: now, ExitCode: &code})
		if err != nil {
			t.Fatal(err)
		}
		return run, result
	}
	finish("failed", 1)
	run, result := finish("noop", 3)
	if result == nil || result.Event != "on_noop" || result.Status != "ok" {
		t.Fatalf("noop hook result = %+v", result)
	}
	data, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NOON_STATUS=noop", "NOON_EXIT_CODE=3", "NOON_CONSECUTIVE_FAILURES=1", "NOON_PREVIOUS_STATUS=failed"} {
		if !strings.Contains(string(data), want+"\n") {
			t.Fatalf("noop hook environment lacks %s:\n%s", want, data)
		}
	}
	stored, err := state.RunByID(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if hook := RunFromStore(state, stored).Hook; hook == nil || hook.Event != "on_noop" || hook.Status != "ok" {
		t.Fatalf("history hook = %+v", hook)
	}
}
