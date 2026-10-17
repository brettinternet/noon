// Package httpapi adapts signed webhook requests to shared app operations.
package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

const MaxWebhookBodyBytes = store.MaxWebhookBodyBytes

type WebhookJob struct {
	Definition config.Job
	Secret     []byte
	Paused     bool
	Approved   bool
}

type WebhookProject struct {
	ID      int64
	Config  config.Config
	Enabled bool
	Jobs    map[string]WebhookJob
}

type Handler struct {
	mu       sync.RWMutex
	projects map[int64]WebhookProject
	healthy  atomic.Bool
}

func NewHandler(projects []WebhookProject) *Handler {
	handler := &Handler{}
	handler.ReplaceProjects(projects)
	return handler
}

// ReplaceProjects atomically publishes the scheduler's latest immutable config snapshot.
func (h *Handler) ReplaceProjects(projects []WebhookProject) {
	next := make(map[int64]WebhookProject, len(projects))
	for _, project := range projects {
		jobs := make(map[string]WebhookJob, len(project.Jobs))
		for name, job := range project.Jobs {
			job.Secret = append([]byte(nil), job.Secret...)
			jobs[name] = job
		}
		project.Jobs = jobs
		next[project.ID] = project
	}
	h.mu.Lock()
	h.projects = next
	h.mu.Unlock()
}

func (h *Handler) SetHealthy(healthy bool) { h.healthy.Store(healthy) }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !h.healthy.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unhealthy"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	projectID, jobName, ok := parseWebhookPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	project, exists := h.project(projectID)
	if !exists {
		http.NotFound(w, r)
		return
	}
	job, exists := project.Jobs[jobName]
	if !exists || job.Definition.Webhook == nil || len(job.Secret) == 0 {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxWebhookBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !verifySignature(r.Header, job.Secret, body) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !job.Definition.MatchesPlatform(project.Config.Platform) {
		failure := app.UnsupportedPlatformError(job.Definition, project.Config.Platform)
		writeJSON(w, http.StatusConflict, map[string]string{"code": failure.Code, "error": failure.Message, "hint": failure.Hint})
		return
	}
	if reason := inactiveReason(project, job); reason != "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": reason})
		return
	}

	key, present := deliveryKey(r.Header)
	if present && !app.ValidIdempotencyKey(key) {
		http.Error(w, "invalid delivery ID", http.StatusBadRequest)
		return
	}
	now := time.Now()
	queued, err := app.QueueRun(project.Config, jobName, key, now, now, time.Now, true,
		"webhook:"+r.RemoteAddr,
		app.QueueOptions{
			EventData: body, EventSource: "webhook", EventHeaders: webhookHeaders(r.Header), Trigger: "webhook",
		},
	)
	if err != nil {
		var queueError *app.QueueError
		if errors.As(err, &queueError) {
			switch queueError.Code {
			case "not_enabled":
				writeJSON(w, http.StatusConflict, map[string]string{"error": "project is disabled"})
			case "disabled":
				writeJSON(w, http.StatusConflict, map[string]string{"error": "job is disabled"})
			case "paused":
				writeJSON(w, http.StatusConflict, map[string]string{"error": "job is paused"})
			case "needs_approval":
				writeJSON(w, http.StatusConflict, map[string]string{"error": "job needs approval"})
			case "not_found":
				http.NotFound(w, r)
			case "queue_full":
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "job queue is full"})
			default:
				http.Error(w, "request could not be queued", http.StatusInternalServerError)
			}
			return
		}
		var operationError *app.Error
		if errors.As(err, &operationError) && operationError.Code == "usage" {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "request could not be queued", http.StatusInternalServerError)
		return
	}
	status := http.StatusOK
	if queued.Created {
		status = http.StatusAccepted
	}
	writeJSON(w, status, struct {
		RunID   int64  `json:"run_id"`
		Created bool   `json:"created"`
		Reason  string `json:"reason,omitempty"`
	}{RunID: queued.Run.ID, Created: queued.Created, Reason: queued.Reason})
}

func (h *Handler) project(id int64) (WebhookProject, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	project, exists := h.projects[id]
	return project, exists
}

func parseWebhookPath(path string) (int64, string, bool) {
	if !strings.HasPrefix(path, "/hooks/") {
		return 0, "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, "/hooks/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, "", false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id < 1 {
		return 0, "", false
	}
	return id, parts[1], true
}

func verifySignature(headers http.Header, secret, body []byte) bool {
	values := append(headers.Values("X-Hub-Signature-256"), headers.Values("X-Noon-Signature-256")...)
	for _, value := range values {
		prefix, encoded, ok := strings.Cut(value, "=")
		if !ok || prefix != "sha256" || len(encoded) != sha256.Size*2 {
			continue
		}
		provided, err := hex.DecodeString(encoded)
		if err != nil {
			continue
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(body)
		if subtle.ConstantTimeCompare(mac.Sum(nil), provided) == 1 {
			return true
		}
	}
	return false
}

func inactiveReason(project WebhookProject, job WebhookJob) string {
	switch {
	case !project.Enabled:
		return "project is disabled"
	case !job.Definition.Enabled:
		return "job is disabled"
	case job.Paused:
		return "job is paused"
	case job.Definition.Shared && !job.Approved:
		return "job needs approval"
	default:
		return ""
	}
}

func deliveryKey(headers http.Header) (string, bool) {
	if values, exists := headers[http.CanonicalHeaderKey("X-GitHub-Delivery")]; exists {
		if len(values) != 1 {
			return "", true
		}
		return values[0], true
	}
	if values, exists := headers[http.CanonicalHeaderKey("X-Noon-Delivery")]; exists {
		if len(values) != 1 {
			return "", true
		}
		return values[0], true
	}
	return "", false
}

func webhookHeaders(headers http.Header) map[string]string {
	result := make(map[string]string)
	for _, name := range []string{"Content-Type", "User-Agent", "X-GitHub-Event", "X-GitHub-Delivery", "X-Noon-Delivery"} {
		if values, exists := headers[http.CanonicalHeaderKey(name)]; exists && len(values) > 0 {
			result[strings.ToLower(name)] = headers.Get(name)
		}
	}
	return result
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func NewServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func IsLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ListenerAddress(listener net.Listener) string {
	if listener == nil || listener.Addr() == nil {
		return ""
	}
	return fmt.Sprint(listener.Addr())
}
