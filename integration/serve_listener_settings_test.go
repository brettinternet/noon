package integration

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The unchanged service unit runs `noon serve` with no listener flags, so the
// listener must come from user config, and only from user config.
func TestServeListenerSettings(t *testing.T) {
	root := t.TempDir()
	home, configHome, stateHome, stateDir := remoteDirectories(t, root)
	environment := isolatedEnvironment(home, configHome, stateHome, stateDir)
	userFile := filepath.Join(configHome, "noon", "config.yaml")
	token := strings.Repeat("t", 32)
	project := filepath.Join(root, "repo")
	for _, dir := range []string{filepath.Dir(userFile), project} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeRemoteFile(t, filepath.Join(configHome, "noon", "api-token"), token+"\n", 0o600)

	unit := func() string {
		t.Helper()
		code, output := runRemoteCommand(noonBinary, home, environment, "service", "install", "--print")
		if code != 0 {
			t.Fatalf("service install --print exit=%d output=%s", code, output)
		}
		return output
	}
	withoutSetting := unit()

	writeRemoteFile(t, userFile, "serve:\n  listen: 127.0.0.1:0\n  api_token_file: api-token\n", 0o600)
	if withSetting := unit(); withSetting != withoutSetting {
		t.Fatalf("service unit changed with listener settings:\n%s\n---\n%s", withoutSetting, withSetting)
	}

	address, stop := startRemoteScheduler(t, noonBinary, home, environment)
	client := &http.Client{Timeout: time.Second}
	response, body := remoteRequest(t, client, http.MethodGet, "http://"+address+"/v1/projects", token, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated projects status=%d body=%s", response.StatusCode, body)
	}
	if response, _ := remoteRequest(t, client, http.MethodGet, "http://"+address+"/v1/projects", "", nil); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated projects status=%d", response.StatusCode)
	}
	if output := stop(); strings.Contains(output, token) {
		t.Fatalf("serve output contains the token: %s", output)
	}

	// The flag wins over the setting; 127.0.0.2 would fail or differ if the setting were used.
	writeRemoteFile(t, userFile, "serve:\n  listen: 127.0.0.2:0\n  api_token_file: api-token\n", 0o600)
	address, stop = startRemoteScheduler(t, noonBinary, home, environment, "--listen", "127.0.0.1:0")
	if host, _, _ := net.SplitHostPort(address); host != "127.0.0.1" {
		t.Fatalf("--listen did not override serve.listen: %s", address)
	}
	stop()

	writeRemoteFile(t, userFile, "serve:\n  api_token_file: api-token\n", 0o600)
	if code, output := runRemoteCommand(noonBinary, home, environment, "serve"); code != 2 {
		t.Fatalf("token file without listen address exit=%d output=%s", code, output)
	}

	// Repository content can never configure the listener.
	if err := os.Remove(userFile); err != nil {
		t.Fatal(err)
	}
	writeRemoteFile(t, filepath.Join(project, "noon.yaml"), "serve:\n  listen: 127.0.0.1:0\n", 0o600)
	if code, output := runRemoteCommand(noonBinary, project, environment, "config", "check"); code != 1 || !strings.Contains(output, userFile) {
		t.Fatalf("repository serve block: config check exit=%d output=%s", code, output)
	}
	// A fresh state directory makes the heartbeat wait observe this process.
	repoState := filepath.Join(root, "repo-state")
	command := exec.Command(noonBinary, "serve")
	command.Dir, command.Env = project, isolatedEnvironment(home, configHome, stateHome, repoState)
	var stderr lockedBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	waitForScheduler(t, repoState, done, &stderr)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("serve shutdown: %v; stderr=%s", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "listener listening") {
		t.Fatalf("repository config opened a listener: %s", stderr.String())
	}
}
