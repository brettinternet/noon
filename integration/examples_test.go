package integration

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"
)

// Users copy examples verbatim, and strict validation rejects any field or schedule
// that drifts from the implementation, so every shipped example must validate.
func TestExamplesValidate(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	if err := os.MkdirAll(filepath.Join(configHome, "noon"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	// Prompt jobs name profiles defined in the agent-profiles user config.
	profiles, err := os.ReadFile("../examples/ai/agent-profiles/user-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "noon", "config.yaml"), profiles, 0o600); err != nil {
		t.Fatal(err)
	}
	env := isolatedEnvironment(home, configHome, filepath.Join(root, "xdg-state"), filepath.Join(root, "state"))
	check := func(t *testing.T, args ...string) {
		t.Helper()
		command := exec.Command(noonBinary, append(args, "config", "check")...)
		command.Dir, command.Env = home, env
		if output, err := command.CombinedOutput(); err != nil {
			t.Errorf("noon %s config check: %v\n%s", strings.Join(args, " "), err, output)
		}
	}

	examples, err := filepath.Abs("../examples")
	if err != nil {
		t.Fatal(err)
	}
	projects := 0
	err = filepath.WalkDir(examples, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch entry.Name() {
		case "noon.yaml":
			projects++
			check(t, "-C", filepath.Dir(path))
		case "user-config.yaml", "config.yaml", "jobs.yaml":
			check(t, "--config", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if projects < 20 {
		t.Fatalf("found %d example projects; expected the examples tree", projects)
	}

	// The private override only makes sense layered over its shared noon.yaml.
	project := filepath.Join(root, "local-overrides")
	if err := os.MkdirAll(filepath.Join(project, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	for source, target := range map[string]string{"noon.yaml": "noon.yaml", "private.yaml": filepath.Join(".git", "noon.yaml")} {
		data, err := os.ReadFile(filepath.Join(examples, "basics", "local-overrides", source))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(project, target), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	check(t, "-C", project)
}

// A dirty tree must not bypass verification, and repeated Stop callbacks must
// eventually release the gate. This exercises our script, not a vendor agent.
// Guard the copied event consumer against reading the wrong envelope field or
// teaching merge/duplicate behavior that silently replaces the original payload.
func TestExampleEventRequests(t *testing.T) {
	root := t.TempDir()
	home, configHome, stateHome, stateDir := remoteDirectories(t, root)
	env := isolatedEnvironment(home, configHome, stateHome, stateDir)
	project := filepath.Join(root, "project")
	if err := os.CopyFS(project, os.DirFS("../examples/basics/event-requests")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	run := func(args ...string) []byte {
		t.Helper()
		command := exec.CommandContext(ctx, noonBinary, args...)
		command.Dir, command.Env = project, env
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("noon %v: %v\n%s", args, err, output)
		}
		return output
	}
	run("enable")
	type queuedRun struct {
		Created bool   `json:"created"`
		Reason  string `json:"reason"`
		Run     struct {
			ID        int64  `json:"id"`
			EventFile string `json:"event_file"`
		} `json:"run"`
	}
	queue := func(job, key, fixture string) queuedRun {
		t.Helper()
		var result queuedRun
		output := run("run", job, "--detach", "--key", key, "--data", "@fixtures/"+fixture+".json", "--json")
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("queue JSON: %v\n%s", err, output)
		}
		return result
	}
	refresh := queue("refresh", "refresh-first", "first")
	merged := queue("refresh", "refresh-second", "second")
	first := queue("event", "event-first", "first")
	second := queue("event", "event-second", "second")
	duplicate := queue("event", "event-first", "second")
	if !refresh.Created || merged.Created || merged.Reason != "queued" || merged.Run.ID != refresh.Run.ID ||
		!first.Created || !second.Created || first.Run.ID == second.Run.ID ||
		duplicate.Created || duplicate.Reason != "duplicate" || duplicate.Run.ID != first.Run.ID {
		t.Fatalf("unexpected queue results: %+v %+v %+v %+v %+v", refresh, merged, first, second, duplicate)
	}
	// Read the public event_file paths before dispatch: the duplicate's new
	// payload must not overwrite the first event, nor the merged refresh.
	for _, check := range []struct {
		queued  queuedRun
		message string
	}{{refresh, "first event"}, {first, "first event"}, {second, "second event"}} {
		data, err := os.ReadFile(check.queued.Run.EventFile)
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			Data struct {
				Message string `json:"message"`
			} `json:"data"`
		}
		if err := json.Unmarshal(data, &event); err != nil || event.Data.Message != check.message {
			t.Fatalf("event file = %s, err=%v; want %q", data, err, check.message)
		}
	}
	_, stop := startRemoteScheduler(t, noonBinary, home, env, "--listen", "127.0.0.1:0")
	defer stop()
	for _, check := range []struct {
		id      int64
		message string
	}{{refresh.Run.ID, "first event"}, {first.Run.ID, "first event"}, {second.Run.ID, "second event"}} {
		run("logs", fmt.Sprint(check.id), "--follow")
		output := run("logs", fmt.Sprint(check.id), "--section", "job", "--json")
		var result struct {
			Log string `json:"log"`
		}
		if err := json.Unmarshal(output, &result); err != nil || strings.TrimSpace(result.Log) != fmt.Sprintf(`{"message":%q}`, check.message) {
			t.Fatalf("shipped consumer run %d: %s, %v", check.id, output, err)
		}
	}
	var history struct {
		Runs []struct {
			Status string `json:"status"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(run("history", "--json"), &history); err != nil || len(history.Runs) != 3 {
		t.Fatalf("expected exactly three runs: %+v, %v", history, err)
	}
	for _, r := range history.Runs {
		if r.Status != "succeeded" {
			t.Fatalf("example did not succeed: %+v", history)
		}
	}
}

// Exercise the shipped job, not a substitute command: malformed event consumption
// must not look like a successful remote integration. Broader auth/MCP boundaries
// are already covered by TestRemoteAPI.
func TestExampleRemoteAccess(t *testing.T) {
	root := t.TempDir()
	home, configHome, stateHome, stateDir := remoteDirectories(t, root)
	env := isolatedEnvironment(home, configHome, stateHome, stateDir)
	project := filepath.Join(root, "project")
	if err := os.CopyFS(project, os.DirFS("../examples/integrations/remote-access")); err != nil {
		t.Fatal(err)
	}
	if code, output := runRemoteCommand(noonBinary, project, env, "enable"); code != 0 {
		t.Fatalf("enable: %d\n%s", code, output)
	}
	token := rand.Text() + rand.Text()
	tokenFile := filepath.Join(root, "api-token")
	writeRemoteFile(t, tokenFile, token+"\n", 0o600)
	address, stop := startRemoteScheduler(t, noonBinary, home, env, "--listen", "127.0.0.1:0", "--api-token-file", tokenFile)
	defer stop()
	baseURL := "http://" + address
	client := &http.Client{Timeout: 45 * time.Second}
	response, body := remoteRequest(t, client, http.MethodGet, baseURL+"/v1/projects", "", nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated discovery: %d %s", response.StatusCode, body)
	}
	response, body = remoteRequest(t, client, http.MethodGet, baseURL+"/v1/projects", token, nil)
	var projects struct {
		Projects []struct {
			ID int64 `json:"id"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &projects); err != nil || response.StatusCode != http.StatusOK || len(projects.Projects) != 1 {
		t.Fatalf("discovery: %d %s, %v", response.StatusCode, body, err)
	}
	queueURL := fmt.Sprintf("%s/v1/projects/%d/jobs/show-event/runs", baseURL, projects.Projects[0].ID)
	response, body = remoteRequest(t, client, http.MethodPost, queueURL, token, []byte(`{"key":"demo-first","data":{"message":"hello from the API"}}`))
	var queued struct {
		Run struct {
			ID int64 `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(body, &queued); err != nil || response.StatusCode != http.StatusAccepted || queued.Run.ID == 0 {
		t.Fatalf("queue: %d %s, %v", response.StatusCode, body, err)
	}
	response, body = remoteRequest(t, client, http.MethodGet, fmt.Sprintf("%s/v1/runs/%d?wait_seconds=30&log_bytes=4096", baseURL, queued.Run.ID), token, nil)
	var result struct {
		Run struct {
			Status string `json:"status"`
		} `json:"run"`
		Log string `json:"log"`
	}
	if err := json.Unmarshal(body, &result); err != nil || response.StatusCode != http.StatusOK || result.Run.Status != "succeeded" || strings.TrimSpace(result.Log) != `{"message":"hello from the API"}` {
		t.Fatalf("shipped remote job: %d %s, %v", response.StatusCode, body, err)
	}
}

func TestExampleVerifyGate(t *testing.T) {
	for _, tool := range []string{"git", "jq"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("verify-gate test requires %s; run task init: %v", tool, err)
		}
	}
	root := t.TempDir()
	project, home, bin, tmp := filepath.Join(root, "project"), filepath.Join(root, "home"), filepath.Join(root, "bin"), filepath.Join(root, "tmp")
	for _, dir := range []string{project, home, bin, tmp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(noonBinary, filepath.Join(bin, "noon")); err != nil {
		t.Fatal(err)
	}
	// Do not inherit Git overrides, agent credentials, or user XDG paths.
	env := []string{
		"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + home, "XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_STATE_HOME=" + filepath.Join(root, "xdg-state"), "XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
		"NOON_STATE_DIR=" + filepath.Join(root, "state"), "TMPDIR=" + tmp,
		"MAX_ATTEMPTS=3", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir, cmd.Env = project, env
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	write(filepath.Join(project, "noon.yaml"), `version: 1
jobs:
  verify:
    run: [sh, verify.sh]
`)
	write(filepath.Join(project, "verify.sh"), "printf 'ran\\n' >> \"$TMPDIR/runs\"\nif test -f passing; then exit 0; fi\ni=0; while test $i -lt 100; do printf '%0300d failure-%s\\n' 0 \"$i\"; i=$((i+1)); done\nexit 1\n")
	git("init", "--quiet")
	git("add", ".")
	git("-c", "user.name=Gate Test", "-c", "user.email=gate@example.invalid", "-c", "core.hooksPath="+tmp, "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture")
	gate, err := filepath.Abs("../examples/ai/verify-loop/scripts/verify-gate.sh")
	if err != nil {
		t.Fatal(err)
	}
	run := func(session string, active bool, wantExit int, runEnv []string) string {
		t.Helper()
		cmd := exec.CommandContext(ctx, "/bin/sh", gate)
		cmd.Dir, cmd.Env = project, runEnv
		cmd.Stdin = strings.NewReader(fmt.Sprintf(`{"session_id":%q,"stop_hook_active":%t}`, session, active))
		output, err := cmd.CombinedOutput()
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != wantExit {
			t.Fatalf("gate: %v, want exit %d\n%s", err, wantExit, output)
		}
		return string(output)
	}
	counter := filepath.Join(tmp, "noon-verify-gate-test-session")
	assertAbsent := func(path string) {
		t.Helper()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected absent %s, got %v", path, err)
		}
	}
	run("test-session", false, 0, env)
	assertAbsent(filepath.Join(tmp, "runs")) // Clean tree must not invoke Noon.
	write(filepath.Join(project, "dirty"), "untracked change")
	output := run("test-session", false, 2, env)
	if !strings.Contains(output, "attempt 1 of 3") || !strings.Contains(output, "failure-99") || strings.Contains(output, "failure-0\n") || len(output) > 12500 {
		t.Fatalf("missing or unbounded failure feedback: %s", output)
	}
	if data, err := os.ReadFile(counter); err != nil || string(data) != "1\n" {
		t.Fatalf("counter = %q, %v", data, err)
	}
	write(filepath.Join(project, "passing"), "success") // Still dirty: must actually run verify.
	run("test-session", true, 0, env)
	assertAbsent(counter)
	if data, err := os.ReadFile(filepath.Join(tmp, "runs")); err != nil || string(data) != "ran\nran\n" {
		t.Fatalf("verify invocations = %q, %v", data, err)
	}
	if err := os.Remove(filepath.Join(project, "passing")); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		run("test-session", true, 2, env)
	}
	if output := run("test-session", true, 0, env); !strings.Contains(output, "after 3 attempts") {
		t.Fatalf("missing escape feedback: %s", output)
	}
	assertAbsent(counter)
	missingJQEnv := append([]string{}, env...)
	for i, value := range missingJQEnv {
		if strings.HasPrefix(value, "PATH=") {
			missingJQEnv[i] = "PATH=" + bin // Contains only noon, deliberately no jq.
		}
	}
	// Setup errors must not block: no session counter could bound the retries.
	if output := run("test-session", false, 1, missingJQEnv); !strings.Contains(output, "requires jq; install jq") {
		t.Fatalf("missing actionable jq error: %s", output)
	}
	if output := run("../invalid", false, 1, env); !strings.Contains(output, "valid session_id") {
		t.Fatalf("unsafe session ID accepted: %s", output)
	}
}

// Exercise the shipped consumers: coalescing must not lose failure diagnostics,
// and hook stdin must survive queueing as the next stage's event data.
func TestExampleScripts(t *testing.T) {
	for _, tool := range []string{"jq", "git", "tar", "shasum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("example scripts require %s; run task init: %v", tool, err)
		}
	}
	for _, example := range []string{"ai/failure-triage", "local-dev/pipelines"} {
		t.Run(filepath.Base(example), func(t *testing.T) {
			root := t.TempDir()
			project, home, bin, state := filepath.Join(root, "project"), filepath.Join(root, "home"), filepath.Join(root, "bin"), filepath.Join(root, "state")
			if err := os.CopyFS(project, os.DirFS(filepath.Join("..", "examples", example))); err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{home, bin} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, content string, mode fs.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			read := func(path string) string {
				t.Helper()
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return string(data)
			}
			if err := os.Symlink(noonBinary, filepath.Join(bin, "noon")); err != nil {
				t.Fatal(err)
			}
			env := append(isolatedEnvironment(home, filepath.Join(root, "config"), filepath.Join(root, "xdg-state"), state),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			run := func(want int, executable string, args ...string) string {
				t.Helper()
				cmd := exec.CommandContext(ctx, executable, args...)
				cmd.Dir, cmd.Env = project, env
				output, err := cmd.CombinedOutput()
				if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != want {
					t.Fatalf("%s %v: %v, want exit %d\n%s", executable, args, err, want, output)
				}
				return string(output)
			}
			var manifest map[string]any
			if err := yaml.Unmarshal([]byte(read(filepath.Join(project, "noon.yaml"))), &manifest); err != nil {
				t.Fatal(err)
			}
			jobs := manifest["jobs"].(map[string]any)
			for _, value := range jobs {
				delete(value.(map[string]any), "schedule")
			}
			triage := strings.Contains(example, "triage")
			if triage {
				// Keep both shipped queue hooks; add chatter to prove logs stay job-only.
				for _, name := range []string{"test", "e2e"} {
					job := jobs[name].(map[string]any)
					job["run"] = []string{"sh", "-c", "echo evidence-" + name + "; exit 1"}
					hook := job["on_failure"].([]any)
					words := make([]string, len(hook))
					for i, word := range hook {
						words[i] = word.(string)
					}
					job["on_failure"] = "echo hook-only-marker; " + strings.Join(words, " ")
				}
				jobs["triage"].(map[string]any)["env"].(map[string]any)["AGENT_CMD"] = "fake-agent"
				write(filepath.Join(bin, "fake-agent"), "#!/bin/sh\ncat | tee -a agent-input\necho called >> agent-calls\n", 0o700)
				for _, tool := range []string{"osascript", "notify-send"} {
					write(filepath.Join(bin, tool), "#!/bin/sh\nexit 0\n", 0o700)
				}
			} else {
				// Only build/test work is fake; hooks and packaging remain shipped code.
				write(filepath.Join(bin, "make"), "#!/bin/sh\ncase $1 in\nbuild) mkdir -p bin; printf 'artifact\\n' > bin/app;;\ntest) test -f bin/app;;\n*) exit 1;;\nesac\n", 0o700)
				jobs["package"].(map[string]any)["on_success"] = "noon run fourth --detach --json > recursion.json"
				jobs["fourth"] = map[string]any{"run": []string{"sh", "-c", "exit 99"}}
			}
			data, err := yaml.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			write(filepath.Join(project, "noon.yaml"), string(data), 0o600)
			run(0, "git", "init", "--quiet")
			run(0, "git", "add", ".")
			run(0, "git", "-c", "user.name=Example Test", "-c", "user.email=example@example.invalid", "-c", "core.hooksPath="+bin, "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture")
			run(0, noonBinary, "enable")
			type scriptRun struct {
				ID         int64      `json:"id"`
				Job        string     `json:"job"`
				Status     string     `json:"status"`
				Depth      int        `json:"depth"`
				DueAt      *time.Time `json:"due_at"`
				StartedAt  *time.Time `json:"started_at"`
				FinishedAt *time.Time `json:"finished_at"`
				EventFile  *string    `json:"event_file"`
				Hook       *struct {
					Status string `json:"status"`
				} `json:"hook"`
			}
			history := func() []scriptRun {
				t.Helper()
				var result struct {
					Runs []scriptRun `json:"runs"`
				}
				if err := json.Unmarshal([]byte(run(0, noonBinary, "history", "--json")), &result); err != nil {
					t.Fatal(err)
				}
				return result.Runs
			}
			if triage {
				run(1, noonBinary, "run", "test")
				run(1, noonBinary, "run", "e2e")
			} else {
				run(0, noonBinary, "run", "build")
			}
			initial := history()
			queued := 0
			for _, r := range initial {
				if r.Status == "queued" {
					queued++
					if r.DueAt == nil || r.DueAt.After(time.Now()) || (triage && r.Job != "triage") {
						t.Fatalf("unexpected queued run: %+v", r)
					}
				}
			}
			if queued != 1 || (triage && len(initial) != 3) {
				t.Fatalf("offline requests did not coalesce: %+v", initial)
			}
			serve := exec.Command(noonBinary, "serve")
			serve.Dir, serve.Env = project, env
			var stderr lockedBuffer
			serve.Stderr = &stderr
			if err := serve.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				done <- serve.Wait()
				close(done)
			}()
			defer func() {
				_ = serve.Process.Signal(syscall.SIGTERM)
				select {
				case err := <-done:
					if err != nil {
						t.Errorf("scheduler shutdown: %v\n%s", err, stderr.String())
					}
				case <-time.After(15 * time.Second):
					_ = serve.Process.Kill()
					<-done
					t.Error("scheduler did not stop")
				}
			}()
			waitForScheduler(t, state, done, &stderr)
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			var completed []scriptRun
			for {
				completed = history()
				ready := len(completed) == 3
				for _, r := range completed {
					ready = ready && r.FinishedAt != nil && (r.Hook == nil || r.Hook.Status == "ok" || r.Hook.Status == "failed")
				}
				if ready {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("pipeline did not finish: %+v\n%s", completed, stderr.String())
				case <-ticker.C:
				}
			}
			byJob := make(map[string]scriptRun)
			for _, r := range completed {
				byJob[r.Job] = r
			}
			if triage {
				if byJob["triage"].Status != "succeeded" {
					t.Fatalf("triage failed: %+v", completed)
				}
				reports := make(map[string]string)
				for job, other := range map[string]string{"test": "e2e", "e2e": "test"} {
					r := byJob[job]
					path := filepath.Join(project, ".noon-triage", fmt.Sprintf("%s-%d.md", job, r.ID))
					report := read(path)
					if r.Status != "failed" || !strings.Contains(report, fmt.Sprintf("run %d", r.ID)) || !strings.Contains(report, "evidence-"+job) || strings.Contains(report, "evidence-"+other) || strings.Contains(report, "hook-only-marker") {
						t.Fatalf("lost or polluted diagnosis: %s", report)
					}
					reports[path] = report
				}
				calls := read(filepath.Join(project, "agent-calls"))
				input := read(filepath.Join(project, "agent-input"))
				if calls != "called\ncalled\n" || !strings.Contains(input, "evidence-test") || !strings.Contains(input, "evidence-e2e") || strings.Contains(input, "hook-only-marker") {
					t.Fatalf("agent calls=%q input=%s", calls, input)
				}
				if output := run(0, noonBinary, "run", "triage"); !strings.Contains(output, "no undiagnosed failures") {
					t.Fatalf("repeat triage: %s", output)
				}
				if read(filepath.Join(project, "agent-calls")) != calls {
					t.Fatal("repeat triage invoked agent")
				}
				for path, report := range reports {
					if read(path) != report {
						t.Fatalf("repeat triage changed %s", path)
					}
				}
			} else {
				var previous scriptRun
				for depth, job := range []string{"build", "test", "package"} {
					r := byJob[job]
					if r.Status != "succeeded" || r.Depth != depth || r.StartedAt == nil || (depth > 0 && (r.ID <= previous.ID || r.StartedAt.Before(*previous.FinishedAt))) {
						t.Fatalf("invalid pipeline ordering: %+v", completed)
					}
					previous = r
				}
				pkg := byJob["package"]
				if pkg.EventFile == nil {
					t.Fatal("package lost event file")
				}
				var event struct {
					Data struct {
						Run scriptRun `json:"run"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(read(*pkg.EventFile)), &event); err != nil {
					t.Fatal(err)
				}
				if event.Data.Run.ID != byJob["test"].ID || event.Data.Run.Job != "test" || event.Data.Run.Status != "succeeded" {
					t.Fatalf("lost test event: %+v", event)
				}
				logs := run(0, noonBinary, "logs", fmt.Sprint(pkg.ID), "--section", "job")
				if !strings.Contains(logs, fmt.Sprintf("after test run %d: succeeded", byJob["test"].ID)) {
					t.Fatalf("package did not consume event: %s", logs)
				}
				archives, err := filepath.Glob(filepath.Join(project, "dist", "*.tar.gz"))
				if err != nil || len(archives) != 1 {
					t.Fatalf("archives=%v err=%v", archives, err)
				}
				if listing := run(0, "tar", "-tzf", archives[0]); !strings.Contains(listing, "bin/app") {
					t.Fatalf("archive contents: %s", listing)
				}
				run(0, "sh", "-c", "cd dist && shasum -a 256 -c *.sha256")
				var refusal struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal([]byte(read(filepath.Join(project, "recursion.json"))), &refusal); err != nil {
					t.Fatal(err)
				}
				if refusal.Error.Code != "recursion" || pkg.Hook == nil || pkg.Hook.Status != "failed" || len(history()) != 3 {
					t.Fatalf("fourth stage was not refused: %+v, %+v", refusal, completed)
				}
			}
		})
	}
}

func TestExampleNotifications(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	home := filepath.Join(root, "home")
	project := filepath.Join(root, "project")
	for _, dir := range []string{bin, home, project} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, data []byte, mode fs.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	wrapper, err := os.ReadFile("../examples/integrations/notifications/bin/noon-on-change")
	if err != nil {
		t.Fatal(err)
	}
	wrapperPath := filepath.Join(bin, "noon-on-change")
	write(wrapperPath, wrapper, 0o700)
	write(filepath.Join(bin, "noon-notify"), []byte("#!/bin/sh\nprintf '%s\\n' \"$NOON_STATUS\" >> \"$CAPTURE\"\n"), 0o700)
	capture := filepath.Join(root, "notifications")
	write(capture, nil, 0o600)
	env := append(isolatedEnvironment(home, filepath.Join(root, "config"), filepath.Join(root, "xdg-state"), filepath.Join(root, "state")), "CAPTURE="+capture)
	manifest := fmt.Sprintf(`version: 1
jobs:
  healthy:
    run: [sh, -c, 'exit 0']
    on_success: [%q]
    on_failure: [%q]
  changing:
    run: [sh, -c, 'test -f healthy']
    on_success: [%q]
    on_failure: [%q]
`, wrapperPath, wrapperPath, wrapperPath, wrapperPath)
	write(filepath.Join(project, "noon.yaml"), []byte(manifest), 0o600)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	assertCapture := func(want string) {
		t.Helper()
		got, err := os.ReadFile(capture)
		if err != nil || string(got) != want {
			t.Fatalf("notifications = %q, %v; want %q", got, err, want)
		}
	}
	run := func(job string, wantExit int) {
		t.Helper()
		cmd := exec.CommandContext(ctx, noonBinary, "run", job)
		cmd.Dir, cmd.Env = project, env
		output, err := cmd.CombinedOutput()
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != wantExit {
			t.Fatalf("run %s: %v\n%s", job, err, output)
		}
	}
	run("healthy", 0)
	assertCapture("")
	run("changing", 1)
	assertCapture("failed\n")
	run("changing", 1)
	assertCapture("failed\n")
	write(filepath.Join(project, "healthy"), nil, 0o600)
	run("changing", 0)
	assertCapture("failed\nrecovered\n")
	run("changing", 0)
	assertCapture("failed\nrecovered\n")

	write(capture, nil, 0o600)
	for _, transition := range [][2]string{{"failed", "timed_out"}, {"timed_out", "failed"}, {"timed_out", "timed_out"}, {"timed_out", "succeeded"}} {
		cmd := exec.CommandContext(ctx, wrapperPath)
		cmd.Dir = project
		cmd.Env = append(append([]string{}, env...), "NOON_PREVIOUS_STATUS="+transition[0], "NOON_STATUS="+transition[1])
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("transition %v: %v\n%s", transition, err, output)
		}
	}
	assertCapture("recovered\n")

	// The wrapper must pause in the owning invocation's non-default store and
	// preserve stdin for the chained hook, without invoking a real notifier.
	pauseWrapper, err := os.ReadFile("../examples/integrations/notifications/bin/noon-pause-after")
	if err != nil {
		t.Fatal(err)
	}
	pausePath := filepath.Join(bin, "noon-pause-after")
	write(pausePath, pauseWrapper, 0o700)
	if err := os.Symlink(noonBinary, filepath.Join(bin, "noon")); err != nil {
		t.Fatal(err)
	}
	for i, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			env[i] = "PATH=" + bin + string(os.PathListSeparator) + strings.TrimPrefix(entry, "PATH=")
		}
	}
	env = append(env, "NOON_PAUSE_AFTER=2")
	captureHook := filepath.Join(bin, "capture-hook")
	write(captureHook, []byte("#!/bin/sh\ncat >> \"$CAPTURE\"\nprintf '\\n' >> \"$CAPTURE\"\n"), 0o700)
	write(capture, nil, 0o600)
	manifest += fmt.Sprintf("  pausing:\n    run: 'exit 1'\n    on_failure: [%q, %q]\n", pausePath, captureHook)
	write(filepath.Join(project, "noon.yaml"), []byte(manifest), 0o600)
	assertPaused := func(want bool) {
		t.Helper()
		cmd := exec.CommandContext(ctx, noonBinary, "list", "--json")
		cmd.Dir, cmd.Env = project, env
		output, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Jobs []struct {
				Name   string `json:"name"`
				Paused bool   `json:"paused"`
			} `json:"jobs"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatal(err)
		}
		for _, job := range result.Jobs {
			if job.Name == "pausing" {
				if job.Paused != want {
					t.Fatalf("paused=%v want=%v: %s", job.Paused, want, output)
				}
				return
			}
		}
		t.Fatalf("pausing job missing: %s", output)
	}
	// Invalid thresholds must fail before attempting to pause or chaining.
	for _, threshold := range []string{"0", "-1", "bad", ""} {
		cmd := exec.CommandContext(ctx, pausePath, captureHook)
		cmd.Dir = project
		cmd.Env = append(append([]string{}, env...), "NOON_PAUSE_AFTER="+threshold,
			"NOON_CONSECUTIVE_FAILURES=9", "NOON_PROJECT="+project, "NOON_JOB=pausing")
		if output, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("invalid threshold %q accepted: %s", threshold, output)
		}
		assertPaused(false)
		assertCapture("")
	}
	for i := 1; i <= 2; i++ {
		run("pausing", 1)
		assertPaused(i == 2)
		data, err := os.ReadFile(capture)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != i {
			t.Fatalf("capture calls=%d want=%d: %s", len(lines), i, data)
		}
		var payload struct {
			Failures int `json:"consecutive_failures"`
		}
		if err := json.Unmarshal([]byte(lines[i-1]), &payload); err != nil || payload.Failures != i {
			t.Fatalf("chained stdin=%s err=%v", lines[i-1], err)
		}
	}
}
