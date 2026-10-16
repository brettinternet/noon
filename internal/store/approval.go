package store

import (
	"database/sql"
	"errors"
	"time"
)

// ProjectApproval is a consistent snapshot of a checkout's opt-in and approvals.
type ProjectApproval struct {
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
	rows, err := s.db.Query(`SELECT p.enabled, p.enabled_at, p.config_file, a.job, a.digest
 FROM projects p LEFT JOIN approvals a ON a.project_id=p.id WHERE p.path=?`, path)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var job, digest sql.NullString
		if err := rows.Scan(&result.Enabled, &result.EnabledAt, &result.ConfigFile, &job, &digest); err != nil {
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

// DisableProject revokes opt-in and approvals without deleting run history.
func (s *Store) DisableProject(path string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM approvals WHERE project_id IN (SELECT id FROM projects WHERE path=?)`, path); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE projects SET enabled=0 WHERE path=?`, path); err != nil {
		return err
	}
	return tx.Commit()
}
