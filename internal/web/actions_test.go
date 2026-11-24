package web

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

type actionFixture struct {
	h           *Handler
	cookie      *http.Cookie
	stateDir    string
	project     string
	id          int64
	disabledID  int64
	running     int64
	disabledRun int64
	now         time.Time
}

// newActionFixture enables a project with hello, slow, and a temporary schedule
// on slow, a running slow run, and a disabled project with its own run.
func newActionFixture(t *testing.T) actionFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state")
	userConfigDir := filepath.Join(root, "user-config")
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	ids := map[string]int64{}
	// The test process owns the running runs, so an unconfirmed cancel finds them live.
	identity, err := runner.ProcessStartIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"on", "off"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "noon.yaml"), []byte("jobs:\n  hello:\n    run: 'true'\n  slow:\n    run: sleep 30\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := config.Load(config.Input{CWD: dir, UserConfigDir: userConfigDir, StateDirDefault: stateDir})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := app.Enable(cfg, now); err != nil {
			t.Fatal(err)
		}
		state, err := store.Open(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		records, err := state.ProjectRecords()
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range records {
			if record.Path == dir {
				ids[name] = record.ID
			}
		}
		run, err := state.CreateRun(store.NewRun{ProjectID: ids[name], Job: "slow", Trigger: "manual", Digest: cfg.Jobs["slow"].Digest, Now: now, OwnerPID: os.Getpid(), OwnerStart: identity})
		if err != nil {
			t.Fatal(err)
		}
		ids[name+" run"] = run.ID
		if name == "on" {
			schedule := store.TemporarySchedule{ProjectID: ids[name], Job: "slow", Schedule: "every 1h", Until: now.Add(24 * time.Hour), NextAt: now.Add(time.Hour), RequestedBy: "cli", CreatedAt: now, Digest: cfg.Jobs["slow"].Digest}
			if _, _, _, err := state.CreateTemporarySchedule(schedule, store.QueueRunInput{ProjectID: ids[name], Job: "slow", Digest: cfg.Jobs["slow"].Digest, Now: now, JobExists: true, JobEnabled: true, JobShared: true}); err != nil {
				t.Fatal(err)
			}
		}
		if err := state.Close(); err != nil {
			t.Fatal(err)
		}
		if name == "off" {
			if _, err := app.Disable(cfg, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	h := New(Options{
		Token:    webToken,
		Input:    config.Input{CWD: root, UserConfigDir: userConfigDir, StateDirDefault: stateDir},
		StateDir: stateDir,
		Version:  "test",
		Logf:     t.Logf,
		Now:      func() time.Time { return now },
	})
	t.Cleanup(h.Shutdown)
	return actionFixture{
		h: h, cookie: sessionCookie(t, h, loginRequest(string(webToken))), stateDir: stateDir,
		project: filepath.Join(root, "on"), id: ids["on"], disabledID: ids["off"],
		running: ids["on run"], disabledRun: ids["off run"], now: now,
	}
}

func (f actionFixture) post(path string, form url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	return serve(f.h, r)
}

func actionPaths(project, run int64) []string {
	p := fmt.Sprintf("/projects/%d", project)
	return []string{
		fmt.Sprintf("/runs/%d/cancel", run),
		p + "/jobs/hello/runs",
		p + "/jobs/hello/pause",
		p + "/jobs/hello/resume",
		p + "/pause",
		p + "/resume",
	}
}

func TestActionRoutesRequireSessionSameOriginAndPost(t *testing.T) {
	f := newActionFixture(t)
	form := url.Values{"window": {"open"}}
	for _, path := range actionPaths(f.id, f.running) {
		if response := f.post(path, form, nil); response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/login" {
			t.Fatalf("no session POST %s: status=%d location=%q", path, response.Code, response.Header().Get("Location"))
		}
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Sec-Fetch-Site", "cross-site")
		r.AddCookie(f.cookie)
		if response := serve(f.h, r); response.Code != http.StatusForbidden {
			t.Fatalf("cross-site POST %s: status=%d", path, response.Code)
		}
		get := httptest.NewRequest(http.MethodGet, path, nil)
		get.AddCookie(f.cookie)
		if response := serve(f.h, get); response.Code != http.StatusMethodNotAllowed || !strings.Contains(response.Header().Get("Allow"), "POST") {
			t.Fatalf("GET %s: status=%d allow=%q", path, response.Code, response.Header().Get("Allow"))
		}
	}
	for _, path := range append(actionPaths(f.disabledID, f.disabledRun), actionPaths(999, 999)...) {
		if response := f.post(path, url.Values{"window": {"open"}, "confirm": {"1"}}, f.cookie); response.Code != http.StatusNotFound {
			t.Fatalf("POST %s: status=%d, want 404", path, response.Code)
		}
	}
	if response := f.post(fmt.Sprintf("/projects/%d/jobs/nope/pause", f.id), form, f.cookie); response.Code != http.StatusNotFound {
		t.Fatalf("unknown job pause: status=%d", response.Code)
	}
	// Decision-6: the viewer never changes what may run.
	p := fmt.Sprintf("/projects/%d", f.id)
	for _, path := range []string{p + "/enable", p + "/disable", p + "/approve", p + "/jobs", p + "/jobs/hello", p + "/jobs/hello/approve", p + "/jobs/hello/enable", p + "/jobs/hello/disable", p + "/jobs/hello/edit", p + "/jobs/hello/remove"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			r := httptest.NewRequest(method, path, nil)
			r.AddCookie(f.cookie)
			if response := serve(f.h, r); response.Code != http.StatusNotFound && response.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s: status=%d", method, path, response.Code)
			}
		}
	}
}

// Ending a temporary schedule, pausing a project, and stopping a running run
// change nothing until the form carries confirm=1.
func TestRiskyActionsNeedConfirmation(t *testing.T) {
	f := newActionFixture(t)
	pauseAll := fmt.Sprintf("/projects/%d/pause", f.id)
	pauseSlow := fmt.Sprintf("/projects/%d/jobs/slow/pause", f.id)
	cancel := fmt.Sprintf("/runs/%d/cancel", f.running)
	for path, want := range map[string]string{pauseAll: "Pause all 2 jobs? This ends the temporary schedule of slow", pauseSlow: "Pausing slow ends its temporary schedule", cancel: fmt.Sprintf("Stop running #%d slow?", f.running)} {
		response := f.post(path, url.Values{"window": {"1h"}}, f.cookie)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), want) || !strings.Contains(response.Body.String(), `name="confirm" value="1"`) {
			t.Fatalf("unconfirmed %s: status=%d body=%s", path, response.Code, response.Body)
		}
	}
	state, err := store.OpenExisting(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	pauses, err := state.Pauses(f.project, f.now)
	if err != nil || len(pauses) != 0 {
		t.Fatalf("pauses after unconfirmed actions = %+v err=%v", pauses, err)
	}
	schedules, err := state.TemporarySchedules(f.project)
	if err != nil || len(schedules) != 1 {
		t.Fatalf("temporary schedules after unconfirmed actions = %+v err=%v", schedules, err)
	}
	run, err := state.RunByID(f.running)
	if err != nil || run.Status != "running" || run.CancelRequestedAt.Valid {
		t.Fatalf("running run after unconfirmed cancel = %+v err=%v", run, err)
	}

	// A plain job pause needs no confirmation and records the viewer.
	response := f.post(fmt.Sprintf("/projects/%d/jobs/hello/pause", f.id), url.Values{"window": {"1h"}, "back": {fmt.Sprintf("/projects/%d", f.id)}}, f.cookie)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != fmt.Sprintf("/projects/%d?done=paused", f.id) {
		t.Fatalf("pause hello: status=%d location=%q body=%s", response.Code, response.Header().Get("Location"), response.Body)
	}
	pauses, err = state.Pauses(f.project, f.now)
	if err != nil || len(pauses) != 1 || !strings.HasPrefix(pauses["hello"].RequestedBy, "web:") {
		t.Fatalf("pauses after hello pause = %+v err=%v", pauses, err)
	}
}
