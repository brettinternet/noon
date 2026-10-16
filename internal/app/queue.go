package app

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,200}$`)

func ValidIdempotencyKey(value string) bool { return idempotencyKeyPattern.MatchString(value) }

type QueueError struct {
	Code, Message, Hint string
}

func (e *QueueError) Error() string { return e.Message }

type QueuedRun struct {
	Run       Run    `json:"run"`
	Created   bool   `json:"created"`
	Reason    string `json:"reason,omitempty"`
	Scheduler string `json:"scheduler"`
}

// QueueRun is the shared client operation for durable one-shot requests.
func QueueRun(cfg config.Config, name, key string, dueAt, now time.Time, nowFunc func() time.Time, coalesce bool, requestedBy string) (QueuedRun, error) {
	if key != "" && !ValidIdempotencyKey(key) {
		return QueuedRun{}, &QueueError{Code: "usage", Message: "--key must be 1–200 characters of [A-Za-z0-9._:-]"}
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
		RequestedBy: requestedBy, DueAt: dueAt, Now: now, NowFunc: nowFunc,
		JobExists: exists, JobEnabled: job.Enabled, JobShared: job.Shared,
		Coalesce: coalesce,
	})
	if err != nil {
		switch {
		case errors.Is(err, store.ErrProjectNotEnabled):
			err = &QueueError{Code: "not_enabled", Message: "project is not enabled for background runs", Hint: "Run noon enable"}
		case errors.Is(err, store.ErrJobNotFound):
			err = &QueueError{Code: "not_found", Message: fmt.Sprintf("job %q not found", name), Hint: "Run noon run to list jobs"}
		case errors.Is(err, store.ErrJobDisabled):
			err = &QueueError{Code: "disabled", Message: fmt.Sprintf("job %q is disabled", name), Hint: fmt.Sprintf("Enable it in %q", job.Source)}
		case errors.Is(err, store.ErrNeedsApproval):
			err = &QueueError{Code: "needs_approval", Message: fmt.Sprintf("job %q needs approval", name), Hint: "Run noon enable"}
		}
		return QueuedRun{}, err
	}
	return QueuedRun{Run: RunFromStore(result.Run), Created: result.Created, Reason: result.Reason, Scheduler: result.Scheduler}, nil
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
	return mapRuns(stored), nil
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
	return mapRuns(stored), nil
}

func mapRuns(stored []store.Run) []Run {
	runs := make([]Run, len(stored))
	for i, run := range stored {
		runs[i] = RunFromStore(run)
	}
	return runs
}

type CancelResult struct {
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
			}
			if stored[i].Status == "running" {
				pendingIDs = append(pendingIDs, stored[i].ID)
			}
		}
		if len(pendingIDs) == 0 {
			return CancelResult{Runs: mapRuns(stored), AlreadyFinished: alreadyFinished}, nil
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
				}
				pending = pending || stored[i].Status == "running"
			}
			return CancelResult{Runs: mapRuns(stored), Pending: pending, AlreadyFinished: alreadyFinished}, nil
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
