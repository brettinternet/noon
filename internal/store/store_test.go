package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/runner"
)

func testStore(t *testing.T) (*Store, int64) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	project, err := s.RegisterProject(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return s, project
}

// Never-started scheduled records must remain readable and bounded without a
// successful process completion to trigger retention.
func TestScheduledHistoryRetention(t *testing.T) {
	s, projectID := testStore(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := s.SetScheduler(SchedulerRecord{PID: 1, StartIdentity: "test", StartedAt: FormatTime(now), HeartbeatAt: FormatTime(now)}); err != nil {
		t.Fatal(err)
	}
	_, err := s.ScheduleTick(now, 1, "test", func(SchedulerSnapshot) (SchedulerPlan, error) {
		plan := SchedulerPlan{}
		for i := 0; i < 51; i++ {
			plan.Create = append(plan.Create, ScheduledRunInput{ProjectID: projectID, Job: "job", Status: "missed", Trigger: "schedule", ScheduledAt: now, DueAt: now})
		}
		plan.Create = append(plan.Create, ScheduledRunInput{ProjectID: projectID, Job: "job", Status: "queued", Trigger: "schedule", ScheduledAt: now, DueAt: now})
		return plan, nil
	}, func(SchedulerSnapshot) (DispatchPlan, error) { return DispatchPlan{}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunByID(1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("old missed run retained: %v", err)
	}
	queued, err := s.RunByID(52)
	if err != nil || queued.Status != "queued" || queued.StartedAt != "" {
		t.Fatalf("queued record: %+v %v", queued, err)
	}
	var project string
	if err := s.db.QueryRow(`SELECT path FROM projects WHERE id=?`, projectID).Scan(&project); err != nil {
		t.Fatal(err)
	}
	if runs, err := s.RunsForProject(project, "", 50); err != nil || len(runs) != 50 {
		t.Fatalf("history length=%d error=%v", len(runs), err)
	}
	if runs, err := s.LatestRunsForProject(project); err != nil || len(runs) != 1 || runs[0].ID != 52 {
		t.Fatalf("latest=%+v error=%v", runs, err)
	}
	_, err = s.ScheduleTick(now, 1, "test", func(SchedulerSnapshot) (SchedulerPlan, error) {
		return SchedulerPlan{}, nil
	}, func(SchedulerSnapshot) (DispatchPlan, error) { return DispatchPlan{Expire: []int64{52}}, nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunByID(2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expiry did not prune oldest terminal: %v", err)
	}
	if run, err := s.RunByID(52); err != nil || run.Status != "expired" || run.StartedAt != "" {
		t.Fatalf("expired record: %+v %v", run, err)
	}
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	return pid
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("file %q was not created", path)
}

func TestQueueRunEachRequestsAndCap(t *testing.T) {
	s, projectID := testStore(t)
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	var projectPath string
	if err := s.db.QueryRow(`SELECT path FROM projects WHERE id=?`, projectID).Scan(&projectPath); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableProject(projectPath, nil, nil, now); err != nil {
		t.Fatal(err)
	}
	queue := func(job string, requestEach, coalesce bool, key string, dueAt time.Time, n int) (QueueRunResult, error) {
		t.Helper()
		data := []byte(fmt.Sprintf(`{"n":%d}`, n))
		return s.QueueRun(QueueRunInput{
			ProjectID: projectID, Job: job, Digest: "test", IdempotencyKey: key,
			RequestedBy: "test", DueAt: dueAt, Now: now, JobExists: true, JobEnabled: true,
			Coalesce: coalesce, RequestsEach: requestEach, EventData: data, EventSource: "cli",
		})
	}
	readEventValue := func(run Run) float64 {
		t.Helper()
		path, ok := s.EventFilePath(run)
		if !ok {
			t.Fatalf("run %d has no event file", run.ID)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		value, ok := event.Data["n"].(float64)
		if !ok {
			t.Fatalf("event data = %+v", event.Data)
		}
		return value
	}

	mergedFirst, err := queue("merge", false, true, "", now, 1)
	if err != nil || !mergedFirst.Created {
		t.Fatalf("first merge request: %+v err=%v", mergedFirst, err)
	}
	mergedSecond, err := queue("merge", false, true, "", now, 2)
	if err != nil || mergedSecond.Created || mergedSecond.Reason != "queued" || mergedSecond.Run.ID != mergedFirst.Run.ID || readEventValue(mergedFirst.Run) != 1 {
		t.Fatalf("merge behavior: first=%+v second=%+v err=%v", mergedFirst, mergedSecond, err)
	}

	eachFirst, err := queue("each", true, true, "", now, 1)
	if err != nil || !eachFirst.Created {
		t.Fatalf("first each request: %+v err=%v", eachFirst, err)
	}
	eachSecond, err := queue("each", true, true, "", now, 2)
	if err != nil || !eachSecond.Created || eachSecond.Run.ID == eachFirst.Run.ID || readEventValue(eachFirst.Run) != 1 || readEventValue(eachSecond.Run) != 2 {
		t.Fatalf("each requests did not retain separate payloads: first=%+v second=%+v err=%v", eachFirst, eachSecond, err)
	}
	keyed, err := queue("each", true, true, "delivery-1", now, 3)
	if err != nil || !keyed.Created {
		t.Fatalf("keyed each request: %+v err=%v", keyed, err)
	}
	entriesBeforeDuplicate, err := os.ReadDir(filepath.Join(s.dir, "events"))
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := queue("each", true, true, "delivery-1", now, 4)
	entriesAfterDuplicate, readErr := os.ReadDir(filepath.Join(s.dir, "events"))
	if err != nil || readErr != nil || duplicate.Created || duplicate.Reason != "duplicate" || duplicate.Run.ID != keyed.Run.ID || len(entriesAfterDuplicate) != len(entriesBeforeDuplicate) {
		t.Fatalf("duplicate each request: result=%+v err=%v readErr=%v event counts=%d/%d", duplicate, err, readErr, len(entriesBeforeDuplicate), len(entriesAfterDuplicate))
	}

	future := now.Add(time.Hour)
	for i := 1; i <= 20; i++ {
		result, err := queue("full", true, false, "", future, i)
		if err != nil || !result.Created {
			t.Fatalf("seed future queued run %d: %+v err=%v", i, result, err)
		}
	}
	beforeFull, err := os.ReadDir(filepath.Join(s.dir, "events"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queue("full", true, true, "", now, 99); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("due-now request at cap error = %v, want ErrQueueFull", err)
	}
	afterFull, err := os.ReadDir(filepath.Join(s.dir, "events"))
	if err != nil || len(afterFull) != len(beforeFull) {
		t.Fatalf("full queue created an event file: before=%d after=%d err=%v", len(beforeFull), len(afterFull), err)
	}
	fullRuns, err := s.RunsForJob(projectID, "full")
	if err != nil || len(fullRuns) != 20 {
		t.Fatalf("full queue runs=%d err=%v, want 20", len(fullRuns), err)
	}
	for _, run := range fullRuns {
		if run.Status != "queued" || !run.DueAt.Valid || run.DueAt.String != FormatTime(future) {
			t.Fatalf("future queued run not counted at cap: %+v", run)
		}
	}

	for i := 1; i <= 19; i++ {
		if result, err := queue("race", true, false, "", future, i); err != nil || !result.Created {
			t.Fatalf("seed concurrent queue request %d: %+v err=%v", i, result, err)
		}
	}
	beforeRace, err := os.ReadDir(filepath.Join(s.dir, "events"))
	if err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		result QueueRunResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	var workers sync.WaitGroup
	for i := 20; i <= 21; i++ {
		workers.Add(1)
		go func(n int) {
			defer workers.Done()
			<-start
			result, err := queue("race", true, true, "", now, n)
			results <- outcome{result: result, err: err}
		}(i)
	}
	close(start)
	workers.Wait()
	close(results)
	created, full := 0, 0
	for result := range results {
		switch {
		case result.err == nil && result.result.Created:
			created++
		case errors.Is(result.err, ErrQueueFull):
			full++
		default:
			t.Fatalf("concurrent queue result = %+v err=%v", result.result, result.err)
		}
	}
	if created != 1 || full != 1 {
		t.Fatalf("concurrent cap outcomes: created=%d full=%d", created, full)
	}
	afterRace, err := os.ReadDir(filepath.Join(s.dir, "events"))
	racedRuns, runsErr := s.RunsForJob(projectID, "race")
	if err != nil || runsErr != nil || len(afterRace) != len(beforeRace)+1 || len(racedRuns) != 20 {
		t.Fatalf("concurrent cap state: event files=%d->%d queued runs=%d errors=%v/%v", len(beforeRace), len(afterRace), len(racedRuns), err, runsErr)
	}
}

func createTestRun(t *testing.T, s *Store, project int64, job string) Run {
	t.Helper()
	identity, err := runner.ProcessStartIdentity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateRun(NewRun{ProjectID: project, Job: job, Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: os.Getpid(), OwnerStart: identity})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestStore(t *testing.T) {
	t.Run("previous result follows completion order and retained eligible identity", func(t *testing.T) {
		s, project := testStore(t)
		now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
		other, err := s.RegisterProject(t.TempDir(), now)
		if err != nil {
			t.Fatal(err)
		}
		// Seed a delayed queued ID before the manual runs that complete ahead of it.
		insert := func(projectID int64, job, status string, finished time.Time) int64 {
			t.Helper()
			result, err := s.db.Exec(`INSERT INTO runs(project_id, job, status, trigger, digest, created_at, finished_at)
VALUES(?, ?, ?, 'manual', 'old-definition', ?, ?)`, projectID, job, status, FormatTime(now), FormatTime(finished))
			if err != nil {
				t.Fatal(err)
			}
			id, err := result.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
		current := insert(project, "flip", "queued", now)
		check := func(want string) {
			t.Helper()
			if got, err := s.PreviousStatus(project, "flip", current); err != nil || got != want {
				t.Fatalf("previous=%q err=%v, want %q", got, err, want)
			}
		}
		check("")
		insert(project, "flip", "failed", now.Add(time.Second))
		check("failed")
		insert(project, "flip", "succeeded", now) // higher ID, earlier completion
		check("failed")
		insert(project, "flip", "timed_out", now.Add(time.Second)) // timestamp tie
		check("timed_out")
		insert(other, "flip", "succeeded", now.Add(time.Hour))
		insert(project, "other", "succeeded", now.Add(time.Hour))
		for _, status := range []string{"cancelled", "interrupted", "skipped", "missed", "expired", "queued", "running"} {
			insert(project, "flip", status, now.Add(time.Hour))
			check("timed_out")
		}
		// Exclude only the current ID, even if queried after its completion.
		if _, err := s.db.Exec("UPDATE runs SET status='succeeded', finished_at=? WHERE id=?", FormatTime(now.Add(time.Hour)), current); err != nil {
			t.Fatal(err)
		}
		check("timed_out")
		if _, err := s.db.Exec("DELETE FROM runs WHERE project_id=? AND job=? AND id!=?", project, "flip", current); err != nil {
			t.Fatal(err)
		}
		check("") // pruned eligible history is unknown, not another identity
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := s.PreviousStatus(project, "flip", current); err == nil {
			t.Fatal("lookup error reported as missing history")
		}
	})

	t.Run("migrate and reopen; secure WAL store; absent open is read-only", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "state")
		if _, err := OpenExisting(dir); !errors.Is(err, ErrNoDatabase) {
			t.Fatalf("OpenExisting = %v, want ErrNoDatabase", err)
		}
		if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("OpenExisting created state: %v", err)
		}
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 9 {
			t.Fatalf("user_version=%d err=%v", version, err)
		}
		for _, statement := range []string{
			`SELECT project_id, job, schedule_key, next_at FROM job_state`,
			`SELECT id, pid, start_identity, version, started_at, heartbeat_at FROM scheduler`,
			`SELECT due_at FROM runs`,
			`SELECT config_error FROM projects`,
			`SELECT idempotency_key, cancel_requested_at FROM runs`,
			`SELECT hook_status, hook_exit_code FROM runs`,
			`SELECT event_path, depth FROM runs`,
			`SELECT hook_log_offset FROM runs`,
		} {
			rows, err := s.db.Query(statement)
			if err != nil {
				t.Fatalf("migration schema %q: %v", statement, err)
			}
			_ = rows.Close()
		}
		var journal string
		if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil || journal != "wal" {
			t.Fatalf("journal_mode=%q err=%v", journal, err)
		}
		var foreignKeys int
		if err := s.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
			t.Fatalf("foreign_keys=%d err=%v", foreignKeys, err)
		}
		var busyTimeout int
		if err := s.db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil || busyTimeout != 5000 {
			t.Fatalf("busy_timeout=%d err=%v", busyTimeout, err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		for path, want := range map[string]os.FileMode{dir: 0o700, filepath.Join(dir, databaseName): 0o600} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != want {
				t.Fatalf("%s mode %o, want %o", path, info.Mode().Perm(), want)
			}
		}
		reopened, err := OpenExisting(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("read-only v1 inspection preserves and reads the legacy schema", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, databaseName)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(migrations[0]); err != nil {
			t.Fatal(err)
		}
		created := FormatTime(time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC))
		if _, err := db.Exec(`INSERT INTO projects(path, created_at) VALUES(?, ?)`, "/legacy-project", created); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO runs(project_id, job, status, trigger, digest, created_at, started_at, log_path, log_bytes, log_truncated)
VALUES(1, 'legacy', 'succeeded', 'manual', 'digest', ?, ?, 'logs/1/1.log', 3, 0)`, created, created); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("PRAGMA user_version=1"); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "logs", "1"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "logs", "1", "1.log"), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}

		state, err := OpenExisting(dir)
		if err != nil {
			t.Fatal(err)
		}
		approval, err := state.ApprovalForProject("/legacy-project")
		if err != nil || approval.Enabled || len(approval.Approvals) != 0 {
			t.Fatalf("legacy approval: %+v %v", approval, err)
		}
		legacy, err := state.RunByID(1)
		if err != nil || legacy.LogOverflow {
			t.Fatalf("legacy run = %+v err=%v, want false overflow fallback", legacy, err)
		}
		runs, err := state.RunsForProject("/legacy-project", "", 10)
		if err != nil || len(runs) != 1 || runs[0].ID != 1 || runs[0].LogOverflow {
			t.Fatalf("legacy history = %+v err=%v", runs, err)
		}
		latest, err := state.LatestRunsForProject("/legacy-project")
		if err != nil || len(latest) != 1 || latest[0].Job != "legacy" {
			t.Fatalf("legacy latest runs = %+v err=%v", latest, err)
		}
		log, err := state.ReadRunLog(legacy)
		if err != nil || string(log) != "old" {
			t.Fatalf("legacy log = %q err=%v", log, err)
		}
		if _, err := state.db.Exec(`UPDATE projects SET path=path WHERE id=1`); err == nil {
			t.Fatal("OpenExisting allowed a database write")
		}
		var version int
		if err := state.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 1 {
			t.Fatalf("read-only user_version=%d err=%v, want 1", version, err)
		}
		if err := state.Close(); err != nil {
			t.Fatal(err)
		}

		writer, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		if err := writer.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 9 {
			t.Fatalf("writer user_version=%d err=%v, want migration to 9", version, err)
		}
		migrated, err := writer.RunByID(1)
		if err != nil || migrated.LogOverflow {
			t.Fatalf("migrated legacy run = %+v err=%v", migrated, err)
		}
	})

	t.Run("read-only previous-schema inspection exposes no hook offset", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, databaseName)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		for i, migration := range migrations[:8] {
			if _, err := db.Exec(migration); err != nil {
				t.Fatalf("apply previous migration %d: %v", i+1, err)
			}
			if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
				t.Fatal(err)
			}
		}
		created := FormatTime(time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC))
		if _, err := db.Exec(`INSERT INTO projects(path, created_at) VALUES(?, ?)`, "/previous-project", created); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO runs(project_id, job, status, trigger, digest, created_at, started_at, log_path, log_bytes, log_truncated)
VALUES(1, 'previous', 'succeeded', 'manual', 'digest', ?, ?, 'logs/1/1.log', 3, 0)`, created, created); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		state, err := OpenExisting(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer state.Close()
		previous, err := state.RunByID(1)
		if err != nil || previous.Job != "previous" {
			t.Fatalf("previous-schema run = %+v err=%v", previous, err)
		}
		if previous.HookLogOffset.Valid {
			t.Fatalf("previous-schema hook offset = %+v; want null", previous.HookLogOffset)
		}
	})

	t.Run("newer database is refused without modifying it", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, databaseName)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("PRAGMA user_version=10"); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Open(dir)
		var newer *StateTooNewError
		if !errors.As(err, &newer) || newer.Version != 10 {
			t.Fatalf("Open error = %v, want StateTooNewError(10)", err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("newer database file changed")
		}
	})

	t.Run("one running run per project and job", func(t *testing.T) {
		s, project := testStore(t)
		_ = createTestRun(t, s, project, "tests")
		_, err := s.CreateRun(NewRun{ProjectID: project, Job: "tests", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: 1, OwnerStart: "old"})
		var running *AlreadyRunningError
		if !errors.As(err, &running) || running.RunID == 0 || running.StartedAt == "" {
			t.Fatalf("second run error = %v", err)
		}
	})

	t.Run("retains 50 terminal rows and 10 logs", func(t *testing.T) {
		s, project := testStore(t)
		identity, err := runner.ProcessStartIdentity(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		for i := range 55 {
			now := time.Now().Add(time.Duration(i) * time.Millisecond)
			run, err := s.CreateRun(NewRun{ProjectID: project, Job: "tests", Trigger: "manual", Digest: "test", Now: now, OwnerPID: os.Getpid(), OwnerStart: identity})
			if err != nil {
				t.Fatal(err)
			}
			log, err := s.RunLog(project, run.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := log.Write([]byte(fmt.Sprintf("run %d", i))); err != nil {
				t.Fatal(err)
			}
			logBytes, truncated, err := log.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err := s.SetRunLog(run.ID, log.Path()); err != nil {
				t.Fatal(err)
			}
			exit := 0
			if err := s.FinishRun(run.ID, project, "tests", Outcome{Status: "succeeded", FinishedAt: now, ExitCode: &exit, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated}); err != nil {
				t.Fatal(err)
			}
		}
		runs, err := s.RunsForJob(project, "tests")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 50 {
			t.Fatalf("kept %d runs, want 50", len(runs))
		}
		logs := 0
		for _, run := range runs {
			if run.LogPath.Valid {
				logs++
				if _, err := os.Stat(filepath.Join(s.dir, run.LogPath.String)); err != nil {
					t.Fatalf("retained log %s: %v", run.LogPath.String, err)
				}
			}
		}
		if logs != 10 {
			t.Fatalf("kept %d logs, want 10", logs)
		}
	})

	t.Run("failed log cleanup preserves references and terminal outcome for retry", func(t *testing.T) {
		s, project := testStore(t)
		var oldest Run
		for i := range 50 {
			run := createTestRun(t, s, project, "cleanup")
			if i == 0 {
				oldest = run
			}
			exit := 0
			if err := s.FinishRun(run.ID, project, "cleanup", Outcome{Status: "succeeded", FinishedAt: time.Now(), ExitCode: &exit}); err != nil {
				t.Fatal(err)
			}
		}
		blockedPath := filepath.Join(s.dir, "logs", fmt.Sprint(project), "blocked.log")
		if err := os.MkdirAll(filepath.Dir(blockedPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(blockedPath, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(blockedPath, "child"), []byte("blocks removal"), 0o600); err != nil {
			t.Fatal(err)
		}
		relativePath := filepath.ToSlash(filepath.Join("logs", fmt.Sprint(project), "blocked.log"))
		if _, err := s.db.Exec(`UPDATE runs SET log_path=? WHERE id=?`, relativePath, oldest.ID); err != nil {
			t.Fatal(err)
		}

		finished := createTestRun(t, s, project, "cleanup")
		exit := 0
		if err := s.FinishRun(finished.ID, project, "cleanup", Outcome{Status: "succeeded", FinishedAt: time.Now(), ExitCode: &exit}); err == nil {
			t.Fatal("FinishRun succeeded despite an unremovable old log")
		}
		persisted, err := s.RunByID(finished.ID)
		if err != nil || persisted.Status != "succeeded" || !persisted.ExitCode.Valid || persisted.ExitCode.Int64 != 0 {
			t.Fatalf("terminal outcome was not durable after cleanup failure: run=%+v err=%v", persisted, err)
		}
		retained, err := s.RunByID(oldest.ID)
		if err != nil || !retained.LogPath.Valid || retained.LogPath.String != relativePath {
			t.Fatalf("failed cleanup lost its reference: run=%+v err=%v", retained, err)
		}

		// Cleanup failure must not strand a newly committed hook before execution.
		hooked := createTestRun(t, s, project, "cleanup")
		if err := s.FinishRun(hooked.ID, project, "cleanup", Outcome{Status: "succeeded", FinishedAt: time.Now(), ExitCode: &exit, HookEvent: "on_success"}); err != nil {
			t.Fatalf("retention prevented hook execution: %v", err)
		}
		if err := s.CompleteHook(hooked.ID, "on_success", "ok", &exit, nil); err == nil {
			t.Fatal("hook completion did not report failed retention")
		}
		persisted, err = s.RunByID(hooked.ID)
		if err != nil || persisted.HookStatus.String != "ok" {
			t.Fatalf("retention stranded completed hook: run=%+v err=%v", persisted, err)
		}

		if err := os.Remove(filepath.Join(blockedPath, "child")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(blockedPath); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(blockedPath, []byte("old log"), 0o600); err != nil {
			t.Fatal(err)
		}
		retry := createTestRun(t, s, project, "cleanup")
		if err := s.FinishRun(retry.ID, project, "cleanup", Outcome{Status: "succeeded", FinishedAt: time.Now(), ExitCode: &exit}); err != nil {
			t.Fatalf("retry retention after fixing unlink failure: %v", err)
		}
		if _, err := s.RunByID(oldest.ID); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("old row survived successful cleanup retry: err=%v", err)
		}
		if _, err := os.Stat(blockedPath); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("old log survived successful cleanup retry: %v", err)
		}
	})

	t.Run("running record and log are exempt from retention", func(t *testing.T) {
		s, project := testStore(t)
		active := createTestRun(t, s, project, "tests")
		log, err := s.RunLog(project, active.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte("still running")); err != nil {
			t.Fatal(err)
		}
		if err := s.SetRunLog(active.ID, log.Path()); err != nil {
			t.Fatal(err)
		}
		for i := range 55 {
			path := filepath.Join(s.dir, "logs", fmt.Sprint(project), fmt.Sprintf("terminal-%d.log", i))
			if err := os.WriteFile(path, []byte("terminal"), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := s.db.Exec(`INSERT INTO runs(project_id,job,status,trigger,digest,created_at,started_at,finished_at,log_path,log_bytes) VALUES(?,?,'succeeded','manual','test',?,?,?, ?,8)`, project, "tests", FormatTime(time.Now().Add(time.Duration(i)*time.Millisecond)), FormatTime(time.Now()), FormatTime(time.Now()), fmt.Sprintf("logs/%d/terminal-%d.log", project, i))
			if err != nil {
				t.Fatal(err)
			}
		}
		tx, err := s.db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		if err := pruneRuns(tx, s.dir, project, "tests"); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		got, err := s.RunByID(active.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "running" || !got.LogPath.Valid {
			t.Fatalf("active run pruned: %+v", got)
		}
		if _, err := os.Stat(filepath.Join(s.dir, got.LogPath.String)); err != nil {
			t.Fatalf("active log removed: %v", err)
		}
		_, _, _ = log.Close()
	})

	t.Run("bounded output preserves first and last MiB", func(t *testing.T) {
		s, project := testStore(t)
		run := createTestRun(t, s, project, "output")
		log, err := s.RunLog(project, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		const mib = 1 << 20
		input := bytes.Join([][]byte{bytes.Repeat([]byte{'A'}, mib), bytes.Repeat([]byte{'B'}, mib), bytes.Repeat([]byte{'C'}, mib)}, nil)
		if _, err := log.Write(input); err != nil {
			t.Fatal(err)
		}
		active, err := s.RunByID(run.ID)
		if err != nil || !active.LogOverflow {
			t.Fatalf("live overflow was not recorded: run=%+v err=%v", active, err)
		}
		logBytes, truncated, err := log.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !truncated {
			t.Fatal("large output was not marked truncated")
		}
		if err := s.SetRunLog(run.ID, log.Path()); err != nil {
			t.Fatal(err)
		}
		exit := 0
		if err := s.FinishRun(run.ID, project, "output", Outcome{Status: "succeeded", FinishedAt: time.Now(), ExitCode: &exit, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated}); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(s.dir, log.Path()))
		if err != nil {
			t.Fatal(err)
		}
		want := bytes.Join([][]byte{input[:mib], []byte(fmt.Sprintf("\n[noon: %d bytes omitted]\n", mib)), input[2*mib:]}, nil)
		if !bytes.Equal(got, want) {
			t.Fatalf("bounded log differs: got %d bytes, want %d", len(got), len(want))
		}
		if logBytes != int64(len(want)) {
			t.Fatalf("stored byte count=%d want=%d", logBytes, len(want))
		}
	})
}

func TestHookRecoveryAndRetention(t *testing.T) {
	t.Run("orphaned lifecycle hook is marked interrupted without changing run status", func(t *testing.T) {
		state, project := testStore(t)
		ownerPID := deadPID(t)
		run, err := state.CreateRun(NewRun{ProjectID: project, Job: "hook", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: ownerPID, OwnerStart: "dead-owner"})
		if err != nil {
			t.Fatal(err)
		}
		exit := 0
		if err := state.FinishRun(run.ID, project, "hook", Outcome{Status: "succeeded", FinishedAt: time.Now(), ExitCode: &exit, HookEvent: "on_success"}); err != nil {
			t.Fatal(err)
		}
		if err := state.Recover(time.Now(), time.Second); err != nil {
			t.Fatal(err)
		}
		run, err = state.RunByID(run.ID)
		if err != nil || run.Status != "succeeded" || run.HookStatus.String != "interrupted" || run.HookExitCode.Valid {
			t.Fatalf("recovered hook run = %+v err=%v", run, err)
		}
	})

	t.Run("hook logs are protected from retention until the hook completes", func(t *testing.T) {
		state, project := testStore(t)
		active := createTestRun(t, state, project, "retention")
		log, err := state.RunLog(project, active.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.SetRunLog(active.ID, log.Path()); err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte("job output\n")); err != nil {
			t.Fatal(err)
		}
		logBytes, truncated, err := log.Close()
		if err != nil {
			t.Fatal(err)
		}
		exit := 0
		if err := state.FinishRun(active.ID, project, "retention", Outcome{Status: "succeeded", FinishedAt: time.Now(), ExitCode: &exit, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated, HookEvent: "on_success"}); err != nil {
			t.Fatal(err)
		}
		active, err = state.RunByID(active.ID)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 10; i++ {
			run := createTestRun(t, state, project, "retention")
			if err := state.FinishRun(run.ID, project, "retention", Outcome{Status: "succeeded", FinishedAt: time.Now().Add(time.Duration(i+1) * time.Second), ExitCode: &exit}); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := os.Stat(filepath.Join(state.dir, active.LogPath.String)); err != nil {
			t.Fatalf("running hook log was pruned early: %v", err)
		}
		if err := state.CompleteHook(active.ID, "on_success", "ok", &exit, []byte("hook output\n")); err != nil {
			t.Fatal(err)
		}
		stored, err := state.RunByID(active.ID)
		if err != nil || stored.HookStatus.String != "ok" || stored.LogPath.Valid {
			t.Fatalf("completed old hook was not safely pruned: %+v err=%v", stored, err)
		}
	})
}

func TestRecovery(t *testing.T) {
	t.Run("dead owner is interrupted and never restarted", func(t *testing.T) {
		s, project := testStore(t)
		ownerPID := deadPID(t)
		run, err := s.CreateRun(NewRun{ProjectID: project, Job: "dead", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: ownerPID, OwnerStart: "dead-start"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Recover(time.Now(), 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		got, err := s.RunsForJob(project, "dead")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != run.ID || got[0].Status != "interrupted" {
			t.Fatalf("recovery rows = %+v", got)
		}
	})

	t.Run("zombie owner is interrupted while its start identity remains available", func(t *testing.T) {
		s, project := testStore(t)
		cmd := exec.Command("sleep", "0.05")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		defer cmd.Wait()
		ownerPID := cmd.Process.Pid
		ownerStart, err := runner.ProcessStartIdentity(ownerPID)
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			identity, err := runner.ProcessStartIdentity(ownerPID)
			if err != nil {
				t.Fatalf("zombie owner lost its start identity: %v", err)
			}
			if identity != ownerStart {
				t.Fatalf("owner identity changed from %q to %q", ownerStart, identity)
			}
			alive, err := runner.ProcessOwnerAlive(ownerPID, ownerStart)
			if err != nil {
				t.Fatal(err)
			}
			if !alive {
				break
			}
			select {
			case <-deadline.C:
				t.Fatal("exited child did not become an owner zombie")
			case <-ticker.C:
			}
		}
		run, err := s.CreateRun(NewRun{ProjectID: project, Job: "zombie", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: ownerPID, OwnerStart: ownerStart})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Recover(time.Now(), 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		got, err := s.RunByID(run.ID)
		if err != nil || got.Status != "interrupted" {
			t.Fatalf("zombie owner recovery = run %+v, err=%v", got, err)
		}
		identity, err := runner.ProcessStartIdentity(ownerPID)
		if err != nil || identity != ownerStart {
			t.Fatalf("zombie leader identity unavailable for safe group checks: %q err=%v", identity, err)
		}
	})

	t.Run("matching leader identity terminates an orphan group", func(t *testing.T) {
		s, project := testStore(t)
		cmd := exec.Command("/bin/sh", "-c", `trap '' TERM; exec sleep 30`)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		pid := cmd.Process.Pid
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		leaderStart, err := runner.ProcessStartIdentity(pid)
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.CreateRun(NewRun{ProjectID: project, Job: "orphan", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: deadPID(t), OwnerStart: "dead-owner"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetProcessIdentity(run.ID, pid, leaderStart); err != nil {
			t.Fatal(err)
		}
		if err := s.Recover(time.Now(), 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Wait(); err == nil {
			t.Fatal("orphaned process group survived recovery")
		}
		got, err := s.RunByID(run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "interrupted" {
			t.Fatalf("stale row status=%q, want interrupted", got.Status)
		}
	})

	t.Run("wrong leader identity is never signalled", func(t *testing.T) {
		s, project := testStore(t)
		cmd := exec.Command("sleep", "30")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		actual, err := runner.ProcessStartIdentity(cmd.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		if actual == "wrong-start-identity" {
			t.Fatal("impossible identity collision")
		}
		ownerPID := deadPID(t)
		run, err := s.CreateRun(NewRun{ProjectID: project, Job: "wrong", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: ownerPID, OwnerStart: "dead-owner"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetProcessIdentity(run.ID, cmd.Process.Pid, "wrong-start-identity"); err != nil {
			t.Fatal(err)
		}
		if err := s.Recover(time.Now(), 20*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
			t.Fatalf("process was signalled or exited despite wrong identity %q: %v", actual, err)
		}
		got, err := s.RunByID(run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != "interrupted" {
			t.Fatalf("stale row status=%q, want interrupted", got.Status)
		}
	})
}

// Two cancellation callers can snapshot the same orphan before either records its terminal state.
func TestConcurrentRecoveryOfSameCancelledOrphan(t *testing.T) {
	s, project := testStore(t)
	other, err := Open(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()

	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf(`trap '' TERM; printf ready > %s; while :; do :; done`, ready))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	waitForFile(t, ready)
	leaderStart, err := runner.ProcessStartIdentity(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateRun(NewRun{ProjectID: project, Job: "concurrent-cancel", Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: os.Getpid(), OwnerStart: "stale-owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProcessIdentity(run.ID, cmd.Process.Pid, leaderStart); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestCancelRun(run.ID, time.Now()); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	for _, state := range []*Store{s, other} {
		go func(state *Store) {
			<-start
			results <- state.RecoverRuns([]int64{run.ID}, time.Now(), 200*time.Millisecond)
		}(state)
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("concurrent recovery failed: %v", err)
		}
	}
	finished, err := s.RunByID(run.ID)
	if err != nil || finished.Status != "cancelled" {
		t.Fatalf("recovered cancellation = %+v, err=%v", finished, err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("orphaned process group survived concurrent recovery")
	}
}

// Recovery for a cancellation is limited to its selected runs, and a single grace budget covers all selected groups.
func TestRecoverRunsScopesAndSharesCancellationBudget(t *testing.T) {
	s, project := testStore(t)
	type orphan struct {
		run Run
		cmd *exec.Cmd
	}
	startOrphan := func(job string) orphan {
		t.Helper()
		ready := filepath.Join(t.TempDir(), "ready")
		cmd := exec.Command("/bin/sh", "-c", fmt.Sprintf(`trap '' TERM; printf ready > %s; while :; do :; done`, ready))
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		waitForFile(t, ready)
		leaderStart, err := runner.ProcessStartIdentity(cmd.Process.Pid)
		if err != nil {
			t.Fatal(err)
		}
		run, err := s.CreateRun(NewRun{ProjectID: project, Job: job, Trigger: "manual", Digest: "test", Now: time.Now(), OwnerPID: os.Getpid(), OwnerStart: "stale-owner"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetProcessIdentity(run.ID, cmd.Process.Pid, leaderStart); err != nil {
			t.Fatal(err)
		}
		return orphan{run: run, cmd: cmd}
	}
	first := startOrphan("selected-one")
	second := startOrphan("selected-two")
	unrelated := startOrphan("unrelated")
	for _, selected := range []orphan{first, second} {
		if _, err := s.RequestCancelRun(selected.run.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	started := time.Now()
	if err := s.RecoverRuns([]int64{first.run.ID, second.run.ID}, time.Now(), 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("selected recovery exceeded shared budget: %s", elapsed)
	}
	for _, selected := range []orphan{first, second} {
		finished, err := s.RunByID(selected.run.ID)
		if err != nil || finished.Status != "cancelled" {
			t.Fatalf("selected orphan recovery = %+v, err=%v", finished, err)
		}
		if err := selected.cmd.Wait(); err == nil {
			t.Fatalf("selected process group for run %d survived recovery", selected.run.ID)
		}
	}
	if err := unrelated.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("recovery signalled unrelated process group: %v", err)
	}
	unrelatedRun, err := s.RunByID(unrelated.run.ID)
	if err != nil || unrelatedRun.Status != "running" {
		t.Fatalf("unrelated orphan changed: %+v, err=%v", unrelatedRun, err)
	}
}

func TestDatabaseTimeFormat(t *testing.T) {
	got := FormatTime(time.Date(2026, 2, 3, 4, 5, 6, 789123000, time.FixedZone("offset", 3600)))
	if got != "2026-02-03T03:05:06.789Z" || strings.Contains(got, "+") {
		t.Fatalf("FormatTime = %q", got)
	}
}
