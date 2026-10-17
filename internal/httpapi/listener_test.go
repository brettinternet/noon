package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListenerAuthenticationPrecedesRouting(t *testing.T) {
	token := strings.Repeat("test", 8)
	webhooks := NewHandler(nil)
	webhooks.SetHealthy(true)
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	mcp := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler := NewListenerHandler(webhooks, api, mcp, []byte(token))
	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/unknown", nil),
		httptest.NewRequest(http.MethodDelete, "/v1/projects/1", nil),
		httptest.NewRequest(http.MethodPost, "/mcp", nil),
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" || response.Body.Len() != 0 {
			t.Fatalf("unauthenticated %s %s: status=%d headers=%v body=%q", request.Method, request.URL.Path, response.Code, response.Header(), response.Body.String())
		}
	}

	request := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("authorized MCP request status=%d", response.Code)
	}

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("unauthenticated health status=%d body=%s", health.Code, health.Body)
	}

	withoutToken := NewListenerHandler(webhooks, api, mcp, nil)
	for _, path := range []string{"/v1/projects", "/mcp"} {
		missing := httptest.NewRecorder()
		withoutToken.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, path, nil))
		if missing.Code != http.StatusNotFound {
			t.Fatalf("unconfigured %s route status=%d body=%s", path, missing.Code, missing.Body)
		}
	}
}
