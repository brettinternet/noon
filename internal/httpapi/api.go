package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

const MaxAPIRequestBytes int64 = 512 << 10
const defaultAPILogBytes = 16_384

type APIHandler struct {
	input    config.Input
	stateDir string
}

type runRequest struct {
	Key  *string         `json:"key,omitempty"`
	In   *string         `json:"in,omitempty"`
	At   *string         `json:"at,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

type errorBody struct {
	SchemaVersion int        `json:"schema_version"`
	Error         *app.Error `json:"error"`
}

func NewAPIHandler(input config.Input, stateDir string) *APIHandler {
	return &APIHandler{input: input, stateDir: stateDir}
}

func (h *APIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/")
	if path == r.URL.Path || path == "" {
		h.notFound(w)
		return
	}
	parts := strings.Split(path, "/")
	switch {
	case len(parts) == 1 && parts[0] == "projects":
		if !h.method(w, r, http.MethodGet) {
			return
		}
		h.projects(w, r)
	case len(parts) == 2 && parts[0] == "projects":
		if !h.method(w, r, http.MethodGet) {
			return
		}
		id, ok := parseID(parts[1])
		if !ok {
			h.notFound(w)
			return
		}
		h.projectStatus(w, id)
	case len(parts) == 3 && parts[0] == "projects" && parts[2] == "jobs":
		if !h.method(w, r, http.MethodGet) {
			return
		}
		id, ok := parseID(parts[1])
		if !ok {
			h.notFound(w)
			return
		}
		h.jobs(w, id)
	case len(parts) == 3 && parts[0] == "projects" && parts[2] == "runs":
		if !h.method(w, r, http.MethodGet) {
			return
		}
		id, ok := parseID(parts[1])
		if !ok {
			h.notFound(w)
			return
		}
		h.runs(w, r, id)
	case len(parts) == 5 && parts[0] == "projects" && parts[2] == "jobs" && parts[4] == "runs":
		if !h.method(w, r, http.MethodPost) {
			return
		}
		id, ok := parseID(parts[1])
		if !ok || parts[3] == "" {
			h.notFound(w)
			return
		}
		h.queue(w, r, id, parts[3])
	case len(parts) == 2 && parts[0] == "runs":
		if !h.method(w, r, http.MethodGet) {
			return
		}
		id, ok := parseID(parts[1])
		if !ok {
			h.notFound(w)
			return
		}
		h.getRun(w, r, id)
	case len(parts) == 3 && parts[0] == "runs" && parts[2] == "cancel":
		if !h.method(w, r, http.MethodPost) {
			return
		}
		id, ok := parseID(parts[1])
		if !ok {
			h.notFound(w)
			return
		}
		h.cancel(w, id)
	default:
		h.notFound(w)
	}
}

func (h *APIHandler) projects(w http.ResponseWriter, r *http.Request) {
	if err := onlyQueryKeys(map[string]bool{})(r.URL.Query()); err != nil {
		h.writeError(w, err, 0)
		return
	}
	view, err := app.ListProjects(h.stateDir)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *APIHandler) projectStatus(w http.ResponseWriter, id int64) {
	input, err := h.projectInput(id)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	view, err := app.Dashboard(input, time.Now())
	if err != nil {
		var configErr *app.DashboardConfigError
		if errors.As(err, &configErr) {
			err = app.ConfigError(err)
		} else {
			err = app.StateError(err)
		}
		h.writeError(w, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *APIHandler) jobs(w http.ResponseWriter, id int64) {
	input, err := h.projectInput(id)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	view, err := app.ListJobs(input, time.Now())
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *APIHandler) runs(w http.ResponseWriter, r *http.Request, id int64) {
	input, err := h.projectInput(id)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	if err := onlyQueryKeys(map[string]bool{"job": true, "status": true, "limit": true})(r.URL.Query()); err != nil {
		h.writeError(w, err, 0)
		return
	}
	query := r.URL.Query()
	var job *string
	if values, exists := query["job"]; exists {
		if len(values) != 1 {
			h.writeError(w, app.UsageError("job must be specified once", ""), 0)
			return
		}
		job = &values[0]
	}
	statuses := queryStatuses(query["status"])
	limit, err := intQuery(query, "limit", 20)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	view, err := app.ListRuns(input, job, statuses, limit)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *APIHandler) queue(w http.ResponseWriter, r *http.Request, id int64, job string) {
	input, err := h.projectInput(id)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	request, err := decodeRunRequest(w, r)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeError(w, app.UsageError("request body must be at most 512 KiB", "Send a smaller request body"), http.StatusRequestEntityTooLarge)
		} else {
			h.writeError(w, app.UsageError(err.Error(), "Send a JSON object with key, in, at, and/or data"), 0)
		}
		return
	}
	if request.In != nil && request.At != nil {
		h.writeError(w, app.UsageError("in and at cannot be used together", "Pass in or at, not both"), 0)
		return
	}
	if request.Key != nil && !app.ValidIdempotencyKey(*request.Key) {
		h.writeError(w, app.UsageError("key must be 1–200 characters of [A-Za-z0-9._:-]", "Use letters, digits, dot, underscore, colon, or hyphen"), 0)
		return
	}
	cfg, err := config.Load(input)
	if err != nil {
		h.writeError(w, app.ConfigError(err), 0)
		return
	}
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		h.writeError(w, app.ConfigError(err), 0)
		return
	}
	now := time.Now()
	mode, inValue, atValue := "detach", "", ""
	if request.In != nil {
		mode, inValue = "in", *request.In
	}
	if request.At != nil {
		mode, atValue = "at", *request.At
	}
	dueAt, err := app.QueueDueAt(mode, inValue, atValue, now, location)
	if err != nil {
		h.writeError(w, app.UsageError(err.Error(), `Use in "2h" or at "2026-10-16 09:00"`), 0)
		return
	}
	var eventData []byte
	if request.Data != nil {
		eventData = request.Data
	}
	result, err := app.QueueRun(cfg, job, optionalValue(request.Key), dueAt, now, time.Now, mode == "detach", "api:"+r.RemoteAddr,
		app.QueueOptions{EventData: eventData, EventSource: "api"})
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusAccepted
	}
	writeJSON(w, status, result)
}

func (h *APIHandler) getRun(w http.ResponseWriter, r *http.Request, id int64) {
	if _, err := app.EnabledRunProject(h.stateDir, id); err != nil {
		h.writeError(w, err, 0)
		return
	}
	if err := onlyQueryKeys(map[string]bool{
		"wait_seconds": true, "log_bytes": true, "after": true, "tail": true,
		"match": true, "context": true, "section": true, "timestamps": true,
	})(r.URL.Query()); err != nil {
		h.writeError(w, err, 0)
		return
	}
	query := r.URL.Query()
	waitSeconds, err := intQuery(query, "wait_seconds", 0)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	logBytes, err := intQuery(query, "log_bytes", defaultAPILogBytes)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	var after *int64
	if _, exists := query["after"]; exists {
		parsed, parseErr := int64Query(query, "after")
		if parseErr != nil {
			h.writeError(w, parseErr, 0)
			return
		}
		after = &parsed
	}
	tail, err := optionalIntQuery(query, "tail")
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	match, err := optionalStringQuery(query, "match")
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	contextLines, err := optionalIntQuery(query, "context")
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	section, err := optionalStringQuery(query, "section")
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	if section != nil && *section == "" {
		h.writeError(w, app.UsageError("section must be all, job, or hook", ""), 0)
		return
	}
	if section == nil {
		section = stringPointer("all")
	}
	timestamps, err := boolQuery(query, "timestamps", false)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	logQuery := app.LogQuery{Section: *section, After: after, Tail: tail, Match: match, Context: contextLines, LimitBytes: &logBytes, RepairUTF8: true, Timestamps: timestamps}
	view, err := app.GetRun(r.Context(), h.stateDir, id, waitSeconds, logQuery)
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *APIHandler) cancel(w http.ResponseWriter, id int64) {
	if _, err := app.EnabledRunProject(h.stateDir, id); err != nil {
		h.writeError(w, err, 0)
		return
	}
	result, err := app.Cancel(h.stateDir, "", &id, "", time.Now())
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrNoDatabase) {
		err = app.RunNotFoundError(id)
	}
	if err != nil {
		h.writeError(w, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *APIHandler) projectInput(id int64) (config.Input, error) {
	return app.EnabledProjectInput(h.input, h.stateDir, id)
}

func (h *APIHandler) method(w http.ResponseWriter, r *http.Request, expected string) bool {
	if r.Method == expected {
		return true
	}
	w.Header().Set("Allow", expected)
	h.writeError(w, app.UsageError("method not allowed", ""), http.StatusMethodNotAllowed)
	return false
}

func (h *APIHandler) notFound(w http.ResponseWriter) {
	h.writeError(w, &app.Error{Code: "not_found", Message: "not found"}, http.StatusNotFound)
}

func (h *APIHandler) writeError(w http.ResponseWriter, err error, statusOverride int) {
	failure := normalizeError(err)
	status := statusOverride
	if status == 0 {
		status = errorStatus(failure.Code)
	}
	writeJSON(w, status, errorBody{SchemaVersion: 1, Error: failure})
}

func normalizeError(err error) *app.Error {
	var failure *app.Error
	if errors.As(err, &failure) {
		return failure
	}
	var queueError *app.QueueError
	if errors.As(err, &queueError) {
		return &app.Error{Code: queueError.Code, Message: queueError.Message, Hint: queueError.Hint, Usage: queueError.Code == "usage"}
	}
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrNoDatabase) {
		return &app.Error{Code: "not_found", Message: "not found"}
	}
	return app.StateError(err)
}

func errorStatus(code string) int {
	switch code {
	case "usage":
		return http.StatusBadRequest
	case "not_found":
		return http.StatusNotFound
	case "not_enabled", "disabled", "ended", "paused", "needs_approval", "recursion", "unsupported_platform":
		return http.StatusConflict
	case "queue_full":
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func parseID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}

func decodeRunRequest(w http.ResponseWriter, r *http.Request) (runRequest, error) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxAPIRequestBytes))
	if err != nil {
		return runRequest{}, err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return runRequest{}, nil
	}
	if strings.TrimSpace(string(body))[0] != '{' {
		return runRequest{}, errors.New("request body must be a JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	var request runRequest
	if err := decoder.Decode(&request); err != nil {
		return runRequest{}, fmt.Errorf("invalid JSON request body: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return runRequest{}, errors.New("request body must contain one JSON object")
	}
	if len(request.Data) > store.MaxEventBytes {
		return runRequest{}, app.UsageError("data must be at most 256 KiB", "Use a smaller JSON value")
	}
	return request, nil
}

func onlyQueryKeys(allowed map[string]bool) func(url.Values) error {
	return func(query url.Values) error {
		for key := range query {
			if allowed == nil || !allowed[key] {
				return app.UsageError(fmt.Sprintf("unsupported query parameter %q", key), "")
			}
		}
		return nil
	}
}

func queryStatuses(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	var statuses []string
	for _, value := range values {
		statuses = append(statuses, strings.Split(value, ",")...)
	}
	return statuses
}

func intQuery(query url.Values, key string, fallback int) (int, error) {
	values, exists := query[key]
	if !exists {
		return fallback, nil
	}
	if len(values) != 1 {
		return 0, app.UsageError(key+" must be specified once", "")
	}
	value, err := strconv.Atoi(values[0])
	if err != nil {
		return 0, app.UsageError("invalid "+key, "")
	}
	return value, nil
}

func int64Query(query url.Values, key string) (int64, error) {
	values, exists := query[key]
	if !exists || len(values) != 1 {
		return 0, app.UsageError(key+" must be specified once", "")
	}
	value, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil {
		return 0, app.UsageError("invalid "+key, "")
	}
	return value, nil
}

func optionalIntQuery(query url.Values, key string) (*int, error) {
	if _, exists := query[key]; !exists {
		return nil, nil
	}
	value, err := intQuery(query, key, 0)
	return &value, err
}

func boolQuery(query url.Values, key string, fallback bool) (bool, error) {
	values, exists := query[key]
	if !exists {
		return fallback, nil
	}
	if len(values) != 1 || values[0] != "true" && values[0] != "false" {
		return false, app.UsageError("invalid "+key, "Use true or false")
	}
	return values[0] == "true", nil
}

func optionalStringQuery(query url.Values, key string) (*string, error) {
	values, exists := query[key]
	if !exists {
		return nil, nil
	}
	if len(values) != 1 {
		return nil, app.UsageError(key+" must be specified once", "")
	}
	return &values[0], nil
}

func optionalValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPointer(value string) *string { return &value }
