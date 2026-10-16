package integration

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		case "user-config.yaml", "jobs.yaml":
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
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	for source, target := range map[string]string{"noon.yaml": "noon.yaml", "dot-noon.yaml": ".noon.yaml"} {
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
	if output := run("test-session", false, 2, missingJQEnv); !strings.Contains(output, "requires jq; install jq") {
		t.Fatalf("missing actionable jq error: %s", output)
	}
	if output := run("../invalid", false, 2, env); !strings.Contains(output, "valid session_id") {
		t.Fatalf("unsafe session ID accepted: %s", output)
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
}
