package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	ErrProjectNotEnabled = errors.New("project is not enabled")
	ErrJobNotFound       = errors.New("job not found")
	ErrJobDisabled       = errors.New("job is disabled")
	ErrNeedsApproval     = errors.New("job needs approval")
)

// QueueRunInput is a validated client's queued request. Job facts come from the
// current config snapshot; project opt-in and approval are checked transactionally.
type QueueRunInput struct {
	ProjectID                        int64
	Job, Digest, IdempotencyKey      string
	RequestedBy                      string
	DueAt, Now                       time.Time
	NowFunc                          func() time.Time
	JobExists, JobEnabled, JobShared bool
	Coalesce                         bool
}

type QueueRunResult struct {
	Run       Run
	Created   bool
	Reason    string
	Scheduler string
}

// QueueRun performs idempotency, authorization, due-run coalescing, and insert
// under one SQLite immediate transaction.
func (s *Store) QueueRun(input QueueRunInput) (QueueRunResult, error) {
	if !s.hasQueue {
		return QueueRunResult{}, errors.New("queued runs are unavailable in this state database")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return QueueRunResult{}, fmt.Errorf("begin queued request: %w", err)
	}
	defer tx.Rollback()
	if input.NowFunc != nil {
		input.Now = input.NowFunc()
		if input.Coalesce {
			input.DueAt = input.Now
		}
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

	var enabled bool
	if err := tx.QueryRow(`SELECT enabled FROM projects WHERE id=?`, input.ProjectID).Scan(&enabled); errors.Is(err, sql.ErrNoRows) || (err == nil && !enabled) {
		return QueueRunResult{}, ErrProjectNotEnabled
	} else if err != nil {
		return QueueRunResult{}, fmt.Errorf("check project opt-in: %w", err)
	}
	if !input.JobExists {
		return QueueRunResult{}, ErrJobNotFound
	}
	if !input.JobEnabled {
		return QueueRunResult{}, ErrJobDisabled
	}
	if input.JobShared {
		var digest string
		if err := tx.QueryRow(`SELECT digest FROM approvals WHERE project_id=? AND job=?`, input.ProjectID, input.Job).Scan(&digest); errors.Is(err, sql.ErrNoRows) || (err == nil && digest != input.Digest) {
			return QueueRunResult{}, ErrNeedsApproval
		} else if err != nil {
			return QueueRunResult{}, fmt.Errorf("check job approval: %w", err)
		}
	}

	if input.Coalesce {
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

	var key any
	if input.IdempotencyKey != "" {
		key = input.IdempotencyKey
	}
	stamp, due := FormatTime(input.Now), FormatTime(input.DueAt)
	result, err := tx.Exec(`INSERT INTO runs(project_id, job, status, trigger, digest, scheduled_at, due_at, created_at, requested_by, idempotency_key)
VALUES(?, ?, 'queued', 'queued', ?, ?, ?, ?, ?, ?)`, input.ProjectID, input.Job, input.Digest, due, due, stamp, input.RequestedBy, key)
	if err != nil {
		return QueueRunResult{}, fmt.Errorf("create queued run: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return QueueRunResult{}, fmt.Errorf("get queued run ID: %w", err)
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
	return QueueRunResult{Run: run, Created: true, Scheduler: status}, nil
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
