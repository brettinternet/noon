package web

import (
	"bytes"
	"embed"
	"errors"
	"html/template"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/store"
)

const (
	projectRunLimit = 50
	logPageBytes    = 256 << 10
)

//go:embed templates/*.html
var templateFiles embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"stamp":       stamp,
	"status":      statusInfo,
	"duration":    app.DurationText,
	"ansi":        renderANSI,
	"home":        homePath,
	"oldestFirst": oldestFirst,
	"anyPaused": func(jobs []app.Job) bool {
		return slices.ContainsFunc(jobs, func(job app.Job) bool { return job.Paused })
	},
	"anyPausable":     func(jobs []app.Job) bool { return slices.ContainsFunc(jobs, pausable) },
	"pausable":        pausable,
	"confirmCancel":   confirmCancel,
	"confirmPauseJob": confirmPauseJob,
	"confirmPauseAll": confirmPauseAll,
	"temporaryJobs":   temporaryJobs,
	"pausedBy":        pausedBy,
	"dict":            dict,
	"pct":             func(value float64) string { return strconv.FormatFloat(value, 'f', 2, 64) + "%" },
}).ParseFS(templateFiles, "templates/*.html"))

// layout carries what every page shows around its main content.
type layout struct {
	Title    string
	Version  string
	Assets   string
	SignedIn bool
	Seq      uint64
	Failing  int
	Flash    string
	// Crumbs follow "noon" in the header, leading to the page.
	Crumbs []crumb
	Data   any
}

type crumb struct{ Href, Text string }

func (h *Handler) layout(r *http.Request, title string, data any) layout {
	page := layout{Title: title, Version: h.options.Version, Assets: "/assets/" + assetHash + "/", Data: data}
	if r.Method == http.MethodGet {
		page.Flash = flashes[r.URL.Query().Get("done")]
	}
	if h.signedIn(r) {
		current, _ := h.watcher.state()
		page.SignedIn, page.Seq, page.Failing = true, current.Seq, current.Failing
	}
	return page
}

// render buffers the page so a template error never sends half a page.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, name string, page layout) {
	var body bytes.Buffer
	if err := templates.ExecuteTemplate(&body, name, page); err != nil {
		h.options.Logf("web render failed page=%s: %v", name, err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = body.WriteTo(w)
}

// fail renders an app error with the status the API would use.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusInternalServerError, "Noon could not read its state."
	var appErr *app.Error
	if mapped, text, ok := failStatus(err); ok {
		status, message = mapped, text
	} else if errors.As(err, &appErr) {
		switch {
		case appErr.Code == "not_found":
			status, message = http.StatusNotFound, "Not found."
		case appErr.Usage:
			status, message = http.StatusBadRequest, appErr.Message
		}
	} else if app.IsMissingRun(err) || app.NoDatabase(err) {
		status, message = http.StatusNotFound, "Not found."
	}
	if status == http.StatusInternalServerError {
		h.options.Logf("web request failed path=%s: %v", r.URL.Path, err)
	}
	if r.URL.Path == "/events" {
		http.Error(w, message, status)
		return
	}
	h.render(w, r, status, "error", h.layout(r, message, message))
}

func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	view, err := app.Overview(h.options.Input, h.options.Now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "overview", h.layout(r, "noon", view))
}

type projectPage struct {
	app.ProjectView
	Job      string
	Status   string
	Statuses []string
	Chart    *durationChart
}

func (h *Handler) project(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	page := projectPage{Job: query.Get("job"), Status: query.Get("status"), Statuses: app.RunStatuses}
	var statuses []string
	if page.Status != "" {
		statuses = []string{page.Status}
	}
	view, err := app.EnabledProject(h.options.Input, id, page.Job, statuses, projectRunLimit, h.options.Now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page.ProjectView = view
	for _, job := range view.Dashboard.Jobs {
		if job.Name != page.Job {
			continue
		}
		runs, err := app.FinishedRuns(h.options.StateDir, view.Project.Path, job.Name, chartRuns)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		page.Chart = newDurationChart(job.Name, runs, job.Timeout)
	}
	h.render(w, r, http.StatusOK, "project", h.layout(r, view.Project.Name+" · noon", page))
}

type runPage struct {
	app.RunPageView
	Log    app.LogPage
	Active bool
	// Next is the number of the first line the stream will append.
	Next int64
}

func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	view, err := app.EnabledRun(h.options.Input, h.options.StateDir, id, h.options.Now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	var before *int64
	if value := r.URL.Query().Get("before"); value != "" {
		cursor, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			h.fail(w, r, app.UsageError("before must be a log cursor", ""))
			return
		}
		before = &cursor
	}
	page := runPage{RunPageView: view, Active: view.Run.Status == "queued" || view.Run.Status == "running"}
	page.Log, err = app.RunLogPage(h.options.StateDir, id, before, logPageBytes)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	page.Next = 1
	if n := len(page.Log.Lines); n > 0 {
		page.Next = page.Log.Lines[n-1].Number + 1
	}
	// Only the newest page streams; an earlier page is history.
	page.Active = page.Active && before == nil
	full := h.layout(r, "#"+strconv.FormatInt(id, 10)+" "+view.Run.Job+" · noon", page)
	full.Crumbs = []crumb{{projectPath(view.Project.ID), view.Project.Name}}
	h.render(w, r, http.StatusOK, "run", full)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

// timeValue is a <time> element: an RFC 3339 instant for JS and a fallback
// in the project's timezone for readers without it.
type timeValue struct {
	ISO, Text string
}

// stamp accepts stored timestamps, times, and time pointers.
func stamp(value any, timezone string) *timeValue {
	var at time.Time
	switch v := value.(type) {
	case *string:
		if v == nil {
			return nil
		}
		parsed, err := time.Parse(store.TimestampLayout, *v)
		if err != nil {
			return nil
		}
		at = parsed
	case string:
		parsed, err := time.Parse(store.TimestampLayout, v)
		if err != nil {
			return nil
		}
		at = parsed
	case *time.Time:
		if v == nil {
			return nil
		}
		at = *v
	case time.Time:
		at = v
	default:
		return nil
	}
	if loc, err := time.LoadLocation(timezone); err == nil && timezone != "" {
		at = at.In(loc)
	}
	return &timeValue{ISO: at.Format(time.RFC3339), Text: at.Format("Jan 2 15:04 MST")}
}

type status struct {
	Name, Glyph, Class string
}

// statusInfo pairs a run status with a glyph so color is never the only cue.
func statusInfo(name string) status {
	switch {
	case name == "succeeded" || name == "noop":
		return status{name, "✓", "ok"}
	case app.FailedStatus(name):
		return status{strings.ReplaceAll(name, "_", " "), "✗", "bad"}
	case name == "running":
		return status{name, "●", "running"}
	case name == "queued":
		return status{name, "○", "queued"}
	default:
		return status{name, "–", "muted"}
	}
}

// homeDir shortens displayed paths to "~/...", as the TUI does.
var homeDir, _ = os.UserHomeDir()

func homePath(path string) string {
	if homeDir != "" && (path == homeDir || strings.HasPrefix(path, homeDir+string(filepath.Separator))) {
		return "~" + path[len(homeDir):]
	}
	return path
}

// oldestFirst reverses newest-first runs so strips read left to right in
// time, like the duration chart.
func oldestFirst(runs []app.Run) []app.Run {
	runs = slices.Clone(runs)
	slices.Reverse(runs)
	return runs
}

// pausable is whether a job offers Pause: an enabled, approved, unpaused job.
func pausable(job app.Job) bool {
	return job.Enabled && job.Approval != "needs_approval" && !job.Paused
}

// pausedBy names the surface in a pause requester: "web" for web:ADDR.
func pausedBy(requester string) string {
	surface, _, _ := strings.Cut(requester, ":")
	return surface
}

// dict builds a map from key-value pairs so a template can pass several values.
func dict(pairs ...any) map[string]any {
	result := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, _ := pairs[i].(string)
		result[key] = pairs[i+1]
	}
	return result
}
