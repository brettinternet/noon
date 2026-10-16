package runner_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/bump/internal/app"
	"github.com/brettinternet/bump/internal/config"
	"github.com/brettinternet/bump/internal/runner"
)

func TestRunner(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "BUMP_STATE_DIR"} {
		t.Setenv(key, root)
	}
	base := func(argv ...string) runner.Spec {
		return runner.Spec{Argv: argv, Dir: root, Env: []string{"PATH=/bin:/usr/bin"}, Timeout: 2 * time.Second, KillGrace: 40 * time.Millisecond}
	}
	run := func(t *testing.T, spec runner.Spec) (runner.Result, string) {
		t.Helper()
		var out bytes.Buffer
		spec.Stdout = &out
		result := runner.Run(context.Background(), spec)
		if result.FinishedAt.Before(result.StartedAt) || result.StartedAt.IsZero() {
			t.Fatalf("invalid times: %+v", result)
		}
		return result, out.String()
	}
	t.Run("exact argv", func(t *testing.T) {
		result, out := run(t, base("/bin/sh", "-c", `printf '%s\n' "$@"`, "argv0", "with spaces", `a"quote`, "$HOME"))
		if result.ExitCode != 0 || out != "with spaces\na\"quote\n$HOME\n" {
			t.Fatalf("%+v %q", result, out)
		}
	})
	t.Run("shell cwd environment precedence and metadata", func(t *testing.T) {
		project, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		job := config.Job{Name: "tests", Run: `printf '%s\n' "$VALUE" "$BASE" "$BUMP_PROJECT" "$BUMP_JOB" "$BUMP_TRIGGER"; pwd`, CWD: project, Env: map[string]string{"VALUE": "job"}, Timeout: "none"}
		spec, err := app.RunSpec(project, job, []string{"PATH=/bin:/usr/bin", "VALUE=inherited", "BASE=kept", "BUMP_PROJECT=bad", "BUMP_JOB=bad", "BUMP_TRIGGER=bad"})
		if err != nil {
			t.Fatal(err)
		}
		spec.KillGrace = 40 * time.Millisecond
		result, out := run(t, spec)
		if result.ExitCode != 0 || out != "job\nkept\n"+project+"\ntests\nmanual\n"+project+"\n" {
			t.Fatalf("%+v %q", result, out)
		}
	})
	t.Run("dev null stdin", func(t *testing.T) {
		result, out := run(t, base("/bin/cat"))
		if result.ExitCode != 0 || out != "" {
			t.Fatalf("%+v %q", result, out)
		}
	})
	t.Run("supplied stdin", func(t *testing.T) {
		spec := base("/bin/cat")
		spec.Stdin = strings.NewReader("input")
		result, out := run(t, spec)
		if result.ExitCode != 0 || out != "input" {
			t.Fatalf("%+v %q", result, out)
		}
	})
	t.Run("job PATH and relative executable", func(t *testing.T) {
		if err := os.Mkdir(filepath.Join(root, "tools"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "tools", "custom"), []byte("#!/bin/sh\nprintf resolved"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"custom", "./tools/custom"} {
			spec := base(name)
			spec.Env = []string{"PATH=tools"}
			result, out := run(t, spec)
			if result.ExitCode != 0 || out != "resolved" {
				t.Fatalf("%+v %q", result, out)
			}
		}
		spec := base("sh")
		spec.Env = []string{"PATH=tools"}
		result, _ := run(t, spec)
		if result.ExitCode != 127 {
			t.Fatalf("inherited PATH leaked: %+v", result)
		}
	})
	t.Run("exit mappings", func(t *testing.T) {
		denied := filepath.Join(root, "denied")
		if err := os.WriteFile(denied, []byte("echo no"), 0600); err != nil {
			t.Fatal(err)
		}
		malformed := filepath.Join(root, "malformed")
		if err := os.WriteFile(malformed, []byte("not an executable"), 0700); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name   string
			spec   runner.Spec
			code   int
			signal string
			start  bool
		}{
			{"success", base("/bin/sh", "-c", "exit 0"), 0, "", false},
			{"child failure", base("/bin/sh", "-c", "exit 23"), 23, "", false},
			{"signal", base("/bin/sh", "-c", "kill -TERM $$"), 143, "SIGTERM", false},
			{"missing", base("missing-command"), 127, "", true},
			{"permission", base(denied), 126, "", true},
			{"format", base(malformed), 126, "", true},
			{"directory", base(root), 126, "", true},
			{"missing cwd", func() runner.Spec { s := base("/bin/sh"); s.Dir = filepath.Join(root, "absent"); return s }(), 125, "", true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				result, _ := run(t, tc.spec)
				if result.ExitCode != tc.code || result.Signal != tc.signal || (result.StartError != nil) != tc.start {
					t.Fatalf("%+v", result)
				}
				if tc.start && (result.PID != 0 || result.PGID != 0) {
					t.Fatalf("started on failure: %+v", result)
				}
			})
		}
	})
	for _, mode := range []string{"timeout", "cancel", "second signal", "leader exits with open grandchild pipe"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			script := `sh -c 'echo $$ > child; sleep 30 & echo $! > grandchild; wait' &
while [ ! -f grandchild ]; do :; done
`
			if mode == "leader exits with open grandchild pipe" {
				script += "exit 0"
			} else {
				script += "sleep 30 & echo $! > sibling; wait"
			}
			if mode == "second signal" {
				script = "trap '' TERM\n" + script
			}
			spec := base("/bin/sh", "-c", script)
			spec.Dir = dir
			spec.Timeout = 200 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			force := make(chan struct{})
			spec.ForceKill = force
			if mode == "second signal" {
				spec.KillGrace = 5 * time.Second
			}
			done := make(chan runner.Result, 1)
			go func() { done <- runner.Run(ctx, spec) }()
			grandchild := awaitPID(t, filepath.Join(dir, "grandchild"))
			child := awaitPID(t, filepath.Join(dir, "child"))
			if mode == "cancel" || mode == "second signal" {
				cancel()
			}
			if mode == "second signal" {
				close(force)
			}
			var result runner.Result
			select {
			case result = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("runner did not finish")
			}
			t.Cleanup(func() { _ = syscall.Kill(-result.PGID, syscall.SIGKILL) })
			switch mode {
			case "timeout":
				if result.ExitCode != 124 || !result.TimedOut {
					t.Fatalf("%+v", result)
				}
			case "cancel", "second signal":
				if result.ExitCode != 130 || !result.Cancelled {
					t.Fatalf("%+v", result)
				}
			default:
				if result.ExitCode != 0 {
					t.Fatalf("%+v", result)
				}
			}
			for _, pid := range []int{result.PID, child, grandchild} {
				awaitGone(t, pid)
			}
			if data, err := os.ReadFile(filepath.Join(dir, "sibling")); err == nil {
				pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
				awaitGone(t, pid)
			}
		})
	}
	t.Run("pre cancelled does not start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result := runner.Run(ctx, base("/bin/sh", "-c", "exit 0"))
		if result.ExitCode != 130 || result.PID != 0 {
			t.Fatalf("%+v", result)
		}
	})
}

func awaitPID(t *testing.T, path string) int {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if data, err := os.ReadFile(path); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid
			}
		}
		select {
		case <-timer.C:
			t.Fatalf("no PID in %s", path)
		case <-tick.C:
		}
	}
}
func awaitGone(t *testing.T, pid int) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		// Orphan zombies on container PID 1 are dead, even though kill(0) succeeds.
		output, err := exec.Command("/bin/ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if err != nil || strings.TrimSpace(string(output)) == "" || strings.HasPrefix(strings.TrimSpace(string(output)), "Z") {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("PID %d survived: %s", pid, output)
		case <-tick.C:
		}
	}
}
