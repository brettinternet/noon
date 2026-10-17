package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const maxQueuedRequestsPerJob = 20

var (
	ErrProjectNotEnabled = errors.New("project is not enabled")
	ErrJobNotFound       = errors.New("job not found")
	ErrJobDisabled       = errors.New("job is disabled")
	ErrJobPaused         = errors.New("job is paused")
	ErrNeedsApproval     = errors.New("job needs approval")
	ErrQueueFull         = errors.New("job queue is full")
)

// QueueRunInput is a validated client's queued request. Job facts come from the
// current config snapshot; project opt-in and approval are checked transactionally.
type QueueRunInput struct {
	ProjectID                        int64
	Job, Digest, IdempotencyKey      string
	RequestedBy                      string
	Trigger                          string
	DueAt, Now                       time.Time
	NowFunc                          func() time.Time
	JobExists, JobEnabled, JobShared bool
	RequireUnpaused                  bool
	Coalesce                         bool
	RequestsEach                     bool
	Cooldown                         time.Duration
	Depth                            int
	EventData                        []byte
	EventSource                      string
	EventHeaders                     map[string]string
}

type QueueRunResult struct {
	Run           Run
	Created       bool
	Reason        string
	Scheduler     string
	CooldownUntil string
}

// QueueRun performs idempotency, authorization, due-run merging, queue limits,
// and insert under one SQLite immediate transaction.
func (s *Store) QueueRun(input QueueRunInput) (QueueRunResult, error) {
	if !s.hasQueue {
		return QueueRunResult{}, errors.New("queued runs are unavailable in this state database")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return QueueRunResult{}, fmt.Errorf("begin queued request: %w", err)
	}
	defer tx.Rollback()
	eventPath := ""
	committed := false
	defer func() {
		if !committed && eventPath != "" {
			_ = s.removeEvent(eventPath)
		}
	}()
	if input.NowFunc != nil {
		input.Now = input.NowFunc()
		if input.Coalesce {
			input.DueAt = input.Now
		}
	}

	eligibilityChecked := false
	if input.Trigger == "webhook" {
		if err := s.checkQueueEligibility(tx, input); err != nil {
			return QueueRunResult{}, err
		}
		eligibilityChecked = true
	}
	if input.IdempotencyKey != "" {
		run, err := scanRun(tx.QueryRow(`SELECT `+s.runSelectColumns()+` FROM runs WHERE project_id=? AND job=? AND idempotency_key=?`, input.ProjectID, input.Job, input.IdempotencyKey))
		if err == nil {
			status, statusErr := schedulerStatus(tx, input.Now)
			if statusErr != nil {
				return QueueRunResult{}, statusErr
			}
			return QueueRunResult{Run: run, Created: false, Reason: "duplicate", Scheduler: status}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return QueueRunResult{}, fmt.Errorf("find idempotent run: %w", err)
		}
	}

	if !eligibilityChecked {
		if err := s.checkQueueEligibility(tx, input); err != nil {
			return QueueRunResult{}, err
		}
	}

	if input.Cooldown > 0 {
		cutoff := FormatTime(input.Now.Add(-input.Cooldown))
		run, err := scanRun(tx.QueryRow(`SELECT `+s.runSelectColumns()+` FROM runs WHERE project_id=? AND job=? AND created_at>=? ORDER BY id DESC LIMIT 1`, input.ProjectID, input.Job, cutoff))
		if err == nil {
			createdAt, parseErr := time.Parse(TimestampLayout, run.CreatedAt)
			if parseErr != nil {
				return QueueRunResult{}, fmt.Errorf("parse cooldown run creation time: %w", parseErr)
			}
			until := createdAt.Add(input.Cooldown)
			if until.After(input.Now) {
				status, statusErr := schedulerStatus(tx, input.Now)
				if statusErr != nil {
					return QueueRunResult{}, statusErr
				}
				return QueueRunResult{Run: run, Created: false, Reason: "cooldown", Scheduler: status, CooldownUntil: FormatTime(until)}, nil
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return QueueRunResult{}, fmt.Errorf("find cooldown run: %w", err)
		}
	}

	if input.Coalesce && !input.RequestsEach {
		run, err := scanRun(tx.QueryRow(`SELECT `+s.runSelectColumns()+` FROM runs WHERE project_id=? AND job=? AND status='queued' AND due_at<=? ORDER BY due_at, id LIMIT 1`, input.ProjectID, input.Job, FormatTime(input.Now)))
		if err == nil {
			status, statusErr := schedulerStatus(tx, input.Now)
			if statusErr != nil {
				return QueueRunResult{}, statusErr
			}
			return QueueRunResult{Run: run, Created: false, Reason: "queued", Scheduler: status}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return QueueRunResult{}, fmt.Errorf("find due queued run: %w", err)
		}
	}
	if input.Coalesce && input.RequestsEach {
		var queued int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM runs WHERE project_id=? AND job=? AND status='queued'`, input.ProjectID, input.Job).Scan(&queued); err != nil {
			return QueueRunResult{}, fmt.Errorf("count queued runs: %w", err)
		}
		if queued >= maxQueuedRequestsPerJob {
			return QueueRunResult{}, ErrQueueFull
		}
	}

	var key any
	if input.IdempotencyKey != "" {
		key = input.IdempotencyKey
	}
	trigger := input.Trigger
	if trigger == "" {
		trigger = "queued"
	}
	if trigger != "queued" && trigger != "webhook" {
		return QueueRunResult{}, fmt.Errorf("invalid queued run trigger %q", trigger)
	}
	stamp, due := FormatTime(input.Now), FormatTime(input.DueAt)
	result, err := tx.Exec(`INSERT INTO runs(project_id, job, status, trigger, digest, scheduled_at, due_at, created_at, requested_by, idempotency_key, depth)
VALUES(?, ?, 'queued', ?, ?, ?, ?, ?, ?, ?, ?)`, input.ProjectID, input.Job, trigger, input.Digest, due, due, stamp, input.RequestedBy, key, input.Depth)
	if err != nil {
		return QueueRunResult{}, fmt.Errorf("create queued run: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return QueueRunResult{}, fmt.Errorf("get queued run ID: %w", err)
	}
	if len(input.EventData) > 0 || input.EventSource == "webhook" {
		eventPath, err = s.writeEvent(id, input.EventSource, input.Now, input.EventData, input.EventHeaders)
		if err != nil {
			return QueueRunResult{}, err
		}
		if _, err := tx.Exec(`UPDATE runs SET event_path=? WHERE id=?`, eventPath, id); err != nil {
			return QueueRunResult{}, fmt.Errorf("store queued event path: %w", err)
		}
	}
	run, err := scanRun(tx.QueryRow(`SELECT `+s.runSelectColumns()+` FROM runs WHERE id=?`, id))
	if err != nil {
		return QueueRunResult{}, fmt.Errorf("read queued run: %w", err)
	}
	status, err := schedulerStatus(tx, input.Now)
	if err != nil {
		return QueueRunResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return QueueRunResult{}, fmt.Errorf("commit queued request: %w", err)
	}
	committed = true
	return QueueRunResult{Run: run, Created: true, Scheduler: status}, nil
}

func (s *Store) checkQueueEligibility(tx *sql.Tx, input QueueRunInput) error {
	var enabled bool
	if err := tx.QueryRow(`SELECT enabled FROM projects WHERE id=?`, input.ProjectID).Scan(&enabled); errors.Is(err, sql.ErrNoRows) || (err == nil && !enabled) {
		return ErrProjectNotEnabled
	} else if err != nil {
		return fmt.Errorf("check project opt-in: %w", err)
	}
	if !input.JobExists {
		return ErrJobNotFound
	}
	if !input.JobEnabled {
		return ErrJobDisabled
	}
	if (input.Trigger == "webhook" || input.RequireUnpaused) && s.hasPauses {
		var paused bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM pauses WHERE project_id=? AND job=?)`, input.ProjectID, input.Job).Scan(&paused); err != nil {
			return fmt.Errorf("check job pause: %w", err)
		}
		if paused {
			return ErrJobPaused
		}
	}
	if input.JobShared {
		var digest string
		if err := tx.QueryRow(`SELECT digest FROM approvals WHERE project_id=? AND job=?`, input.ProjectID, input.Job).Scan(&digest); errors.Is(err, sql.ErrNoRows) || (err == nil && digest != input.Digest) {
			return ErrNeedsApproval
		} else if err != nil {
			return fmt.Errorf("check job approval: %w", err)
		}
	}
	return nil
}

func schedulerStatus(tx *sql.Tx, now time.Time) (string, error) {
	var heartbeat string
	if err := tx.QueryRow(`SELECT heartbeat_at FROM scheduler WHERE id=1`).Scan(&heartbeat); errors.Is(err, sql.ErrNoRows) {
		return "offline", nil
	} else if err != nil {
		return "", fmt.Errorf("read scheduler heartbeat: %w", err)
	}
	at, err := time.Parse(TimestampLayout, heartbeat)
	if err != nil {
		return "offline", nil
	}
	if now.Sub(at) >= 0 && now.Sub(at) < 5*time.Second {
		return "running", nil
	}
	return "offline", nil
}
