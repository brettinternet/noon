package store

import (
	"database/sql"
	"errors"
	"time"
)

// ProjectApproval is a consistent snapshot of a checkout's opt-in and approvals.
type ProjectApproval struct {
	ID         int64
	Enabled    bool
	EnabledAt  sql.NullString
	ConfigFile sql.NullString
	Approvals  map[string]string
}

// ApprovalForProject reads old databases as disabled without migrating them.
func (s *Store) ApprovalForProject(path string) (ProjectApproval, error) {
	result := ProjectApproval{Approvals: map[string]string{}}
	if !s.hasApprovals {
		return result, nil
	}
	rows, err := s.db.Query(`SELECT p.id, p.enabled, p.enabled_at, p.config_file, a.job, a.digest
 FROM projects p LEFT JOIN approvals a ON a.project_id=p.id WHERE p.path=?`, path)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var job, digest sql.NullString
		if err := rows.Scan(&result.ID, &result.Enabled, &result.EnabledAt, &result.ConfigFile, &job, &digest); err != nil {
			return result, err
		}
		if job.Valid {
			result.Approvals[job.String] = digest.String
		}
	}
	return result, rows.Err()
}

// EnableProject atomically records the validated canonical checkout and replaces
// its approval set, preserving timestamps for unchanged approvals and opt-in.
func (s *Store) EnableProject(path string, configFile *string, digests map[string]string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := FormatTime(now)
	wasEnabled := false
	if err := tx.QueryRow(`SELECT enabled FROM projects WHERE path=?`, path).Scan(&wasEnabled); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(`INSERT INTO projects(path, created_at, enabled, enabled_at, config_file) VALUES(?, ?, 1, ?, ?)
 ON CONFLICT(path) DO UPDATE SET enabled=1,
 enabled_at=CASE WHEN projects.enabled=0 THEN excluded.enabled_at ELSE projects.enabled_at END,
 config_file=excluded.config_file`, path, stamp, stamp, configFile)
	if err != nil {
		return err
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM projects WHERE path=?`, path).Scan(&id); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT job, digest FROM approvals WHERE project_id=?`, id)
	if err != nil {
		return err
	}
	previous := make(map[string]string)
	for rows.Next() {
		var name, digest string
		if err := rows.Scan(&name, &digest); err != nil {
			rows.Close()
			return err
		}
		previous[name] = digest
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return err
	}
	var obsolete, reset []string
	for name, digest := range previous {
		requested, ok := digests[name]
		if !ok {
			obsolete = append(obsolete, name)
		} else if requested != digest {
			reset = append(reset, name)
		}
	}
	for name := range digests {
		if _, ok := previous[name]; !ok {
			reset = append(reset, name)
		}
	}
	for _, name := range append(reset, obsolete...) {
		if _, err := tx.Exec("DELETE FROM temporary_schedules WHERE project_id=? AND job=?", id, name); err != nil {
			return err
		}
	}
	for _, name := range obsolete {
		if _, err := tx.Exec(`DELETE FROM approvals WHERE project_id=? AND job=?`, id, name); err != nil {
			return err
		}
	}
	if !wasEnabled {
		if _, err := tx.Exec(`DELETE FROM job_state WHERE project_id=?`, id); err != nil {
			return err
		}
	} else {
		for _, name := range append(reset, obsolete...) {
			if _, err := tx.Exec(`DELETE FROM job_state WHERE project_id=? AND job=?`, id, name); err != nil {
				return err
			}
		}
	}
	for name, digest := range digests {
		if _, err := tx.Exec(`INSERT INTO approvals(project_id, job, digest, approved_at) VALUES(?, ?, ?, ?)
 ON CONFLICT(project_id, job) DO UPDATE SET digest=excluded.digest, approved_at=excluded.approved_at
 WHERE approvals.digest<>excluded.digest`, id, name, digest, stamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// EnableAddedJob enables scheduling while approving only the newly written job.
func (s *Store) EnableAddedJob(path string, configFile *string, name, digest string, shared bool, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stamp := FormatTime(now)
	wasEnabled := false
	if err := tx.QueryRow(`SELECT enabled FROM projects WHERE path=?`, path).Scan(&wasEnabled); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = tx.Exec(`INSERT INTO projects(path,created_at,enabled,enabled_at,config_file) VALUES(?,?,1,?,?) ON CONFLICT(path) DO UPDATE SET enabled=1, enabled_at=CASE WHEN projects.enabled=0 THEN excluded.enabled_at ELSE projects.enabled_at END, config_file=excluded.config_file`, path, stamp, stamp, configFile)
	if err != nil {
		return err
	}
	var id int64
	if err = tx.QueryRow(`SELECT id FROM projects WHERE path=?`, path).Scan(&id); err != nil {
		return err
	}
	if shared {
		if _, err = tx.Exec(`INSERT INTO approvals(project_id,job,digest,approved_at) VALUES(?,?,?,?) ON CONFLICT(project_id,job) DO UPDATE SET digest=excluded.digest,approved_at=excluded.approved_at WHERE approvals.digest<>excluded.digest`, id, name, digest, stamp); err != nil {
			return err
		}
	}
	// Activation resets every watermark, as EnableProject does, so jobs never
	// catch up on occurrences that passed while the project was disabled.
	if wasEnabled {
		_, err = tx.Exec(`DELETE FROM job_state WHERE project_id=? AND job=?`, id, name)
	} else {
		_, err = tx.Exec(`DELETE FROM job_state WHERE project_id=?`, id)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

// SetPaused changes background eligibility and resets schedule state on resume.
func (s *Store) SetPaused(project, job string, paused bool, now time.Time) error {
	_, err := s.SetJobsPaused(project, []string{job}, paused, now)
	return err
}

// SetJobsPaused pauses the selected jobs open-ended, or resumes them, atomically
// and returns the number changed.
func (s *Store) SetJobsPaused(project string, jobs []string, paused bool, now time.Time) (int, error) {
	if paused {
		return s.PauseJobs(project, jobs, PauseWindow{}, now)
	}
	return s.updatePauses(project, jobs, func(tx *sql.Tx, id int64, job string) (int64, error) {
		result, err := tx.Exec(`DELETE FROM pauses WHERE project_id=? AND job=?`, id, job)
		if err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM job_state WHERE project_id=? AND job=?`, id, job); err != nil {
			return 0, err
		}
		return result.RowsAffected()
	}, now)
}

// PauseWindow bounds a pause. A nil Start begins it now; a nil End leaves it open-ended.
type PauseWindow struct{ Start, End *time.Time }

// PauseJobs atomically records the window for the selected jobs and returns the
// number of rows written. An open-ended pause is never shortened, a timed pause
// replaces a timed one, and an open-ended pause replaces any row.
func (s *Store) PauseJobs(project string, jobs []string, window PauseWindow, now time.Time) (int, error) {
	var start, end any
	if window.Start != nil {
		start = FormatTime(*window.Start)
	}
	if window.End != nil {
		end = FormatTime(*window.End)
	}
	active, activeArgs := s.pausePredicate(now, false)
	return s.updatePauses(project, jobs, func(tx *sql.Tx, id int64, job string) (int64, error) {
		var wasActive bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM pauses WHERE project_id=? AND job=? AND `+active+`)`, append([]any{id, job}, activeArgs...)...).Scan(&wasActive); err != nil {
			return 0, err
		}
		result, err := tx.Exec(`INSERT INTO pauses(project_id,job,paused_at,starts_at,resumes_at) VALUES(?,?,?,?,?)
 ON CONFLICT(project_id,job) DO UPDATE SET paused_at=excluded.paused_at, starts_at=excluded.starts_at, resumes_at=excluded.resumes_at
 WHERE pauses.resumes_at IS NOT NULL`, id, job, FormatTime(now), start, end)
		if err != nil {
			return 0, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		var isActive bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM pauses WHERE project_id=? AND job=? AND `+active+`)`, append([]any{id, job}, activeArgs...)...).Scan(&isActive); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM temporary_schedules WHERE project_id=? AND job=?`, id, job); err != nil {
			return 0, err
		}
		if isActive {
			_, err = tx.Exec(`UPDATE runs SET status='cancelled',finished_at=?,error='job paused' WHERE project_id=? AND job=? AND status='queued'`, FormatTime(now), id, job)
		} else if wasActive {
			// Deferring an active pause ends it now, as resume does.
			_, err = tx.Exec(`DELETE FROM job_state WHERE project_id=? AND job=?`, id, job)
		}
		return changed, err
	}, now)
}

// updatePauses applies change to each job in one transaction after removing
// expired rows, whose expiry resets schedule state as resume does.
func (s *Store) updatePauses(project string, jobs []string, change func(*sql.Tx, int64, string) (int64, error), now time.Time) (int, error) {
	if len(jobs) == 0 {
		return 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`SELECT id FROM projects WHERE path=?`, project).Scan(&id); err != nil {
		return 0, err
	}
	changed := 0
	for _, job := range jobs {
		result, err := tx.Exec(`DELETE FROM pauses WHERE project_id=? AND job=? AND resumes_at<=?`, id, job, FormatTime(now))
		if err != nil {
			return 0, err
		}
		if expired, err := result.RowsAffected(); err != nil {
			return 0, err
		} else if expired > 0 {
			if _, err := tx.Exec(`DELETE FROM job_state WHERE project_id=? AND job=?`, id, job); err != nil {
				return 0, err
			}
		}
		count, err := change(tx, id, job)
		if err != nil {
			return 0, err
		}
		changed += int(count)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

// pausePredicate is the one pause predicate for every reader: a row is active
// from starts_at (NULL: since paused_at) until resumes_at (NULL: open-ended).
// With pending, it also matches windows that have not started. Rows in stores
// older than pause windows are open-ended and active.
func (s *Store) pausePredicate(now time.Time, pending bool) (string, []any) {
	if !s.hasPauseWindows {
		return "1", nil
	}
	stamp := FormatTime(now)
	if pending {
		return "(resumes_at IS NULL OR resumes_at>?)", []any{stamp}
	}
	return "(starts_at IS NULL OR starts_at<=?) AND (resumes_at IS NULL OR resumes_at>?)", []any{stamp, stamp}
}

// Pause is an unexpired pause row. Active is false for a window that has not started.
type Pause struct {
	Active           bool
	StartsAt, EndsAt *time.Time
}

// Pauses returns the project's unexpired pauses by job, even without a scheduler.
func (s *Store) Pauses(project string, now time.Time) (map[string]Pause, error) {
	result := map[string]Pause{}
	if !s.hasPauses {
		return result, nil
	}
	active, args := s.pausePredicate(now, false)
	unexpired, unexpiredArgs := s.pausePredicate(now, true)
	columns := "NULL, NULL"
	if s.hasPauseWindows {
		columns = "starts_at, resumes_at"
	}
	args = append(append(args, project), unexpiredArgs...)
	rows, err := s.db.Query(`SELECT pauses.job, `+columns+`, `+active+` FROM pauses JOIN projects ON projects.id=pauses.project_id WHERE projects.path=? AND `+unexpired, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var start, end sql.NullString
		var pause Pause
		if err := rows.Scan(&name, &start, &end, &pause.Active); err != nil {
			return nil, err
		}
		for _, item := range []struct {
			text sql.NullString
			dest **time.Time
		}{{start, &pause.StartsAt}, {end, &pause.EndsAt}} {
			if item.text.Valid {
				at, err := time.Parse(TimestampLayout, item.text.String)
				if err != nil {
					return nil, err
				}
				*item.dest = &at
			}
		}
		result[name] = pause
	}
	return result, rows.Err()
}

// DisableProject revokes opt-in and approvals without deleting run history.
func (s *Store) DisableProject(path string, now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM approvals WHERE project_id IN (SELECT id FROM projects WHERE path=?)`, path); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT DISTINCT runs.project_id, runs.job FROM runs JOIN projects ON projects.id=runs.project_id WHERE projects.path=? AND runs.status='queued'`, path)
	if err != nil {
		return err
	}
	var queuedJobs []JobStateKey
	for rows.Next() {
		var key JobStateKey
		if err := rows.Scan(&key.ProjectID, &key.Job); err != nil {
			_ = rows.Close()
			return err
		}
		queuedJobs = append(queuedJobs, key)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	stamp := FormatTime(now)
	if _, err := tx.Exec(`UPDATE runs SET status='cancelled', finished_at=?, error='project disabled before start' WHERE project_id IN (SELECT id FROM projects WHERE path=?) AND status='queued'`, stamp, path); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM temporary_schedules WHERE project_id IN (SELECT id FROM projects WHERE path=?)`, path); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE projects SET enabled=0 WHERE path=?`, path); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, key := range queuedJobs {
		if err := s.pruneJobRuns(key.ProjectID, key.Job); err != nil {
			return err
		}
	}
	return nil
}
