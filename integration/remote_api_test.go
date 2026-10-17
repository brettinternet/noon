package integration

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/store"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRemoteAPI(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, configHome, stateHome, stateDir := remoteDirectories(t, root)
	environment := isolatedEnvironment(home, configHome, stateHome, stateDir)
	const token = "noon-remote-api-token-0123456789-abcdef-0123456789"
	tokenFile := filepath.Join(root, "api-token")
	writeRemoteFile(t, tokenFile, token+"\n", 0o600)

	// Flag and token-file failures must happen before scheduler state or heartbeat creation.
	if code, output := runRemoteCommand(noonBinary, home, environment, "serve", "--api-token-file", tokenFile); code != 2 {
		t.Fatalf("--api-token-file without --listen exit=%d output=%s", code, output)
	}
	for _, test := range []struct {
		name string
		file string
		data string
	}{
		{name: "missing", file: filepath.Join(root, "missing-token")},
		{name: "short", file: filepath.Join(root, "short-token"), data: strings.Repeat("x", 31)},
	} {
		t.Run("token startup "+test.name, func(t *testing.T) {
			if test.data != "" {
				writeRemoteFile(t, test.file, test.data, 0o600)
			}
			isolatedRoot := filepath.Join(root, test.name)
			isolatedHome, isolatedConfig, isolatedStateHome, isolatedState := remoteDirectories(t, isolatedRoot)
			isolatedEnv := isolatedEnvironment(isolatedHome, isolatedConfig, isolatedStateHome, isolatedState)
			code, output := runRemoteCommand(noonBinary, isolatedHome, isolatedEnv,
				"serve", "--listen", "127.0.0.1:0", "--api-token-file", test.file)
			if code != 1 || strings.Contains(output, token) {
				t.Fatalf("invalid token startup exit=%d output=%s", code, output)
			}
			assertNoSchedulerHeartbeat(t, isolatedState)
		})
	}

	enabledProject := filepath.Join(root, "enabled")
	disabledProject := filepath.Join(root, "disabled")
	for _, project := range []string{enabledProject, disabledProject} {
		if err := os.MkdirAll(project, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeRemoteFile(t, filepath.Join(enabledProject, "noon.yaml"), `version: 1
jobs:
  hello:
    run: [sh, -c, 'printf hello-log; cat "$NOON_EVENT_FILE"']
  env:
    run: [env]
  slow:
    run: [sh, -c, 'sleep 11; printf slow-finished']
  cancel:
    run: "true"
  webhook:
    run: "true"
    webhook:
      secret_env: REMOTE_WEBHOOK_SECRET
`, 0o600)
	writeRemoteFile(t, filepath.Join(disabledProject, "noon.yaml"), `version: 1
jobs:
  hello:
    run: "true"
`, 0o600)
	environment = append(environment, "REMOTE_WEBHOOK_SECRET=remote-webhook-secret")
	for _, project := range []string{enabledProject, disabledProject} {
		if code, output := runRemoteCommand(noonBinary, project, environment, "enable"); code != 0 {
			t.Fatalf("enable %s exit=%d output=%s", project, code, output)
		}
	}
	if code, output := runRemoteCommand(noonBinary, disabledProject, environment, "disable"); code != 0 {
		t.Fatalf("disable fixture project exit=%d output=%s", code, output)
	}

	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	records, err := state.ProjectRecords()
	if err != nil {
		_ = state.Close()
		t.Fatal(err)
	}
	var enabledID, disabledID int64
	for _, record := range records {
		switch record.Path {
		case enabledProject:
			enabledID = record.ID
		case disabledProject:
			disabledID = record.ID
		}
	}
	if enabledID == 0 || disabledID == 0 {
		_ = state.Close()
		t.Fatalf("fixture projects not registered: %+v", records)
	}
	disabledRun, err := state.CreateRun(store.NewRun{ProjectID: disabledID, Job: "hello", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: os.Getpid(), OwnerStart: "remote-api-test"})
	if err != nil {
		_ = state.Close()
		t.Fatal(err)
	}
	if err := state.FinishRun(disabledRun.ID, disabledID, "hello", store.Outcome{Status: "succeeded", FinishedAt: time.Now()}); err != nil {
		_ = state.Close()
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}

	// With no API token, the listener remains webhooks and health only.
	address, stop := startRemoteScheduler(t, noonBinary, home, environment, "--listen", "127.0.0.1:0")
	for _, path := range []string{"/v1/projects", "/mcp"} {
		response, body := remoteRequest(t, http.DefaultClient, http.MethodGet, "http://"+address+path, "", nil)
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("no-token %s status=%d body=%s", path, response.StatusCode, body)
		}
	}
	response, body := remoteRequest(t, http.DefaultClient, http.MethodGet, "http://"+address+"/healthz", "", nil)
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"status":"ok"`) {
		t.Fatalf("no-token health status=%d body=%s", response.StatusCode, body)
	}
	webhookBody := []byte(`{"action":"opened"}`)
	webhookRequest, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s/hooks/%d/webhook", address, enabledID), bytes.NewReader(webhookBody))
	if err != nil {
		t.Fatal(err)
	}
	webhookRequest.Header.Set("X-Noon-Signature-256", webhookSignature("remote-webhook-secret", webhookBody))
	webhookResponse, err := http.DefaultClient.Do(webhookRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = webhookResponse.Body.Close()
	if webhookResponse.StatusCode != http.StatusAccepted {
		t.Fatalf("signed webhook without API token status=%d", webhookResponse.StatusCode)
	}
	stop()

	address, stop = startRemoteScheduler(t, noonBinary, home, environment, "--listen", "127.0.0.1:0", "--api-token-file", tokenFile)
	defer func() { _ = stop() }()
	baseURL := "http://" + address
	client := &http.Client{Timeout: 40 * time.Second}

	// Every API route and /mcp authenticate before path, method, or resource routing.
	unauthorized := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/v1/projects", ""},
		{http.MethodGet, fmt.Sprintf("/v1/projects/%d", enabledID), ""},
		{http.MethodGet, fmt.Sprintf("/v1/projects/%d/jobs", enabledID), ""},
		{http.MethodGet, fmt.Sprintf("/v1/projects/%d/runs", enabledID), ""},
		{http.MethodPost, fmt.Sprintf("/v1/projects/%d/jobs/hello/runs", enabledID), `{"data":{"unauthorized":true}}`},
		{http.MethodGet, "/v1/runs/99999999?wait_seconds=1", ""},
		{http.MethodPost, "/v1/runs/99999999/cancel", ""},
		{http.MethodGet, "/v1/not-a-route", ""},
		{http.MethodGet, "/mcp", ""},
		{http.MethodPost, "/mcp", `{}`},
	}
	for _, test := range unauthorized {
		for _, suppliedToken := range []string{"", "wrong-token"} {
			response, body := remoteRequest(t, client, test.method, baseURL+test.path, suppliedToken, []byte(test.body))
			if response.StatusCode != http.StatusUnauthorized || response.Header.Get("WWW-Authenticate") != "Bearer" || len(body) != 0 {
				t.Fatalf("unauthorized %s %s token=%q: status=%d headers=%v body=%q", test.method, test.path, suppliedToken, response.StatusCode, response.Header, body)
			}
		}
	}

	response, body = remoteRequest(t, client, http.MethodGet, baseURL+"/v1/projects", token, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/projects status=%d body=%s", response.StatusCode, body)
	}
	var projectList struct {
		SchemaVersion int `json:"schema_version"`
		Projects      []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(body, &projectList); err != nil || projectList.SchemaVersion != 1 || len(projectList.Projects) != 1 || projectList.Projects[0].ID != enabledID || projectList.Projects[0].Path != enabledProject {
		t.Fatalf("enabled project listing = %+v err=%v body=%s", projectList, err, body)
	}
	for _, path := range []string{
		fmt.Sprintf("/v1/projects/%d", enabledID),
		fmt.Sprintf("/v1/projects/%d/jobs", enabledID),
		fmt.Sprintf("/v1/projects/%d/runs?job=hello&limit=5", enabledID),
	} {
		response, body = remoteRequest(t, client, http.MethodGet, baseURL+path, token, nil)
		var result map[string]any
		if err := json.Unmarshal(body, &result); err != nil || response.StatusCode != http.StatusOK || result["schema_version"] != float64(1) {
			t.Fatalf("GET %s status=%d body=%s err=%v", path, response.StatusCode, body, err)
		}
	}

	response, body = remoteRequest(t, client, http.MethodPost, fmt.Sprintf("%s/v1/projects/%d/jobs/hello/runs", baseURL, enabledID), token, []byte(`{"key":"api-hello","data":{"source":"remote"}}`))
	var queued struct {
		Run struct {
			ID int64 `json:"id"`
		} `json:"run"`
		Created bool `json:"created"`
	}
	if err := json.Unmarshal(body, &queued); err != nil || response.StatusCode != http.StatusAccepted || !queued.Created || queued.Run.ID == 0 {
		t.Fatalf("queue hello status=%d result=%+v body=%s err=%v", response.StatusCode, queued, body, err)
	}
	helloURL := fmt.Sprintf("%s/v1/runs/%d?wait_seconds=30", baseURL, queued.Run.ID)
	response, body = remoteRequest(t, client, http.MethodGet, helloURL, token, nil)
	var helloResult struct {
		Run struct {
			Status string `json:"status"`
		} `json:"run"`
		Log string `json:"log"`
	}
	if err := json.Unmarshal(body, &helloResult); err != nil || response.StatusCode != http.StatusOK || helloResult.Run.Status != "succeeded" || !strings.Contains(helloResult.Log, "hello-log") || !strings.Contains(helloResult.Log, `"source":"remote"`) {
		t.Fatalf("get hello status=%d result=%+v body=%s err=%v", response.StatusCode, helloResult, body, err)
	}
	response, body = remoteRequest(t, client, http.MethodGet, fmt.Sprintf("%s/v1/runs/%d?wait_seconds=61", baseURL, queued.Run.ID), token, nil)
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), `"code":"usage"`) {
		t.Fatalf("API accepted wait_seconds=61: status=%d body=%s", response.StatusCode, body)
	}
	response, body = remoteRequest(t, client, http.MethodPost, fmt.Sprintf("%s/v1/projects/%d/jobs/slow/runs", baseURL, enabledID), token, nil)
	var slowRun struct {
		Run struct {
			ID int64 `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(body, &slowRun); err != nil || response.StatusCode != http.StatusAccepted {
		t.Fatalf("queue slow run status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	slowStarted := time.Now()
	response, body = remoteRequest(t, client, http.MethodGet, fmt.Sprintf("%s/v1/runs/%d?wait_seconds=60", baseURL, slowRun.Run.ID), token, nil)
	if response.StatusCode != http.StatusOK || time.Since(slowStarted) < 10*time.Second || !strings.Contains(string(body), "slow-finished") {
		t.Fatalf("60-second API wait did not survive the 10-second server write timeout: elapsed=%s status=%d body=%s", time.Since(slowStarted), response.StatusCode, body)
	}

	response, body = remoteRequest(t, client, http.MethodPost, fmt.Sprintf("%s/v1/projects/%d/jobs/hello/runs", baseURL, enabledID), token, []byte(`{"in":"1h","key":"api-cancel"}`))
	var future struct {
		Run struct {
			ID int64 `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(body, &future); err != nil || response.StatusCode != http.StatusAccepted || future.Run.ID == 0 {
		t.Fatalf("queue future run status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	response, body = remoteRequest(t, client, http.MethodPost, fmt.Sprintf("%s/v1/runs/%d/cancel", baseURL, future.Run.ID), token, nil)
	var cancelResult struct {
		Runs []struct {
			Status string `json:"status"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(body, &cancelResult); err != nil || response.StatusCode != http.StatusOK || len(cancelResult.Runs) != 1 || cancelResult.Runs[0].Status != "cancelled" {
		t.Fatalf("cancel future run status=%d result=%+v body=%s err=%v", response.StatusCode, cancelResult, body, err)
	}

	response, body = remoteRequest(t, client, http.MethodPost, fmt.Sprintf("%s/v1/projects/%d/jobs/env/runs", baseURL, enabledID), token, nil)
	var environmentRun struct {
		Run struct {
			ID int64 `json:"id"`
		} `json:"run"`
	}
	if err := json.Unmarshal(body, &environmentRun); err != nil || response.StatusCode != http.StatusAccepted {
		t.Fatalf("queue env run status=%d body=%s err=%v", response.StatusCode, body, err)
	}
	response, body = remoteRequest(t, client, http.MethodGet, fmt.Sprintf("%s/v1/runs/%d?wait_seconds=30", baseURL, environmentRun.Run.ID), token, nil)
	if response.StatusCode != http.StatusOK || bytes.Contains(body, []byte(token)) {
		t.Fatalf("env run response status=%d contains token: %s", response.StatusCode, body)
	}
	var envResult struct {
		Run struct {
			Status string `json:"status"`
		} `json:"run"`
		Log string `json:"log"`
	}
	if err := json.Unmarshal(body, &envResult); err != nil || envResult.Run.Status != "succeeded" || strings.Contains(envResult.Log, token) {
		t.Fatalf("job environment exposed API token: status=%s log=%s err=%v", envResult.Run.Status, envResult.Log, err)
	}

	for _, path := range []string{
		fmt.Sprintf("/v1/projects/%d", disabledID),
		fmt.Sprintf("/v1/projects/%d/jobs", disabledID),
		fmt.Sprintf("/v1/projects/%d/runs", disabledID),
		"/v1/projects/99999999",
		"/v1/projects/99999999/jobs",
		fmt.Sprintf("/v1/runs/%d", disabledRun.ID),
		fmt.Sprintf("/v1/runs/%d/cancel", disabledRun.ID),
	} {
		method := http.MethodGet
		if strings.HasSuffix(path, "/cancel") {
			method = http.MethodPost
		}
		response, body = remoteRequest(t, client, method, baseURL+path, token, nil)
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("disabled or unknown path %s status=%d body=%s", path, response.StatusCode, body)
		}
	}
	response, _ = remoteRequest(t, client, http.MethodPost, fmt.Sprintf("%s/v1/projects/%d/jobs/hello/runs", baseURL, disabledID), token, nil)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("queue disabled project status=%d", response.StatusCode)
	}
	for _, test := range []struct {
		method string
		path   string
		allow  string
	}{
		{http.MethodPut, fmt.Sprintf("/v1/projects/%d/jobs", enabledID), http.MethodGet},
		{http.MethodDelete, fmt.Sprintf("/v1/projects/%d", enabledID), http.MethodGet},
	} {
		response, _ = remoteRequest(t, client, test.method, baseURL+test.path, token, nil)
		if response.StatusCode != http.StatusMethodNotAllowed || response.Header.Get("Allow") != test.allow {
			t.Fatalf("%s %s status=%d Allow=%q", test.method, test.path, response.StatusCode, response.Header.Get("Allow"))
		}
	}
	for _, path := range []string{
		fmt.Sprintf("/v1/projects/%d/jobs", enabledID),
		fmt.Sprintf("/v1/projects/%d/jobs/hello/enable", enabledID),
		fmt.Sprintf("/v1/projects/%d/jobs/hello/approve", enabledID),
		fmt.Sprintf("/v1/projects/%d/jobs/hello/pause", enabledID),
		fmt.Sprintf("/v1/projects/%d/disable", enabledID),
	} {
		method := http.MethodPost
		if strings.HasSuffix(path, "/jobs") {
			method = http.MethodPost
		}
		response, _ = remoteRequest(t, client, method, baseURL+path, token, nil)
		wantStatus := http.StatusNotFound
		if strings.HasSuffix(path, "/jobs") {
			wantStatus = http.StatusMethodNotAllowed
		}
		if response.StatusCode != wantStatus {
			t.Fatalf("admin route %s status=%d want=%d", path, response.StatusCode, wantStatus)
		}
	}

	// The SDK's Streamable HTTP client confirms the same tools and project boundary.
	authorizedHTTP := &http.Client{Timeout: 40 * time.Second, Transport: bearerRoundTripper{token: token, base: http.DefaultTransport}}
	mcpClient := protocol.NewClient(&protocol.Implementation{Name: "remote-client", Version: "1.0"}, nil)
	mcpSession, err := mcpClient.Connect(context.Background(), &protocol.StreamableClientTransport{
		Endpoint: baseURL + "/mcp", HTTPClient: authorizedHTTP, DisableStandaloneSSE: true, MaxRetries: -1,
	}, nil)
	if err != nil {
		t.Fatalf("connect Streamable HTTP MCP client: %v", err)
	}
	defer func() { _ = mcpSession.Close() }()
	tools, err := mcpSession.ListTools(context.Background(), &protocol.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	toolNames := make([]string, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		toolNames = append(toolNames, tool.Name)
	}
	sort.Strings(toolNames)
	if got, want := strings.Join(toolNames, ","), "cancel_run,get_run,list_jobs,list_projects,list_runs,run_job,status"; got != want {
		t.Fatalf("HTTP MCP tools = %s, want %s", got, want)
	}
	mcpQueued := callRemoteTool(t, mcpSession, "run_job", map[string]any{"project": enabledProject, "name": "hello", "data": map[string]any{"source": "mcp"}})
	if mcpQueued.IsError {
		t.Fatalf("HTTP MCP run_job: %s", remoteToolText(mcpQueued))
	}
	mcpRunID := int64(remoteToolObject(t, mcpQueued.StructuredContent)["run"].(map[string]any)["id"].(float64))
	mcpDetail := callRemoteTool(t, mcpSession, "get_run", map[string]any{"run_id": mcpRunID, "wait_seconds": 30})
	mcpResult := remoteToolObject(t, mcpDetail.StructuredContent)
	if mcpDetail.IsError || mcpResult["run"].(map[string]any)["status"] != "succeeded" || !strings.Contains(mcpResult["log"].(string), "hello-log") {
		t.Fatalf("HTTP MCP get_run: error=%v result=%+v", mcpDetail.IsError, mcpResult)
	}
	invalidMCPWait := callRemoteTool(t, mcpSession, "get_run", map[string]any{"run_id": mcpRunID, "wait_seconds": 61})
	if !invalidMCPWait.IsError || remoteToolObject(t, invalidMCPWait.StructuredContent)["error"].(map[string]any)["code"] != "usage" {
		t.Fatalf("HTTP MCP accepted wait_seconds=61: %+v", invalidMCPWait)
	}
	mcpSlow := callRemoteTool(t, mcpSession, "run_job", map[string]any{"project": enabledProject, "name": "slow"})
	if mcpSlow.IsError {
		t.Fatalf("HTTP MCP slow run_job: %s", remoteToolText(mcpSlow))
	}
	mcpSlowID := int64(remoteToolObject(t, mcpSlow.StructuredContent)["run"].(map[string]any)["id"].(float64))
	mcpSlowStarted := time.Now()
	mcpSlowDetail := callRemoteTool(t, mcpSession, "get_run", map[string]any{"run_id": mcpSlowID, "wait_seconds": 60})
	mcpSlowResult := remoteToolObject(t, mcpSlowDetail.StructuredContent)
	if mcpSlowDetail.IsError || time.Since(mcpSlowStarted) < 10*time.Second || mcpSlowResult["run"].(map[string]any)["status"] != "succeeded" {
		t.Fatalf("60-second HTTP MCP wait did not survive the server write timeout: elapsed=%s error=%v result=%+v", time.Since(mcpSlowStarted), mcpSlowDetail.IsError, mcpSlowResult)
	}
	for _, test := range []struct {
		name string
		args map[string]any
		code string
	}{
		{name: "disabled project", args: map[string]any{"project": disabledProject}, code: "not_found"},
		{name: "unregistered project", args: map[string]any{"project": filepath.Join(root, "unknown")}, code: "not_found"},
		{name: "missing project", args: map[string]any{}, code: "usage"},
	} {
		result := callRemoteTool(t, mcpSession, "list_jobs", test.args)
		failure := remoteToolObject(t, result.StructuredContent)["error"].(map[string]any)
		if !result.IsError || failure["code"] != test.code {
			t.Fatalf("HTTP MCP %s: result=%+v", test.name, result)
		}
	}
	for _, tool := range []string{"get_run", "cancel_run"} {
		arguments := map[string]any{"run_id": disabledRun.ID}
		result := callRemoteTool(t, mcpSession, tool, arguments)
		failure := remoteToolObject(t, result.StructuredContent)["error"].(map[string]any)
		if !result.IsError || failure["code"] != "not_found" {
			t.Fatalf("HTTP MCP %s accessed a disabled project's run: %+v", tool, result)
		}
	}

	// Check every persisted/output surface the bearer token must never enter.
	state, err = store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []struct {
		id     int64
		source string
	}{{queued.Run.ID, "api"}, {mcpRunID, "mcp"}} {
		stored, err := state.RunByID(expected.id)
		if err != nil {
			_ = state.Close()
			t.Fatal(err)
		}
		path, ok := state.EventFilePath(stored)
		if !ok {
			_ = state.Close()
			t.Fatalf("run %d has no event file", expected.id)
		}
		eventBytes, err := os.ReadFile(path)
		if err != nil {
			_ = state.Close()
			t.Fatal(err)
		}
		if bytes.Contains(eventBytes, []byte(token)) {
			_ = state.Close()
			t.Fatalf("API token stored in event file %s", path)
		}
		var event struct {
			Source string         `json:"source"`
			Data   map[string]any `json:"data"`
		}
		if err := json.Unmarshal(eventBytes, &event); err != nil || event.Source != expected.source || event.Data["source"] == nil {
			_ = state.Close()
			t.Fatalf("event file for run %d has source=%q data=%v err=%v", expected.id, event.Source, event.Data, err)
		}
	}
	runs, err := state.RunsForProject(enabledProject, "", 50)
	if err != nil {
		_ = state.Close()
		t.Fatal(err)
	}
	for _, run := range runs {
		if !run.LogPath.Valid {
			continue
		}
		log, err := state.ReadRunLog(run)
		if err != nil {
			_ = state.Close()
			t.Fatal(err)
		}
		if bytes.Contains(log, []byte(token)) {
			_ = state.Close()
			t.Fatalf("API token stored in run %d log", run.ID)
		}
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	historyCommand := exec.Command(noonBinary, "--project", enabledProject, "history", "--json")
	historyCommand.Dir, historyCommand.Env = home, environment
	history, err := historyCommand.CombinedOutput()
	if err != nil || bytes.Contains(history, []byte(token)) {
		t.Fatalf("history exposed token: err=%v output=%s", err, history)
	}
	if output := stop(); strings.Contains(output, token) {
		t.Fatalf("scheduler output contains API token: %s", output)
	}
}

type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (transport bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+transport.token)
	// Reverse proxies preserve the public Host even when dialing loopback.
	clone.Host = "noon.example.com"
	return transport.base.RoundTrip(clone)
}

func callRemoteTool(t *testing.T, session *protocol.ClientSession, name string, arguments map[string]any) *protocol.CallToolResult {
	t.Helper()
	result, err := session.CallTool(context.Background(), &protocol.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("HTTP MCP CallTool(%s): %v", name, err)
	}
	return result
}

func remoteToolObject(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("decode HTTP MCP tool result: %v: %s", err, encoded)
	}
	return object
}

func remoteToolText(result *protocol.CallToolResult) string {
	var values []string
	for _, content := range result.Content {
		if text, ok := content.(*protocol.TextContent); ok {
			values = append(values, text.Text)
		}
	}
	return strings.Join(values, "\n")
}

func remoteDirectories(t *testing.T, root string) (home, configHome, stateHome, stateDir string) {
	t.Helper()
	home = filepath.Join(root, "home")
	configHome = filepath.Join(root, "config")
	stateHome = filepath.Join(root, "xdg-state")
	stateDir = filepath.Join(root, "state")
	for _, path := range []string{home, configHome, stateHome} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return home, configHome, stateHome, stateDir
}

func runRemoteCommand(binary, directory string, environment []string, args ...string) (int, string) {
	command := exec.Command(binary, args...)
	command.Dir, command.Env = directory, environment
	output, err := command.CombinedOutput()
	if err == nil {
		return 0, string(output)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), string(output)
	}
	return -1, fmt.Sprintf("%v\n%s", err, output)
}

func writeRemoteFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func assertNoSchedulerHeartbeat(t *testing.T, stateDir string) {
	t.Helper()
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if _, exists, err := state.SchedulerInfo(); err != nil || exists {
		t.Fatalf("invalid token created a scheduler heartbeat: exists=%t err=%v", exists, err)
	}
}

func startRemoteScheduler(t *testing.T, binary, directory string, environment []string, args ...string) (string, func() string) {
	t.Helper()
	command := exec.Command(binary, append([]string{"serve"}, args...)...)
	command.Dir, command.Env = directory, environment
	var stdout, stderr lockedBuffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	address := waitForListenerAddress(t, done, &stderr)
	client := &http.Client{Timeout: time.Second}
	waitForHTTPHealth(t, client, address, done, &stderr)
	var stopped bool
	stop := func() string {
		if !stopped {
			stopped = true
			_ = command.Process.Signal(syscall.SIGTERM)
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("scheduler shutdown: %v; stderr=%s", err, stderr.String())
				}
			case <-time.After(15 * time.Second):
				_ = command.Process.Kill()
				t.Errorf("scheduler did not stop; stderr=%s", stderr.String())
			}
		}
		return stdout.String() + stderr.String()
	}
	t.Cleanup(func() { _ = stop() })
	return address, stop
}

func remoteRequest(t *testing.T, client *http.Client, method, address, token string, body []byte) (*http.Response, []byte) {
	t.Helper()
	request, err := http.NewRequest(method, address, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("HTTP %s %s: %v", method, address, err)
	}
	data, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatalf("read HTTP %s %s response: %v", method, address, err)
	}
	return response, data
}

func webhookSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
