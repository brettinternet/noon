package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

func TestServeCLIRestrictionsAndStatus(t *testing.T) {
	root := sandbox(t)
	for _, args := range [][]string{
		{"serve", "unexpected"},
		{"--json", "serve"},
		{"serve", "--json=false"},
		{"serve", "--config", "custom.yaml"},
		{"--timezone", "UTC", "serve"},
		{"serve", "--project", root},
	} {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr)
		if code != 2 {
			t.Fatalf("args %v returned %d: stdout=%s stderr=%s", args, code, &out, &stderr)
		}
	}
	for _, key := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE", "NOON_JSON"} {
		t.Setenv(key, "set")
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "serve"}, config.BuildOpts{}, &out, &stderr)
		if code != 2 {
			t.Fatalf("%s returned %d: stdout=%s stderr=%s", key, code, &out, &stderr)
		}
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	manifest := "jobs:\n  tests:\n    schedule: every 1h\n    run: 'true'\n"
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"noon", "enable"}, config.BuildOpts{}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), "Next: noon service install") {
		t.Fatalf("offline enable guidance: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon"}, config.BuildOpts{}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), "Next: noon service install") {
		t.Fatalf("offline dashboard guidance: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "list"}, config.BuildOpts{}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), "Scheduler offline") {
		t.Fatalf("offline list status: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
}

func TestServeRejectsNewerStateWithExit75(t *testing.T) {
	root := sandbox(t)
	stateDir := filepath.Join(root, "state", "noon")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite", filepath.Join(stateDir, "noon.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec("PRAGMA user_version=9"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"noon", "serve"}, config.BuildOpts{}, &out, &stderr)
	if code != 75 || !strings.Contains(stderr.String(), "state database version 9") || !strings.Contains(stderr.String(), "Upgrade Noon") {
		t.Fatalf("newer database: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
}

func TestListUsesPersistedIntervalPhase(t *testing.T) {
	root := sandbox(t)
	manifest := "timezone: America/Chicago\njobs:\n  interval:\n    schedule: every 1h\n    run: 'true'\n"
	if err := os.WriteFile(filepath.Join(root, ".noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "enable"}, config.BuildOpts{}, &out, &stderr); code != 0 {
		t.Fatalf("enable: %d %s %s", code, &out, &stderr)
	}
	stateDir := filepath.Join(root, "state", "noon")
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	snapshot, err := state.ReadSchedulerSnapshot()
	if err != nil || len(snapshot.Projects) != 1 {
		t.Fatalf("project snapshot: %+v %v", snapshot, err)
	}
	now := time.Date(2020, 10, 16, 0, 0, 0, 0, time.UTC)
	next := now.Add(7 * time.Hour)
	identity := "test-scheduler"
	if err := state.SetScheduler(store.SchedulerRecord{PID: 99, StartIdentity: identity, Version: "test", StartedAt: store.FormatTime(now), HeartbeatAt: store.FormatTime(now)}); err != nil {
		t.Fatal(err)
	}
	_, err = state.ScheduleTick(now, 99, identity,
		func(snapshot store.SchedulerSnapshot) (store.SchedulerPlan, error) {
			return store.SchedulerPlan{Upsert: []store.JobStateChange{{ProjectID: snapshot.Projects[0].ID, Job: "interval", ScheduleKey: "every 1h|America/Chicago", NextAt: next}}}, nil
		},
		func(store.SchedulerSnapshot) (store.DispatchPlan, error) { return store.DispatchPlan{}, nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	view, err := app.WithScheduler(app.Inspection{StateDir: stateDir, Project: app.Project{Path: root, Timezone: "America/Chicago", Scheduler: "offline"}}, now)
	if err != nil || view.Project.Scheduler != "running" {
		t.Fatalf("fresh heartbeat: %+v %v", view, err)
	}
	out.Reset()
	stderr.Reset()
	code := cli.Run(context.Background(), []string{"noon", "list", "--json"}, config.BuildOpts{}, &out, &stderr)
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("list JSON: %v stdout=%s stderr=%s", err, &out, &stderr)
	}
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}
	job := result["jobs"].([]any)[0].(map[string]any)
	project := result["project"].(map[string]any)
	if code != 0 || job["next_at"] != next.In(chicago).Format(time.RFC3339Nano) || project["scheduler"] != "offline" {
		t.Fatalf("persisted status/next_at: code=%d result=%+v stderr=%s; want next %s and scheduler offline", code, result, &stderr, next.Format(time.RFC3339Nano))
	}
	if err := state.ClearScheduler(99, identity); err != nil {
		t.Fatal(err)
	}
}
