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
	job := config.Job{Name: "fail", CWD: project, OnFailure: "env > " + strconv.Quote(envFile)}
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
