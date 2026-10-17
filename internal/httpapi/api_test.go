package httpapi

import (
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
