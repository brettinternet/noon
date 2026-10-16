// Package store persists per-user project and foreground-run state.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/runner"
	_ "modernc.org/sqlite"
)

const (
	databaseName    = "noon.db"
	TimestampLayout = "2006-01-02T15:04:05.000Z"
	maxRunLogBytes  = 1 << 20
)

var (
	ErrNoDatabase     = errors.New("run store does not exist")
	ErrLogUnavailable = errors.New("run log is no longer available")
)

var migrations = []string{`
CREATE TABLE projects (id INTEGER PRIMARY KEY, path TEXT NOT NULL UNIQUE, created_at TEXT NOT NULL);
CREATE TABLE runs (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id),
  job TEXT NOT NULL, status TEXT NOT NULL, trigger TEXT NOT NULL, digest TEXT NOT NULL,
  scheduled_at TEXT, created_at TEXT NOT NULL, started_at TEXT, finished_at TEXT,
  exit_code INTEGER, signal TEXT, error TEXT, requested_by TEXT,
  owner_pid INTEGER, owner_start TEXT, pgid INTEGER, leader_start TEXT,
  log_path TEXT, log_bytes INTEGER NOT NULL DEFAULT 0, log_truncated INTEGER NOT NULL DEFAULT 0);
CREATE UNIQUE INDEX runs_one_running ON runs(project_id, job) WHERE status = 'running';
CREATE INDEX runs_by_job ON runs(project_id, job, id DESC);
`, `
ALTER TABLE runs ADD COLUMN log_overflow INTEGER NOT NULL DEFAULT 0;
`, `
ALTER TABLE projects ADD COLUMN enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN enabled_at TEXT;
ALTER TABLE projects ADD COLUMN config_file TEXT;
CREATE TABLE approvals (
  project_id INTEGER NOT NULL REFERENCES projects(id), job TEXT NOT NULL,
  digest TEXT NOT NULL, approved_at TEXT NOT NULL, PRIMARY KEY(project_id, job));
`, `
CREATE TABLE job_state (
  project_id INTEGER NOT NULL REFERENCES projects(id), job TEXT NOT NULL,
  schedule_key TEXT NOT NULL, next_at TEXT NOT NULL, PRIMARY KEY(project_id, job));
CREATE TABLE scheduler (
  id INTEGER PRIMARY KEY CHECK (id = 1), pid INTEGER NOT NULL,
  start_identity TEXT NOT NULL, version TEXT NOT NULL,
  started_at TEXT NOT NULL, heartbeat_at TEXT NOT NULL);
ALTER TABLE runs ADD COLUMN due_at TEXT;
ALTER TABLE projects ADD COLUMN config_error TEXT;
`, `
ALTER TABLE runs ADD COLUMN idempotency_key TEXT;
ALTER TABLE runs ADD COLUMN cancel_requested_at TEXT;
CREATE UNIQUE INDEX runs_key ON runs(project_id, job, idempotency_key) WHERE idempotency_key IS NOT NULL;
`, `
CREATE TABLE pauses (
  project_id INTEGER NOT NULL REFERENCES projects(id), job TEXT NOT NULL,
  paused_at TEXT NOT NULL, PRIMARY KEY(project_id, job));
`, `
ALTER TABLE runs ADD COLUMN hook_status TEXT;
ALTER TABLE runs ADD COLUMN hook_exit_code INTEGER;
`, `
ALTER TABLE runs ADD COLUMN event_path TEXT;
ALTER TABLE runs ADD COLUMN depth INTEGER NOT NULL DEFAULT 0;
`}

type Store struct {
	dir            string
	db             *sql.DB
	hasLogOverflow bool
	hasApprovals   bool
	hasScheduler   bool
	hasQueue       bool
	hasPauses      bool
	hasHooks       bool
	hasTriggers    bool
}

type StateTooNewError struct{ Version int }

func (e *StateTooNewError) Error() string {
	return fmt.Sprintf("state database version %d is newer than this Noon binary supports", e.Version)
}

type AlreadyRunningError struct {
	RunID     int64
	StartedAt string
}

func (e *AlreadyRunningError) Error() string {
	return fmt.Sprintf("run %d started at %s is already running", e.RunID, e.StartedAt)
}

type Run struct {
	ID, ProjectID                int64
	Job, Status, Trigger, Digest string
	ScheduledAt                  sql.NullString
	CreatedAt, StartedAt         string
	FinishedAt, Error, Signal    sql.NullString
	ExitCode, OwnerPID, PGID     sql.NullInt64
	OwnerStart, LeaderStart      sql.NullString
	LogPath                      sql.NullString
	DueAt                        sql.NullString
	RequestedBy                  sql.NullString
	IdempotencyKey               sql.NullString
	CancelRequestedAt            sql.NullString
	LogBytes                     int64
	LogTruncated, LogOverflow    bool
	HookStatus                   sql.NullString
	HookExitCode                 sql.NullInt64
	EventPath                    sql.NullString
	Depth                        int
}

const (
	runColumnsV1          = `id, project_id, job, status, trigger, digest, scheduled_at, created_at, started_at, finished_at, exit_code, signal, error, owner_pid, owner_start, pgid, leader_start, log_path, log_bytes, log_truncated, requested_by`
	qualifiedRunColumnsV1 = `runs.id, runs.project_id, runs.job, runs.status, runs.trigger, runs.digest, runs.scheduled_at, runs.created_at, runs.started_at, runs.finished_at, runs.exit_code, runs.signal, runs.error, runs.owner_pid, runs.owner_start, runs.pgid, runs.leader_start, runs.log_path, runs.log_bytes, runs.log_truncated, runs.requested_by`
	runColumns            = runColumnsV1 + `, log_overflow`
	qualifiedRunColumns   = qualifiedRunColumnsV1 + `, runs.log_overflow`
)

func (s *Store) runSelectColumns() string {
	columns := runColumnsV1 + `, 0`
	if s.hasLogOverflow {
		columns = runColumns
	}
	if s.hasScheduler {
		columns += `, due_at`
	} else {
		columns += `, NULL`
	}
	if s.hasQueue {
		columns += `, idempotency_key, cancel_requested_at`
	} else {
		columns += `, NULL, NULL`
	}
	if s.hasHooks {
		columns += `, hook_status, hook_exit_code`
	} else {
		columns += `, NULL, NULL`
	}
	if s.hasTriggers {
		return columns + `, event_path, depth`
	}
	return columns + `, NULL, 0`
}

func (s *Store) qualifiedRunSelectColumns() string {
	columns := qualifiedRunColumnsV1 + `, 0`
	if s.hasLogOverflow {
		columns = qualifiedRunColumns
	}
	if s.hasScheduler {
		columns += `, runs.due_at`
	} else {
		columns += `, NULL`
	}
	if s.hasQueue {
		columns += `, runs.idempotency_key, runs.cancel_requested_at`
	} else {
		columns += `, NULL, NULL`
	}
	if s.hasHooks {
		columns += `, runs.hook_status, runs.hook_exit_code`
	} else {
		columns += `, NULL, NULL`
	}
	if s.hasTriggers {
		return columns + `, runs.event_path, runs.depth`
	}
	return columns + `, NULL, 0`
}

type runScanner interface {
	Scan(...any) error
}

func scanRun(row runScanner) (Run, error) {
	var run Run
	var startedAt sql.NullString
	err := row.Scan(&run.ID, &run.ProjectID, &run.Job, &run.Status, &run.Trigger, &run.Digest, &run.ScheduledAt, &run.CreatedAt, &startedAt, &run.FinishedAt, &run.ExitCode, &run.Signal, &run.Error, &run.OwnerPID, &run.OwnerStart, &run.PGID, &run.LeaderStart, &run.LogPath, &run.LogBytes, &run.LogTruncated, &run.RequestedBy, &run.LogOverflow, &run.DueAt, &run.IdempotencyKey, &run.CancelRequestedAt, &run.HookStatus, &run.HookExitCode, &run.EventPath, &run.Depth)
	run.StartedAt = startedAt.String
	return run, err
}

type NewRun struct {
	ProjectID            int64
	Job, Trigger, Digest string
	Now                  time.Time
	OwnerPID             int
	OwnerStart           string
	Depth                int
	EventData            []byte
	EventSource          string
}

type Outcome struct {
	Status        string
	FinishedAt    time.Time
	ExitCode      *int
	Signal, Error string
	LogPath       string
	LogBytes      int64
	LogTruncated  bool
	HookEvent     string
}

// Open creates the state directory and database if necessary.
func Open(dir string) (*Store, error) { return open(dir, true) }

// OpenExisting opens a database without creating state; absent state is ErrNoDatabase.
func OpenExisting(dir string) (*Store, error) { return open(dir, false) }

func open(dir string, create bool) (*Store, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("state directory must be absolute: %q", dir)
	}
	dir = filepath.Clean(dir)
	dbPath := filepath.Join(dir, databaseName)
	if create {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create state directory: %w", err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return nil, fmt.Errorf("secure state directory: %w", err)
		}
	} else {
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoDatabase
		} else if err != nil {
			return nil, fmt.Errorf("inspect state directory: %w", err)
		}
		if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoDatabase
		} else if err != nil {
			return nil, fmt.Errorf("inspect state database: %w", err)
		}
	}
	uri := url.URL{Scheme: "file", Path: filepath.ToSlash(dbPath)}
	query := url.Values{}
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "foreign_keys(1)")
	if create {
		query.Set("_txlock", "immediate")
	} else {
		query.Set("mode", "ro")
	}
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, fmt.Errorf("open state database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	state := &Store{dir: dir, db: db}
	closeOnError := func(err error) (*Store, error) {
		_ = db.Close()
		return nil, err
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return closeOnError(fmt.Errorf("read state database version: %w", err))
	}
	if version > len(migrations) {
		return closeOnError(&StateTooNewError{Version: version})
	}
	if create {
		var mode string
		if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
			return closeOnError(fmt.Errorf("enable SQLite WAL: %w", err))
		}
		if !strings.EqualFold(mode, "wal") {
			return closeOnError(fmt.Errorf("enable SQLite WAL: got %q", mode))
		}
	}
	if !create {
		state.hasLogOverflow = version >= 2
		state.hasApprovals = version >= 3
		state.hasScheduler = version >= 4
		state.hasQueue = version >= 5
		state.hasPauses = version >= 6
		state.hasHooks = version >= 7
		state.hasTriggers = version >= 8
		return state, nil
	}
	for {
		tx, err := db.Begin()
		if err != nil {
			return closeOnError(fmt.Errorf("begin state migration: %w", err))
		}
		var current int
		if err := tx.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
			_ = tx.Rollback()
			return closeOnError(fmt.Errorf("read state migration version: %w", err))
		}
		if current > len(migrations) {
			_ = tx.Rollback()
			return closeOnError(&StateTooNewError{Version: current})
		}
		if current == len(migrations) {
			_ = tx.Rollback()
			break
		}
		next := current + 1
		_, err = tx.Exec(migrations[current])
		if err == nil {
			_, err = tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", next))
		}
		if err != nil {
			_ = tx.Rollback()
			return closeOnError(fmt.Errorf("apply state migration %d: %w", next, err))
		}
		if err := tx.Commit(); err != nil {
			return closeOnError(fmt.Errorf("commit state migration %d: %w", next, err))
		}
	}
	state.hasLogOverflow = true
	state.hasApprovals = true
	state.hasScheduler = true
	state.hasQueue = true
	state.hasPauses = true
	state.hasHooks = true
	state.hasTriggers = true
	if err := secureFiles(dir); err != nil {
		return closeOnError(err)
	}
	return state, nil
}

func secureFiles(dir string) error {
	for _, name := range []string{databaseName, databaseName + "-wal", databaseName + "-shm"} {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect state file: %w", err)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return fmt.Errorf("secure state file: %w", err)
		}
	}
	return nil
}

func (s *Store) Close() error { return s.db.Close() }

func FormatTime(value time.Time) string { return value.UTC().Format(TimestampLayout) }

// RegisterProject returns the ID for the project's canonical, existing directory.
func (s *Store) RegisterProject(path string, now time.Time) (int64, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("project path: %w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return 0, fmt.Errorf("canonicalize project path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("inspect project path: %w", err)
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("project path %s is not a directory", path)
	}
	if _, err := s.db.Exec("INSERT INTO projects(path, created_at) VALUES(?, ?) ON CONFLICT(path) DO NOTHING", path, FormatTime(now)); err != nil {
		return 0, fmt.Errorf("register project: %w", err)
	}
	var id int64
	if err := s.db.QueryRow("SELECT id FROM projects WHERE path = ?", path).Scan(&id); err != nil {
		return 0, fmt.Errorf("find project: %w", err)
	}
	return id, nil
}

// LastSuccess returns the stored start time of the most recent successful run.
func (s *Store) LastSuccess(projectID int64, job string) (string, bool, error) {
	var value sql.NullString
	err := s.db.QueryRow(`SELECT started_at FROM runs WHERE project_id=? AND job=? AND status='succeeded' ORDER BY id DESC LIMIT 1`, projectID, job).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value.String, value.Valid, nil
}

// CreateRun records the running row and event before the child is started.
func (s *Store) CreateRun(input NewRun) (Run, error) {
	created := FormatTime(input.Now)
	tx, err := s.db.Begin()
	if err != nil {
		return Run{}, fmt.Errorf("begin create run: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.Exec(`INSERT INTO runs(project_id, job, status, trigger, digest, created_at, started_at, owner_pid, owner_start, depth)
VALUES(?, ?, 'running', ?, ?, ?, ?, ?, ?, ?)`, input.ProjectID, input.Job, input.Trigger, input.Digest, created, created, input.OwnerPID, input.OwnerStart, input.Depth)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "runs_one_running") || strings.Contains(err.Error(), "UNIQUE constraint failed: runs.project_id, runs.job") {
			var existing Run
			e := tx.QueryRow(`SELECT id, started_at FROM runs WHERE project_id=? AND job=? AND status='running'`, input.ProjectID, input.Job).Scan(&existing.ID, &existing.StartedAt)
			if e == nil {
				return Run{}, &AlreadyRunningError{RunID: existing.ID, StartedAt: existing.StartedAt}
			}
		}
		return Run{}, fmt.Errorf("create run: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Run{}, fmt.Errorf("get run ID: %w", err)
	}
	var eventPath string
	if len(input.EventData) > 0 {
		eventPath, err = s.writeEvent(id, input.EventSource, input.Now, input.EventData)
		if err != nil {
			return Run{}, err
		}
		if _, err := tx.Exec(`UPDATE runs SET event_path=? WHERE id=?`, eventPath, id); err != nil {
			_ = s.removeEvent(eventPath)
			return Run{}, fmt.Errorf("store run event path: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		if eventPath != "" {
			_ = s.removeEvent(eventPath)
		}
		return Run{}, fmt.Errorf("commit run: %w", err)
	}
	var savedEventPath sql.NullString
	if eventPath != "" {
		savedEventPath = sql.NullString{String: eventPath, Valid: true}
	}
	return Run{ID: id, ProjectID: input.ProjectID, Job: input.Job, Status: "running", Trigger: input.Trigger, Digest: input.Digest, CreatedAt: created, StartedAt: created, Depth: input.Depth, EventPath: savedEventPath}, nil
}

// SetRunLog makes the live log path visible before the child starts writing.
func (s *Store) SetRunLog(runID int64, path string) error {
	result, err := s.db.Exec(`UPDATE runs SET log_path=? WHERE id=? AND status='running'`, path, runID)
	if err != nil {
		return fmt.Errorf("record run log path: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("run %d is no longer running", runID)
	}
	return nil
}

// SetProcessIdentity records the child process group once it has started.
func (s *Store) SetProcessIdentity(runID int64, pgid int, leaderStart string) error {
	result, err := s.db.Exec(`UPDATE runs SET pgid=?, leader_start=? WHERE id=? AND status='running'`, pgid, nullableString(leaderStart), runID)
	if err != nil {
		return fmt.Errorf("record process identity: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("run %d is no longer running", runID)
	}
	return secureFiles(s.dir)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// Recover marks runs whose identity-checked owner has exited as interrupted.
func (s *Store) Recover(now time.Time, grace time.Duration) error {
	return s.recoverRuns(nil, now, grace, false)
}

// RecoverRuns recovers only the selected runs, sharing one grace budget across
// every stale process group so a multi-run cancellation stays bounded.
func (s *Store) RecoverRuns(ids []int64, now time.Time, budget time.Duration) error {
	if len(ids) == 0 {
		return nil
	}
	return s.recoverRuns(ids, now, budget, true)
}

func (s *Store) recoverRuns(ids []int64, now time.Time, grace time.Duration, sharedBudget bool) error {
	query := `SELECT id, owner_pid, owner_start, pgid, leader_start, log_path, project_id, job, cancel_requested_at FROM runs WHERE status='running'`
	var args []any
	if ids != nil {
		query += ` AND id IN (` + strings.TrimSuffix(strings.Repeat(`?,`, len(ids)), `,`) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("read running runs: %w", err)
	}
	type staleRun struct {
		id, projectID                    int64
		pid, pgid                        sql.NullInt64
		ownerStart, leaderStart, logPath sql.NullString
		cancelRequestedAt                sql.NullString
		job                              string
	}
	var candidates []staleRun
	for rows.Next() {
		var row staleRun
		if err := rows.Scan(&row.id, &row.pid, &row.ownerStart, &row.pgid, &row.leaderStart, &row.logPath, &row.projectID, &row.job, &row.cancelRequestedAt); err != nil {
			_ = rows.Close()
			return err
		}
		candidates = append(candidates, row)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	var stale []staleRun
	for _, row := range candidates {
		if !row.pid.Valid || !row.ownerStart.Valid {
			return fmt.Errorf("run %d has no owner identity", row.id)
		}
		alive, err := runner.ProcessOwnerAlive(int(row.pid.Int64), row.ownerStart.String)
		if err == nil && alive {
			continue
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check owner for run %d: %w", row.id, err)
		}
		stale = append(stale, row)
	}
	deadline := time.Now().Add(grace)
	for i, row := range stale {
		groupGrace := grace
		if sharedBudget {
			groupGrace = time.Until(deadline) / time.Duration(len(stale)-i)
			if groupGrace <= 0 {
				groupGrace = time.Nanosecond
			}
		}
		if row.pgid.Valid && row.leaderStart.Valid {
			if err := runner.TerminateProcessGroup(int(row.pgid.Int64), row.leaderStart.String, groupGrace); err != nil {
				return fmt.Errorf("terminate stale run %d: %w", row.id, err)
			}
		}
		status, message := "interrupted", "owner process exited unexpectedly"
		if row.cancelRequestedAt.Valid {
			status, message = "cancelled", "cancellation completed after owner exited"
		}
		if err := s.finishRecovered(row.id, row.projectID, row.job, Outcome{Status: status, FinishedAt: now, Error: message, LogPath: row.logPath.String}); err != nil {
			return err
		}
	}
	return s.recoverHooks(ids)
}

func (s *Store) recoverHooks(ids []int64) error {
	if !s.hasHooks {
		return nil
	}
	query := `SELECT id, owner_pid, owner_start FROM runs WHERE hook_status='running'`
	var args []any
	if ids != nil {
		query += ` AND id IN (` + strings.TrimSuffix(strings.Repeat(`?,`, len(ids)), `,`) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return fmt.Errorf("read running hooks: %w", err)
	}
	type runningHook struct {
		id         int64
		ownerPID   sql.NullInt64
		ownerStart sql.NullString
	}
	var hooks []runningHook
	for rows.Next() {
		var hook runningHook
		if err := rows.Scan(&hook.id, &hook.ownerPID, &hook.ownerStart); err != nil {
			_ = rows.Close()
			return err
		}
		hooks = append(hooks, hook)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, hook := range hooks {
		alive := false
		if hook.ownerPID.Valid && hook.ownerStart.Valid {
			var err error
			alive, err = runner.ProcessOwnerAlive(int(hook.ownerPID.Int64), hook.ownerStart.String)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("check owner for hook on run %d: %w", hook.id, err)
			}
		}
		if alive {
			continue
		}
		if _, err := s.db.Exec(`UPDATE runs SET hook_status='interrupted' WHERE id=? AND hook_status='running'`, hook.id); err != nil {
			return fmt.Errorf("recover hook for run %d: %w", hook.id, err)
		}
	}
	return nil
}

// FinishRun records a terminal result and applies run/log retention.
func (s *Store) FinishRun(runID, projectID int64, job string, outcome Outcome) error {
	return s.finish(runID, projectID, job, outcome)
}

func (s *Store) finish(runID, projectID int64, job string, outcome Outcome) error {
	return s.finishWithTerminalTolerance(runID, projectID, job, outcome, false)
}

func (s *Store) finishRecovered(runID, projectID int64, job string, outcome Outcome) error {
	return s.finishWithTerminalTolerance(runID, projectID, job, outcome, true)
}

func (s *Store) finishWithTerminalTolerance(runID, projectID int64, job string, outcome Outcome, tolerateTerminal bool) error {
	if outcome.Status == "queued" || outcome.Status == "running" || outcome.Status == "" {
		return fmt.Errorf("invalid terminal run status %q", outcome.Status)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin finish run: %w", err)
	}
	defer tx.Rollback()
	var exit any
	if outcome.ExitCode != nil {
		exit = *outcome.ExitCode
	}
	hookStatus, hookExitCode := any(nil), any(nil)
	if outcome.HookEvent != "" {
		hookStatus = "running"
	}
	query := `UPDATE runs SET status=?, finished_at=?, exit_code=?, signal=?, error=?, log_path=?, log_bytes=?, log_truncated=?`
	args := []any{outcome.Status, FormatTime(outcome.FinishedAt), exit, nullableString(outcome.Signal), nullableString(outcome.Error), nullableString(outcome.LogPath), outcome.LogBytes, outcome.LogTruncated}
	if s.hasHooks {
		query += `, hook_status=?, hook_exit_code=?`
		args = append(args, hookStatus, hookExitCode)
	}
	query += ` WHERE id=? AND status='running'`
	args = append(args, runID)
	result, err := tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		if tolerateTerminal {
			var status string
			if err := tx.QueryRow(`SELECT status FROM runs WHERE id=?`, runID).Scan(&status); err != nil {
				return fmt.Errorf("read recovered run %d: %w", runID, err)
			}
			if status != "running" && status != "queued" {
				return nil
			}
		}
		return fmt.Errorf("run %d is not running", runID)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run result: %w", err)
	}
	// Hook completion applies retention. Do not let cleanup failures prevent
	// execution after the durable running-hook transition.
	if outcome.HookEvent != "" {
		return nil
	}
	cleanupTx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin run retention: %w", err)
	}
	defer cleanupTx.Rollback()
	if err := pruneRuns(cleanupTx, s.dir, projectID, job); err != nil {
		return err
	}
	if err := cleanupTx.Commit(); err != nil {
		return fmt.Errorf("commit run retention: %w", err)
	}
	return secureFiles(s.dir)
}

func pruneRuns(tx *sql.Tx, dir string, projectID int64, job string) error {
	rows, err := tx.Query(`SELECT id, log_path, event_path FROM runs WHERE project_id=? AND job=? AND status NOT IN ('queued','running') AND (hook_status IS NULL OR hook_status!='running') ORDER BY id DESC`, projectID, job)
	if err != nil {
		return err
	}
	type retained struct {
		id        int64
		logPath   sql.NullString
		eventPath sql.NullString
	}
	var all []retained
	for rows.Next() {
		var r retained
		if err := rows.Scan(&r.id, &r.logPath, &r.eventPath); err != nil {
			_ = rows.Close()
			return err
		}
		all = append(all, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for i, r := range all {
		if i >= 10 {
			if r.logPath.Valid {
				if err := removeLog(dir, r.logPath.String); err != nil {
					return err
				}
			}
			if r.eventPath.Valid {
				if err := removeEventFile(dir, r.eventPath.String); err != nil {
					return err
				}
			}
			if i < 50 {
				if _, err := tx.Exec(`UPDATE runs SET log_path=NULL, event_path=NULL WHERE id=?`, r.id); err != nil {
					return err
				}
			}
		}
		if i >= 50 {
			if _, err := tx.Exec(`DELETE FROM runs WHERE id=?`, r.id); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeLog(dir, relative string) error {
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe log path %q", relative)
	}
	err := os.Remove(filepath.Join(dir, relative))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove old run log: %w", err)
	}
	return nil
}

func (s *Store) RunLog(projectID, runID int64) (*RunLog, error) {
	dir := filepath.Join(s.dir, "logs", fmt.Sprint(projectID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create run log directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(dir), 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, fmt.Sprintf("%d.log", runID))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create run log: %w", err)
	}
	return &RunLog{file: file, path: filepath.ToSlash(filepath.Join("logs", fmt.Sprint(projectID), fmt.Sprintf("%d.log", runID))), onOverflow: func() error {
		_, err := s.db.Exec(`UPDATE runs SET log_overflow=1 WHERE id=? AND status='running'`, runID)
		return err
	}}, nil
}

func (s *Store) RunByID(id int64) (Run, error) {
	return scanRun(s.db.QueryRow(`SELECT `+s.runSelectColumns()+` FROM runs WHERE id=?`, id))
}

// RunLogFilePath returns an absolute path only for a valid retained log path.
func (s *Store) RunLogFilePath(run Run) (string, bool) {
	if !run.LogPath.Valid {
		return "", false
	}
	if filepath.IsAbs(run.LogPath.String) || filepath.Clean(run.LogPath.String) != run.LogPath.String || strings.HasPrefix(run.LogPath.String, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.Join(s.dir, run.LogPath.String), true
}

// CompleteHook records a hook result and appends bounded output while preserving
// the run's terminal status and protecting its log from concurrent retention.
func (s *Store) CompleteHook(runID int64, event, status string, exitCode *int, output []byte) error {
	if !s.hasHooks {
		return errors.New("lifecycle hooks are unavailable in this state database")
	}
	if event != "on_success" && event != "on_failure" {
		return fmt.Errorf("invalid hook event %q", event)
	}
	switch status {
	case "ok", "failed", "timed_out":
	default:
		return fmt.Errorf("invalid hook status %q", status)
	}
	if len(output) > 64<<10 {
		output = output[:64<<10]
	}
	run, err := s.RunByID(runID)
	if err != nil {
		return err
	}
	if run.HookStatus.String != "running" {
		return fmt.Errorf("hook for run %d is not running", runID)
	}
	logBytes := run.LogBytes
	var logErr error
	if path, ok := s.RunLogFilePath(run); ok {
		file, openErr := os.OpenFile(path, os.O_RDWR|os.O_APPEND, 0)
		if openErr != nil {
			logErr = fmt.Errorf("open run log for hook: %w", openErr)
		} else {
			marker := "[noon: " + event + " hook "
			if status == "timed_out" {
				marker += "timed out]\n"
			} else {
				code := -1
				if exitCode != nil {
					code = *exitCode
				}
				marker += fmt.Sprintf("exited %d]\n", code)
			}
			prefix := []byte(nil)
			if run.LogBytes > 0 {
				var last [1]byte
				if n, readErr := file.ReadAt(last[:], run.LogBytes-1); readErr == nil && n == 1 && last[0] != '\n' {
					prefix = append(prefix, '\n')
				}
			}
			payload := append(prefix, marker...)
			payload = append(payload, output...)
			written, writeErr := file.Write(payload)
			if writeErr == nil && written != len(payload) {
				writeErr = fmt.Errorf("short hook log write")
			}
			if writeErr == nil {
				writeErr = file.Sync()
			}
			if closeErr := file.Close(); writeErr == nil {
				writeErr = closeErr
			}
			if info, statErr := os.Stat(path); statErr == nil {
				logBytes = info.Size()
			} else if writeErr == nil {
				writeErr = statErr
			}
			if writeErr != nil {
				logErr = fmt.Errorf("append hook output: %w", writeErr)
			}
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return errors.Join(logErr, fmt.Errorf("begin hook completion: %w", err))
	}
	defer tx.Rollback()
	var exit any
	if exitCode != nil {
		exit = *exitCode
	}
	result, err := tx.Exec(`UPDATE runs SET hook_status=?, hook_exit_code=?, log_bytes=? WHERE id=? AND hook_status='running'`, status, exit, logBytes, runID)
	if err != nil {
		return errors.Join(logErr, fmt.Errorf("complete hook: %w", err))
	}
	if count, _ := result.RowsAffected(); count != 1 {
		return errors.Join(logErr, fmt.Errorf("hook for run %d is no longer running", runID))
	}
	if err := tx.Commit(); err != nil {
		return errors.Join(logErr, fmt.Errorf("commit hook result: %w", err))
	}
	cleanup, err := s.db.Begin()
	if err != nil {
		return errors.Join(logErr, fmt.Errorf("begin hook retention: %w", err))
	}
	defer cleanup.Rollback()
	if err := pruneRuns(cleanup, s.dir, run.ProjectID, run.Job); err != nil {
		return errors.Join(logErr, err)
	}
	if err := cleanup.Commit(); err != nil {
		return errors.Join(logErr, fmt.Errorf("commit hook retention: %w", err))
	}
	if err := secureFiles(s.dir); err != nil {
		return errors.Join(logErr, err)
	}
	return logErr
}

// CancellationRequested reports a durable request for an active run.
func (s *Store) CancellationRequested(id int64) (bool, error) {
	if !s.hasQueue {
		return false, nil
	}
	var requested bool
	err := s.db.QueryRow(`SELECT cancel_requested_at IS NOT NULL FROM runs WHERE id=? AND status='running'`, id).Scan(&requested)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return requested, err
}

// RunsForJob returns all retained runs, newest first; it is also useful to clients of the store.
func (s *Store) RunsForJob(projectID int64, job string) ([]Run, error) {
	rows, err := s.db.Query(`SELECT `+s.runSelectColumns()+` FROM runs WHERE project_id=? AND job=? ORDER BY id DESC`, projectID, job)
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

// RunsForProject returns retained runs for a project, newest first. An empty job matches all jobs.
func (s *Store) RunsForProject(project, job string, limit int) ([]Run, error) {
	query := `SELECT ` + s.qualifiedRunSelectColumns() + ` FROM runs JOIN projects ON projects.id=runs.project_id WHERE projects.path=?`
	args := []any{project}
	if job != "" {
		query += ` AND runs.job=?`
		args = append(args, job)
	}
	query += ` ORDER BY runs.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
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

// LatestRunsForProject returns the latest retained run per job for a canonical project path.
func (s *Store) LatestRunsForProject(project string) ([]Run, error) {
	rows, err := s.db.Query(`SELECT `+s.qualifiedRunSelectColumns()+` FROM runs JOIN projects ON projects.id=runs.project_id WHERE projects.path=? AND runs.id IN (SELECT MAX(id) FROM runs WHERE project_id=(SELECT id FROM projects WHERE path=?) GROUP BY job) ORDER BY runs.id DESC`, project, project)
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

// ReadRunLog reads the persisted bytes without creating or changing the log file.
func (s *Store) ReadRunLog(run Run) ([]byte, error) {
	if !run.LogPath.Valid {
		return nil, ErrLogUnavailable
	}
	relative := run.LogPath.String
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("unsafe log path %q", relative)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, relative))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrLogUnavailable
	}
	if err != nil {
		return nil, fmt.Errorf("read run log: %w", err)
	}
	return data, nil
}
