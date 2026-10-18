package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/cli"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

// Noon's styling must never alter child bytes or become part of retained logs.
func TestRunColorPreservesChildOutput(t *testing.T) {
	root := sandbox(t)
	manifest := "jobs:\n  raw:\n    run: |\n      printf '\\033[35mchild\\033[0m\\n'; exit 3\n"
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	child := "\x1b[35mchild\x1b[0m\n"
	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"noon", "run", "raw", "--color=always"}, config.BuildOpts{}, &out, &stderr)
	if code != 3 || out.String() != child || !strings.Contains(stderr.String(), "\x1b[31mfailed\x1b[0m") || !strings.Contains(stderr.String(), "\x1b[36m▶\x1b[0m") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "logs", "raw", "--section=job", "--color=always"}, config.BuildOpts{}, &out, &stderr)
	if code != 0 || out.String() != child || strings.ContainsRune(stderr.String(), '\x1b') {
		t.Fatalf("log bytes changed: exit=%d stdout=%q stderr=%q", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "run", "raw", "--json", "--color=always"}, config.BuildOpts{}, &out, &stderr)
	if code != 3 || !json.Valid(out.Bytes()) || strings.ContainsRune(out.String(), '\x1b') || strings.ContainsRune(strings.ReplaceAll(stderr.String(), child, ""), '\x1b') {
		t.Fatalf("JSON run added styling or lost output routing: exit=%d stdout=%q stderr=%q", code, &out, &stderr)
	}
}

func TestRunNoopSemantics(t *testing.T) {
	root := sandbox(t)
	latestMarker := filepath.Join(root, "latest-ran")
	latestCommand := "if [ ! -e " + strconv.Quote(latestMarker) + " ]; then touch " + strconv.Quote(latestMarker) + "; exit 0; fi; exit 78"
	manifest := "jobs:\n" +
		"  poll:\n    run: 'echo noop-output; exit 78'\n    noop_exit_code: 78\n    on_success: [touch, noop-success-hook]\n    on_failure: [touch, noop-failure-hook]\n" +
		"  latest:\n    run: " + strconv.Quote(latestCommand) + "\n    noop_exit_code: 78\n" +
		"  plain:\n    run: 'exit 78'\n" +
		"  start_failure:\n    run: [noon-noop-missing-command]\n    noop_exit_code: 78\n"
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"noon", "run", "poll"}, config.BuildOpts{}, &out, &stderr)
	if code != 0 || !strings.Contains(stderr.String(), "noop-output") || !strings.Contains(stderr.String(), "noop") || !strings.Contains(stderr.String(), "exit 78") {
		t.Fatalf("human noop: exit=%d stdout=%q stderr=%q", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "run", "poll", "--json"}, config.BuildOpts{}, &out, &stderr)
	var jsonResult map[string]any
	if err := json.Unmarshal(out.Bytes(), &jsonResult); err != nil {
		t.Fatalf("noop JSON: %v stdout=%q stderr=%q", err, &out, &stderr)
	}
	run := jsonResult["run"].(map[string]any)
	if code != 0 || run["status"] != "noop" || run["exit_code"] != float64(78) {
		t.Fatalf("JSON noop: exit=%d run=%+v stderr=%q", code, run, &stderr)
	}

	for _, args := range [][]string{{"run", "latest"}, {"run", "latest"}} {
		out.Reset()
		stderr.Reset()
		if code := cli.Run(context.Background(), append([]string{"noon"}, args...), config.BuildOpts{}, &out, &stderr); code != 0 {
			t.Fatalf("run latest %v: exit=%d stdout=%q stderr=%q", args, code, &out, &stderr)
		}
	}
	out.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"noon", "run", "plain"}, config.BuildOpts{}, &out, &stderr); code != 78 || !strings.Contains(stderr.String(), "failed") {
		t.Fatalf("unconfigured exit 78: exit=%d stdout=%q stderr=%q", code, &out, &stderr)
	}
	out.Reset()
	stderr.Reset()
	code = cli.Run(context.Background(), []string{"noon", "run", "start_failure", "--json"}, config.BuildOpts{}, &out, &stderr)
	jsonResult = nil
	if err := json.Unmarshal(out.Bytes(), &jsonResult); err != nil {
		t.Fatalf("start failure JSON: %v stdout=%q stderr=%q", err, &out, &stderr)
	}
	run = jsonResult["run"].(map[string]any)
	if code != 127 || run["status"] != "failed" {
		t.Fatalf("start failure with noop code: exit=%d run=%+v stderr=%q", code, run, &stderr)
	}
	for _, marker := range []string{"noop-success-hook", "noop-failure-hook"} {
		if _, err := os.Stat(filepath.Join(root, marker)); !os.IsNotExist(err) {
			t.Fatalf("noop fired %s: %v", marker, err)
		}
	}

	code, history := invoke(t, "history", "--status", "noop", "--json")
	if code != 0 {
		t.Fatalf("history noop: %d %+v", code, history)
	}
	noops := history["runs"].([]any)
	if len(noops) != 3 {
		t.Fatalf("history --status noop returned %d runs, want 3: %+v", len(noops), noops)
	}
	for _, value := range noops {
		if value.(map[string]any)["status"] != "noop" {
			t.Fatalf("noop history filter included another status: %+v", value)
		}
	}
	for _, command := range []string{"list", "status"} {
		code, result := invoke(t, command, "--json")
		if code != 0 {
			t.Fatalf("%s: %d %+v", command, code, result)
		}
		for _, value := range result["jobs"].([]any) {
			job := value.(map[string]any)
			if job["name"] == "poll" && job["noop_exit_code"] != float64(78) {
				t.Fatalf("%s omitted configured noop_exit_code: %+v", command, job)
			}
			if job["name"] == "latest" {
				last := job["last_run"].(map[string]any)
				if last["status"] != "succeeded" {
					t.Fatalf("%s LAST includes noop: %+v", command, job)
				}
			}
		}
	}
}

func TestRunRejectsUnsupportedPlatform(t *testing.T) {
	root := sandbox(t)
	platform := config.CurrentPlatform()
	otherOS := "darwin"
	if platform.OS == "darwin" {
		otherOS = "linux"
	}
	manifest := fmt.Sprintf("jobs:\n  here:\n    os: [%s]\n    arch: [%s]\n    run: [\"true\"]\n  elsewhere:\n    os: [%s]\n    schedule: every 1h\n    run: [noon-missing-command]\n", platform.OS, platform.Arch, otherOS)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	code, listed := invoke(t, "list", "--json")
	if code != 0 {
		t.Fatalf("list: %d %+v", code, listed)
	}
	jobs := listed["jobs"].([]any)
	platformSupport := map[string]bool{}
	for _, raw := range jobs {
		job := raw.(map[string]any)
		platformSupport[job["name"].(string)] = job["platform_supported"].(bool)
		if job["name"] == "elsewhere" && job["os"].([]any)[0] != otherOS {
			t.Fatalf("nonmatching job OS not exposed: %+v", job)
		}
	}
	if !platformSupport["here"] || platformSupport["elsewhere"] {
		t.Fatalf("platform support = %+v", platformSupport)
	}

	var out, stderr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "list"}, config.BuildOpts{Version: "test"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "other platform") {
		t.Fatalf("human list did not identify nonmatching job: exit=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
	out.Reset()
	stderr.Reset()
	if code := cli.Run(context.Background(), []string{"noon", "status"}, config.BuildOpts{Version: "test"}, &out, &stderr); code != 0 || !strings.Contains(out.String(), "other platform") {
		t.Fatalf("dashboard did not identify nonmatching job: exit=%d stdout=%q stderr=%q", code, out.String(), stderr.String())
	}
	var runOut, runErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "run", "here"}, config.BuildOpts{Version: "test"}, &runOut, &runErr); code != 0 {
		t.Fatalf("matching run: code=%d stdout=%q stderr=%q", code, runOut.String(), runErr.String())
	}
	for _, args := range [][]string{{"run", "elsewhere", "--json"}, {"run", "elsewhere", "--detach", "--json"}} {
		code, result := invoke(t, args...)
		failure, ok := result["error"].(map[string]any)
		if code != 1 || !ok || failure["code"] != "unsupported_platform" || !strings.Contains(failure["hint"].(string), platform.OS+"/"+platform.Arch) {
			t.Fatalf("unsupported run %v: code=%d result=%+v", args, code, result)
		}
	}
	code, history := invoke(t, "history", "--json")
	if code != 0 {
		t.Fatalf("history: %d %+v", code, history)
	}
	runs := history["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["job"] != "here" {
		t.Fatalf("unsupported run entered history: %+v", runs)
	}
	code, doctor := invoke(t, "doctor", "--json")
	if code != 0 {
		t.Fatalf("doctor: %d %+v", code, doctor)
	}
	for _, raw := range doctor["checks"].([]any) {
		check := raw.(map[string]any)
		if strings.Contains(check["message"].(string), "noon-missing-command") {
			t.Fatalf("doctor reported an ineligible command: %+v", check)
		}
	}
}

func TestPromptJobs(t *testing.T) {
	root := sandbox(t)
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'started\\n' >> \"$AGENT_STARTS_FILE\"\nprintf '%s\\0' \"$@\" > \"$AGENT_ARGV_FILE\"\nprintf '%s' \"$NOON_AGENT\" > \"$AGENT_NAME_FILE\"\nprintf '%s' \"${NOON_AGENT_SESSION_ID-unset}\" > \"$AGENT_SESSION_FILE\"\ncat > \"$AGENT_STDIN_FILE\"\n"
	for _, name := range []string{"record-agent", "codex", "gemini", "opencode"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NOON_AGENT_SESSION_ID", "inherited-session-leak")
	for key, name := range map[string]string{
		"AGENT_STARTS_FILE": "starts", "AGENT_ARGV_FILE": "argv", "AGENT_NAME_FILE": "agent",
		"AGENT_STDIN_FILE": "stdin", "AGENT_SESSION_FILE": "session",
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
  session:
    command: [record-agent, session-base]
    prompt: argument
    session_id_flag: --session-id
    prompt_args: [--profile-tail, session-tail]
  stdin_session:
    command: [record-agent, stdin-session-base]
    session_id_flag: --session-id
    prompt_args: [--profile-tail, stdin-tail]
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
  session_run:
    prompt: "session prompt"
    agent: session
    agent_args: [--model, haiku]
  stdin_args:
    prompt: "stdin profile prompt\n"
    agent: stdin_session
    agent_args: [--model, haiku, '', --effort, low]
  builtin_gemini:
    prompt: "built-in gemini prompt"
    agent: gemini
  builtin_opencode:
    prompt: "built-in opencode prompt"
    agent: opencode
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
		{"builtin_gemini", "gemini", []string{"-p", "built-in gemini prompt"}, ""},
		{"builtin_opencode", "opencode", []string{"run", "--", "built-in opencode prompt"}, ""},
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
			session, err := os.ReadFile(filepath.Join(root, "session"))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotArgs, tc.argv) || string(stdin) != tc.stdin || string(agent) != tc.agent || string(session) != "unset" || !strings.Contains(string(mustRead(t, filepath.Join(root, "starts"))), "started") {
				t.Fatalf("argv=%q stdin=%q agent=%q starts=%q", gotArgs, stdin, agent, mustRead(t, filepath.Join(root, "starts")))
			}
		})
	}
	var sessionIDs []string
	sessionPattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for i := 0; i < 2; i++ {
		var out, stderr bytes.Buffer
		if code := cli.Run(context.Background(), []string{"noon", "--json", "run", "session_run"}, config.BuildOpts{}, &out, &stderr); code != 0 {
			t.Fatalf("session run exit=%d stdout=%s stderr=%s", code, &out, &stderr)
		}
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("session run JSON %q: %v", out.String(), err)
		}
		run := result["run"].(map[string]any)
		sessionID, ok := run["agent_session_id"].(string)
		if !ok || !sessionPattern.MatchString(sessionID) {
			t.Fatalf("session ID = %v, want RFC 4122 v4 UUID", run["agent_session_id"])
		}
		argvData := mustRead(t, filepath.Join(root, "argv"))
		argv := bytes.Split([]byte(argvData), []byte{0})
		argv = argv[:len(argv)-1]
		gotArgs := make([]string, len(argv))
		for i := range argv {
			gotArgs[i] = string(argv[i])
		}
		wantArgs := []string{"session-base", "--session-id", sessionID, "--model", "haiku", "--profile-tail", "session-tail", "session prompt"}
		if !reflect.DeepEqual(gotArgs, wantArgs) || mustRead(t, filepath.Join(root, "session")) != sessionID {
			t.Fatalf("session argv=%q env=%q, want argv=%q and matching environment", gotArgs, mustRead(t, filepath.Join(root, "session")), wantArgs)
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	if sessionIDs[0] == sessionIDs[1] {
		t.Fatalf("two prompt runs reused session ID %s", sessionIDs[0])
	}
	var stdinArgsOut, stdinArgsErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "--json", "run", "stdin_args"}, config.BuildOpts{}, &stdinArgsOut, &stdinArgsErr); code != 0 {
		t.Fatalf("stdin args run exit=%d stdout=%s stderr=%s", code, &stdinArgsOut, &stdinArgsErr)
	}
	stdinSessionID := mustRead(t, filepath.Join(root, "session"))
	if !sessionPattern.MatchString(stdinSessionID) {
		t.Fatalf("stdin session ID = %q, want RFC 4122 v4 UUID", stdinSessionID)
	}
	stdinArgvData := mustRead(t, filepath.Join(root, "argv"))
	stdinArgv := bytes.Split([]byte(stdinArgvData), []byte{0})
	stdinArgv = stdinArgv[:len(stdinArgv)-1]
	gotStdinArgs := make([]string, len(stdinArgv))
	for i := range stdinArgv {
		gotStdinArgs[i] = string(stdinArgv[i])
	}
	wantStdinArgs := []string{"stdin-session-base", "--session-id", stdinSessionID, "--model", "haiku", "", "--effort", "low", "--profile-tail", "stdin-tail"}
	if !reflect.DeepEqual(gotStdinArgs, wantStdinArgs) || mustRead(t, filepath.Join(root, "stdin")) != "stdin profile prompt\n" {
		t.Fatalf("stdin args argv=%q stdin=%q want argv=%q", gotStdinArgs, mustRead(t, filepath.Join(root, "stdin")), wantStdinArgs)
	}
	var unflaggedOut, unflaggedErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "--json", "run", "default_profile"}, config.BuildOpts{}, &unflaggedOut, &unflaggedErr); code != 0 {
		t.Fatalf("unflagged JSON run exit=%d stdout=%s stderr=%s", code, &unflaggedOut, &unflaggedErr)
	}
	var unflaggedResult map[string]any
	if err := json.Unmarshal(unflaggedOut.Bytes(), &unflaggedResult); err != nil {
		t.Fatal(err)
	}
	if unflaggedResult["run"].(map[string]any)["agent_session_id"] != nil {
		t.Fatalf("unflagged run JSON session ID = %v, want null", unflaggedResult["run"].(map[string]any)["agent_session_id"])
	}
	var historyOut, historyErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "--json", "history", "session_run"}, config.BuildOpts{}, &historyOut, &historyErr); code != 0 {
		t.Fatalf("session history exit=%d stdout=%s stderr=%s", code, &historyOut, &historyErr)
	}
	var history map[string]any
	if err := json.Unmarshal(historyOut.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	runs := history["runs"].([]any)
	if len(runs) != 2 || runs[0].(map[string]any)["agent_session_id"] != sessionIDs[1] || runs[1].(map[string]any)["agent_session_id"] != sessionIDs[0] {
		t.Fatalf("session history IDs = %+v", runs)
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
	if cfg["default_agent"] != "argument" || !reflect.DeepEqual(agents["claude"], []any{"record-agent", "claude-override"}) || !reflect.DeepEqual(agents["codex"], []any{"codex", "exec"}) {
		t.Fatalf("config output: %+v", cfg)
	}
	profiles := cfg["agent_profiles"].(map[string]any)
	if profiles["session"].(map[string]any)["session_id_flag"] != "--session-id" || profiles["claude"].(map[string]any)["session_id_flag"] != nil || !reflect.DeepEqual(profiles["session"].(map[string]any)["prompt_args"], []any{"--profile-tail", "session-tail"}) {
		t.Fatalf("agent profile config output: %+v", profiles)
	}
	var configOut, configErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "config"}, config.BuildOpts{}, &configOut, &configErr); code != 0 || !strings.Contains(configOut.String(), "session_id_flag=--session-id") || !strings.Contains(configOut.String(), `prompt_args=["--profile-tail", "session-tail"]`) {
		t.Fatalf("human config omitted session_id_flag: exit=%d stdout=%s stderr=%s", code, &configOut, &configErr)
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
	if code != 1 || duplicateError["code"] != "already_running" || duplicateError["hint"] != fmt.Sprintf("Follow it: noon logs --state-dir %s %d --follow", stateDir, activeRunID) {
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
	if entries := projectFiles(t, project); len(entries) != 1 || entries[0] != "noon.yaml" {
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
  h:
    run: 'echo ran-h'
  help:
    run: 'echo ran-help'
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
		{"bare", []string{"run"}, 0, "Next: noon run h", "", false},
		{"bare json", []string{"run", "--json"}, 0, "description", "", true},
		{"suggestion", []string{"--json", "run", "tset"}, 1, `"code":"not_found"`, "", true},
		{"disabled", []string{"run", "disabled", "--json"}, 1, `"code":"disabled"`, "", true},
		{"extra argument", []string{"run", "tests", "extra", "--json"}, 2, `"code":"usage"`, "", true},
		{"json child exit", []string{"run", "--json", "tests"}, 7, `"exit_code":7`, "child-out", true},
		{"json after name", []string{"run", "tests", "--json"}, 7, `"status":"failed"`, "child-err", true},
		{"human child exit", []string{"run", "tests"}, 7, "child-out", "failed: exit 7", false},
		{"success", []string{"run", "safe"}, 0, "success", "succeeded", false},
		{"job named h", []string{"run", "h"}, 0, "ran-h", "succeeded", false},
		{"job named help", []string{"run", "help"}, 0, "ran-help", "succeeded", false},
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

func TestHooks(t *testing.T) {
	root := sandbox(t)
	projectPath, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	writeScript := func(name, body string) string {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
		return path
	}
	recordHook := writeScript("record-hook", `
printf 'call\n' >> "$HOOK_CAPTURE.calls"
cat > "$HOOK_CAPTURE.json"
printf '%s\n' "$NOON_JOB" "$NOON_STATUS" "$NOON_EXIT_CODE" "$NOON_LOG_FILE" "${NOON_AGENT_SESSION_ID-unset}" > "$HOOK_CAPTURE.env"
printf '%s\n' "${NOON_PREVIOUS_STATUS-unset}" > "$HOOK_CAPTURE.previous"
printf '%s\n' "${NOON_CONSECUTIVE_FAILURES-unset}" > "$HOOK_CAPTURE.failures"
`)
	outputHook := writeScript("output-hook", `head -c 70000 /dev/zero | tr '\000' X
`)
	failingHook := writeScript("failing-hook", `printf 'hook-failure-output\n'
exit 1
`)
	slowHook := writeScript("slow-hook", `sleep 1
printf 'hook-follow-output\n'
`)
	timeoutHook := writeScript("timeout-hook", `sleep 60 &
printf '%s\n' "$!" > "$HOOK_CHILD_PID_FILE"
wait
`)
	userConfigDir := filepath.Join(root, "user", "noon")
	if err := os.MkdirAll(userConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	userConfig := fmt.Sprintf("on_success: [%s]\non_failure: [%s]\nagents:\n  start_failure:\n    command: [%s]\n    session_id_flag: --session-id\n", strconv.Quote(recordHook), strconv.Quote(recordHook), strconv.Quote(filepath.Join(root, "missing-agent")))
	if err := os.WriteFile(filepath.Join(userConfigDir, "config.yaml"), []byte(userConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`jobs:
  success:
    run: 'test -z "${NOON_AGENT_SESSION_ID+x}"'
  failure:
    run: [sh, -c, 'exit 7']
  flip:
    run: 'test -e ok'
  agent_start_failure:
    prompt: "fail to start the agent"
    agent: start_failure
  timed_job:
    run: [sh, -c, 'sleep 5']
    timeout: 1s
  empty_override:
    run: 'true'
    on_success: []
  hooked:
    run: 'printf job-out; exit 3'
    on_failure: 'printf hook-out'
  hook_failure:
    run: 'true'
    on_success: [%s]
  large_output:
    run: 'true'
    on_success: [%s]
  hook_log_error:
    run: 'exit 7'
    on_failure: 'mv "$NOON_LOG_FILE" "$NOON_LOG_FILE.saved"; mkdir "$NOON_LOG_FILE"; exit 1'
  slow_follow:
    run: 'printf job-partial; while test ! -f follow-release; do sleep 0.01; done'
    on_success: [%s]
  hook_timeout:
    run: 'true'
    on_success: [%s]
`, strconv.Quote(failingHook), strconv.Quote(outputHook), strconv.Quote(slowHook), strconv.Quote(timeoutHook))
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOON_AGENT_SESSION_ID", "inherited-session-leak")
	stateDir := filepath.Join(root, "state", "noon")
	t.Setenv("NOON_STATE_DIR", stateDir)
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := state.RegisterProject(projectPath, time.Now())
	if err != nil {
		_ = state.Close()
		t.Fatal(err)
	}
	defer state.Close()

	var lastStderr string
	runJSON := func(name string, wantCode int) map[string]any {
		t.Helper()
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "--json", "run", name}, config.BuildOpts{}, &out, &stderr)
		if code != wantCode {
			t.Fatalf("run %s exit=%d want=%d stdout=%s stderr=%s", name, code, wantCode, out.String(), stderr.String())
		}
		lastStderr = stderr.String()
		var result map[string]any
		if err := json.Unmarshal(out.Bytes(), &result); err != nil {
			t.Fatalf("run %s returned invalid JSON %q: %v", name, out.String(), err)
		}
		return result["run"].(map[string]any)
	}
	setCapture := func(name string) string {
		t.Helper()
		path := filepath.Join(root, name)
		t.Setenv("HOOK_CAPTURE", path)
		return path
	}
	hookOf := func(run map[string]any, event, status string, exitCode float64) map[string]any {
		t.Helper()
		hook, ok := run["hook"].(map[string]any)
		if !ok || hook["event"] != event || hook["status"] != status || hook["exit_code"] != exitCode {
			t.Fatalf("run hook = %+v, want %s/%s/%v", run["hook"], event, status, exitCode)
		}
		return hook
	}
	captureSummary := func(path string, job, status, code string, expectedSession ...string) map[string]any {
		t.Helper()
		calls := mustRead(t, path+".calls")
		if calls != "call\n" {
			t.Fatalf("hook call count = %q, want one call", calls)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(mustRead(t, path+".json")), &payload); err != nil {
			t.Fatalf("hook stdin is not JSON: %v", err)
		}
		project := payload["project"].(map[string]any)
		if payload["schema_version"] != float64(1) || project["path"] != projectPath || project["name"] != filepath.Base(projectPath) {
			t.Fatalf("hook payload project/schema = %+v", payload)
		}
		run := payload["run"].(map[string]any)
		if run["job"] != job || run["status"] != status {
			t.Fatalf("hook payload run = %+v", run)
		}
		inputHook := run["hook"].(map[string]any)
		if inputHook["status"] != "running" {
			t.Fatalf("hook stdin does not show durable running transition: %+v", inputHook)
		}
		env := strings.Split(strings.TrimSuffix(mustRead(t, path+".env"), "\n"), "\n")
		wantSession := "unset"
		if len(expectedSession) > 0 {
			wantSession = expectedSession[0]
		}
		if len(env) != 5 || env[0] != job || env[1] != status || env[2] != code || !filepath.IsAbs(env[3]) || env[4] != wantSession {
			t.Fatalf("hook environment = %q", env)
		}
		log := run["log"].(map[string]any)
		if log["path"] != env[3] {
			t.Fatalf("hook run.log.path = %v, NOON_LOG_FILE = %q", log["path"], env[3])
		}
		return run
	}

	// One snapshot must agree in both outputs and never leak inherited/job values.
	t.Setenv("NOON_PREVIOUS_STATUS", "inherited-leak")
	t.Setenv("NOON_CONSECUTIVE_FAILURES", "999")
	for i, want := range []string{"", "failed", "failed"} {
		if i == 2 {
			if err := os.WriteFile(filepath.Join(root, "ok"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		capture := setCapture(fmt.Sprintf("flip-%d", i))
		code, status := 1, "failed"
		if i == 2 {
			code, status = 0, "succeeded"
		}
		result := runJSON("flip", code)
		nested := captureSummary(capture, "flip", status, strconv.Itoa(code))
		var payload map[string]any
		if err := json.Unmarshal([]byte(mustRead(t, capture+".json")), &payload); err != nil {
			t.Fatal(err)
		}
		wantCount := []int{1, 2, 0}[i]
		if payload["consecutive_failures"] != float64(wantCount) || mustRead(t, capture+".failures") != strconv.Itoa(wantCount)+"\n" {
			t.Fatalf("failure count mismatch: payload=%v env=%q", payload, mustRead(t, capture+".failures"))
		}
		value, present := payload["previous_status"]
		if !present || (want == "" && value != nil) || (want != "" && value != want) {
			t.Fatalf("previous_status=%v present=%v want=%q", value, present, want)
		}
		wantEnv := want
		if wantEnv == "" {
			wantEnv = "unset"
		}
		if got := mustRead(t, capture+".previous"); got != wantEnv+"\n" {
			t.Fatalf("previous env=%q want=%q", got, wantEnv)
		}
		if _, ok := nested["previous_status"]; ok {
			t.Fatal("hook-only context leaked into nested run")
		}
		if len(nested) != len(result) {
			t.Fatal("hook run shape differs from shared run JSON")
		}
		for key := range result {
			if _, ok := nested[key]; !ok {
				t.Fatalf("hook run missing shared field %s", key)
			}
		}
	}

	// Config cannot inject reserved hook metadata in the first place.
	invalidManifest := strings.Replace(manifest, "  flip:\n", "  flip:\n    env: {NOON_PREVIOUS_STATUS: job-leak, NOON_CONSECUTIVE_FAILURES: '999', NOON_AGENT_SESSION_ID: job-leak}\n", 1)
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(invalidManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	var invalidOut, invalidErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "run", "flip"}, config.BuildOpts{}, &invalidOut, &invalidErr); code == 0 || !strings.Contains(invalidErr.String(), "NOON_ is reserved") {
		t.Fatalf("reserved previous status accepted: exit=%d stderr=%s", code, invalidErr.String())
	}
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	capture := setCapture("success-record")
	run := runJSON("success", 0)
	if run["status"] != "succeeded" {
		t.Fatalf("successful run status=%v", run["status"])
	}
	input := captureSummary(capture, "success", "succeeded", "0")
	if got := mustRead(t, capture+".previous"); got != "unset\n" {
		t.Fatalf("inherited previous status leaked: %q", got)
	}
	if input["hook"].(map[string]any)["event"] != "on_success" {
		t.Fatalf("success ran the wrong hook: %+v", input["hook"])
	}
	hookOf(run, "on_success", "ok", 0)

	capture = setCapture("failure-record")
	run = runJSON("failure", 7)
	if run["status"] != "failed" {
		t.Fatalf("failed run status=%v", run["status"])
	}
	input = captureSummary(capture, "failure", "failed", "7")
	if input["hook"].(map[string]any)["event"] != "on_failure" {
		t.Fatalf("failure ran the wrong hook: %+v", input["hook"])
	}
	hookOf(run, "on_failure", "ok", 0)

	capture = setCapture("timeout-record")
	run = runJSON("timed_job", 124)
	if run["status"] != "timed_out" {
		t.Fatalf("timed-out run status=%v", run["status"])
	}
	input = captureSummary(capture, "timed_job", "timed_out", "124")
	if input["hook"].(map[string]any)["event"] != "on_failure" {
		t.Fatalf("timed-out run used the wrong hook: %+v", input["hook"])
	}
	hookOf(run, "on_failure", "ok", 0)

	capture = setCapture("empty-record")
	run = runJSON("empty_override", 0)
	if run["hook"] != nil {
		t.Fatalf("empty list did not disable the inherited default: %+v", run["hook"])
	}
	if _, err := os.Stat(capture + ".calls"); !os.IsNotExist(err) {
		t.Fatalf("disabled inherited hook ran: %v", err)
	}
	if logInfo := run["log"].(map[string]any); logInfo["hook_offset"] != nil {
		t.Fatalf("run without a hook has hook_offset=%v, want null", logInfo["hook_offset"])
	}
	var noHookOut, noHookErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(run["id"]), "--section", "hook"}, config.BuildOpts{}, &noHookOut, &noHookErr); code != 0 || noHookOut.Len() != 0 {
		t.Fatalf("empty hook section: exit=%d stdout=%q stderr=%q", code, noHookOut.String(), noHookErr.String())
	}

	run = runJSON("hooked", 3)
	hookOffset := run["log"].(map[string]any)["hook_offset"]
	if hookOffset != float64(len("job-out\n")) {
		t.Fatalf("hook_offset=%v, want job section length %d including separator", hookOffset, len("job-out\n"))
	}
	var jobOut, jobErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(run["id"]), "--section", "job", "--tail", "1"}, config.BuildOpts{}, &jobOut, &jobErr); code != 0 || jobOut.String() != "job-out\n" || !strings.Contains(jobErr.String(), "--section job --after-cursor") {
		t.Fatalf("job section tail: exit=%d stdout=%q stderr=%q", code, jobOut.String(), jobErr.String())
	}
	var hookOut, hookErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "logs", fmt.Sprint(run["id"]), "--section", "hook"}, config.BuildOpts{}, &hookOut, &hookErr); code != 0 || hookOut.String() != "[noon: on_failure hook exited 0]\nhook-out" {
		t.Fatalf("hook section: exit=%d stdout=%q stderr=%q", code, hookOut.String(), hookErr.String())
	}

	run = runJSON("hook_failure", 0)
	if run["status"] != "succeeded" {
		t.Fatalf("hook failure changed run status: %+v", run)
	}
	hookOf(run, "on_success", "failed", 1)
	if !strings.Contains(lastStderr, "on_success hook: exit 1") {
		t.Fatalf("foreground output omitted hook result: %q", lastStderr)
	}

	// A log append failure must not replace the job exit or strand its hook.
	run = runJSON("hook_log_error", 7)
	if run["status"] != "failed" || !strings.Contains(lastStderr, "hook recording failed") {
		t.Fatalf("hook log error lost original result: %+v stderr=%s", run, lastStderr)
	}
	hookOf(run, "on_failure", "failed", 1)

	capture = setCapture("agent-start-failure-record")
	run = runJSON("agent_start_failure", 127)
	sessionID, ok := run["agent_session_id"].(string)
	if !ok || sessionID == "" {
		t.Fatalf("failed agent run has no requested session ID: %+v", run)
	}
	nested := captureSummary(capture, "agent_start_failure", "failed", "127", sessionID)
	if nested["agent_session_id"] != sessionID {
		t.Fatalf("failed run hook JSON session ID=%v, want %s", nested["agent_session_id"], sessionID)
	}

	run = runJSON("large_output", 0)
	hookOf(run, "on_success", "ok", 0)
	stored, err := state.RunByID(int64(run["id"].(float64)))
	if err != nil {
		t.Fatal(err)
	}
	log, err := state.ReadRunLog(stored)
	if err != nil {
		t.Fatal(err)
	}
	wantLog := "[noon: on_success hook exited 0]\n" + strings.Repeat("X", 64<<10)
	if string(log) != wantLog {
		t.Fatalf("hook output length=%d, expected bounded %d-byte output", len(log), len(wantLog))
	}

	type runResult struct {
		code int
		out  string
		err  string
	}
	slowDone := make(chan runResult, 1)
	go func() {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "--json", "run", "slow_follow"}, config.BuildOpts{}, &out, &stderr)
		slowDone <- runResult{code: code, out: out.String(), err: stderr.String()}
	}()
	var slowRun store.Run
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := state.RunsForJob(projectID, "slow_follow")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) > 0 && runs[0].Status == "running" {
			slowRun = runs[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if slowRun.ID == 0 {
		t.Fatal("slow job did not enter its running state")
	}
	followDone := make(chan runResult, 1)
	go func() {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "logs", strconv.FormatInt(slowRun.ID, 10), "--follow"}, config.BuildOpts{}, &out, &stderr)
		followDone <- runResult{code: code, out: out.String(), err: stderr.String()}
	}()
	if err := os.WriteFile(filepath.Join(root, "follow-release"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hookStarted := false
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, err := state.RunByID(slowRun.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == "succeeded" && current.HookStatus.Valid && current.HookStatus.String == "running" {
			hookStarted = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !hookStarted {
		t.Fatal("slow hook did not enter its running state")
	}
	var jobWhileHook, jobWhileHookErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"noon", "logs", strconv.FormatInt(slowRun.ID, 10), "--section", "job"}, config.BuildOpts{}, &jobWhileHook, &jobWhileHookErr); code != 0 || jobWhileHook.String() != "job-partial" {
		t.Fatalf("job section while hook runs: exit=%d stdout=%q stderr=%q", code, jobWhileHook.String(), jobWhileHookErr.String())
	}
	select {
	case result := <-slowDone:
		if result.code != 0 {
			t.Fatalf("slow hook run exit=%d stderr=%s", result.code, result.err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("run did not finish after its hook")
	}
	select {
	case result := <-followDone:
		if result.code != 0 || !strings.Contains(result.out, "hook-follow-output") || !strings.Contains(result.out, "[noon: on_success hook exited 0]") {
			t.Fatalf("followed hook output code=%d stdout=%q stderr=%q", result.code, result.out, result.err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("logs --follow stopped waiting before the hook completed")
	}

	childPIDFile := filepath.Join(root, "hook-child-pid")
	t.Setenv("HOOK_CHILD_PID_FILE", childPIDFile)
	timeoutDone := make(chan runResult, 1)
	go func() {
		var out, stderr bytes.Buffer
		code := cli.Run(context.Background(), []string{"noon", "--json", "run", "hook_timeout"}, config.BuildOpts{}, &out, &stderr)
		timeoutDone <- runResult{code: code, out: out.String(), err: stderr.String()}
	}()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(childPIDFile); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(childPIDFile); err != nil {
		t.Fatalf("timeout hook did not start its child: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(mustRead(t, childPIDFile)))
	if err != nil {
		t.Fatal(err)
	}
	childIdentity, err := runner.ProcessStartIdentity(childPID)
	if err != nil {
		t.Fatalf("identify timeout child: %v", err)
	}
	var timeoutResult runResult
	select {
	case timeoutResult = <-timeoutDone:
	case <-time.After(40 * time.Second):
		t.Fatal("timed-out hook did not finish within its timeout and grace")
	}
	if timeoutResult.code != 0 {
		t.Fatalf("hook timeout changed run exit code: %d stderr=%s", timeoutResult.code, timeoutResult.err)
	}
	var timeoutJSON map[string]any
	if err := json.Unmarshal([]byte(timeoutResult.out), &timeoutJSON); err != nil {
		t.Fatal(err)
	}
	timeoutRun := timeoutJSON["run"].(map[string]any)
	if timeoutRun["status"] != "succeeded" {
		t.Fatalf("hook timeout changed run status: %+v", timeoutRun)
	}
	hookOf(timeoutRun, "on_success", "timed_out", 124)
	alive, err := runner.ProcessOwnerAlive(childPID, childIdentity)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("check timed-out hook child: %v", err)
	}
	if alive {
		t.Fatalf("timed-out hook process group left child %d alive", childPID)
	}
}
