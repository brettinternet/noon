package app

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/schedule"
	"github.com/brettinternet/noon/internal/store"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,200}$`)

func ValidIdempotencyKey(value string) bool { return idempotencyKeyPattern.MatchString(value) }

type QueueError struct {
	Code, Message, Hint string
}

func (e *QueueError) Error() string { return e.Message }

func UnsupportedPlatformError(job config.Job, platform config.Platform) *Error {
	return &Error{
		Code:    "unsupported_platform",
		Message: fmt.Sprintf("job %q does not support the current platform", job.Name),
		Hint:    job.PlatformHint(platform),
	}
}

type QueuedRun struct {
	SchemaVersion int    `json:"schema_version"`
	Run           Run    `json:"run"`
	Created       bool   `json:"created"`
	Reason        string `json:"reason,omitempty"`
	Scheduler     string `json:"scheduler"`
	CooldownUntil string `json:"cooldown_until,omitempty"`
}

type QueueOptions struct {
	RequireUnpaused bool
	EventData       []byte
	EventSource     string
	EventHeaders    map[string]string
	Trigger         string
}

const MaxWebhookBodyBytes = store.MaxWebhookBodyBytes

// QueueRun is the shared client operation for durable one-shot requests.
func QueueRun(cfg config.Config, name, key string, dueAt, now time.Time, nowFunc func() time.Time, coalesce bool, requestedBy string, queueOptions ...QueueOptions) (QueuedRun, error) {
	var options QueueOptions
	if len(queueOptions) > 0 {
		options = queueOptions[0]
	}
	if key != "" && !ValidIdempotencyKey(key) {
		return QueuedRun{}, &QueueError{Code: "usage", Message: "--key must be 1–200 characters of [A-Za-z0-9._:-]"}
	}
	// Webhook runs start a new chain at depth 0; the scheduler's own environment
	// is not the requester's.
	depth := 0
	if options.Trigger != "webhook" {
		if err := CheckRecursion(cfg.Project, name, os.Environ()); err != nil {
			return QueuedRun{}, err
		}
		depth = RequestDepth(os.Environ())
	}
	if options.EventSource == "webhook" {
		if len(options.EventData) > MaxWebhookBodyBytes {
			return QueuedRun{}, UsageError("webhook body must be at most 1 MiB", "Send a smaller request body")
		}
	} else if len(options.EventData) > 0 {
		if err := ValidateEventData(options.EventData); err != nil {
			return QueuedRun{}, err
		}
	}
	job, exists := cfg.Jobs[name]
	if exists && !job.MatchesPlatform(cfg.Platform) {
		failure := UnsupportedPlatformError(job, cfg.Platform)
		return QueuedRun{}, &QueueError{Code: failure.Code, Message: failure.Message, Hint: failure.Hint}
	}
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return QueuedRun{}, err
	}
	defer state.Close()
	projectID, err := state.RegisterProject(cfg.Project, now)
	if err != nil {
		return QueuedRun{}, err
	}
	result, err := state.QueueRun(store.QueueRunInput{
		ProjectID: projectID, Job: name, Digest: job.Digest, IdempotencyKey: key,
		RequestedBy: requestedBy, Trigger: options.Trigger, DueAt: dueAt, Now: now, NowFunc: nowFunc,
		JobExists: exists, JobEnabled: job.Enabled, JobShared: job.Shared, JobUntil: job.Until,
		Coalesce: coalesce, Cooldown: job.Cooldown, RequestsEach: job.Requests == "each", Depth: depth, RequireUnpaused: options.RequireUnpaused,
		EventData: options.EventData, EventSource: options.EventSource, EventHeaders: options.EventHeaders,
	})
	if err != nil {
		err = queueRequestError(err, cfg, name, job, now, nowFunc)
		return QueuedRun{}, err
	}
	return QueuedRun{SchemaVersion: 1, Run: RunFromStore(state, result.Run), Created: result.Created, Reason: result.Reason, Scheduler: result.Scheduler, CooldownUntil: result.CooldownUntil}, nil
}

func queueRequestError(err error, cfg config.Config, name string, job config.Job, now time.Time, nowFunc func() time.Time) error {
	switch {
	case errors.Is(err, store.ErrProjectNotEnabled):
		err = &QueueError{Code: "not_enabled", Message: "project is not enabled for background runs", Hint: "Run noon enable"}
	case errors.Is(err, store.ErrJobNotFound):
		err = &QueueError{Code: "not_found", Message: fmt.Sprintf("job %q not found", name), Hint: "Run noon run to list jobs"}
	case errors.Is(err, store.ErrJobEnded):
		tense := "ends"
		checkNow := now
		if nowFunc != nil {
			checkNow = nowFunc()
		}
		if job.Ended(checkNow) {
			tense = "ended"
		}
		loc, _ := time.LoadLocation(cfg.Timezone)
		if loc == nil {
			loc = time.Local
		}
		err = &QueueError{Code: "ended", Message: fmt.Sprintf("job %q %s at %s", name, tense, job.Until.In(loc).Format(time.RFC3339)), Hint: fmt.Sprintf("Edit until in %q", job.Source)}
	case errors.Is(err, store.ErrJobDisabled):
		err = &QueueError{Code: "disabled", Message: fmt.Sprintf("job %q is disabled", name), Hint: fmt.Sprintf("Enable it in %q", job.Source)}
	case errors.Is(err, store.ErrJobPaused):
		err = &QueueError{Code: "paused", Message: fmt.Sprintf("job %q is paused", name)}
	case errors.Is(err, store.ErrNeedsApproval):
		err = &QueueError{Code: "needs_approval", Message: fmt.Sprintf("job %q needs approval", name), Hint: "Run noon enable"}
	case errors.Is(err, store.ErrQueueFull):
		err = &QueueError{Code: "queue_full", Message: fmt.Sprintf("job %q queue is full", name), Hint: fmt.Sprintf("Run noon cancel %s to clear queued runs", name)}
	}
	return err
}

type TemporaryResult struct {
	SchemaVersion     int                     `json:"schema_version"`
	TemporarySchedule store.TemporarySchedule `json:"temporary_schedule"`
	Created           bool                    `json:"created"`
	Reason            string                  `json:"reason,omitempty"`
	Scheduler         string                  `json:"scheduler"`
}

// ResolveTime resolves a one-shot time: HH:MM as its next occurrence after now,
// YYYY-MM-DD HH:MM in loc with the schedule DST rules (first repeated instant,
// first instant after a gap), or RFC 3339. It does not reject past times.
func ResolveTime(value string, now time.Time, loc *time.Location) (time.Time, error) {
	if _, err := time.Parse("15:04", value); err != nil {
		return config.ParseUntil(value, loc)
	}
	local := now.In(loc)
	resolved, err := config.ParseUntil(local.Format("2006-01-02")+" "+value, loc)
	if err == nil && !resolved.After(now) {
		resolved, err = config.ParseUntil(local.AddDate(0, 0, 1).Format("2006-01-02")+" "+value, loc)
	}
	return resolved, err
}

// ResolveEnd resolves exactly one of a relative (for) or absolute (until) end
// once, in loc. A duration is at least 1m and rounds up to a whole minute;
// HH:MM means its next occurrence. The end must be after now.
func ResolveEnd(forValue, untilValue string, now time.Time, loc *time.Location) (time.Time, error) {
	if (forValue == "") == (untilValue == "") {
		return time.Time{}, errors.New("exactly one of for or until is required")
	}
	var until time.Time
	if forValue != "" {
		duration, err := schedule.ParseDuration(forValue)
		if err != nil || duration < time.Minute {
			return time.Time{}, errors.New("for must be a duration of at least 1m")
		}
		until = now.Add(duration)
		if rounded := until.Truncate(time.Minute); !rounded.Equal(until) {
			until = rounded.Add(time.Minute)
		}
	} else {
		var err error
		until, err = ResolveTime(untilValue, now, loc)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid until time %q: use HH:MM, YYYY-MM-DD HH:MM, or RFC 3339", untilValue)
		}
	}
	if !until.After(now) {
		return time.Time{}, errors.New("until must be after now")
	}
	return until.In(loc), nil
}

// TemporaryTimes validates the common schedule and bounded end syntax.
func TemporaryTimes(text, forValue, untilValue string, now time.Time, loc *time.Location) (time.Time, time.Time, error) {
	parsed, err := schedule.Parse(text)
	if err != nil {
		return time.Time{}, time.Time{}, UsageError(err.Error(), "Use the same schedule grammar as job config")
	}
	fail := func(message string) (time.Time, time.Time, error) {
		return time.Time{}, time.Time{}, UsageError(message, "Use --schedule 'every 10m' --for 2h")
	}
	if (forValue == "") == (untilValue == "") {
		return fail("a temporary schedule requires exactly one of for or until")
	}
	until, err := ResolveEnd(forValue, untilValue, now, loc)
	if err != nil {
		return fail(err.Error())
	}
	if until.Sub(now) > 7*24*time.Hour {
		return fail("temporary schedules must end within 7 days (including minute rounding)")
	}
	next := parsed.Next(now, loc)
	if next.IsZero() || !next.Before(until) {
		return fail("schedule has no occurrence before its end")
	}
	return until, next, nil
}

// ScheduleTemporary only requeues an existing eligible job; it never edits config.
func ScheduleTemporary(cfg config.Config, name, text, forValue, untilValue, key, requestedBy, source string, data []byte, now time.Time) (TemporaryResult, error) {
	if key != "" && !ValidIdempotencyKey(key) {
		return TemporaryResult{}, UsageError("invalid key", "Use 1–200 characters of [A-Za-z0-9._:-]")
	}
	if err := CheckRecursion(cfg.Project, name, os.Environ()); err != nil {
		return TemporaryResult{}, err
	}
	if len(data) > 0 {
		if err := ValidateEventData(data); err != nil {
			return TemporaryResult{}, err
		}
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return TemporaryResult{}, err
	}
	until, next, err := TemporaryTimes(text, forValue, untilValue, now, loc)
	if err != nil {
		return TemporaryResult{}, err
	}
	job, exists := cfg.Jobs[name]
	if exists && !job.MatchesPlatform(cfg.Platform) {
		return TemporaryResult{}, UnsupportedPlatformError(job, cfg.Platform)
	}
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return TemporaryResult{}, err
	}
	defer state.Close()
	id, err := state.RegisterProject(cfg.Project, now)
	if err != nil {
		return TemporaryResult{}, err
	}
	value, created, status, err := state.CreateTemporarySchedule(store.TemporarySchedule{ProjectID: id, Job: name, Schedule: text, Until: until, NextAt: next, RequestedBy: requestedBy, CreatedAt: now, Key: key, Digest: job.Digest, Data: data, EventSource: source}, store.QueueRunInput{ProjectID: id, Job: name, Digest: job.Digest, JobExists: exists, JobEnabled: job.Enabled, JobShared: job.Shared, JobUntil: job.Until, Now: now})
	if errors.Is(err, store.ErrAlreadyScheduled) {
		return TemporaryResult{}, &QueueError{Code: "already_scheduled", Message: fmt.Sprintf("job %q already has a temporary schedule", name), Hint: "noon cancel " + name}
	}
	if err != nil {
		return TemporaryResult{}, queueRequestError(err, cfg, name, job, now, nil)
	}
	reason := ""
	if !created {
		reason = "duplicate"
	}
	return TemporaryResult{SchemaVersion: 1, TemporarySchedule: value.In(loc), Created: created, Reason: reason, Scheduler: status}, nil
}

// QueueDueAt applies the same one-shot time syntax as the CLI.
func QueueDueAt(mode, inValue, atValue string, now time.Time, location *time.Location) (time.Time, error) {
	switch mode {
	case "detach":
		return now, nil
	case "in":
		duration, err := schedule.ParseDuration(inValue)
		if err != nil {
			return time.Time{}, err
		}
		if duration < time.Second {
			return time.Time{}, fmt.Errorf("--in must be at least 1s")
		}
		return now.Add(duration), nil
	case "at":
		value := atValue
		due, err := ResolveTime(value, now, location)
		if err != nil {
			return time.Time{}, invalidQueueAt(value)
		}
		if due.Before(now) {
			return time.Time{}, fmt.Errorf("--at time %s is in the past", value)
		}
		return due, nil
	default:
		return time.Time{}, fmt.Errorf("missing queued run time")
	}
}

func invalidQueueAt(value string) error {
	return fmt.Errorf("invalid --at time %q: use HH:MM, YYYY-MM-DD HH:MM, or RFC 3339", value)
}

// QueuedRuns lists queued work for a project without creating state.
func QueuedRuns(stateDir, project string) ([]Run, error) {
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return []Run{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer state.Close()
	stored, err := state.QueuedRunsForProject(project)
	if err != nil {
		return nil, err
	}
	runs, err := mapRuns(state, stored)
	return runs, err
}

// ActiveRuns lists queued and running work for a project without creating state.
func ActiveRuns(stateDir, project string) ([]Run, error) {
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return []Run{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer state.Close()
	stored, err := state.ActiveRunsForProject(project)
	if err != nil {
		return nil, err
	}
	return mapRuns(state, stored)
}

func mapRuns(state *store.Store, stored []store.Run) ([]Run, error) {
	runs := make([]Run, len(stored))
	for i, run := range stored {
		mapped, err := inspectAndMapRun(state, run)
		if err != nil {
			return nil, err
		}
		runs[i] = mapped
	}
	return runs, nil
}

type CancelResult struct {
	SchemaVersion int   `json:"schema_version"`
	Runs          []Run `json:"runs"`
	Pending       bool  `json:"pending"`
	// EndedTemporarySchedules names jobs whose temporary schedule this request ended.
	EndedTemporarySchedules []string `json:"ended_temporary_schedules,omitempty"`
	AlreadyFinished         bool     `json:"-"`
}

// Cancel requests cancellation and waits up to 15 seconds for live owners.
func Cancel(stateDir, project string, runID *int64, job string, now time.Time) (CancelResult, error) {
	deadline := time.Now().Add(15 * time.Second)
	state, err := store.Open(stateDir)
	if err != nil {
		return CancelResult{}, err
	}
	defer state.Close()
	var stored []store.Run
	alreadyFinished, ended := false, false
	if runID != nil {
		previous, err := state.RunByID(*runID)
		if err != nil {
			return CancelResult{}, err
		}
		alreadyFinished = previous.Status != "queued" && previous.Status != "running"
		run, err := state.RequestCancelRun(*runID, now)
		if err != nil {
			return CancelResult{}, err
		}
		stored = []store.Run{run}
	} else {
		stored, ended, err = state.RequestCancelJob(project, job, now)
		if err != nil {
			return CancelResult{}, err
		}
	}
	result, err := waitForCancellation(state, stored, alreadyFinished, deadline)
	if ended {
		result.EndedTemporarySchedules = []string{job}
	}
	return result, err
}

// CancellationOutcome retains per-run results when some requests fail.
type CancellationOutcome struct {
	Target string
	ID     int64
	Result CancelResult
	Err    error
}

// CancelRuns records every request before waiting for any live process owner.
type CancelJob struct{ Project, Job string }

// CancelRuns records every job and ID stop request before waiting for any owner.
func CancelRuns(stateDir string, ids []int64, now time.Time, jobs ...CancelJob) ([]CancellationOutcome, error) {
	outcomes := make([]CancellationOutcome, 0, len(ids)+len(jobs))
	if len(ids) == 0 && len(jobs) == 0 {
		return outcomes, nil
	}
	state, err := store.Open(stateDir)
	if err != nil {
		return nil, err
	}
	defer state.Close()
	type pending struct {
		index    int
		runs     []store.Run
		finished bool
		ended    bool
	}
	var waiting []pending
	seen := map[int64]bool{}
	for _, job := range jobs {
		runs, ended, err := state.RequestCancelJob(job.Project, job.Job, now)
		index := len(outcomes)
		outcomes = append(outcomes, CancellationOutcome{Target: job.Job, Err: err})
		if err != nil {
			continue
		}
		unique := []store.Run{}
		for _, run := range runs {
			if !seen[run.ID] {
				seen[run.ID] = true
				unique = append(unique, run)
			}
		}
		waiting = append(waiting, pending{index: index, runs: unique, ended: ended})
	}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		index := len(outcomes)
		outcomes = append(outcomes, CancellationOutcome{ID: id})
		previous, err := state.RunByID(id)
		if err != nil {
			outcomes[index].Err = err
			continue
		}
		run, err := state.RequestCancelRun(id, now)
		if err != nil {
			outcomes[index].Err = err
			continue
		}
		waiting = append(waiting, pending{index: index, runs: []store.Run{run}, finished: previous.Status != "queued" && previous.Status != "running"})
	}
	deadline := time.Now().Add(15 * time.Second)
	for _, item := range waiting {
		outcomes[item.index].Result, outcomes[item.index].Err = waitForCancellation(state, item.runs, item.finished, deadline)
		if item.ended {
			outcomes[item.index].Result.EndedTemporarySchedules = []string{outcomes[item.index].Target}
		}
	}
	return outcomes, nil
}

// EndTemporarySchedules invalidates durable requests during explicit config removal,
// even when no scheduler is running. An absent store stays absent.
func EndTemporarySchedules(stateDir, project string, jobs []string) error {
	existing, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return nil
	}
	if err != nil {
		return err
	}
	values, err := existing.TemporarySchedules(project)
	closeErr := existing.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	if len(values) == 0 {
		return nil
	}
	state, err := store.Open(stateDir)
	if err != nil {
		return err
	}
	defer state.Close()
	return state.DeleteTemporarySchedules(project, jobs)
}

func waitForCancellation(state *store.Store, stored []store.Run, alreadyFinished bool, deadline time.Time) (CancelResult, error) {
	for {
		pendingIDs := make([]int64, 0, len(stored))
		for i := range stored {
			if current, err := state.RunByID(stored[i].ID); err == nil {
				stored[i] = current
			} else if !errors.Is(err, sql.ErrNoRows) {
				return CancelResult{}, err
			} else {
				// Retention may prune this run as cancellation makes it terminal.
				stored[i].LogPath = sql.NullString{}
				stored[i].EventPath = sql.NullString{}
			}
			if stored[i].Status == "running" {
				pendingIDs = append(pendingIDs, stored[i].ID)
			}
		}
		if len(pendingIDs) == 0 {
			runs, err := mapRuns(state, stored)
			if err != nil {
				return CancelResult{}, err
			}
			return CancelResult{SchemaVersion: 1, Runs: runs, AlreadyFinished: alreadyFinished}, nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if err := state.RecoverRuns(pendingIDs, time.Now(), time.Nanosecond); err != nil {
				return CancelResult{}, err
			}
			pending := false
			for i := range stored {
				if current, err := state.RunByID(stored[i].ID); err == nil {
					stored[i] = current
				} else if !errors.Is(err, sql.ErrNoRows) {
					return CancelResult{}, err
				} else {
					stored[i].LogPath = sql.NullString{}
					stored[i].EventPath = sql.NullString{}
				}
				pending = pending || stored[i].Status == "running"
			}
			runs, err := mapRuns(state, stored)
			if err != nil {
				return CancelResult{}, err
			}
			return CancelResult{SchemaVersion: 1, Runs: runs, Pending: pending, AlreadyFinished: alreadyFinished}, nil
		}
		budget := min(remaining-100*time.Millisecond, 10*time.Second)
		if budget <= 0 {
			budget = time.Nanosecond
		}
		if err := state.RecoverRuns(pendingIDs, time.Now(), budget); err != nil {
			return CancelResult{}, err
		}
		if wait := min(100*time.Millisecond, time.Until(deadline)); wait > 0 {
			time.Sleep(wait)
		}
	}
}
