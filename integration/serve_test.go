package integration

import (
	"errors"
	"fmt"
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
