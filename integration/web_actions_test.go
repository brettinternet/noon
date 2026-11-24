package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/store"
)

func TestWebActions(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, configHome, stateHome, stateDir := remoteDirectories(t, root)
	environment := isolatedEnvironment(home, configHome, stateHome, stateDir)
	apiToken := "noon-web-actions-api-token-0123456789-abcdef"
	webToken := "noon-web-actions-web-token-0123456789-abcdef"
	userDir := filepath.Join(configHome, "noon")
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeRemoteFile(t, filepath.Join(userDir, "api-token"), apiToken+"\n", 0o600)
	writeRemoteFile(t, filepath.Join(userDir, "web-token"), webToken+"\n", 0o600)
	writeRemoteFile(t, filepath.Join(userDir, "config.yaml"), "serve:\n  listen: 127.0.0.1:0\n  api_token_file: api-token\n  web_token_file: web-token\n", 0o600)

	project := filepath.Join(root, "actions")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	writeRemoteFile(t, filepath.Join(project, "noon.yaml"), "version: 1\njobs:\n  hello:\n    run: [sh, -c, 'echo hello-web']\n  slow:\n    run: [sleep, \"30\"]\n", 0o600)
	if code, output := runRemoteCommand(noonBinary, project, environment, "enable"); code != 0 {
		t.Fatalf("enable exit=%d output=%s", code, output)
	}
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	records, err := state.ProjectRecords()
	if err != nil || len(records) != 1 {
		t.Fatalf("project records=%+v err=%v", records, err)
	}
	projectPath := "/projects/" + strconv.FormatInt(records[0].ID, 10)
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}

	address, _ := startRemoteScheduler(t, noonBinary, home, environment)
	client := &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	login, err := client.PostForm("http://"+address+"/login", url.Values{"token": {webToken}})
	if err != nil {
		t.Fatal(err)
	}
	_ = login.Body.Close()
	var session *http.Cookie
	for _, cookie := range login.Cookies() {
		if cookie.Name == "noon_session" {
			session = cookie
		}
	}
	if session == nil {
		t.Fatalf("login status=%d set no session", login.StatusCode)
	}
	post := func(path string, form url.Values) (*http.Response, string) {
		t.Helper()
		request, err := http.NewRequest(http.MethodPost, "http://"+address+path, strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.AddCookie(session)
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		return response, string(body)
	}
	runLocation := regexp.MustCompile(`^/runs/([0-9]+)\?done=queued$`)
	queue := func(job string, form url.Values) int64 {
		t.Helper()
		response, body := post(projectPath+"/jobs/"+job+"/runs", form)
		match := runLocation.FindStringSubmatch(response.Header.Get("Location"))
		if response.StatusCode != http.StatusSeeOther || match == nil {
			t.Fatalf("queue %s %v: status=%d location=%q body=%s", job, form, response.StatusCode, response.Header.Get("Location"), body)
		}
		id, _ := strconv.ParseInt(match[1], 10, 64)
		return id
	}
	storedRun := func(id int64) store.Run {
		t.Helper()
		state, err := store.OpenExisting(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		defer state.Close()
		run, err := state.RunByID(id)
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	follow := func(id int64) {
		t.Helper()
		if code, output := runRemoteCommand(noonBinary, project, environment, "logs", strconv.FormatInt(id, 10), "--follow"); code != 0 && code != 3 {
			t.Fatalf("follow run %d exit=%d output=%s", id, code, output)
		}
	}
	listJobs := func() map[string]map[string]any {
		t.Helper()
		code, output := runRemoteCommand(noonBinary, project, environment, "list", "--json")
		var listed struct {
			Jobs []map[string]any `json:"jobs"`
		}
		if err := json.Unmarshal([]byte(output), &listed); code != 0 || err != nil {
			t.Fatalf("list exit=%d err=%v output=%s", code, err, output)
		}
		jobs := map[string]map[string]any{}
		for _, job := range listed.Jobs {
			jobs[job["name"].(string)] = job
		}
		return jobs
	}

	// Queue now: the run succeeds as the viewer's request with a web event.
	hello := queue("hello", url.Values{"when": {"now"}})
	follow(hello)
	run := storedRun(hello)
	if run.Status != "succeeded" || !strings.HasPrefix(run.RequestedBy.String, "web:") {
		t.Fatalf("web-queued run: status=%s requested_by=%q", run.Status, run.RequestedBy.String)
	}
	state, err = store.OpenExisting(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	eventPath, ok := state.EventFilePath(run)
	_ = state.Close()
	if !ok {
		t.Fatalf("web-queued run %d has no event file", hello)
	}
	eventBytes, err := os.ReadFile(eventPath)
	var event struct {
		Source string          `json:"source"`
		Data   json.RawMessage `json:"data"`
	}
	if err != nil || json.Unmarshal(eventBytes, &event) != nil || event.Source != "web" || string(event.Data) != "null" {
		t.Fatalf("event file %s: %s err=%v", eventPath, eventBytes, err)
	}

	// Queue later, then cancel it before it is due.
	later := queue("hello", url.Values{"when": {"in"}, "in": {"1h"}})
	if run := storedRun(later); run.Status != "queued" || !run.DueAt.Valid {
		t.Fatalf("delayed run: %+v", run)
	}
	cancelPath := "/runs/" + strconv.FormatInt(later, 10) + "/cancel"
	if response, body := post(cancelPath, url.Values{"back": {"/runs/" + strconv.FormatInt(later, 10)}}); response.StatusCode != http.StatusSeeOther || !strings.HasSuffix(response.Header.Get("Location"), "?done=cancelled") {
		t.Fatalf("cancel queued: status=%d location=%q body=%s", response.StatusCode, response.Header.Get("Location"), body)
	}
	if run := storedRun(later); run.Status != "cancelled" {
		t.Fatalf("cancelled delayed run status=%s", run.Status)
	}

	// Cancel a running run: confirmation first, then its process group exits.
	slow := queue("slow", url.Values{})
	deadline := time.Now().Add(10 * time.Second)
	for run = storedRun(slow); run.Status != "running" || !run.PGID.Valid; run = storedRun(slow) {
		if time.Now().After(deadline) {
			t.Fatalf("slow never started: %+v", run)
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancelPath = "/runs/" + strconv.FormatInt(slow, 10) + "/cancel"
	if response, body := post(cancelPath, url.Values{}); response.StatusCode != http.StatusOK || !strings.Contains(body, `name="confirm" value="1"`) {
		t.Fatalf("unconfirmed cancel: status=%d body=%s", response.StatusCode, body)
	}
	if run := storedRun(slow); run.Status != "running" {
		t.Fatalf("unconfirmed cancel changed the run: %s", run.Status)
	}
	if response, body := post(cancelPath, url.Values{"confirm": {"1"}}); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("confirmed cancel: status=%d body=%s", response.StatusCode, body)
	}
	follow(slow)
	if run := storedRun(slow); run.Status != "cancelled" {
		t.Fatalf("cancelled slow run status=%s", run.Status)
	}
	if err := syscall.Kill(-int(run.PGID.Int64), 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("slow process group %d still exists: %v", run.PGID.Int64, err)
	}

	// Pause one job for an hour: list shows who and until when, and queueing is refused.
	if response, body := post(projectPath+"/jobs/hello/pause", url.Values{"window": {"1h"}, "back": {projectPath}}); response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != projectPath+"?done=paused" {
		t.Fatalf("pause hello: status=%d location=%q body=%s", response.StatusCode, response.Header.Get("Location"), body)
	}
	job := listJobs()["hello"]
	if job["paused"] != true || !strings.HasPrefix(job["paused_by"].(string), "web:") || job["pause_ends_at"] == nil {
		t.Fatalf("paused hello in list: %v", job)
	}
	if response, body := post(projectPath+"/jobs/hello/runs", url.Values{}); response.StatusCode != http.StatusConflict || !strings.Contains(body, "paused") {
		t.Fatalf("queue paused hello: status=%d body=%s", response.StatusCode, body)
	}
	if response, _ := post(projectPath+"/jobs/hello/resume", url.Values{"back": {projectPath}}); response.StatusCode != http.StatusSeeOther || listJobs()["hello"]["paused"] != false {
		t.Fatalf("resume hello: status=%d", response.StatusCode)
	}

	// Pause all needs confirmation, then pauses every job until resumed.
	if response, body := post(projectPath+"/pause", url.Values{"window": {"open"}}); response.StatusCode != http.StatusOK || !strings.Contains(body, "Pause all 2 jobs?") {
		t.Fatalf("unconfirmed pause all: status=%d body=%s", response.StatusCode, body)
	}
	if response, _ := post(projectPath+"/pause", url.Values{"window": {"open"}, "confirm": {"1"}}); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("pause all: status=%d", response.StatusCode)
	}
	for name, job := range listJobs() {
		if job["paused"] != true || job["pause_ends_at"] != nil || !strings.HasPrefix(job["paused_by"].(string), "web:") {
			t.Fatalf("after pause all, %s: %v", name, job)
		}
	}
	if response, _ := post(projectPath+"/resume", url.Values{}); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("resume all: status=%d", response.StatusCode)
	}
	for name, job := range listJobs() {
		if job["paused"] != false || job["paused_by"] != nil {
			t.Fatalf("after resume all, %s: %v", name, job)
		}
	}

	// The web token is still not an API credential.
	if response, _ := remoteRequest(t, client, http.MethodGet, "http://"+address+"/v1/projects", webToken, nil); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("web token on /v1/ status=%d", response.StatusCode)
	}
}
