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
		{"serve", "--api-token-file", "secret"},
		{"serve", "--api-token-file="},
		{"serve", "--web-token-file="},
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
	if code != 0 || !strings.Contains(out.String(), "Suggested action: noon service install") {
		t.Fatalf("offline enable guidance: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon"}, config.BuildOpts{}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), "Suggested action: noon service install") {
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
	if _, err := database.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"noon", "serve"}, config.BuildOpts{}, &out, &stderr)
	if code != 75 || !strings.Contains(stderr.String(), "state database version 999") || !strings.Contains(stderr.String(), "Upgrade Noon") {
		t.Fatalf("newer database: exit=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
}

func TestListUsesPersistedIntervalPhase(t *testing.T) {
	root := sandbox(t)
	manifest := "timezone: America/Chicago\njobs:\n  interval:\n    schedule: every 1h\n    run: 'true'\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "noon.yaml"), []byte(manifest), 0o600); err != nil {
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
	snapshot, err := state.ReadSchedulerSnapshot(time.Now())
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

func TestListJSONWebhookPath(t *testing.T) {
	root := sandbox(t)
	manifest := "jobs:\n  hook:\n    run: 'true'\n    webhook:\n      secret_env: HOOK_SECRET\n  plain:\n    run: 'true'\n"
	if err := os.WriteFile(filepath.Join(root, ".git", "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "enable"}, config.BuildOpts{}, &out, &stderr); code != 0 {
		t.Fatalf("enable: code=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"noon", "list", "--json"}, config.BuildOpts{}, &out, &stderr); code != 0 {
		t.Fatalf("list: code=%d stdout=%s stderr=%s", code, &out, &stderr)
	}
	var result struct {
		Jobs []map[string]any `json:"jobs"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("list JSON: %v: %s", err, out.String())
	}
	var path string
	for _, job := range result.Jobs {
		switch job["name"] {
		case "hook":
			path, _ = job["webhook_path"].(string)
		case "plain":
			if _, exists := job["webhook_path"]; exists {
				t.Fatalf("non-webhook job has webhook_path: %+v", job)
			}
		}
	}
	if path == "" || !strings.HasPrefix(path, "/hooks/") || !strings.HasSuffix(path, "/hook") {
		t.Fatalf("webhook path = %q", path)
	}
}

func TestListenerSettingsInConfigAndDoctor(t *testing.T) {
	root := sandbox(t)
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr)
		return code, out.String() + stderr.String()
	}
	if code, output := run("config"); code != 0 || !strings.Contains(output, "Listener:      off\n") {
		t.Fatalf("no listener: exit=%d output=%s", code, output)
	}
	userFile := filepath.Join(root, "user", "noon", "config.yaml")
	tokenFile := filepath.Join(root, "api-token")
	token := strings.Repeat("s", 40)
	writeUser := func(data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(userFile), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(userFile, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeUser("serve:\n  listen: 127.0.0.1:8787\n  api_token_file: ~/api-token\n")
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, output := run("config"); code != 0 || !strings.Contains(output, "Listener:      127.0.0.1:8787 (API)\n") {
		t.Fatalf("listener with API: exit=%d output=%s", code, output)
	}
	doctor := func() (int, map[string]app.Check, string) {
		t.Helper()
		code, output := run("doctor", "--json")
		var view app.DoctorView
		if err := json.Unmarshal([]byte(output), &view); err != nil {
			t.Fatalf("doctor json: %v: %s", err, output)
		}
		checks := map[string]app.Check{}
		for _, check := range view.Checks {
			checks[check.Name] = check
		}
		return code, checks, output
	}
	if _, checks, output := doctor(); checks["api_token_file"].Status != "ok" || checks["listener"].Status != "ok" || strings.Contains(output, token) {
		t.Fatalf("healthy listener: %s", output)
	}
	if err := os.Chmod(tokenFile, 0o644); err != nil {
		t.Fatal(err)
	}
	writeUser("serve:\n  listen: 0.0.0.0:8787\n  api_token_file: ~/api-token\n")
	if _, checks, output := doctor(); checks["api_token_file"].Status != "warn" || checks["listener"].Status != "warn" || strings.Contains(output, token) {
		t.Fatalf("readable token and public address should warn: %s", output)
	}
	for name, data := range map[string]string{"short": strings.Repeat("s", 31), "unreadable": ""} {
		if name == "unreadable" {
			if err := os.Remove(tokenFile); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(tokenFile, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		code, checks, output := doctor()
		check := checks["api_token_file"]
		if code == 0 || check.Status != "fail" || !strings.Contains(check.Message, tokenFile) || strings.Contains(output, token) {
			t.Fatalf("%s token: exit=%d %s", name, code, output)
		}
	}

	// The web token gets the same checks, and must differ from the API token.
	webFile := filepath.Join(root, "web-token")
	webToken := strings.Repeat("w", 40)
	for path, data := range map[string]string{tokenFile: token, webFile: webToken} {
		if err := os.WriteFile(path, []byte(data+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeUser("serve:\n  listen: 127.0.0.1:8787\n  api_token_file: ~/api-token\n  web_token_file: ~/web-token\n")
	if code, output := run("config"); code != 0 || !strings.Contains(output, "Listener:      127.0.0.1:8787 (API, web)\n") {
		t.Fatalf("listener with API and web: exit=%d output=%s", code, output)
	}
	if code, checks, output := doctor(); code != 0 || checks["web_token_file"].Status != "ok" || strings.Contains(output, webToken) {
		t.Fatalf("healthy web token: exit=%d %s", code, output)
	}
	if err := os.WriteFile(webFile, []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, output := run("config", "check"); code == 0 || !strings.Contains(output, "web token must differ") || strings.Contains(output, token) {
		t.Fatalf("reused token config check: exit=%d output=%s", code, output)
	}
	if code, checks, output := doctor(); code == 0 || checks["web_token_file"].Status != "fail" || strings.Contains(output, token) {
		t.Fatalf("reused token doctor: exit=%d %s", code, output)
	}
	if code, output := run("serve"); code != 2 || strings.Contains(output, token) {
		t.Fatalf("reused token serve: exit=%d output=%s", code, output)
	}

	// Unusable web tokens stop serve before it writes a heartbeat.
	stateDir := filepath.Join(root, "state", "noon")
	for name, data := range map[string]string{"short": strings.Repeat("w", 31), "unreadable": ""} {
		if name == "unreadable" {
			if err := os.Remove(webFile); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(webFile, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if code, output := run("serve"); code != 1 || !strings.Contains(output, webFile) {
			t.Fatalf("%s web token serve: exit=%d output=%s", name, code, output)
		}
		if state, err := store.OpenExisting(stateDir); err == nil {
			_, exists, err := state.SchedulerInfo()
			_ = state.Close()
			if err != nil || exists {
				t.Fatalf("%s web token wrote a heartbeat: exists=%t err=%v", name, exists, err)
			}
		}
	}

	writeUser("serve:\n  web_token_file: ~/web-token\n")
	if code, output := run("config", "check"); code == 0 || !strings.Contains(output, "serve.web_token_file") {
		t.Fatalf("web token without listen in config: exit=%d output=%s", code, output)
	}
	writeUser("")
	if code, output := run("serve", "--web-token-file", webFile); code != 2 {
		t.Fatalf("--web-token-file without a listener: exit=%d output=%s", code, output)
	}
}
