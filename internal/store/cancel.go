package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RequestCancelRun cancels queued work immediately or records a durable request
// for the process owner of a running run.
func (s *Store) RequestCancelRun(id int64, now time.Time) (Run, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Run{}, fmt.Errorf("begin run cancellation: %w", err)
	}
	defer tx.Rollback()
	run, err := scanRun(tx.QueryRow(`SELECT `+s.runSelectColumns()+` FROM runs WHERE id=?`, id))
	if err != nil {
		return Run{}, err
	}
	queued := run.Status == "queued"
	switch run.Status {
	case "queued":
		stamp := FormatTime(now)
		if _, err := tx.Exec(`UPDATE runs SET status='cancelled', finished_at=?, error='cancelled before start' WHERE id=? AND status='queued'`, stamp, id); err != nil {
			return Run{}, fmt.Errorf("cancel queued run: %w", err)
		}
		run.Status = "cancelled"
		run.FinishedAt = sql.NullString{String: stamp, Valid: true}
		run.Error = sql.NullString{String: "cancelled before start", Valid: true}
	case "running":
		stamp := FormatTime(now)
		if _, err := tx.Exec(`UPDATE runs SET cancel_requested_at=COALESCE(cancel_requested_at, ?) WHERE id=? AND status='running'`, stamp, id); err != nil {
			return Run{}, fmt.Errorf("request running cancellation: %w", err)
		}
		run.CancelRequestedAt = sql.NullString{String: stamp, Valid: true}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, fmt.Errorf("commit run cancellation: %w", err)
	}
	if queued {
		if err := s.pruneJobRuns(run.ProjectID, run.Job); err != nil {
			return Run{}, err
		}
	}
	return run, nil
}

// RequestCancelJob cancels every queued and running run for a project's job.
// It also deletes the job's temporary schedule and reports whether one existed.
func (s *Store) RequestCancelJob(projectPath, job string, now time.Time) ([]Run, bool, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, false, fmt.Errorf("begin job cancellation: %w", err)
	}
	defer tx.Rollback()
	deleted, err := tx.Exec(`DELETE FROM temporary_schedules WHERE project_id IN (SELECT id FROM projects WHERE path=?) AND job=?`, projectPath, job)
	if err != nil {
		return nil, false, false, err
	}
	count, err := deleted.RowsAffected()
	if err != nil {
		return nil, false, false, err
	}
	ended, err := tx.Exec(`UPDATE temporary_jobs SET ended_at=COALESCE(ended_at, MIN(until_at, ?)) WHERE project_id IN (SELECT id FROM projects WHERE path=?) AND name=?`, FormatTime(now), projectPath, job)
	if err != nil {
		return nil, false, false, err
	}
	endedCount, err := ended.RowsAffected()
	if err != nil {
		return nil, false, false, err
	}
	rows, err := tx.Query(`SELECT `+s.qualifiedRunSelectColumns()+` FROM runs JOIN projects ON projects.id=runs.project_id WHERE projects.path=? AND runs.job=? AND runs.status IN ('queued','running') ORDER BY runs.id`, projectPath, job)
	if err != nil {
		return nil, false, false, fmt.Errorf("find active job runs: %w", err)
	}
	var runs []Run
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			_ = rows.Close()
			return nil, false, false, scanErr
		}
		runs = append(runs, run)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, false, false, err
	}
	stamp := FormatTime(now)
	cancelledJobs := map[JobStateKey]bool{}
	for i := range runs {
		run := &runs[i]
		if run.Status == "queued" {
			if _, err := tx.Exec(`UPDATE runs SET status='cancelled', finished_at=?, error='cancelled before start' WHERE id=? AND status='queued'`, stamp, run.ID); err != nil {
				return nil, false, false, fmt.Errorf("cancel queued run: %w", err)
			}
			run.Status = "cancelled"
			run.FinishedAt = sql.NullString{String: stamp, Valid: true}
			run.Error = sql.NullString{String: "cancelled before start", Valid: true}
			cancelledJobs[JobStateKey{ProjectID: run.ProjectID, Job: run.Job}] = true
		} else {
			if _, err := tx.Exec(`UPDATE runs SET cancel_requested_at=COALESCE(cancel_requested_at, ?) WHERE id=? AND status='running'`, stamp, run.ID); err != nil {
				return nil, false, false, fmt.Errorf("request running cancellation: %w", err)
			}
			run.CancelRequestedAt = sql.NullString{String: stamp, Valid: true}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, false, fmt.Errorf("commit job cancellation: %w", err)
	}
	for key := range cancelledJobs {
		if err := s.pruneJobRuns(key.ProjectID, key.Job); err != nil {
			return nil, false, false, err
		}
	}
	return runs, count > 0, endedCount > 0, nil
}

// ActiveRunsForProject lists current work without creating a state database.
func (s *Store) ActiveRunsForProject(project string) ([]Run, error) {
	order := `runs.created_at, runs.id`
	if s.hasScheduler {
		order = `COALESCE(runs.due_at, runs.created_at), runs.id`
	}
	return s.projectRuns(project, `runs.status IN ('queued','running') ORDER BY `+order)
}

// QueuedRunsForProject returns queued work in dispatch order.
func (s *Store) QueuedRunsForProject(project string) ([]Run, error) {
	order := `runs.created_at, runs.id`
	if s.hasScheduler {
		order = `runs.due_at, runs.id`
	}
	return s.projectRuns(project, `runs.status='queued' ORDER BY `+order)
}

func (s *Store) projectRuns(project, order string) ([]Run, error) {
	rows, err := s.db.Query(`SELECT `+s.qualifiedRunSelectColumns()+` FROM runs JOIN projects ON projects.id=runs.project_id WHERE projects.path=? AND `+order, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		run, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func (s *Store) pruneJobRuns(projectID int64, job string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin run retention: %w", err)
	}
	defer tx.Rollback()
	if err := pruneRuns(tx, s.dir, projectID, job); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run retention: %w", err)
	}
	return nil
}
