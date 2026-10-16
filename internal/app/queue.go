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
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return QueuedRun{}, err
	}
	defer state.Close()
	projectID, err := state.RegisterProject(cfg.Project, now)
	if err != nil {
		return QueuedRun{}, err
	}
	job, exists := cfg.Jobs[name]
	result, err := state.QueueRun(store.QueueRunInput{
		ProjectID: projectID, Job: name, Digest: job.Digest, IdempotencyKey: key,
		RequestedBy: requestedBy, Trigger: options.Trigger, DueAt: dueAt, Now: now, NowFunc: nowFunc,
		JobExists: exists, JobEnabled: job.Enabled, JobShared: job.Shared,
		Coalesce: coalesce, Cooldown: job.Cooldown, Depth: depth, RequireUnpaused: options.RequireUnpaused,
		EventData: options.EventData, EventSource: options.EventSource, EventHeaders: options.EventHeaders,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrProjectNotEnabled):
			err = &QueueError{Code: "not_enabled", Message: "project is not enabled for background runs", Hint: "Run noon enable"}
		case errors.Is(err, store.ErrJobNotFound):
			err = &QueueError{Code: "not_found", Message: fmt.Sprintf("job %q not found", name), Hint: "Run noon run to list jobs"}
		case errors.Is(err, store.ErrJobDisabled):
			err = &QueueError{Code: "disabled", Message: fmt.Sprintf("job %q is disabled", name), Hint: fmt.Sprintf("Enable it in %q", job.Source)}
		case errors.Is(err, store.ErrJobPaused):
			err = &QueueError{Code: "paused", Message: fmt.Sprintf("job %q is paused", name)}
		case errors.Is(err, store.ErrNeedsApproval):
			err = &QueueError{Code: "needs_approval", Message: fmt.Sprintf("job %q needs approval", name), Hint: "Run noon enable"}
		}
		return QueuedRun{}, err
	}
	return QueuedRun{SchemaVersion: 1, Run: RunFromStore(state, result.Run), Created: result.Created, Reason: result.Reason, Scheduler: result.Scheduler, CooldownUntil: result.CooldownUntil}, nil
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
		var due time.Time
		switch {
		case len(value) == len("15:04"):
			clock, err := time.ParseInLocation("15:04", value, location)
			if err != nil {
				return time.Time{}, invalidQueueAt(value)
			}
			local := now.In(location)
			due = time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
			if !due.After(now) {
				due = time.Date(local.Year(), local.Month(), local.Day()+1, clock.Hour(), clock.Minute(), 0, 0, location)
			}
		case len(value) == len("2006-01-02 15:04"):
			parsed, err := time.ParseInLocation("2006-01-02 15:04", value, location)
			if err != nil {
				return time.Time{}, invalidQueueAt(value)
			}
			due = parsed
		default:
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return time.Time{}, invalidQueueAt(value)
			}
			due = parsed
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
	return mapRuns(state, stored), nil
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
	return mapRuns(state, stored), nil
}

func mapRuns(state *store.Store, stored []store.Run) []Run {
	runs := make([]Run, len(stored))
	for i, run := range stored {
		runs[i] = RunFromStore(state, run)
	}
	return runs
}

type CancelResult struct {
	SchemaVersion   int   `json:"schema_version"`
	Runs            []Run `json:"runs"`
	Pending         bool  `json:"pending"`
	AlreadyFinished bool  `json:"-"`
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
	alreadyFinished := false
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
		stored, err = state.RequestCancelJob(project, job, now)
		if err != nil {
			return CancelResult{}, err
		}
	}
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
			return CancelResult{SchemaVersion: 1, Runs: mapRuns(state, stored), AlreadyFinished: alreadyFinished}, nil
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
			return CancelResult{SchemaVersion: 1, Runs: mapRuns(state, stored), Pending: pending, AlreadyFinished: alreadyFinished}, nil
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
