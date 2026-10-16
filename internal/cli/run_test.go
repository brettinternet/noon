package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

func TestPromptJobs(t *testing.T) {
	root := sandbox(t)
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'started\\n' >> \"$AGENT_STARTS_FILE\"\nprintf '%s\\0' \"$@\" > \"$AGENT_ARGV_FILE\"\nprintf '%s' \"$NOON_AGENT\" > \"$AGENT_NAME_FILE\"\ncat > \"$AGENT_STDIN_FILE\"\n"
	for _, name := range []string{"record-agent", "codex"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for key, name := range map[string]string{
		"AGENT_STARTS_FILE": "starts", "AGENT_ARGV_FILE": "argv", "AGENT_NAME_FILE": "agent",
		"AGENT_STDIN_FILE": "stdin",
	} {
		t.Setenv(key, filepath.Join(root, name))
	}
	userConfig := `default_agent: argument
agents:
  claude:
    command: [record-agent, claude-override]
  argument:
    command: [record-agent, argument-mode]
    prompt: argument
`
	if err := os.MkdirAll(filepath.Join(root, "user", "noon"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "user", "noon", "config.yaml"), []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "prompts"), 0o700); err != nil {
		t.Fatal(err)
	}
	filePrompts := map[string]string{
		"stdin.md":     "file stdin first\nfile literal $HOME\n",
		"argument.md":  "file argument first\nfile literal $HOME\n",
		"oversized.md": strings.Repeat("x", config.MaxPromptBytes+1),
	}
	for name, prompt := range filePrompts {
		if err := os.WriteFile(filepath.Join(root, "prompts", name), []byte(prompt), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(root, "prompts", "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `jobs:
  inline_stdin:
    prompt: "inline stdin first\ninline second line\nliteral $HOME\n"
    agent: claude
  file_stdin:
    prompt_file: prompts/stdin.md
    agent: codex
  inline_argument:
    prompt: "inline argument first\ninline second line\nliteral $HOME\n"
    agent: argument
  file_argument:
    prompt_file: prompts/argument.md
    agent: argument
  default_profile:
    prompt: "default profile\n$HOME\n"
  missing_file:
    prompt_file: prompts/missing.md
    agent: argument
  oversized_file:
    prompt_file: prompts/oversized.md
    agent: argument
  fifo_file:
    prompt_file: prompts/fifo
    agent: argument
`
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, agent string
		argv        []string
		stdin       string
	}{
		{"inline_stdin", "claude", []string{"claude-override"}, "inline stdin first\ninline second line\nliteral $HOME\n"},
		{"file_stdin", "codex", []string{"exec", "-"}, filePrompts["stdin.md"]},
		{"inline_argument", "argument", []string{"argument-mode", "inline argument first\ninline second line\nliteral $HOME\n"}, ""},
		{"file_argument", "argument", []string{"argument-mode", filePrompts["argument.md"]}, ""},
		{"default_profile", "argument", []string{"argument-mode", "default profile\n$HOME\n"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, "starts"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			code := cli.Run(context.Background(), []string{"noon", "run", tc.name}, config.BuildOpts{}, &out, &stderr)
			if code != 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &stderr)
			}
			argvData, err := os.ReadFile(filepath.Join(root, "argv"))
			if err != nil {
				t.Fatal(err)
			}
			argv := bytes.Split(argvData, []byte{0})
			argv = argv[:len(argv)-1]
			gotArgs := make([]string, len(argv))
			for i := range argv {
				gotArgs[i] = string(argv[i])
			}
			stdin, err := os.ReadFile(filepath.Join(root, "stdin"))
			if err != nil {
				t.Fatal(err)
			}
			agent, err := os.ReadFile(filepath.Join(root, "agent"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotArgs, tc.argv) || string(stdin) != tc.stdin || string(agent) != tc.agent || !strings.Contains(string(mustRead(t, filepath.Join(root, "starts"))), "started") {
				t.Fatalf("argv=%q stdin=%q agent=%q starts=%q", gotArgs, stdin, agent, mustRead(t, filepath.Join(root, "starts")))
			}
		})
	}
	for _, name := range []string{"missing_file", "oversized_file", "fifo_file"} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, "starts"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			var out, stderr bytes.Buffer
			code := cli.Run(context.Background(), []string{"noon", "run", name}, config.BuildOpts{}, &out, &stderr)
			starts := mustRead(t, filepath.Join(root, "starts"))
			if code != 125 || starts != "" || !strings.Contains(stderr.String(), "failed: exit 125") {
				t.Fatalf("code=%d stderr=%q starts=%q", code, stderr.String(), starts)
			}
		})
	}
	code, result := invoke(t, "--json", "config")
	if code != 0 {
		t.Fatalf("config output: %d %+v", code, result)
	}
	cfg := result["config"].(map[string]any)
	agents := cfg["agents"].(map[string]any)
	if cfg["default_agent"] != "argument" || !reflect.DeepEqual(agents["claude"], []any{"record-agent", "claude-override"}) || !reflect.DeepEqual(agents["codex"], []any{"codex", "exec", "-"}) {
		t.Fatalf("config output: %+v", cfg)
	}
	code, result = invoke(t, "list", "--json")
	if code != 0 {
		t.Fatalf("list output: %d %+v", code, result)
	}
	jobs := result["jobs"].([]any)
	for _, raw := range jobs {
		job := raw.(map[string]any)
		if job["name"] == "inline_stdin" && job["agent"] != "claude" {
			t.Fatalf("effective agent missing from list JSON: %+v", job)
		}
	}
	// Approve before checking command-column rendering; pending approval uses more table space.
	if code, result := invoke(t, "enable", "--json"); code != 0 {
		t.Fatalf("enable: %+v", result)
	}
	var listOut, listErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "list"}, config.BuildOpts{}, &listOut, &listErr); code != 0 || !strings.Contains(listOut.String(), "claude: inline stdin first…") || !strings.Contains(listOut.String(), "codex: prompts/stdin.md") {
		t.Fatalf("human prompt list: code=%d stdout=%s stderr=%s", code, &listOut, &listErr)
	}
}

func TestRunRecordsHistory(t *testing.T) {
	project := sandbox(t)
	stateDir := filepath.Join(t.TempDir(), "state")
	t.Setenv("NOON_STATE_DIR", stateDir)
	manifest := `jobs:
  tests:
    run: 'printf "run=%s last=%s\\n" "$NOON_RUN_ID" "$NOON_LAST_SUCCESS_AT"; sleep 0.3'
`
	if err := os.WriteFile(filepath.Join(project, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	var firstOut, firstErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "run", "tests"}, config.BuildOpts{}, &firstOut, &firstErr); code != 0 {
		t.Fatalf("first run exit=%d stdout=%s stderr=%s", code, firstOut.String(), firstErr.String())
	}
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	projectID, err := state.RegisterProject(project, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	firstRuns, err := state.RunsForJob(projectID, "tests")
	if err != nil || len(firstRuns) != 1 || firstRuns[0].Status != "succeeded" || !firstRuns[0].ExitCode.Valid || firstRuns[0].ExitCode.Int64 != 0 {
		t.Fatalf("first persisted run = %+v, err=%v", firstRuns, err)
	}
	if !firstRuns[0].LogPath.Valid {
		t.Fatalf("first run has no log: %+v", firstRuns[0])
	}
	firstLog, err := os.ReadFile(filepath.Join(stateDir, firstRuns[0].LogPath.String))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(firstLog), fmt.Sprintf("run=%d last=", firstRuns[0].ID)) {
		t.Fatalf("first log is missing run metadata: %q", firstLog)
	}

	secondDone := make(chan struct {
		code     int
		out, err string
	}, 1)
	go func() {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "run", "tests"}, config.BuildOpts{}, &out, &stderr)
		secondDone <- struct {
			code     int
			out, err string
		}{code, out.String(), stderr.String()}
	}()
	deadline := time.Now().Add(3 * time.Second)
	var active bool
	var activeRunID int64
	for time.Now().Before(deadline) {
		runs, err := state.RunsForJob(projectID, "tests")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) == 2 && runs[0].Status == "running" {
			active = true
			activeRunID = runs[0].ID
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !active {
		t.Fatal("second run did not become running before the deadline")
	}
	var duplicateOut, duplicateErr bytes.Buffer
	code := cli.Run(context.Background(), []string{"noon", "--json", "run", "tests"}, config.BuildOpts{}, &duplicateOut, &duplicateErr)
	var duplicateResult map[string]any
	if err := json.Unmarshal(duplicateOut.Bytes(), &duplicateResult); err != nil {
		t.Fatal(err)
	}
	duplicateError := duplicateResult["error"].(map[string]any)
	if code != 1 || duplicateError["code"] != "already_running" || duplicateError["hint"] != fmt.Sprintf("Follow it: noon logs %d --follow", activeRunID) {
		t.Fatalf("concurrent duplicate exit=%d stdout=%s stderr=%s", code, duplicateOut.String(), duplicateErr.String())
	}
	select {
	case result := <-secondDone:
		if result.code != 0 {
			t.Fatalf("second run exit=%d stdout=%s stderr=%s", result.code, result.out, result.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second run did not finish")
	}
	runs, err := state.RunsForJob(projectID, "tests")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("stored %d runs after duplicate refusal, want 2: %+v", len(runs), runs)
	}
	for _, run := range runs {
		if run.Status != "succeeded" || !run.ExitCode.Valid || run.ExitCode.Int64 != 0 || !run.LogPath.Valid {
			t.Fatalf("incomplete run record: %+v", run)
		}
		if _, err := os.Stat(filepath.Join(stateDir, run.LogPath.String)); err != nil {
			t.Fatalf("missing run log: %v", err)
		}
	}
	lastAt, err := time.Parse(store.TimestampLayout, firstRuns[0].StartedAt)
	if err != nil {
		t.Fatal(err)
	}
	secondLog, err := os.ReadFile(filepath.Join(stateDir, runs[0].LogPath.String))
	if err != nil {
		t.Fatal(err)
	}
	wantLast := lastAt.UTC().Format(time.RFC3339Nano)
	if !strings.Contains(string(secondLog), fmt.Sprintf("run=%d last=%s", runs[0].ID, wantLast)) {
		t.Fatalf("last success metadata missing: %q", secondLog)
	}
	entries, err := os.ReadDir(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "noon.yaml" {
		t.Fatalf("run added files to project: %v", entries)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRunCommand(t *testing.T) {
	root := sandbox(t)
	manifest := `jobs:
  tests:
    description: Run the test suite
    run: 'echo child-out; echo child-err >&2; exit 7'
  safe:
    run: 'echo success'
  disabled:
    enabled: false
    run: 'touch SHOULD_NOT_EXIST'
`
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name           string
		args           []string
		code           int
		stdout, stderr string
		json           bool
	}{
		{"bare", []string{"run"}, 0, "Next: noon run safe", "", false},
		{"bare json", []string{"run", "--json"}, 0, "description", "", true},
		{"suggestion", []string{"--json", "run", "tset"}, 1, `"code":"not_found"`, "", true},
		{"disabled", []string{"run", "disabled", "--json"}, 1, `"code":"disabled"`, "", true},
		{"extra argument", []string{"run", "tests", "extra", "--json"}, 2, `"code":"usage"`, "", true},
		{"json child exit", []string{"run", "--json", "tests"}, 7, `"exit_code":7`, "child-out", true},
		{"json after name", []string{"run", "tests", "--json"}, 7, `"status":"failed"`, "child-err", true},
		{"human child exit", []string{"run", "tests"}, 7, "child-out", "failed: exit 7", false},
		{"success", []string{"run", "safe"}, 0, "success", "succeeded", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := cli.Run(context.Background(), append([]string{"noon"}, tc.args...), config.BuildOpts{}, &out, &stderr)
			if code != tc.code || !strings.Contains(out.String(), tc.stdout) || !strings.Contains(stderr.String(), tc.stderr) {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &out, &stderr)
			}
			if tc.json {
				var result map[string]any
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatalf("impure JSON: %s: %v", &out, err)
				}
				if result["schema_version"] != float64(1) {
					t.Fatalf("%v", result)
				}
				if tc.name == "suggestion" && !strings.Contains(result["error"].(map[string]any)["hint"].(string), "tests") {
					t.Fatalf("%v", result)
				}
				if tc.name == "disabled" && !strings.Contains(result["error"].(map[string]any)["hint"].(string), "noon.yaml") {
					t.Fatalf("%v", result)
				}
				if strings.HasPrefix(tc.name, "json ") {
					if strings.Contains(out.String(), "child-") || !strings.Contains(stderr.String(), "child-out") || !strings.Contains(stderr.String(), "child-err") {
						t.Fatalf("stdout=%s stderr=%s", &out, &stderr)
					}
				}
			}
			if tc.name == "bare" && (!strings.Contains(out.String(), "Run the test suite") || strings.Contains(out.String(), "disabled") || stderr.Len() != 0) {
				t.Fatalf("stdout=%s stderr=%s", &out, &stderr)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, "SHOULD_NOT_EXIST")); !os.IsNotExist(err) {
		t.Fatalf("disabled job ran: %v", err)
	}
}
