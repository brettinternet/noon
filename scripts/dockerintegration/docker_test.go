//go:build dockerintegration

package dockerintegration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

// This is an explicit Docker acceptance test, not part of the ordinary Go suite.
// It catches lost history, incorrect signal propagation, and unreaped orphans.
func TestProductionContainer(t *testing.T) {
	run := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("docker %q: %v\n%s\n%s", args, err, out, stderr.String())
		}
		return strings.TrimSpace(string(out))
	}
	if run("info", "--format", "{{.OSType}}") != "linux" {
		t.Fatal("requires a Linux Docker engine")
	}
	run("compose", "version")
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	prefix := "noon-integration-" + hex.EncodeToString(nonce[:])
	t.Logf("fixture prefix: %s", prefix)
	// Keep the bind source in the checkout: desktop Docker engines may not
	// share the host's OS temporary directory with their Linux VM.
	fixture, err := os.MkdirTemp(".", prefix+"-")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err = filepath.Abs(fixture)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(fixture); err != nil {
			t.Error(err)
		}
	})
	// Docker runs as UID 10001, not the host test user's UID.
	if err := os.Chmod(fixture, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `version: 1
timezone: Asia/Kolkata
jobs:
  quick:
    run: [sh, -c, "echo container-ok"]
  orphan:
    run: [sh, -c, 'sh -c "sleep 1 </dev/null >/dev/null 2>&1 & echo \$! > /tmp/orphan.pid"']
  slow:
    run: [sh, -c, "echo ready > /tmp/slow-ready; exec sleep 300"]
  zoned:
    schedule: every day at 9am
    run: ["true"]
`
	if err := os.WriteFile(filepath.Join(fixture, "noon.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}
	composeFile, err := filepath.Abs("../../examples/compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	compose := func(args ...string) string {
		t.Helper()
		return run(append([]string{"compose", "--project-directory", fixture, "-f", composeFile, "-p", prefix}, args...)...)
	}
	t.Setenv("NOON_IMAGE", "noon:dev")
	// Register cleanup before creation, including partially failed Compose starts.
	t.Cleanup(func() {
		compose("down", "--volumes", "--remove-orphans", "--timeout", "15")
		for _, args := range [][]string{
			{"ps", "-a", "--filter", "name=" + prefix, "-q"},
			{"volume", "ls", "--filter", "name=" + prefix, "-q"},
			{"image", "ls", "--filter", "reference=" + prefix + "*", "-q"},
		} {
			if leftovers := run(args...); leftovers != "" {
				t.Errorf("fixture resources remain: %s", leftovers)
			}
		}
	})
	compose("up", "-d")
	container := compose("ps", "-q", "noon")
	if container == "" {
		t.Fatal("Compose did not start noon")
	}
	wait := func(description string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			if ready() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out: %s\n%s", description, run("logs", container))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	schedulerReady := func() bool {
		return strings.Contains(run("exec", container, "ps", "-o", "args"), "noon serve")
	}
	wait("foreground scheduler", schedulerReady)
	if uid := run("exec", container, "id", "-u"); uid != "10001" {
		t.Fatalf("UID = %s", uid)
	}
	if ownership := run("exec", container, "stat", "-c", "%u", "/state"); ownership != "10001" {
		t.Fatalf("new volume owner = %s", ownership)
	}
	type runRecord struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
	}
	history := func(job string) []runRecord {
		t.Helper()
		var result struct {
			Runs []runRecord `json:"runs"`
		}
		out := run("exec", container, "noon", "history", job, "--json")
		if err := json.Unmarshal([]byte(out), &result); err != nil {
			t.Fatal(err)
		}
		return result.Runs
	}
	status := func(job, want string) bool {
		runs := history(job)
		return len(runs) == 1 && runs[0].Status == want
	}
	run("exec", container, "noon", "run", "quick", "--detach")
	wait("queued run succeeds", func() bool { return status("quick", "succeeded") })
	quickID := history("quick")[0].ID
	run("exec", container, "noon", "run", "orphan", "--detach")
	wait("orphan job succeeds", func() bool { return status("orphan", "succeeded") })
	orphanPID := run("exec", container, "cat", "/tmp/orphan.pid")
	wait("orphan exits and is reaped", func() bool {
		for _, pid := range strings.Fields(run("exec", container, "ps", "-o", "pid")) {
			if pid == orphanPID {
				return false
			}
		}
		return true
	})
	for _, state := range strings.Fields(run("exec", container, "ps", "-o", "stat")) {
		if strings.HasPrefix(state, "Z") {
			t.Fatalf("zombie state: %s", state)
		}
	}
	run("exec", container, "noon", "run", "slow", "--detach")
	wait("slow run starts", func() bool { return status("slow", "running") })
	// A running record precedes process start; wait for the child's own marker.
	wait("slow child starts", func() bool {
		return run("exec", container, "sh", "-c", "test ! -f /tmp/slow-ready || echo ready") == "ready"
	})
	slowID := history("slow")[0].ID
	started := time.Now()
	run("stop", "--time", "15", container)
	if elapsed := time.Since(started); elapsed >= 15*time.Second {
		t.Fatalf("stop took %s, want under 15s", elapsed)
	}
	if code := run("inspect", "--format", "{{.State.ExitCode}}", container); code != "0" {
		t.Fatalf("scheduler did not shut down cleanly: exit %s", code)
	}
	run("start", container)
	wait("restarted scheduler", schedulerReady)
	if !status("slow", "interrupted") || history("slow")[0].ID != slowID {
		t.Fatal("stopped run was not preserved as interrupted")
	}
	compose("up", "-d", "--force-recreate")
	replacement := compose("ps", "-q", "noon")
	if replacement == container {
		t.Fatal("container was not recreated")
	}
	container = replacement
	wait("recreated scheduler", schedulerReady)
	if !status("quick", "succeeded") || history("quick")[0].ID != quickID ||
		!status("slow", "interrupted") || history("slow")[0].ID != slowID {
		t.Fatal("history was lost or changed after recreation")
	}

	// No network and no system zoneinfo: the binary must use embedded IANA data.
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	next := func(now time.Time) time.Time {
		now = now.In(loc)
		at := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, loc)
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
		return at
	}
	offlineName := prefix + "-offline"
	// Killing an attached Docker client does not stop its container. Keep this
	// separate from Compose cleanup so a timed-out preview cannot leak it.
	t.Cleanup(func() {
		if run("ps", "-a", "--filter", "name=^/"+offlineName+"$", "-q") != "" {
			run("container", "remove", "--force", offlineName)
		}
	})
	before := next(time.Now())
	out := run("run", "--rm", "--name", offlineName, "--network", "none", "--read-only",
		"--mount", fmt.Sprintf("type=bind,src=%s,dst=/workspace,readonly", fixture),
		"noon:dev", "list", "--json")
	after := next(time.Now())
	var list struct {
		Jobs []struct {
			Name   string
			NextAt time.Time `json:"next_at"`
		}
	}
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, job := range list.Jobs {
		if job.Name == "zoned" {
			found = true
			if !job.NextAt.Equal(before) && !job.NextAt.Equal(after) {
				t.Fatalf("offline next time = %s, want %s or %s", job.NextAt, before, after)
			}
		}
	}
	if !found {
		t.Fatal("missing timezone job")
	}
	t.Log("Compose dispatch, reaping, graceful interruption, persistent history, and offline timezone passed")
}
