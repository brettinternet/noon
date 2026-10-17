package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

const noonSignatureHeader = "X-Noon-Signature-256"

func TestWebhooks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	now := time.Date(2026, 10, 16, 1, 2, 3, 0, time.UTC)
	projectID, err := state.RegisterProject(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.EnableProject(root, nil, nil, now); err != nil {
		t.Fatal(err)
	}
	secret := []byte("test-webhook-secret")
	definition := config.Job{Name: "hooks", Run: []string{"true"}, Enabled: true, Digest: "sha256:test", Webhook: &config.Webhook{SecretEnv: "NOON_TEST_SECRET"}}
	eachDefinition := config.Job{Name: "each", Run: []string{"true"}, Requests: "each", Enabled: true, Digest: "sha256:each", Webhook: &config.Webhook{SecretEnv: "NOON_TEST_SECRET"}}
	plain := config.Job{Name: "plain", Run: []string{"true"}, Enabled: true, Digest: "sha256:plain"}
	textJob := config.Job{Name: "text", Run: []string{"true"}, Enabled: true, Digest: "sha256:text", Webhook: &config.Webhook{SecretEnv: "NOON_TEST_SECRET"}}
	largeJob := config.Job{Name: "large", Run: []string{"true"}, Enabled: true, Digest: "sha256:large", Webhook: &config.Webhook{SecretEnv: "NOON_TEST_SECRET"}}
	cooldownJob := config.Job{Name: "cooldown", Run: []string{"true"}, Enabled: true, Digest: "sha256:cooldown", Cooldown: time.Minute, Webhook: &config.Webhook{SecretEnv: "NOON_TEST_SECRET"}}
	missingSecretJob := config.Job{Name: "no-secret", Run: []string{"true"}, Enabled: true, Digest: "sha256:no-secret", Webhook: &config.Webhook{SecretEnv: "MISSING_SECRET"}}
	project := WebhookProject{
		ID: projectID, Config: config.Config{Project: root, StateDir: stateDir, Jobs: map[string]config.Job{"hooks": definition, "each": eachDefinition, "plain": plain, "text": textJob, "large": largeJob, "cooldown": cooldownJob, "no-secret": missingSecretJob}},
		Enabled: true, Jobs: map[string]WebhookJob{
			"hooks":     {Definition: definition, Secret: secret, Approved: true},
			"each":      {Definition: eachDefinition, Secret: secret, Approved: true},
			"plain":     {Definition: plain, Approved: true},
			"text":      {Definition: textJob, Secret: secret, Approved: true},
			"large":     {Definition: largeJob, Secret: secret, Approved: true},
			"cooldown":  {Definition: cooldownJob, Secret: secret, Approved: true},
			"no-secret": {Definition: missingSecretJob, Approved: true},
		},
	}
	handler := NewHandler([]WebhookProject{project})
	handler.SetHealthy(true)
	path := fmt.Sprintf("/hooks/%d/hooks", projectID)
	body := []byte(`{"action":"opened"}`)

	for _, test := range []struct {
		name      string
		signature bool
		want      int
	}{
		{name: "missing signature", want: http.StatusUnauthorized},
		{name: "wrong signature", signature: true, want: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
			if test.signature {
				req.Header.Set("X-Hub-Signature-256", signature([]byte("wrong"), body))
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.want, response.Body)
			}
		})
	}
	for _, unknownPath := range []string{fmt.Sprintf("/hooks/%d/missing", projectID), fmt.Sprintf("/hooks/%d/plain", projectID), fmt.Sprintf("/hooks/%d/no-secret", projectID), "/hooks/999999/hooks"} {
		req := signedRequest(http.MethodPost, unknownPath, body, secret, "X-Hub-Signature-256")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		if response.Code != http.StatusNotFound {
			t.Fatalf("unknown endpoint %s status = %d, want 404", unknownPath, response.Code)
		}
	}
	unknownOversized := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/hooks/%d/missing", projectID), strings.NewReader(strings.Repeat("x", MaxWebhookBodyBytes+1)))
	unknownOversizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unknownOversizedResponse, unknownOversized)
	if unknownOversizedResponse.Code != http.StatusNotFound {
		t.Fatalf("unknown oversized request status = %d, want 404", unknownOversizedResponse.Code)
	}
	for _, pathWithMethod := range []struct {
		method string
		path   string
		want   int
	}{{http.MethodGet, path, http.StatusMethodNotAllowed}, {http.MethodGet, "/unknown", http.StatusNotFound}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(pathWithMethod.method, pathWithMethod.path, nil))
		if response.Code != pathWithMethod.want {
			t.Fatalf("%s %s status = %d, want %d", pathWithMethod.method, pathWithMethod.path, response.Code, pathWithMethod.want)
		}
	}
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health response = %d %s", health.Code, health.Body)
	}
	oversized := make([]byte, MaxWebhookBodyBytes+1)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(oversized))))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want 413", response.Code)
	}
	unauthorizedRuns, err := state.RunsForJob(projectID, "hooks")
	if err != nil || len(unauthorizedRuns) != 0 {
		t.Fatalf("unauthorized requests queued runs: %+v err=%v", unauthorizedRuns, err)
	}
	// The HTTP contract permits a 1 MiB raw payload even though CLI/MCP event data stays at 256 KiB.
	largeBody, err := json.Marshal(strings.Repeat("x", 300<<10))
	if err != nil {
		t.Fatal(err)
	}
	// A scheduler started from inside a job must not apply its own depth or
	// identity to webhook runs, which always start at depth 0.
	t.Setenv("NOON_DEPTH", "5")
	t.Setenv("NOON_PROJECT", root)
	t.Setenv("NOON_JOB", "large")
	largeRequest := signedRequest(http.MethodPost, fmt.Sprintf("/hooks/%d/large", projectID), largeBody, secret, noonSignatureHeader)
	largeRequest.Header.Set("X-Noon-Delivery", "large-delivery")
	largeResponse := httptest.NewRecorder()
	handler.ServeHTTP(largeResponse, largeRequest)
	if largeResponse.Code != http.StatusAccepted {
		t.Fatalf("signed 300 KiB body status = %d, want 202: %s", largeResponse.Code, largeResponse.Body)
	}
	if largeRuns, err := state.RunsForJob(projectID, "large"); err != nil || len(largeRuns) != 1 || largeRuns[0].Depth != 0 {
		t.Fatalf("webhook run depth: %+v err=%v", largeRuns, err)
	}
	cooldownPath := fmt.Sprintf("/hooks/%d/cooldown", projectID)
	firstCooldown := signedRequest(http.MethodPost, cooldownPath, body, secret, "X-Hub-Signature-256")
	firstCooldown.Header.Set("X-GitHub-Delivery", "cooldown-001")
	firstCooldownResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstCooldownResponse, firstCooldown)
	if firstCooldownResponse.Code != http.StatusAccepted {
		t.Fatalf("first cooldown request status = %d: %s", firstCooldownResponse.Code, firstCooldownResponse.Body)
	}
	secondCooldown := signedRequest(http.MethodPost, cooldownPath, body, secret, "X-Hub-Signature-256")
	secondCooldown.Header.Set("X-GitHub-Delivery", "cooldown-002")
	secondCooldownResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondCooldownResponse, secondCooldown)
	var cooldownResult struct {
		Created bool   `json:"created"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(secondCooldownResponse.Body.Bytes(), &cooldownResult); err != nil || secondCooldownResponse.Code != http.StatusOK || cooldownResult.Created || cooldownResult.Reason != "cooldown" {
		t.Fatalf("cooldown response = status %d result %+v err=%v body=%s", secondCooldownResponse.Code, cooldownResult, err, secondCooldownResponse.Body)
	}

	// An invalid signature must be rejected before an inactive endpoint leaks its state.
	inactive := project
	inactive.Enabled = false
	handler.ReplaceProjects([]WebhookProject{inactive})
	wrong := signedRequest(http.MethodPost, path, body, []byte("wrong"), "X-Hub-Signature-256")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, wrong)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("inactive endpoint with bad signature status = %d, want 401", response.Code)
	}
	for _, inactiveProject := range []struct {
		name    string
		project WebhookProject
		reason  string
	}{
		{name: "project disabled", project: inactive, reason: "project is disabled"},
		{name: "job disabled", project: withJobState(project, func(job *WebhookJob) { job.Definition.Enabled = false }), reason: "job is disabled"},
		{name: "paused", project: withJobState(project, func(job *WebhookJob) { job.Paused = true }), reason: "job is paused"},
		{name: "needs approval", project: withJobState(project, func(job *WebhookJob) { job.Definition.Shared = true; job.Approved = false }), reason: "job needs approval"},
	} {
		t.Run(inactiveProject.name, func(t *testing.T) {
			handler.ReplaceProjects([]WebhookProject{inactiveProject.project})
			req := signedRequest(http.MethodPost, path, body, secret, "X-Hub-Signature-256")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), inactiveProject.reason) {
				t.Fatalf("inactive response = %d %s", response.Code, response.Body)
			}
		})
	}
	handler.ReplaceProjects([]WebhookProject{project})
	if err := state.DisableProject(root, now); err != nil {
		t.Fatal(err)
	}
	staleProject := signedRequest(http.MethodPost, path, body, secret, "X-Hub-Signature-256")
	staleDisabledResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleDisabledResponse, staleProject)
	if staleDisabledResponse.Code != http.StatusConflict || !strings.Contains(staleDisabledResponse.Body.String(), "project is disabled") {
		t.Fatalf("stale disabled-project response = %d %s", staleDisabledResponse.Code, staleDisabledResponse.Body)
	}
	if err := state.EnableProject(root, nil, nil, now); err != nil {
		t.Fatal(err)
	}
	if err := state.SetPaused(root, "hooks", true, now); err != nil {
		t.Fatal(err)
	}
	stalePaused := signedRequest(http.MethodPost, path, body, secret, "X-Hub-Signature-256")
	stalePausedResponse := httptest.NewRecorder()
	handler.ServeHTTP(stalePausedResponse, stalePaused)
	if stalePausedResponse.Code != http.StatusConflict || !strings.Contains(stalePausedResponse.Body.String(), "job is paused") {
		t.Fatalf("stale paused-job response = %d %s", stalePausedResponse.Code, stalePausedResponse.Body)
	}
	if err := state.SetPaused(root, "hooks", false, now); err != nil {
		t.Fatal(err)
	}

	invalidDelivery := signedRequest(http.MethodPost, path, body, secret, "X-Hub-Signature-256")
	invalidDelivery.Header.Set("X-GitHub-Delivery", "not a valid key")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, invalidDelivery)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid delivery ID status = %d, want 400", response.Code)
	}

	valid := signedRequest(http.MethodPost, path, body, secret, "X-Hub-Signature-256")
	valid.Header.Set("Content-Type", "application/json")
	valid.Header.Set("User-Agent", "GitHub-Hookshot/test")
	valid.Header.Set("X-GitHub-Event", "issues")
	valid.Header.Set("X-GitHub-Delivery", "delivery-001")
	valid.Header.Set("Authorization", "Bearer do-not-store")
	valid.Header.Set("X-Hub-Signature-256", signature(secret, body))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, valid)
	if response.Code != http.StatusAccepted {
		t.Fatalf("valid webhook status = %d: %s", response.Code, response.Body)
	}
	var created struct {
		RunID   int64  `json:"run_id"`
		Created bool   `json:"created"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.RunID == 0 || !created.Created {
		t.Fatalf("created response = %+v err=%v body=%s", created, err, response.Body)
	}
	run, err := state.RunByID(created.RunID)
	if err != nil || run.Trigger != "webhook" || !run.RequestedBy.Valid || run.RequestedBy.String != "webhook:"+valid.RemoteAddr || !run.IdempotencyKey.Valid || run.IdempotencyKey.String != "delivery-001" {
		t.Fatalf("queued run = %+v err=%v", run, err)
	}
	eventPath, ok := state.EventFilePath(run)
	if !ok {
		t.Fatal("webhook run has no event file")
	}
	eventData, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(eventData, &event); err != nil {
		t.Fatalf("event file is not JSON: %v: %s", err, eventData)
	}
	if event["source"] != "webhook" || event["data"].(map[string]any)["action"] != "opened" {
		t.Fatalf("event file payload = %+v", event)
	}
	headers, ok := event["headers"].(map[string]any)
	if !ok || headers["content-type"] != "application/json" || headers["user-agent"] != "GitHub-Hookshot/test" || headers["x-github-event"] != "issues" || headers["x-github-delivery"] != "delivery-001" {
		t.Fatalf("event headers = %+v", event["headers"])
	}
	for key := range headers {
		if key == "authorization" || strings.Contains(key, "signature") {
			t.Fatalf("sensitive header %q stored in event file", key)
		}
	}
	if strings.Contains(string(eventData), "do-not-store") || strings.Contains(string(eventData), "sha256=") {
		t.Fatalf("event file contains authentication material: %s", eventData)
	}

	coalesced := signedRequest(http.MethodPost, path, body, secret, noonSignatureHeader)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, coalesced)
	var coalescedResult struct {
		RunID   int64  `json:"run_id"`
		Created bool   `json:"created"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &coalescedResult); err != nil || response.Code != http.StatusOK || coalescedResult.RunID != created.RunID || coalescedResult.Created || coalescedResult.Reason != "queued" {
		t.Fatalf("coalesced response = status %d result %+v err=%v body=%s", response.Code, coalescedResult, err, response.Body)
	}
	duplicate := signedRequest(http.MethodPost, path, body, secret, noonSignatureHeader)
	duplicate.Header.Set("X-GitHub-Delivery", "delivery-001")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, duplicate)
	var duplicateResult struct {
		RunID   int64  `json:"run_id"`
		Created bool   `json:"created"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &duplicateResult); err != nil || response.Code != http.StatusOK || duplicateResult.RunID != created.RunID || duplicateResult.Created || duplicateResult.Reason != "duplicate" {
		t.Fatalf("duplicate response = status %d result %+v err=%v body=%s", response.Code, duplicateResult, err, response.Body)
	}

	type eachResult struct {
		RunID   int64  `json:"run_id"`
		Created bool   `json:"created"`
		Reason  string `json:"reason"`
	}
	eachPath := fmt.Sprintf("/hooks/%d/each", projectID)
	deliverEach := func(n int, deliveryID string) (int, eachResult, string) {
		body := []byte(fmt.Sprintf(`{"n":%d}`, n))
		request := signedRequest(http.MethodPost, eachPath, body, secret, noonSignatureHeader)
		request.Header.Set("X-GitHub-Delivery", deliveryID)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var result eachResult
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode each webhook response: %v: %s", err, response.Body)
		}
		return response.Code, result, response.Body.String()
	}
	firstEachStatus, firstEach, _ := deliverEach(1, "each-001")
	secondEachStatus, secondEach, _ := deliverEach(2, "each-002")
	if firstEachStatus != http.StatusAccepted || secondEachStatus != http.StatusAccepted || !firstEach.Created || !secondEach.Created || firstEach.RunID == secondEach.RunID {
		t.Fatalf("each webhook deliveries: first=%d %+v second=%d %+v", firstEachStatus, firstEach, secondEachStatus, secondEach)
	}
	duplicateEachStatus, duplicateEach, _ := deliverEach(99, "each-001")
	if duplicateEachStatus != http.StatusOK || duplicateEach.Created || duplicateEach.Reason != "duplicate" || duplicateEach.RunID != firstEach.RunID {
		t.Fatalf("each webhook redelivery: status=%d result=%+v", duplicateEachStatus, duplicateEach)
	}
	for i := 3; i <= 20; i++ {
		status, result, body := deliverEach(i, fmt.Sprintf("each-%03d", i))
		if status != http.StatusAccepted || !result.Created {
			t.Fatalf("each webhook %d: status=%d result=%+v body=%s", i, status, result, body)
		}
	}
	fullEachStatus, _, fullEachBody := deliverEach(21, "each-021")
	fullEachRuns, err := state.RunsForJob(projectID, "each")
	if fullEachStatus != http.StatusTooManyRequests || fullEachBody != "{\"error\":\"job queue is full\"}\n" || err != nil || len(fullEachRuns) != 20 {
		t.Fatalf("full each webhook: status=%d body=%s runs=%d err=%v", fullEachStatus, fullEachBody, len(fullEachRuns), err)
	}

	// Maximum-size non-JSON bodies and permitted headers may expand sixfold in JSON.
	text := []byte(strings.Repeat("<", MaxWebhookBodyBytes))
	textRequest := signedRequest(http.MethodPost, fmt.Sprintf("/hooks/%d/text", projectID), text, secret, noonSignatureHeader)
	textRequest.Header.Set("X-Noon-Delivery", "text-delivery")
	textRequest.Header.Set("User-Agent", strings.Repeat("<", 6000))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, textRequest)
	if response.Code != http.StatusAccepted {
		t.Fatalf("non-JSON webhook status = %d: %s", response.Code, response.Body)
	}
	var textResult struct {
		RunID int64 `json:"run_id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &textResult); err != nil {
		t.Fatal(err)
	}
	textRun, err := state.RunByID(textResult.RunID)
	if err != nil {
		t.Fatal(err)
	}
	textPath, ok := state.EventFilePath(textRun)
	if !ok {
		t.Fatal("non-JSON webhook run has no event file")
	}
	textEvent, err := os.ReadFile(textPath)
	if err != nil {
		t.Fatal(err)
	}
	var textPayload map[string]any
	if err := json.Unmarshal(textEvent, &textPayload); err != nil || textPayload["data"] != string(text) {
		t.Fatalf("non-JSON event data mismatch: err=%v", err)
	}
	if textPayload["headers"].(map[string]any)["user-agent"] != strings.Repeat("<", 6000) {
		t.Fatal("escaped User-Agent was not preserved")
	}
}

func withJobState(project WebhookProject, update func(*WebhookJob)) WebhookProject {
	definitions := make(map[string]config.Job, len(project.Config.Jobs))
	for name, job := range project.Config.Jobs {
		definitions[name] = job
	}
	project.Config.Jobs = definitions
	jobs := make(map[string]WebhookJob, len(project.Jobs))
	for name, job := range project.Jobs {
		jobs[name] = job
	}
	job := jobs["hooks"]
	update(&job)
	jobs["hooks"] = job
	project.Jobs = jobs
	project.Config.Jobs["hooks"] = job.Definition
	return project
}

func signedRequest(method, path string, body, secret []byte, header string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	request.Header.Set(header, signature(secret, body))
	return request
}

func signature(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
