package store

import (
	"database/sql"
	"errors"
	"time"
)

var ErrAlreadyScheduled = errors.New("job already has a temporary schedule")
var ErrJobExists = errors.New("job already exists")

const TemporaryJobGrace = 24 * time.Hour

// TemporaryJob is a complete private definition, independent of temporary schedules.
type TemporaryJob struct {
	ProjectID   int64
	Name        string
	Definition  []byte
	Digest      string
	Until       time.Time
	CreatedAt   time.Time
	RequestedBy string
	Key         string
	EndedAt     *time.Time
}

func (value TemporaryJob) End() time.Time {
	if value.EndedAt != nil {
		return *value.EndedAt
	}
	return value.Until
}

const temporaryJobColumns = "project_id, name, definition, digest, until_at, created_at, requested_by, request_key, ended_at"

func scanTemporaryJob(row interface{ Scan(...any) error }) (TemporaryJob, error) {
	var value TemporaryJob
	var until, created string
	var ended sql.NullString
	if err := row.Scan(&value.ProjectID, &value.Name, &value.Definition, &value.Digest, &until, &created, &value.RequestedBy, &value.Key, &ended); err != nil {
		return value, err
	}
	var err error
	if value.Until, err = time.Parse(TimestampLayout, until); err != nil {
		return value, err
	}
	if value.CreatedAt, err = time.Parse(TimestampLayout, created); err != nil {
		return value, err
	}
	if ended.Valid {
		at, err := time.Parse(TimestampLayout, ended.String)
		if err != nil {
			return value, err
		}
		value.EndedAt = &at
	}
	return value, nil
}

// TemporaryJobs excludes elapsed grace periods even without a running scheduler.
func (s *Store) TemporaryJobs(project string, now time.Time) ([]TemporaryJob, error) {
	if !s.hasTemporaryJobs {
		return nil, nil
	}
	values, err := readTemporaryJobs(s.db, "WHERE project_id IN (SELECT id FROM projects WHERE path=?) AND COALESCE(ended_at, until_at)>? ORDER BY name", project, FormatTime(now.Add(-TemporaryJobGrace)))
	if err != nil {
		return nil, err
	}
	for i := range values {
		value := &values[i]
		if value.EndedAt == nil && !now.Before(value.Until) {
			at := value.Until
			value.EndedAt = &at
		}
	}
	return values, nil
}

func readTemporaryJobs(db interface {
	Query(string, ...any) (*sql.Rows, error)
}, where string, args ...any) ([]TemporaryJob, error) {
	rows, err := db.Query("SELECT "+temporaryJobColumns+" FROM temporary_jobs "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []TemporaryJob
	for rows.Next() {
		value, err := scanTemporaryJob(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// CreateTemporaryJob serializes name ownership and keyed idempotency. Replacing
// an ended definition resets its phase, never its retained run history.
func (s *Store) CreateTemporaryJob(value TemporaryJob, now time.Time) (TemporaryJob, bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return value, false, err
	}
	defer tx.Rollback()
	existing, err := scanTemporaryJob(tx.QueryRow("SELECT "+temporaryJobColumns+" FROM temporary_jobs WHERE project_id=? AND name=?", value.ProjectID, value.Name))
	if err == nil && now.Before(existing.End()) {
		if value.Key == "" || value.Key != existing.Key {
			return value, false, ErrJobExists
		}
		return existing, false, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return value, false, err
	}
	if _, err := tx.Exec("UPDATE runs SET status='cancelled', finished_at=?, error='temporary job replaced before start' WHERE project_id=? AND job=? AND status='queued'", FormatTime(now), value.ProjectID, value.Name); err != nil {
		return value, false, err
	}
	if _, err := tx.Exec("DELETE FROM temporary_jobs WHERE project_id=? AND name=?", value.ProjectID, value.Name); err != nil {
		return value, false, err
	}
	// As with scheduled config add, newly enabling a disabled project must not
	// catch up any occurrences from its disabled interval.
	if _, err := tx.Exec("DELETE FROM job_state WHERE project_id=? AND (job=? OR EXISTS(SELECT 1 FROM projects WHERE id=? AND enabled=0))", value.ProjectID, value.Name, value.ProjectID); err != nil {
		return value, false, err
	}
	if _, err := tx.Exec("DELETE FROM pauses WHERE project_id=? AND job=?", value.ProjectID, value.Name); err != nil {
		return value, false, err
	}
	if _, err := tx.Exec("INSERT INTO temporary_jobs("+temporaryJobColumns+") VALUES(?,?,?,?,?,?,?,?,NULL)", value.ProjectID, value.Name, value.Definition, value.Digest, FormatTime(value.Until), FormatTime(value.CreatedAt), value.RequestedBy, value.Key); err != nil {
		return value, false, err
	}
	if _, err := tx.Exec("INSERT OR IGNORE INTO temporary_job_names(project_id, name) VALUES(?,?)", value.ProjectID, value.Name); err != nil {
		return value, false, err
	}
	if _, err := tx.Exec("UPDATE projects SET enabled=1, enabled_at=CASE WHEN enabled=0 THEN ? ELSE enabled_at END, config_file=NULL WHERE id=?", FormatTime(now), value.ProjectID); err != nil {
		return value, false, err
	}
	if err := tx.Commit(); err != nil {
		return value, false, err
	}
	return value, true, s.pruneJobRuns(value.ProjectID, value.Name)
}

func (s *Store) DeleteTemporaryJob(project, name string, now time.Time) error {
	if !s.hasTemporaryJobs {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var projectID int64
	if err := tx.QueryRow("SELECT project_id FROM temporary_jobs WHERE project_id IN (SELECT id FROM projects WHERE path=?) AND name=?", project, name).Scan(&projectID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	// Effective digests may change with inherited defaults; name ownership,
	// not the creation digest, identifies this definition's queued work.
	if _, err := tx.Exec("UPDATE runs SET status='cancelled', finished_at=?, error='temporary job removed before start' WHERE project_id=? AND job=? AND status='queued'", FormatTime(now), projectID, name); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM temporary_jobs WHERE project_id=? AND name=?", projectID, name); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.pruneJobRuns(projectID, name)
}

// WasTemporaryJob resolves an empty history after its definition has gone.
func (s *Store) WasTemporaryJob(project, name string) (bool, error) {
	if !s.hasTemporaryJobs {
		return false, nil
	}
	var found bool
	err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM temporary_job_names WHERE project_id IN (SELECT id FROM projects WHERE path=?) AND name=?)", project, name).Scan(&found)
	return found, err
}

// PurgeTemporaryJobs records natural endings and drops only expired definitions.
func (s *Store) PurgeTemporaryJobs(now time.Time) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT DISTINCT runs.project_id, runs.job FROM runs JOIN temporary_jobs ON temporary_jobs.project_id=runs.project_id AND temporary_jobs.name=runs.job WHERE runs.status='queued' AND COALESCE(temporary_jobs.ended_at, temporary_jobs.until_at)<=?", FormatTime(now))
	if err != nil {
		return err
	}
	var cancelled []JobStateKey
	for rows.Next() {
		var key JobStateKey
		if err := rows.Scan(&key.ProjectID, &key.Job); err != nil {
			rows.Close()
			return err
		}
		cancelled = append(cancelled, key)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE runs SET status='cancelled', finished_at=?, error='temporary job ended before start' WHERE status='queued' AND EXISTS (SELECT 1 FROM temporary_jobs WHERE project_id=runs.project_id AND name=runs.job AND COALESCE(ended_at, until_at)<=?)", FormatTime(now), FormatTime(now)); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE temporary_jobs SET ended_at=until_at WHERE ended_at IS NULL AND until_at<=?", FormatTime(now)); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM temporary_jobs WHERE ended_at<=?", FormatTime(now.Add(-TemporaryJobGrace))); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, key := range cancelled {
		if err := s.pruneJobRuns(key.ProjectID, key.Job); err != nil {
			return err
		}
	}
	return nil
}

// TemporarySchedule stores the independent occurrence watermark and request metadata.
type TemporarySchedule struct {
	ProjectID   int64     `json:"-"`
	Job         string    `json:"-"`
	Schedule    string    `json:"schedule"`
	Until       time.Time `json:"until"`
	NextAt      time.Time `json:"next_at"`
	RequestedBy string    `json:"requested_by"`
	CreatedAt   time.Time `json:"created_at"`
	Key         string    `json:"key,omitempty"`
	Digest      string    `json:"-"`
	Data        []byte    `json:"-"`
	EventSource string    `json:"-"`
}

// In returns the schedule with its times shown in loc.
func (value TemporarySchedule) In(loc *time.Location) TemporarySchedule {
	value.Until, value.NextAt, value.CreatedAt = value.Until.In(loc), value.NextAt.In(loc), value.CreatedAt.In(loc)
	return value
}

const temporaryColumns = "project_id, job, schedule, until_at, next_at, requested_by, created_at, request_key, digest, data, event_source"

func scanTemporary(row interface{ Scan(...any) error }) (TemporarySchedule, error) {
	var value TemporarySchedule
	var until, next, created string
	err := row.Scan(&value.ProjectID, &value.Job, &value.Schedule, &until, &next, &value.RequestedBy, &created, &value.Key, &value.Digest, &value.Data, &value.EventSource)
	if err != nil {
		return value, err
	}
	for _, item := range []struct {
		text string
		dest *time.Time
	}{{until, &value.Until}, {next, &value.NextAt}, {created, &value.CreatedAt}} {
		*item.dest, err = time.Parse(TimestampLayout, item.text)
		if err != nil {
			return value, err
		}
	}
	return value, nil
}

func readTemporary(tx interface {
	Query(string, ...any) (*sql.Rows, error)
}, where string, args ...any) ([]TemporarySchedule, error) {
	rows, err := tx.Query("SELECT "+temporaryColumns+" FROM temporary_schedules "+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []TemporarySchedule
	for rows.Next() {
		value, err := scanTemporary(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) TemporarySchedules(path string) ([]TemporarySchedule, error) {
	if !s.hasTemporary {
		return nil, nil
	}
	return readTemporary(s.db, "WHERE project_id IN (SELECT id FROM projects WHERE path=?)", path)
}

// DeleteTemporarySchedules ends requests without cancelling already-created runs.
func (s *Store) DeleteTemporarySchedules(project string, jobs []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, job := range jobs {
		if _, err := tx.Exec("DELETE FROM temporary_schedules WHERE project_id IN (SELECT id FROM projects WHERE path=?) AND job=?", project, job); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateTemporarySchedule serializes eligibility, keyed deduplication and creation.
func (s *Store) CreateTemporarySchedule(value TemporarySchedule, input QueueRunInput) (TemporarySchedule, bool, string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return value, false, "", err
	}
	defer tx.Rollback()
	input.RequireUnpaused = true
	if err := s.checkQueueEligibility(tx, input); err != nil {
		return value, false, "", err
	}
	// Like a queued request due at or after the job end, a first occurrence there is refused.
	if input.JobUntil != nil && (!input.Now.Before(*input.JobUntil) || !value.NextAt.Before(*input.JobUntil)) {
		return value, false, "", ErrJobEnded
	}
	// Expired or changed definitions must never be revived by a duplicate request.
	if _, err := tx.Exec("DELETE FROM temporary_schedules WHERE project_id=? AND job=? AND (until_at<=? OR digest<>?)", value.ProjectID, value.Job, FormatTime(input.Now), value.Digest); err != nil {
		return value, false, "", err
	}
	existing, err := scanTemporary(tx.QueryRow("SELECT "+temporaryColumns+" FROM temporary_schedules WHERE project_id=? AND job=?", value.ProjectID, value.Job))
	if err == nil {
		if value.Key == "" || value.Key != existing.Key {
			return value, false, "", ErrAlreadyScheduled
		}
		status, err := schedulerStatus(tx, input.Now)
		return existing, false, status, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return value, false, "", err
	}
	_, err = tx.Exec("INSERT INTO temporary_schedules("+temporaryColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?)", value.ProjectID, value.Job, value.Schedule, FormatTime(value.Until), FormatTime(value.NextAt), value.RequestedBy, FormatTime(value.CreatedAt), value.Key, value.Digest, value.Data, value.EventSource)
	if err != nil {
		return value, false, "", err
	}
	status, err := schedulerStatus(tx, input.Now)
	if err != nil {
		return value, false, "", err
	}
	value, err = scanTemporary(tx.QueryRow("SELECT "+temporaryColumns+" FROM temporary_schedules WHERE project_id=? AND job=?", value.ProjectID, value.Job))
	if err != nil {
		return value, false, "", err
	}
	err = tx.Commit()
	return value, true, status, err
}
