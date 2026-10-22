package store

import (
	"database/sql"
	"errors"
	"time"
)

var ErrAlreadyScheduled = errors.New("job already has a temporary schedule")

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
