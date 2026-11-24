package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
)

const (
	maxActionBytes = 4 << 10
	// actionWriteLimit covers app.Cancel's 15-second wait for a live owner.
	actionWriteLimit = 30 * time.Second
)

// flashes is every result an action can report after its redirect. Pages
// show only these, never text from the request.
var flashes = map[string]string{
	"queued":         "Queued.",
	"already_queued": "Already queued.",
	"cancelled":      "Cancelled.",
	"cancel_pending": "Cancel requested.",
	"finished":       "Already finished.",
	"paused":         "Paused. Queued runs cancelled; running work continues.",
	"still_paused":   "Already paused until resumed.",
	"resumed":        "Resumed. Schedules start from now.",
}

// backPath allows redirects only to viewer pages.
var backPath = regexp.MustCompile(`^/((projects|runs)/[1-9][0-9]*)?$`)

// confirmPage asks before an action that changes nothing until confirmed.
type confirmPage struct {
	Message string
	Action  string
	Fields  map[string]string
	Back    string
}

// action requires a session and a small form body before running next.
func (h *Handler) action(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !h.signedIn(r) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(actionWriteLimit))
		r.Body = http.MaxBytesReader(w, r.Body, maxActionBytes)
		if err := r.ParseForm(); err != nil {
			h.fail(w, r, app.UsageError("invalid form", ""))
			return
		}
		next(w, r)
	}
}

// requester names the viewer client in pause rows and queued runs.
func requester(r *http.Request) string { return "web:" + r.RemoteAddr }

// done redirects to the form's back page, or fallback, with a fixed flash code.
func done(w http.ResponseWriter, r *http.Request, fallback, code string) {
	target := r.PostFormValue("back")
	if !backPath.MatchString(target) {
		target = fallback
	}
	http.Redirect(w, r, target+"?done="+url.QueryEscape(code), http.StatusSeeOther)
}

// confirm renders the server-side confirmation, which resubmits the same
// known fields with confirm=1.
func (h *Handler) confirm(w http.ResponseWriter, r *http.Request, message, fallback string) {
	fields := map[string]string{}
	for _, name := range []string{"when", "in", "at", "window", "back"} {
		if value := r.PostFormValue(name); value != "" {
			fields[name] = value
		}
	}
	back := r.PostFormValue("back")
	if !backPath.MatchString(back) {
		back = fallback
	}
	page := confirmPage{Message: message, Action: r.URL.Path, Fields: fields, Back: back}
	h.render(w, r, http.StatusOK, "confirm", h.layout(r, "Confirm · noon", page))
}

func confirmed(r *http.Request) bool { return r.PostFormValue("confirm") == "1" }

func projectPath(id int64) string { return "/projects/" + strconv.FormatInt(id, 10) }

func (h *Handler) queue(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	when := r.PostFormValue("when")
	if when == "" {
		when = "now"
	}
	result, err := app.QueueEnabledJob(h.options.Input, h.options.StateDir, id, r.PathValue("name"), when, r.PostFormValue("in"), r.PostFormValue("at"), requester(r), "web", h.options.Now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	code := "queued"
	if !result.Created {
		code = "already_queued"
	}
	// Queue always lands on the run it made or matched.
	http.Redirect(w, r, "/runs/"+strconv.FormatInt(result.Run.ID, 10)+"?done="+code, http.StatusSeeOther)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	runPath := "/runs/" + strconv.FormatInt(id, 10)
	result, needsConfirm, err := app.CancelEnabledRun(h.options.StateDir, id, confirmed(r), h.options.Now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if needsConfirm {
		h.confirm(w, r, confirmCancel(id, result.Runs[0].Job), runPath)
		return
	}
	code := "cancelled"
	switch {
	case result.AlreadyFinished:
		code = "finished"
	case result.Pending:
		code = "cancel_pending"
	}
	done(w, r, runPath, code)
}

func (h *Handler) pause(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	job := r.PathValue("name")
	result, err := app.PauseEnabledJobs(h.options.Input, h.options.StateDir, id, job, r.PostFormValue("window"), requester(r), confirmed(r), h.options.Now())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if result.NeedsConfirm {
		message := confirmPauseJob(job)
		if job == "" {
			message = confirmPauseAll(len(result.Jobs), result.EndsTemporary)
		}
		h.confirm(w, r, message, projectPath(id))
		return
	}
	code := "paused"
	if result.Changed == 0 && r.PostFormValue("window") != "open" {
		code = "still_paused"
	}
	done(w, r, projectPath(id), code)
}

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := app.ResumeEnabledJobs(h.options.Input, h.options.StateDir, id, r.PathValue("name"), h.options.Now()); err != nil {
		h.fail(w, r, err)
		return
	}
	done(w, r, projectPath(id), "resumed")
}

// Confirmation text, shared by the server page and the script's dialog.

func confirmCancel(id int64, job string) string {
	return fmt.Sprintf("Stop running #%d %s? Its process group is signalled.", id, job)
}

func confirmPauseJob(job string) string {
	return fmt.Sprintf("Pausing %s ends its temporary schedule. Resume doesn't restore it.", job)
}

func confirmPauseAll(count int, temporary []string) string {
	message := fmt.Sprintf("Pause all %d jobs?", count)
	switch len(temporary) {
	case 0:
	case 1:
		message += fmt.Sprintf(" This ends the temporary schedule of %s; resume doesn't restore it.", temporary[0])
	default:
		message += fmt.Sprintf(" This ends the temporary schedules of %s; resume doesn't restore them.", strings.Join(temporary, ", "))
	}
	return message
}

// temporaryJobs names jobs with a temporary schedule, for the pause-all dialog.
func temporaryJobs(jobs []app.Job) []string {
	var names []string
	for _, job := range jobs {
		if job.TemporarySchedule != nil {
			names = append(names, job.Name)
		}
	}
	return names
}

// failStatus maps an action error to the status the remote API would use.
func failStatus(err error) (int, string, bool) {
	var queueErr *app.QueueError
	if errors.As(err, &queueErr) {
		switch queueErr.Code {
		case "not_found":
			return http.StatusNotFound, "Not found.", true
		case "usage":
			return http.StatusBadRequest, queueErr.Message, true
		case "queue_full":
			return http.StatusTooManyRequests, queueErr.Message, true
		default:
			return http.StatusConflict, queueErr.Message, true
		}
	}
	var appErr *app.Error
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case "not_enabled", "disabled", "ended", "paused", "needs_approval", "recursion", "unsupported_platform":
			return http.StatusConflict, appErr.Message, true
		}
	}
	return 0, "", false
}
