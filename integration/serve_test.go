package integration

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/store"
)

var noonBinary string

func TestMain(m *testing.M) {
	buildDir, err := os.MkdirTemp("", "noon-integration-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	noonBinary = filepath.Join(buildDir, "noon")
	command := exec.Command("go", "build", "-o", noonBinary, "../cmd/noon")
	output, err := command.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build noon integration binary: %v\n%s", err, output)
		_ = os.RemoveAll(buildDir)
		os.Exit(1)
	}
	code := m.Run()
	if err := os.RemoveAll(buildDir); err != nil {
		fmt.Fprintf(os.Stderr, "remove integration binary directory: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}

func TestServeListen(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	stateHome := filepath.Join(root, "xdg-state")
	stateDir := filepath.Join(root, "state")
	for _, path := range []string{home, configHome, stateHome} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	environment := isolatedEnvironment(home, configHome, stateHome, stateDir)
	command := exec.Command(noonBinary, "serve", "--listen", "127.0.0.1:0")
	command.Dir, command.Env = home, environment
	var stderr lockedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	address := waitForListenerAddress(t, done, &stderr)
	client := http.Client{Timeout: 500 * time.Millisecond}
	waitForHTTPHealth(t, &client, address, done, &stderr)
	response, err := client.Get("http://" + address + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	var health struct {
		Status string `json:"status"`
	}
	decodeErr := json.NewDecoder(response.Body).Decode(&health)
	_ = response.Body.Close()
	if decodeErr != nil || response.StatusCode != http.StatusOK || health.Status != "ok" {
		t.Fatalf("health response: status=%d body=%+v err=%v", response.StatusCode, health, decodeErr)
	}
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("listen serve exited with %v; stderr=%s", err, stderr.String())
		}
	case <-time.After(15 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("serve with listener did not stop within 15s")
	}
	if connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond); err == nil {
		_ = connection.Close()
		t.Fatal("webhook listener remained open after serve shutdown")
	}

	// A second instance without --listen runs the scheduler but emits no listener
	// address and has no HTTP endpoint.
	withoutListen := filepath.Join(root, "without-listen")
	if err := os.MkdirAll(withoutListen, 0o700); err != nil {
		t.Fatal(err)
	}
	defaultState := filepath.Join(withoutListen, "state")
	defaultEnvironment := isolatedEnvironment(filepath.Join(withoutListen, "home"), filepath.Join(withoutListen, "config"), filepath.Join(withoutListen, "xdg-state"), defaultState)
	for _, path := range []string{filepath.Join(withoutListen, "home"), filepath.Join(withoutListen, "config"), filepath.Join(withoutListen, "xdg-state")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	defaultCommand := exec.Command(noonBinary, "serve")
	defaultCommand.Dir, defaultCommand.Env = filepath.Join(withoutListen, "home"), defaultEnvironment
	var defaultStderr lockedBuffer
	defaultCommand.Stderr = &defaultStderr
	if err := defaultCommand.Start(); err != nil {
		t.Fatal(err)
	}
	defaultDone := make(chan error, 1)
	go func() { defaultDone <- defaultCommand.Wait() }()
	waitForScheduler(t, defaultState, defaultDone, &defaultStderr)
	if strings.Contains(defaultStderr.String(), "webhook listener") {
		t.Fatalf("serve without --listen started a listener: %s", defaultStderr.String())
	}
	if err := defaultCommand.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-defaultDone:
		if err != nil {
			t.Fatalf("serve without listener exited with %v; stderr=%s", err, defaultStderr.String())
		}
	case <-time.After(15 * time.Second):
		_ = defaultCommand.Process.Kill()
		t.Fatal("serve without listener did not stop within 15s")
	}
}

func waitForListenerAddress(t *testing.T, done <-chan error, stderr *lockedBuffer) string {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		for _, line := range strings.Split(stderr.String(), "\n") {
			if _, address, ok := strings.Cut(line, "listener listening address="); ok {
				address = strings.TrimSpace(address)
				if _, _, err := net.SplitHostPort(address); err == nil {
					return address
				}
			}
		}
		select {
		case err := <-done:
			t.Fatalf("serve exited before listener started: %v; stderr=%s", err, stderr.String())
		case <-deadline.C:
			t.Fatalf("timed out waiting for listener address; stderr=%s", stderr.String())
		case <-ticker.C:
		}
	}
}

func waitForHTTPHealth(t *testing.T, client *http.Client, address string, done <-chan error, stderr *lockedBuffer) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, err := client.Get("http://" + address + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case err := <-done:
			t.Fatalf("serve exited before health became ready: %v; stderr=%s", err, stderr.String())
		case <-deadline.C:
			t.Fatalf("timed out waiting for /healthz; stderr=%s", stderr.String())
		case <-ticker.C:
		}
	}
}

func TestServeProcess(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	stateHome := filepath.Join(root, "xdg-state")
	stateDir := filepath.Join(root, "state")
	for _, path := range []string{home, configHome, stateHome} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	environment := isolatedEnvironment(home, configHome, stateHome, stateDir)
	first := exec.Command(noonBinary, "serve")
	first.Dir, first.Env = home, environment
	var firstStderr lockedBuffer
	first.Stderr = &firstStderr
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() { firstDone <- first.Wait() }()
	waitForScheduler(t, stateDir, firstDone, &firstStderr)

	second := exec.Command(noonBinary, "serve")
	second.Dir, second.Env = home, environment
	output, err := second.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(output), "scheduler_running") || !strings.Contains(string(output), "PID ") || !strings.Contains(string(output), "started at ") {
		t.Fatalf("second serve: err=%v output=%s", err, output)
	}
	if err := first.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatalf("first serve exited with %v; stderr=%s", err, firstStderr.String())
		}
	case <-time.After(15 * time.Second):
		_ = first.Process.Kill()
		t.Fatal("SIGTERM did not stop noon serve within 15s")
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Builder.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Builder.String()
}

func isolatedEnvironment(home, configHome, stateHome, stateDir string) []string {
	result := make([]string, 0)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "HOME" || key == "XDG_CONFIG_HOME" || key == "XDG_STATE_HOME" || strings.HasPrefix(key, "NOON_") {
			continue
		}
		result = append(result, entry)
	}
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/bin:/bin"
	}
	result = append(result,
		"PATH="+path,
		"HOME="+home,
		"XDG_CONFIG_HOME="+configHome,
		"XDG_STATE_HOME="+stateHome,
		"NOON_STATE_DIR="+stateDir,
	)
	return result
}

func waitForScheduler(t *testing.T, stateDir string, done <-chan error, stderr *lockedBuffer) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := store.OpenExisting(stateDir)
		if err == nil {
			record, exists, statusErr := state.SchedulerInfo()
			_ = state.Close()
			if statusErr == nil && exists && record.PID > 0 {
				return
			}
		}
		select {
		case err := <-done:
			t.Fatalf("serve exited before acquiring scheduler: %v; stderr=%s", err, stderr.String())
		case <-deadline.C:
			t.Fatalf("timed out waiting for scheduler; stderr=%s", stderr.String())
		case <-ticker.C:
		}
	}
}

func TestServeReloadsVarsFileChangesAndApproval(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(root, "project")
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "config")
	stateHome := filepath.Join(root, "xdg-state")
	stateDir := filepath.Join(root, "state")
	for _, path := range []string{project, home, configHome, stateHome} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	environment := isolatedEnvironment(home, configHome, stateHome, stateDir)
	manifest := filepath.Join(project, "noon.yaml")
	vars := filepath.Join(project, "vars.env")
	if err := os.WriteFile(manifest, []byte("vars_file: vars.env\njobs:\n  scheduled:\n    schedule: ${SCHEDULE:-every 1h}\n    missed: skip\n    run: [sh, -c, 'echo unexpected > ran']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, []byte) {
		t.Helper()
		command := exec.Command(noonBinary, args...)
		command.Dir, command.Env = project, environment
		output, err := command.CombinedOutput()
		if err == nil {
			return 0, output
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), output
		}
		t.Fatalf("noon %v: %v\n%s", args, err, output)
		return -1, nil
	}
	if code, output := run("enable"); code != 0 {
		t.Fatalf("enable: %d\n%s", code, output)
	}
	_, stop := startRemoteScheduler(t, noonBinary, project, environment, "--listen", "127.0.0.1:0")
	defer stop()
	waitForVarsScheduleKey(t, stateDir, project, "every 1h|Local")

	if err := os.WriteFile(vars, []byte("SCHEDULE=\"broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForProjectConfigError(t, stateDir, project, "vars.env")
	if err := os.WriteFile(vars, []byte("SCHEDULE=every 2h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForProjectConfigError(t, stateDir, project, "")
	code, output := run("list", "--json")
	var listed struct {
		Jobs []struct {
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
			Approval string `json:"approval"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(output, &listed); err != nil || code != 0 || len(listed.Jobs) != 1 || listed.Jobs[0].Schedule != "every 2h" || listed.Jobs[0].Approval != "needs_approval" {
		t.Fatalf("created vars_file change was not loaded or gated: code=%d jobs=%+v err=%v output=%s", code, listed.Jobs, err, output)
	}
	code, output = run("run", "scheduled", "--detach", "--json")
	var queued map[string]any
	if err := json.Unmarshal(output, &queued); err != nil || code != 1 || queued["error"].(map[string]any)["code"] != "needs_approval" {
		t.Fatalf("unapproved schedule queued after vars_file creation: code=%d result=%+v err=%v", code, queued, err)
	}

	if err := os.WriteFile(vars, []byte("SCHEDULE=\"broken again"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForProjectConfigError(t, stateDir, project, "vars.env")
	if err := os.WriteFile(vars, []byte("SCHEDULE=every 3h\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitForProjectConfigError(t, stateDir, project, "")
	if code, output := run("enable"); code != 0 {
		t.Fatalf("re-enable after vars_file edit: %d\n%s", code, output)
	}
	code, output = run("list", "--json")
	listed.Jobs = nil
	if err := json.Unmarshal(output, &listed); err != nil || code != 0 || len(listed.Jobs) != 1 || listed.Jobs[0].Schedule != "every 3h" || listed.Jobs[0].Approval != "approved" {
		t.Fatalf("re-enable did not approve the new schedule: code=%d jobs=%+v err=%v output=%s", code, listed.Jobs, err, output)
	}
	waitForVarsScheduleKey(t, stateDir, project, "every 3h|Local")
	if _, err := os.Stat(filepath.Join(project, "ran")); !os.IsNotExist(err) {
		t.Fatalf("scheduled job ran unexpectedly while approval was pending: %v", err)
	}
}

func waitForProjectConfigError(t *testing.T, stateDir, project, want string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := store.OpenExisting(stateDir)
		if err == nil {
			message, present, readErr := state.ProjectConfigError(project)
			_ = state.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if want == "" && (!present || message == "") || want != "" && strings.Contains(message, want) {
				return
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("scheduler config error did not match %q", want)
		case <-ticker.C:
		}
	}
}

func waitForVarsScheduleKey(t *testing.T, stateDir, project, want string) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		state, err := store.OpenExisting(stateDir)
		if err == nil {
			states, readErr := state.ScheduleStatesForProject(project)
			_ = state.Close()
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, saved := range states {
				if saved.Job == "scheduled" && saved.ScheduleKey == want {
					return
				}
			}
		}
		select {
		case <-deadline.C:
			t.Fatalf("scheduler did not persist schedule key %q", want)
		case <-ticker.C:
		}
	}
}
