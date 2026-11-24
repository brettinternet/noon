package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/store"
)

func TestWebViewer(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, configHome, stateHome, stateDir := remoteDirectories(t, root)
	environment := isolatedEnvironment(home, configHome, stateHome, stateDir)
	apiToken := "noon-web-test-api-token-0123456789-abcdef"
	webToken := "noon-web-test-web-token-0123456789-abcdef"
	userDir := filepath.Join(configHome, "noon")
	if err := os.MkdirAll(userDir, 0o700); err != nil {
		t.Fatal(err)
	}
	apiFile, webFile := filepath.Join(userDir, "api-token"), filepath.Join(userDir, "web-token")
	writeRemoteFile(t, apiFile, apiToken+"\n", 0o600)
	writeRemoteFile(t, webFile, webToken+"\n", 0o600)
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	get := func(address, path string, cookie *http.Cookie) (*http.Response, string) {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, "http://"+address+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if cookie != nil {
			request.AddCookie(cookie)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		return response, string(body)
	}

	// Without a web token the viewer's routes don't exist.
	address, stop := startRemoteScheduler(t, noonBinary, home, environment, "--listen", "127.0.0.1:0", "--api-token-file", apiFile)
	for _, path := range []string{"/", "/login", "/events"} {
		if response, _ := get(address, path, nil); response.StatusCode != http.StatusNotFound {
			t.Fatalf("web off: %s status=%d", path, response.StatusCode)
		}
	}
	stop()
	// The viewer needs no API token.
	address, stop = startRemoteScheduler(t, noonBinary, home, environment, "--listen", "127.0.0.1:0", "--web-token-file", webFile)
	if response, _ := get(address, "/login", nil); response.StatusCode != http.StatusOK {
		t.Fatalf("web-only login status=%d", response.StatusCode)
	}
	stop()

	project := filepath.Join(root, "webproject")
	hidden := filepath.Join(root, "hiddenproject")
	for _, dir := range []string{project, hidden} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeRemoteFile(t, filepath.Join(project, "noon.yaml"), `version: 1
jobs:
  hello:
    schedule: every 1h
    run: [sh, -c, 'echo first-line; sleep 2; echo last-line']
  boom:
    schedule: every 1h
    run: [sh, -c, 'echo boom; exit 3']
  env:
    run: [env]
`, 0o600)
	writeRemoteFile(t, filepath.Join(hidden, "noon.yaml"), "version: 1\njobs:\n  secretjob:\n    run: \"true\"\n", 0o600)
	for _, dir := range []string{project, hidden} {
		if code, output := runRemoteCommand(noonBinary, dir, environment, "enable"); code != 0 {
			t.Fatalf("enable %s exit=%d output=%s", dir, code, output)
		}
	}
	if code, output := runRemoteCommand(noonBinary, hidden, environment, "disable"); code != 0 {
		t.Fatalf("disable exit=%d output=%s", code, output)
	}
	state, err := store.Open(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	records, err := state.ProjectRecords()
	if err != nil {
		t.Fatal(err)
	}
	var hiddenID int64
	for _, record := range records {
		if record.Path == hidden {
			hiddenID = record.ID
		}
	}
	hiddenRun, err := state.CreateRun(store.NewRun{ProjectID: hiddenID, Job: "secretjob", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: os.Getpid(), OwnerStart: "web-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := state.FinishRun(hiddenRun.ID, hiddenID, "secretjob", store.Outcome{Status: "succeeded", FinishedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}

	writeRemoteFile(t, filepath.Join(userDir, "config.yaml"), "serve:\n  listen: 127.0.0.1:0\n  api_token_file: api-token\n  web_token_file: web-token\n", 0o600)
	address, stop = startRemoteScheduler(t, noonBinary, home, environment)
	queue := func(job string) int64 {
		t.Helper()
		code, output := runRemoteCommand(noonBinary, project, environment, "run", job, "--detach", "--json")
		var queued struct {
			Run struct {
				ID int64 `json:"id"`
			} `json:"run"`
		}
		if err := json.Unmarshal([]byte(output), &queued); code != 0 || err != nil || queued.Run.ID == 0 {
			t.Fatalf("queue %s exit=%d output=%s", job, code, output)
		}
		return queued.Run.ID
	}
	wait := func(id int64) string {
		t.Helper()
		code, output := runRemoteCommand(noonBinary, project, environment, "logs", strconv.FormatInt(id, 10), "--follow")
		if code != 0 && code != 3 {
			t.Fatalf("follow run %d exit=%d output=%s", id, code, output)
		}
		return output
	}
	wait(queue("boom"))
	envLog := wait(queue("env"))

	// A proxy preserves Host and says the client used HTTPS.
	form := url.Values{"token": {webToken}}.Encode()
	request, _ := http.NewRequest(http.MethodPost, "http://"+address+"/login", strings.NewReader(form))
	request.Host = "noon.example.com"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("Origin", "https://noon.example.com")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	var session *http.Cookie
	for _, cookie := range response.Cookies() {
		if cookie.Name == "noon_session" {
			session = cookie
		}
	}
	if response.StatusCode != http.StatusSeeOther || session == nil || !session.Secure || !session.HttpOnly {
		t.Fatalf("proxied login status=%d cookies=%v", response.StatusCode, response.Cookies())
	}
	var pages []string

	response, overview := get(address, "/", session)
	pages = append(pages, overview)
	failing, upcoming, ok := strings.Cut(overview, "<h2>Upcoming</h2>")
	_, failing, _ = strings.Cut(failing, "<h2>Failing</h2>")
	if response.StatusCode != http.StatusOK || !ok || !strings.Contains(overview, "webproject") || strings.Contains(overview, "hiddenproject") ||
		!strings.Contains(failing, "<td>boom</td>") || !strings.Contains(upcoming, "<td>hello</td>") || !strings.Contains(upcoming, "<td>boom</td>") {
		t.Fatalf("overview status=%d body=%s", response.StatusCode, overview)
	}
	for _, path := range []string{"/projects/" + strconv.FormatInt(hiddenID, 10), "/runs/" + strconv.FormatInt(hiddenRun.ID, 10), "/projects/999999", "/runs/999999"} {
		if response, body := get(address, path, session); response.StatusCode != http.StatusNotFound || strings.Contains(body, "secretjob") {
			t.Fatalf("%s status=%d", path, response.StatusCode)
		}
	}

	// The web token is not an API credential.
	for _, path := range []string{"/v1/projects", "/mcp"} {
		response, _ := remoteRequest(t, client, http.MethodGet, "http://"+address+path, webToken, nil)
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("web token on %s status=%d", path, response.StatusCode)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream := openEvents(t, ctx, address, "/events", session)
	<-stream // The current state arrives first.
	queued := time.Now()
	id := queue("hello")
	if event := nextEvent(t, stream, "state", 3*time.Second); event.at.Sub(queued) > 3*time.Second {
		t.Fatalf("queued run reached the stream after %s", event.at.Sub(queued))
	}

	logs := openEvents(t, ctx, address, "/events?run="+strconv.FormatInt(id, 10)+"&after=0", session)
	line := nextEvent(t, logs, "log", 5*time.Second)
	end := nextEvent(t, logs, "end", 10*time.Second)
	if !strings.Contains(line.data, "first-line") || end.at.Sub(line.at) < time.Second {
		t.Fatalf("log line %q at %s did not precede the run's end at %s", line.data, line.at, end.at)
	}

	response, runPage := get(address, "/runs/"+strconv.FormatInt(id, 10), session)
	pages = append(pages, runPage)
	if response.StatusCode != http.StatusOK || !strings.Contains(runPage, `id="L2"`) || !strings.Contains(runPage, "last-line") {
		t.Fatalf("run page status=%d body=%s", response.StatusCode, runPage)
	}

	// Open streams must not hold shutdown.
	started := time.Now()
	output := stop()
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("serve took %s to stop with /events open", elapsed)
	}
	if !strings.Contains(output, "web viewer url=http://") {
		t.Fatalf("startup log does not name the web URL: %s", output)
	}
	for _, token := range []string{apiToken, webToken} {
		for label, text := range map[string]string{"pages": strings.Join(pages, "\n"), "cookie": session.Value, "scheduler output": output, "job environment": envLog} {
			if strings.Contains(text, token) {
				t.Fatalf("%s contains a token", label)
			}
		}
	}
}

type serverEvent struct {
	name, data string
	at         time.Time
}

// openEvents reads an SSE stream into a channel until ctx ends.
func openEvents(t *testing.T, ctx context.Context, address, path string, session *http.Cookie) <-chan serverEvent {
	t.Helper()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+path, nil)
	request.AddCookie(session)
	response, err := http.DefaultClient.Do(request)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("open %s: %v %v", path, err, response)
	}
	events := make(chan serverEvent, 64)
	go func() {
		defer response.Body.Close()
		scanner := bufio.NewScanner(response.Body)
		var event serverEvent
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				event.data = strings.TrimPrefix(line, "data: ")
			case line == "" && event.name != "":
				event.at = time.Now()
				events <- event
				event = serverEvent{}
			}
		}
	}()
	return events
}

func nextEvent(t *testing.T, events <-chan serverEvent, name string, timeout time.Duration) serverEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case event := <-events:
			if event.name == name {
				return event
			}
		case <-deadline:
			t.Fatalf("no %s event within %s", name, timeout)
		}
	}
}
