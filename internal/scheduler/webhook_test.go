package scheduler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

func TestWebhookSecretSnapshotAndDiagnostics(t *testing.T) {
	fixture := newServeFixture(t, "jobs:\n  hook:\n    run: 'true'\n    webhook:\n      secret_env: HOOK_SECRET\n")
	defer fixture.stop(t)
	waitFor(t, func() bool { return fixture.configError(t) != "" })
	diagnostic := fixture.configError(t)
	if !strings.Contains(diagnostic, `job "hook" webhook secret environment variable "HOOK_SECRET" is missing or empty`) {
		t.Fatalf("missing secret diagnostic = %q", diagnostic)
	}
	input := config.Input{CWD: fixture.project, UserConfigDir: fixture.userConfig, StateDirDefault: fixture.stateDir}
	dashboard, err := app.Dashboard(input, serveTestStart)
	if err != nil || !strings.Contains(dashboard.Project.ConfigError, "HOOK_SECRET") {
		t.Fatalf("dashboard webhook diagnostic = %+v err=%v", dashboard.Project, err)
	}
	doctor := app.Doctor(context.Background(), input, serveTestStart)
	found := false
	for _, check := range doctor.Checks {
		if check.Name == "scheduler_config" && check.Status == "fail" && strings.Contains(check.Message, "HOOK_SECRET") {
			found = true
		}
	}
	if !found {
		t.Fatalf("doctor did not report the scheduler webhook config error: %+v", doctor.Checks)
	}
	fixture.stop(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.environment = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + fixture.root, "HOOK_SECRET=initial-secret"}
	fixture.listenAddr = address
	fixture.stateDirDefault = filepath.Join(fixture.root, "unused-default-state")
	fixture.start(t)
	fixture.step(t, serveTestStart.Add(time.Second))
	waitFor(t, func() bool {
		response, err := http.Get("http://" + address + "/healthz")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == http.StatusOK
	})

	// A running scheduler must keep the startup value even when the supplied
	// environment slice is changed after Serve has taken its snapshot.
	fixture.environment[2] = "HOOK_SECRET=changed-secret"
	body := []byte(`{"action":"opened"}`)
	status := webhookStatus(t, address, fixture.projectID(t), body, []byte("initial-secret"), "snapshot-001")
	if status != http.StatusAccepted {
		t.Fatalf("startup-secret signature status = %d, want 202", status)
	}
	status = webhookStatus(t, address, fixture.projectID(t), body, []byte("changed-secret"), "snapshot-002")
	if status != http.StatusUnauthorized {
		t.Fatalf("changed environment signature status = %d, want 401", status)
	}
	state, err := store.OpenExisting(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := state.RunsForJob(fixture.projectID(t), "hook")
	_ = state.Close()
	if err != nil || len(runs) != 1 {
		t.Fatalf("snapshot requests queued %+v err=%v", runs, err)
	}
	if _, err := os.Stat(fixture.stateDirDefault); !os.IsNotExist(err) {
		t.Fatalf("webhook touched default state directory: %v", err)
	}
}

func webhookStatus(t *testing.T, address string, projectID int64, body, secret []byte, delivery string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, "http://"+address+"/hooks/"+strconv.FormatInt(projectID, 10)+"/hook", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-Noon-Delivery", delivery)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	request.Header.Set("X-Noon-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}
