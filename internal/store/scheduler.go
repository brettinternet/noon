package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// EnabledProject is the database snapshot used by one scheduler tick.
type EnabledProject struct {
	ID         int64
	Path       string
	ConfigFile sql.NullString
	Approvals  map[string]string
	Paused     map[string]bool
}

type ProjectRecord struct {
	ID         int64
	Path       string
	Enabled    bool
	ConfigFile sql.NullString
}

type PersistedJobState struct {
	ProjectID   int64
	Job         string
	ScheduleKey string
	NextAt      string
}

type ScheduledRun struct {
	ID             int64
	ProjectID      int64
	Job            string
	Status         string
	Trigger        string
	Digest         string
	ScheduledAt    sql.NullString
	DueAt          sql.NullString
	AgentSessionID sql.NullString
}

type SchedulerSnapshot struct {
	Projects  []EnabledProject
	States    []PersistedJobState
	Runs      []ScheduledRun
	Temporary []TemporarySchedule
}

type JobStateChange struct {
	ProjectID   int64
	Job         string
	ScheduleKey string
	NextAt      time.Time
}

type JobStateKey struct {
	ProjectID int64
	Job       string
}

type ScheduledRunInput struct {
	ProjectID            int64
	Job, Status, Trigger string
	Digest               string
	ScheduledAt, DueAt   time.Time
	RequestedBy          string
	EventData            []byte
	EventSource          string
}

type SchedulerPlan struct {
	Upsert          []JobStateChange
	Remove          []JobStateKey
	Create          []ScheduledRunInput
	TemporaryUpsert []TemporarySchedule
	TemporaryRemove []JobStateKey
}

type DispatchPlan struct {
	Start           []int64
	Expire          []int64
	Cancel          []int64
	AgentSessionIDs map[int64]string
}

type SchedulerRecord struct {
	PID           int
	StartIdentity string
	Version       string
	Commit        string
	StartedAt     string
	HeartbeatAt   string
}

// ProjectRecords lists every project ID so adapters can distinguish a disabled
// known project from an unknown one.
func (s *Store) ProjectRecords() ([]ProjectRecord, error) {
	if !s.hasScheduler {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT id, path, enabled, config_file FROM projects ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var projects []ProjectRecord
	for rows.Next() {
		var project ProjectRecord
		if err := rows.Scan(&project.ID, &project.Path, &project.Enabled, &project.ConfigFile); err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}
	return projects, rows.Err()
}

// ReadSchedulerSnapshot returns a consistent view for reload and test setup.
func (s *Store) ReadSchedulerSnapshot() (SchedulerSnapshot, error) {
	if !s.hasScheduler {
		return SchedulerSnapshot{}, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return SchedulerSnapshot{}, err
	}
	defer tx.Rollback()
	return schedulerSnapshot(tx, s.hasPauses, s.hasAgentSessionID, s.hasTemporary)
}

// SchedulerInfo returns the last scheduler heartbeat, or no record for old state
// databases and databases that have not yet run noon serve.
func (s *Store) SchedulerInfo() (SchedulerRecord, bool, error) {
	if !s.hasScheduler {
		return SchedulerRecord{}, false, nil
	}
	var record SchedulerRecord
	commit := `''`
	if s.hasSchedulerCommit {
		commit = `build_commit`
	}
	err := s.db.QueryRow(`SELECT pid, start_identity, version, started_at, heartbeat_at, `+commit+` FROM scheduler WHERE id=1`).Scan(&record.PID, &record.StartIdentity, &record.Version, &record.StartedAt, &record.HeartbeatAt, &record.Commit)
	if errors.Is(err, sql.ErrNoRows) {
		return SchedulerRecord{}, false, nil
	}
	return record, err == nil, err
}

// ProjectConfigError returns the latest validate-first reload diagnostic.
func (s *Store) ProjectConfigError(path string) (string, bool, error) {
	if !s.hasScheduler {
		return "", false, nil
	}
	var value sql.NullString
	err := s.db.QueryRow(`SELECT config_error FROM projects WHERE path=?`, path).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value.String, value.Valid, err
}

// ScheduleStatesForProject reads durable next-occurrence watermarks for clients.
func (s *Store) ScheduleStatesForProject(path string) ([]PersistedJobState, error) {
	if !s.hasScheduler {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT j.project_id, j.job, j.schedule_key, j.next_at FROM job_state j JOIN projects p ON p.id=j.project_id WHERE p.path=?`, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []PersistedJobState
	for rows.Next() {
		var state PersistedJobState
		if err := rows.Scan(&state.ProjectID, &state.Job, &state.ScheduleKey, &state.NextAt); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

// SetScheduler claims the singleton scheduler record after taking the OS lock.
func (s *Store) SetScheduler(record SchedulerRecord) error {
	if !s.hasScheduler {
		return errors.New("scheduler state is unavailable")
	}
	_, err := s.db.Exec(`INSERT INTO scheduler(id, pid, start_identity, version, started_at, heartbeat_at, build_commit) VALUES(1, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET pid=excluded.pid, start_identity=excluded.start_identity, version=excluded.version, started_at=excluded.started_at, heartbeat_at=excluded.heartbeat_at, build_commit=excluded.build_commit`, record.PID, record.StartIdentity, record.Version, record.StartedAt, record.HeartbeatAt, record.Commit)
	return err
}

// SetProjectConfigError stores the current validate-first reload error.
func (s *Store) SetProjectConfigError(projectID int64, message string) error {
	if !s.hasScheduler {
		return errors.New("scheduler state is unavailable")
	}
	var value any
	if message != "" {
		value = message
	}
	_, err := s.db.Exec(`UPDATE projects SET config_error=? WHERE id=? AND enabled=1`, value, projectID)
	return err
}

// ClearScheduler removes only this owner's record when it shuts down cleanly.
func (s *Store) ClearScheduler(pid int, identity string) error {
	if !s.hasScheduler {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM scheduler WHERE id=1 AND pid=? AND start_identity=?`, pid, identity)
	return err
}

// ScheduleTick snapshots inputs, applies both pure planner stages, and reserves
// selected runs in one transaction. The callbacks may only compute decisions.
func (s *Store) ScheduleTick(now time.Time, pid int, identity string,
	plan func(SchedulerSnapshot) (SchedulerPlan, error),
	dispatch func(SchedulerSnapshot) (DispatchPlan, error),
) ([]Run, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin scheduler tick: %w", err)
	}
	defer tx.Rollback()
	stamp := FormatTime(now)
	result, err := tx.Exec(`UPDATE scheduler SET heartbeat_at=? WHERE id=1 AND pid=? AND start_identity=?`, stamp, pid, identity)
	if err != nil {
		return nil, fmt.Errorf("heartbeat scheduler: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return nil, errors.New("scheduler ownership was lost")
	}
	snapshot, err := schedulerSnapshot(tx, s.hasPauses, s.hasAgentSessionID, s.hasTemporary)
	if err != nil {
		return nil, err
	}
	changes, err := plan(snapshot)
	if err != nil {
		return nil, err
	}
	for _, key := range changes.Remove {
		if _, err := tx.Exec(`DELETE FROM job_state WHERE project_id=? AND job=?`, key.ProjectID, key.Job); err != nil {
			return nil, fmt.Errorf("remove schedule state: %w", err)
		}
	}
	for _, state := range changes.Upsert {
		if _, err := tx.Exec(`INSERT INTO job_state(project_id, job, schedule_key, next_at) VALUES(?, ?, ?, ?)
ON CONFLICT(project_id, job) DO UPDATE SET schedule_key=excluded.schedule_key, next_at=excluded.next_at`, state.ProjectID, state.Job, state.ScheduleKey, FormatTime(state.NextAt)); err != nil {
			return nil, fmt.Errorf("update schedule state: %w", err)
		}
	}
	for _, key := range changes.TemporaryRemove {
		if _, err := tx.Exec("DELETE FROM temporary_schedules WHERE project_id=? AND job=?", key.ProjectID, key.Job); err != nil {
			return nil, err
		}
	}
	for _, value := range changes.TemporaryUpsert {
		if _, err := tx.Exec("UPDATE temporary_schedules SET next_at=? WHERE project_id=? AND job=?", FormatTime(value.NextAt), value.ProjectID, value.Job); err != nil {
			return nil, err
		}
	}
	var eventPaths []string
	committed := false
	defer func() {
		if !committed {
			for _, path := range eventPaths {
				_ = s.removeEvent(path)
			}
		}
	}()
	terminalJobs := make(map[JobStateKey]bool)
	for _, run := range changes.Create {
		if run.Status != "queued" && run.Status != "missed" && run.Status != "skipped" {
			return nil, fmt.Errorf("invalid scheduled run status %q", run.Status)
		}
		var finished any
		var message any
		if run.Status != "queued" {
			terminalJobs[JobStateKey{ProjectID: run.ProjectID, Job: run.Job}] = true
			finished = stamp
		}
		if run.Status == "skipped" {
			message = "overlap policy skipped this scheduled occurrence"
		}
		if run.Status == "missed" {
			message = "missed occurrence skipped by policy"
		}
		var requestedBy any
		if run.RequestedBy != "" {
			requestedBy = run.RequestedBy
		}
		result, err := tx.Exec(`INSERT INTO runs(project_id, job, status, trigger, digest, scheduled_at, due_at, created_at, finished_at, error, requested_by)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, run.ProjectID, run.Job, run.Status, run.Trigger, run.Digest, FormatTime(run.ScheduledAt), FormatTime(run.DueAt), stamp, finished, message, requestedBy)
		if err != nil {
			return nil, fmt.Errorf("create scheduled run: %w", err)
		}
		if len(run.EventData) > 0 && run.Status == "queued" {
			id, err := result.LastInsertId()
			if err != nil {
				return nil, err
			}
			path, err := s.writeEvent(id, run.EventSource, now, run.EventData)
			if err != nil {
				return nil, err
			}
			eventPaths = append(eventPaths, path)
			if _, err := tx.Exec("UPDATE runs SET event_path=? WHERE id=?", path, id); err != nil {
				return nil, err
			}
		}
	}
	snapshot, err = schedulerSnapshot(tx, s.hasPauses, s.hasAgentSessionID, s.hasTemporary)
	if err != nil {
		return nil, err
	}
	dispatches, err := dispatch(snapshot)
	if err != nil {
		return nil, err
	}
	terminalIDs := make(map[int64]bool)
	for _, id := range dispatches.Cancel {
		terminalIDs[id] = true
		if _, err := tx.Exec(`UPDATE runs SET status='cancelled', finished_at=?, error='project or job is no longer active' WHERE id=? AND status='queued'`, stamp, id); err != nil {
			return nil, fmt.Errorf("cancel inactive queued run: %w", err)
		}
	}
	for _, id := range dispatches.Expire {
		terminalIDs[id] = true
		if _, err := tx.Exec(`UPDATE runs SET status='expired', finished_at=?, error='queued run expired before start' WHERE id=? AND status='queued'`, stamp, id); err != nil {
			return nil, fmt.Errorf("expire queued run: %w", err)
		}
	}
	for _, run := range snapshot.Runs {
		if terminalIDs[run.ID] {
			terminalJobs[JobStateKey{ProjectID: run.ProjectID, Job: run.Job}] = true
		}
	}
	started := make([]Run, 0, len(dispatches.Start))
	for _, id := range dispatches.Start {
		if sessionID := dispatches.AgentSessionIDs[id]; sessionID != "" {
			if !s.hasAgentSessionID {
				return nil, errors.New("agent session IDs are unavailable in this state database")
			}
			_, err = tx.Exec(`UPDATE runs SET status='running', started_at=?, owner_pid=?, owner_start=?, agent_session_id=COALESCE(agent_session_id, ?) WHERE id=? AND status='queued'`, stamp, pid, identity, sessionID, id)
		} else {
			_, err = tx.Exec(`UPDATE runs SET status='running', started_at=?, owner_pid=?, owner_start=? WHERE id=? AND status='queued'`, stamp, pid, identity, id)
		}
		if err != nil {
			return nil, fmt.Errorf("start scheduled run: %w", err)
		}
		run, err := scanRun(tx.QueryRow(`SELECT `+s.runSelectColumns()+` FROM runs WHERE id=?`, id))
		if err != nil {
			return nil, fmt.Errorf("read started run: %w", err)
		}
		started = append(started, run)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit scheduler tick: %w", err)
	}
	committed = true
	// Retention follows durable state transitions, just as it does for FinishRun.
	if len(terminalJobs) > 0 {
		cleanup, err := s.db.Begin()
		if err != nil {
			return started, err
		}
		defer cleanup.Rollback()
		for key := range terminalJobs {
			if err := pruneRuns(cleanup, s.dir, key.ProjectID, key.Job); err != nil {
				return started, err
			}
		}
		if err := cleanup.Commit(); err != nil {
			return started, err
		}
	}
	return started, nil
}

func schedulerSnapshot(tx *sql.Tx, hasPauses, hasAgentSessionID, hasTemporary bool) (SchedulerSnapshot, error) {
	var snapshot SchedulerSnapshot
	projects, err := tx.Query(`SELECT p.id, p.path, p.config_file, a.job, a.digest FROM projects p LEFT JOIN approvals a ON a.project_id=p.id WHERE p.enabled=1 ORDER BY p.id, a.job`)
	if err != nil {
		return snapshot, fmt.Errorf("read enabled projects: %w", err)
	}
	projectIndex := map[int64]int{}
	for projects.Next() {
		var project EnabledProject
		var job, digest sql.NullString
		if err := projects.Scan(&project.ID, &project.Path, &project.ConfigFile, &job, &digest); err != nil {
			_ = projects.Close()
			return snapshot, err
		}
		index, exists := projectIndex[project.ID]
		if !exists {
			project.Approvals = map[string]string{}
			projectIndex[project.ID] = len(snapshot.Projects)
			snapshot.Projects = append(snapshot.Projects, project)
			index = len(snapshot.Projects) - 1
		}
		if job.Valid {
			snapshot.Projects[index].Approvals[job.String] = digest.String
		}
	}
	if err := errors.Join(projects.Err(), projects.Close()); err != nil {
		return snapshot, err
	}
	if hasPauses {
		pauses, err := tx.Query(`SELECT project_id, job FROM pauses`)
		if err != nil {
			return snapshot, err
		}
		for pauses.Next() {
			var id int64
			var name string
			if err := pauses.Scan(&id, &name); err != nil {
				_ = pauses.Close()
				return snapshot, err
			}
			if index, ok := projectIndex[id]; ok {
				if snapshot.Projects[index].Paused == nil {
					snapshot.Projects[index].Paused = map[string]bool{}
				}
				snapshot.Projects[index].Paused[name] = true
			}
		}
		if err := errors.Join(pauses.Err(), pauses.Close()); err != nil {
			return snapshot, err
		}
	}
	states, err := tx.Query(`SELECT project_id, job, schedule_key, next_at FROM job_state`)
	if err != nil {
		return snapshot, fmt.Errorf("read schedule state: %w", err)
	}
	for states.Next() {
		var state PersistedJobState
		if err := states.Scan(&state.ProjectID, &state.Job, &state.ScheduleKey, &state.NextAt); err != nil {
			_ = states.Close()
			return snapshot, err
		}
		snapshot.States = append(snapshot.States, state)
	}
	if err := errors.Join(states.Err(), states.Close()); err != nil {
		return snapshot, err
	}
	runColumns := `id, project_id, job, status, trigger, digest, scheduled_at, due_at`
	if hasAgentSessionID {
		runColumns += `, agent_session_id`
	} else {
		runColumns += `, NULL`
	}
	runs, err := tx.Query(`SELECT ` + runColumns + ` FROM runs WHERE status IN ('queued','running')`)
	if err != nil {
		return snapshot, fmt.Errorf("read scheduled runs: %w", err)
	}
	for runs.Next() {
		var run ScheduledRun
		if err := runs.Scan(&run.ID, &run.ProjectID, &run.Job, &run.Status, &run.Trigger, &run.Digest, &run.ScheduledAt, &run.DueAt, &run.AgentSessionID); err != nil {
			_ = runs.Close()
			return snapshot, err
		}
		snapshot.Runs = append(snapshot.Runs, run)
	}
	if err := errors.Join(runs.Err(), runs.Close()); err != nil {
		return snapshot, err
	}
	if hasTemporary {
		snapshot.Temporary, err = readTemporary(tx, "")
		if err != nil {
			return snapshot, err
		}
	}
	return snapshot, nil
}
