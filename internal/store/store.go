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
`}

type Store struct {
	dir            string
	db             *sql.DB
	hasLogOverflow bool
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
	LogBytes                     int64
	LogTruncated, LogOverflow    bool
}

const (
	runColumnsV1          = `id, project_id, job, status, trigger, digest, scheduled_at, created_at, started_at, finished_at, exit_code, signal, error, owner_pid, owner_start, pgid, leader_start, log_path, log_bytes, log_truncated`
	qualifiedRunColumnsV1 = `runs.id, runs.project_id, runs.job, runs.status, runs.trigger, runs.digest, runs.scheduled_at, runs.created_at, runs.started_at, runs.finished_at, runs.exit_code, runs.signal, runs.error, runs.owner_pid, runs.owner_start, runs.pgid, runs.leader_start, runs.log_path, runs.log_bytes, runs.log_truncated`
	runColumns            = runColumnsV1 + `, log_overflow`
	qualifiedRunColumns   = qualifiedRunColumnsV1 + `, runs.log_overflow`
)

func (s *Store) runSelectColumns() string {
	if s.hasLogOverflow {
		return runColumns
	}
	return runColumnsV1 + `, 0`
}

func (s *Store) qualifiedRunSelectColumns() string {
	if s.hasLogOverflow {
		return qualifiedRunColumns
	}
	return qualifiedRunColumnsV1 + `, 0`
}

type runScanner interface {
	Scan(...any) error
}

func scanRun(row runScanner) (Run, error) {
	var run Run
	err := row.Scan(&run.ID, &run.ProjectID, &run.Job, &run.Status, &run.Trigger, &run.Digest, &run.ScheduledAt, &run.CreatedAt, &run.StartedAt, &run.FinishedAt, &run.ExitCode, &run.Signal, &run.Error, &run.OwnerPID, &run.OwnerStart, &run.PGID, &run.LeaderStart, &run.LogPath, &run.LogBytes, &run.LogTruncated, &run.LogOverflow)
	return run, err
}

type NewRun struct {
	ProjectID            int64
	Job, Trigger, Digest string
	Now                  time.Time
	OwnerPID             int
	OwnerStart           string
}

type Outcome struct {
	Status        string
	FinishedAt    time.Time
	ExitCode      *int
	Signal, Error string
	LogPath       string
	LogBytes      int64
	LogTruncated  bool
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

// CreateRun records the running row before the child is started.
func (s *Store) CreateRun(input NewRun) (Run, error) {
	created := FormatTime(input.Now)
	result, err := s.db.Exec(`INSERT INTO runs(project_id, job, status, trigger, digest, created_at, started_at, owner_pid, owner_start)
VALUES(?, ?, 'running', ?, ?, ?, ?, ?, ?)`, input.ProjectID, input.Job, input.Trigger, input.Digest, created, created, input.OwnerPID, input.OwnerStart)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "runs_one_running") || strings.Contains(err.Error(), "UNIQUE constraint failed: runs.project_id, runs.job") {
			var existing Run
			e := s.db.QueryRow(`SELECT id, started_at FROM runs WHERE project_id=? AND job=? AND status='running'`, input.ProjectID, input.Job).Scan(&existing.ID, &existing.StartedAt)
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
	return Run{ID: id, ProjectID: input.ProjectID, Job: input.Job, Status: "running", Trigger: input.Trigger, Digest: input.Digest, CreatedAt: created, StartedAt: created}, nil
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
	rows, err := s.db.Query(`SELECT id, owner_pid, owner_start, pgid, leader_start, log_path, project_id, job FROM runs WHERE status='running'`)
	if err != nil {
		return fmt.Errorf("read running runs: %w", err)
	}
	type staleRun struct {
		id, projectID                    int64
		pid, pgid                        sql.NullInt64
		ownerStart, leaderStart, logPath sql.NullString
		job                              string
	}
	var candidates []staleRun
	for rows.Next() {
		var row staleRun
		if err := rows.Scan(&row.id, &row.pid, &row.ownerStart, &row.pgid, &row.leaderStart, &row.logPath, &row.projectID, &row.job); err != nil {
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
		if row.pgid.Valid && row.leaderStart.Valid {
			if err := runner.TerminateProcessGroup(int(row.pgid.Int64), row.leaderStart.String, grace); err != nil {
				return fmt.Errorf("terminate stale run %d: %w", row.id, err)
			}
		}
		if err := s.finish(row.id, row.projectID, row.job, Outcome{Status: "interrupted", FinishedAt: now, Error: "owner process exited unexpectedly", LogPath: row.logPath.String}); err != nil {
			return err
		}
	}
	return nil
}

// FinishRun records a terminal result and applies run/log retention.
func (s *Store) FinishRun(runID, projectID int64, job string, outcome Outcome) error {
	return s.finish(runID, projectID, job, outcome)
}

func (s *Store) finish(runID, projectID int64, job string, outcome Outcome) error {
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
	result, err := tx.Exec(`UPDATE runs SET status=?, finished_at=?, exit_code=?, signal=?, error=?, log_path=?, log_bytes=?, log_truncated=? WHERE id=? AND status='running'`, outcome.Status, FormatTime(outcome.FinishedAt), exit, nullableString(outcome.Signal), nullableString(outcome.Error), nullableString(outcome.LogPath), outcome.LogBytes, outcome.LogTruncated, runID)
	if err != nil {
		return fmt.Errorf("finish run: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("run %d is not running", runID)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit run result: %w", err)
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
	rows, err := tx.Query(`SELECT id, log_path FROM runs WHERE project_id=? AND job=? AND status NOT IN ('queued','running') ORDER BY id DESC`, projectID, job)
	if err != nil {
		return err
	}
	type retained struct {
		id   int64
		path sql.NullString
	}
	var all []retained
	for rows.Next() {
		var r retained
		if err := rows.Scan(&r.id, &r.path); err != nil {
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
		if i >= 10 && r.path.Valid {
			if err := removeLog(dir, r.path.String); err != nil {
				return err
			}
			if i < 50 {
				if _, err := tx.Exec(`UPDATE runs SET log_path=NULL WHERE id=?`, r.id); err != nil {
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
