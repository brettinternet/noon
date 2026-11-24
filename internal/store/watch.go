package store

// DataVersion returns SQLite's data_version for this store's one connection.
// It changes when another connection commits, so a dedicated watcher sees
// commits from every Noon process.
func (s *Store) DataVersion() (int64, error) {
	var version int64
	err := s.db.QueryRow(`PRAGMA data_version`).Scan(&version)
	return version, err
}

// ChangeStamp summarizes the state that inspection views show. It differs
// whenever a run is queued, starts, finishes, or is pruned, or a project,
// approval, pause, or temporary schedule changes, and ignores the scheduler's
// per-tick heartbeat.
func (s *Store) ChangeStamp() (string, error) {
	var stamp string
	err := s.db.QueryRow(`SELECT
  (SELECT COUNT(*) || ':' || COALESCE(MAX(id), 0) || ':' || COALESCE(SUM(status = 'running'), 0) || ':' || COALESCE(SUM(status = 'queued'), 0) || ':' || COALESCE(MAX(started_at), '') || ':' || COALESCE(MAX(finished_at), '') || ':' || COALESCE(MAX(cancel_requested_at), '') FROM runs)
  || '|' || (SELECT COUNT(*) || ':' || COALESCE(SUM(enabled), 0) || ':' || COALESCE(MAX(enabled_at), '') || ':' || COALESCE(GROUP_CONCAT(config_error), '') FROM projects)
  || '|' || (SELECT COUNT(*) || ':' || COALESCE(MAX(approved_at), '') FROM approvals)
  || '|' || (SELECT COUNT(*) || ':' || COALESCE(MAX(paused_at), '') || ':' || COALESCE(MAX(resumes_at), '') FROM pauses)
  || '|' || (SELECT COUNT(*) || ':' || COALESCE(MAX(until_at), '') FROM temporary_schedules)
  || '|' || (SELECT COUNT(*) || ':' || COALESCE(MAX(until_at), '') FROM temporary_jobs)
  || '|' || (SELECT COALESCE(MAX(pid || ':' || started_at), '') FROM scheduler)`).Scan(&stamp)
	return stamp, err
}
