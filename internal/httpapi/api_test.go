package httpapi

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

func TestAPIGetRunTimestampsAreOptInAndUseRawOffsets(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	userConfigDir := filepath.Join(root, "user-config")
	if err := os.MkdirAll(userConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte("version: 1\njobs: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := config.Input{CWD: root, UserConfigDir: userConfigDir, StateDirDefault: stateDir}
	cfg, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.Enable(cfg, time.Now()); err != nil {
		t.Fatal(err)
	}
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	projects, err := state.ProjectRecords()
	if err != nil || len(projects) != 1 {
		t.Fatalf("project records=%+v err=%v", projects, err)
	}
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	run, err := state.CreateRun(store.NewRun{ProjectID: projects[0].ID, Job: "timed", Trigger: "manual", Digest: "test", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	log, err := state.RunLog(projects[0].ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("one\nmissing\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	logBytes, truncated, err := log.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SetRunLog(run.ID, log.Path()); err != nil {
		t.Fatal(err)
	}
	exit := 0
	if err := state.FinishRun(run.ID, projects[0].ID, run.Job, store.Outcome{Status: "succeeded", FinishedAt: now, ExitCode: &exit, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated}); err != nil {
		t.Fatal(err)
	}
	firstAt := now.Add(time.Second)
	lastAt := now.Add(3 * time.Second)
	lastStart := int64(len("one\nmissing\n"))
	records := []store.TimingRecord{
		{Start: 0, End: int64(len("one\n")), UnixMilli: firstAt.UnixMilli()},
		{Start: lastStart, End: int64(len("one\nmissing\ntwo\n")), UnixMilli: lastAt.UnixMilli()},
	}
	encoded := make([]byte, len(records)*24)
	for i, record := range records {
		start := i * 24
		binary.LittleEndian.PutUint64(encoded[start:start+8], uint64(record.Start))
		binary.LittleEndian.PutUint64(encoded[start+8:start+16], uint64(record.End))
		binary.LittleEndian.PutUint64(encoded[start+16:start+24], uint64(record.UnixMilli))
	}
	if err := os.WriteFile(filepath.Join(stateDir, log.Path())+".timing", encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	handler := NewAPIHandler(input, stateDir)
	request := func(query string) map[string]any {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/runs/%d%s", run.ID, query), nil))
		if response.Code != http.StatusOK {
			t.Fatalf("get_run %s: status=%d body=%s", query, response.Code, response.Body)
		}
		var value map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	const raw = "one\nmissing\ntwo\n"
	plain := request("")
	if plain["log"] != raw {
		t.Fatalf("default API log changed: %+v", plain)
	}
	if _, ok := plain["line_times"]; ok {
		t.Fatalf("default API response unexpectedly included line_times: %+v", plain)
	}
	timed := request("?timestamps=true")
	lineTimes := timed["line_times"].([]any)
	if timed["log"] != raw || len(lineTimes) != 3 || lineTimes[0].(map[string]any)["offset"] != float64(0) || lineTimes[1].(map[string]any)["offset"] != float64(len("one\n")) || lineTimes[1].(map[string]any)["time"] != nil || lineTimes[2].(map[string]any)["offset"] != float64(lastStart) {
		t.Fatalf("timestamped API response changed text or offsets: %+v", timed)
	}
	if _, err := time.Parse(store.TimestampLayout, lineTimes[2].(map[string]any)["time"].(string)); err != nil {
		t.Fatalf("API time is not UTC run-time format: %+v", lineTimes[2])
	}
}

func TestAPIQueueRoutingAndErrorMapping(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	userConfigDir := filepath.Join(root, "user-config")
	if err := os.MkdirAll(userConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noon.yaml"), []byte("jobs:\n  full:\n    run: 'true'\n    requests: each\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	input := config.Input{CWD: root, UserConfigDir: userConfigDir, StateDirDefault: stateDir}
	cfg, err := config.Load(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := app.Enable(cfg, time.Now()); err != nil {
		t.Fatal(err)
	}
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	records, err := state.ProjectRecords()
	if err != nil || len(records) != 1 {
		_ = state.Close()
		t.Fatalf("project records = %+v err=%v", records, err)
	}
	projectID := records[0].ID
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	handler := NewAPIHandler(input, stateDir)
	queuePath := fmt.Sprintf("/v1/projects/%d/jobs/full/runs", projectID)
	invalidKey := httptest.NewRecorder()
	handler.ServeHTTP(invalidKey, httptest.NewRequest(http.MethodPost, queuePath, strings.NewReader(`{"key":""}`)))
	if invalidKey.Code != http.StatusBadRequest || !strings.Contains(invalidKey.Body.String(), `"code":"usage"`) {
		t.Fatalf("empty idempotency key response: status=%d body=%s", invalidKey.Code, invalidKey.Body)
	}
	oversizedDataBody := `{"data":"` + strings.Repeat("x", store.MaxEventBytes) + `"}`
	oversizedData := httptest.NewRecorder()
	handler.ServeHTTP(oversizedData, httptest.NewRequest(http.MethodPost, queuePath, strings.NewReader(oversizedDataBody)))
	if oversizedData.Code != http.StatusBadRequest || !strings.Contains(oversizedData.Body.String(), `"code":"usage"`) {
		t.Fatalf("oversized data response: status=%d body=%s", oversizedData.Code, oversizedData.Body)
	}
	for i := 0; i < 20; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, queuePath, nil))
		if response.Code != http.StatusAccepted {
			t.Fatalf("queue request %d status=%d body=%s", i+1, response.Code, response.Body)
		}
	}
	full := httptest.NewRecorder()
	handler.ServeHTTP(full, httptest.NewRequest(http.MethodPost, queuePath, nil))
	var failure struct {
		SchemaVersion int `json:"schema_version"`
		Error         struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(full.Body.Bytes(), &failure); err != nil || full.Code != http.StatusTooManyRequests || failure.SchemaVersion != 1 || failure.Error.Code != "queue_full" {
		t.Fatalf("queue cap response: status=%d body=%s err=%v", full.Code, full.Body, err)
	}

	method := httptest.NewRecorder()
	handler.ServeHTTP(method, httptest.NewRequest(http.MethodPut, fmt.Sprintf("/v1/projects/%d/jobs", projectID), nil))
	if method.Code != http.StatusMethodNotAllowed || method.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("wrong method response: status=%d Allow=%q body=%s", method.Code, method.Header().Get("Allow"), method.Body)
	}
	admin := httptest.NewRecorder()
	handler.ServeHTTP(admin, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/projects/%d/jobs/full/approve", projectID), nil))
	if admin.Code != http.StatusNotFound {
		t.Fatalf("unimplemented approval route status=%d body=%s", admin.Code, admin.Body)
	}

	oversizedBody := strings.NewReader(strings.Repeat(" ", int(MaxAPIRequestBytes+1)))
	oversized := httptest.NewRecorder()
	handler.ServeHTTP(oversized, httptest.NewRequest(http.MethodPost, queuePath, oversizedBody))
	if oversized.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized request status=%d body=%s", oversized.Code, oversized.Body)
	}
}
