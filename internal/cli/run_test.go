package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/brettinternet/bump/internal/cli"
	"github.com/brettinternet/bump/internal/config"
)

func TestPromptJobs(t *testing.T) {
	root := sandbox(t)
	binDir := filepath.Join(root, "bin")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf 'started\\n' >> \"$AGENT_STARTS_FILE\"\nprintf '%s\\0' \"$@\" > \"$AGENT_ARGV_FILE\"\nprintf '%s' \"$BUMP_AGENT\" > \"$AGENT_NAME_FILE\"\ncat > \"$AGENT_STDIN_FILE\"\n"
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
	if err := os.MkdirAll(filepath.Join(root, "user", "bump"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "user", "bump", "config.yaml"), []byte(userConfig), 0o600); err != nil {
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
	if err := os.WriteFile(filepath.Join(root, "bump.yaml"), []byte(manifest), 0o600); err != nil {
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
			code := cli.Run(context.Background(), []string{"bump", "run", tc.name}, config.BuildOpts{}, &out, &stderr)
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
			code := cli.Run(context.Background(), []string{"bump", "run", name}, config.BuildOpts{}, &out, &stderr)
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
	var listOut, listErr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"bump", "list"}, config.BuildOpts{}, &listOut, &listErr); code != 0 || !strings.Contains(listOut.String(), "claude: inline stdin first…") || !strings.Contains(listOut.String(), "codex: prompts/stdin.md") {
		t.Fatalf("human prompt list: code=%d stdout=%s stderr=%s", code, &listOut, &listErr)
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
	if err := os.WriteFile(filepath.Join(root, "bump.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name           string
		args           []string
		code           int
		stdout, stderr string
		json           bool
	}{
		{"bare", []string{"run"}, 0, "Next: bump run safe", "", false},
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
			code := cli.Run(context.Background(), append([]string{"bump"}, tc.args...), config.BuildOpts{}, &out, &stderr)
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
				if tc.name == "disabled" && !strings.Contains(result["error"].(map[string]any)["hint"].(string), "bump.yaml") {
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
