package store

import (
	"bytes"
	"database/sql"
	"encoding/binary"
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

func TestNoopRetentionPreservesResultsAndState(t *testing.T) {
	state, projectID := testStore(t)
	finishedAt := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	finishLogged := func(status string, exitCode int) Run {
		t.Helper()
		run := createTestRun(t, state, projectID, "poll")
		log, err := state.RunLog(projectID, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.SetRunLog(run.ID, log.Path()); err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte(status + " output\n")); err != nil {
			t.Fatal(err)
		}
		logBytes, truncated, err := log.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := state.FinishRun(run.ID, projectID, run.Job, Outcome{
			Status: status, FinishedAt: finishedAt.Add(time.Duration(run.ID) * time.Millisecond),
			ExitCode: &exitCode, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated,
		}); err != nil {
			t.Fatal(err)
		}
		stored, err := state.RunByID(run.ID)
		if err != nil {
			t.Fatal(err)
		}
		return stored
	}
	succeeded := finishLogged("succeeded", 0)
	failed := finishLogged("failed", 3)
	noopIDs := make([]int64, 60)
	for i := range noopIDs {
		noopIDs[i] = finishLogged("noop", 78).ID
	}

	runs, err := state.RunsForJob(projectID, "poll")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 12 {
		t.Fatalf("retained %d runs; want two results and ten noops", len(runs))
	}
	var retainedNoops []Run
	for _, run := range runs {
		if run.Status == "noop" {
			retainedNoops = append(retainedNoops, run)
		}
	}
	if len(retainedNoops) != 10 {
		t.Fatalf("retained %d noops, want 10", len(retainedNoops))
	}
	for i, run := range retainedNoops {
		wantID := noopIDs[len(noopIDs)-1-i]
		if run.ID != wantID || !run.LogPath.Valid {
			t.Fatalf("retained noop %d = %+v, want newest ID %d with log", i, run, wantID)
		}
		log, err := state.ReadRunLog(run)
		if err != nil || string(log) != "noop output\n" {
			t.Fatalf("retained noop log %d = %q, err=%v", run.ID, log, err)
		}
	}
	for _, result := range []struct {
		run  Run
		want string
	}{{succeeded, "succeeded output\n"}, {failed, "failed output\n"}} {
		stored, err := state.RunByID(result.run.ID)
		if err != nil || stored.Status != result.run.Status || !stored.LogPath.Valid {
			t.Fatalf("real result %d not retained: %+v err=%v", result.run.ID, stored, err)
		}
		log, err := state.ReadRunLog(stored)
		if err != nil || string(log) != result.want {
			t.Fatalf("real result log %d = %q err=%v", stored.ID, log, err)
		}
	}
	if _, err := state.RunByID(noopIDs[49]); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("older noop run %d was retained: %v", noopIDs[49], err)
	}
	lastSuccess, found, err := state.LastSuccess(projectID, "poll")
	if err != nil || !found || lastSuccess != succeeded.StartedAt {
		t.Fatalf("last success = %q found=%t err=%v, want %q", lastSuccess, found, err, succeeded.StartedAt)
	}
	if previous, err := state.PreviousStatus(projectID, "poll", noopIDs[59]); err != nil || previous != "failed" {
		t.Fatalf("previous result after noops = %q err=%v, want failed", previous, err)
	}
	var projectPath string
	if err := state.db.QueryRow(`SELECT path FROM projects WHERE id=?`, projectID).Scan(&projectPath); err != nil {
		t.Fatal(err)
	}
	latest, err := state.LatestRunsForProject(projectPath)
	if err != nil || len(latest) != 1 || latest[0].ID != failed.ID || latest[0].Status != "failed" {
		t.Fatalf("latest result = %+v err=%v, want last real failure", latest, err)
	}
}

func TestSkippedRetentionPreservesResultsAndState(t *testing.T) {
	state, projectID := testStore(t)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	var results []Run
	for i, status := range []string{"succeeded", "failed"} {
		run, err := state.CreateRun(NewRun{ProjectID: projectID, Job: "poll", Trigger: "manual", Now: now.Add(time.Duration(i) * time.Second)})
		if err != nil {
			t.Fatal(err)
		}
		log, err := state.RunLog(projectID, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write([]byte(status + " output\n")); err != nil {
			t.Fatal(err)
		}
		logBytes, truncated, err := log.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := state.FinishRun(run.ID, projectID, run.Job, Outcome{
			Status: status, FinishedAt: now.Add(time.Duration(i+1) * time.Second),
			ExitCode: &i, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated,
		}); err != nil {
			t.Fatal(err)
		}
		run.Status = status
		results = append(results, run)
	}
	if err := state.SetScheduler(SchedulerRecord{PID: 1, StartIdentity: "test", StartedAt: FormatTime(now), HeartbeatAt: FormatTime(now)}); err != nil {
		t.Fatal(err)
	}
	for i := range 60 {
		tick := now.Add(time.Duration(i+1) * time.Minute)
		_, err := state.ScheduleTick(tick, 1, "test", func(SchedulerSnapshot) (SchedulerPlan, error) {
			return SchedulerPlan{Create: []ScheduledRunInput{{
				ProjectID: projectID, Job: "poll", Status: "skipped", Trigger: "schedule", ScheduledAt: tick, DueAt: tick,
			}}}, nil
		}, func(SchedulerSnapshot) (DispatchPlan, error) { return DispatchPlan{}, nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	runs, err := state.RunsForJob(projectID, "poll")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 12 {
		t.Fatalf("retained %d runs; want two results and ten skips", len(runs))
	}
	for i, run := range runs[:10] {
		want := FormatTime(now.Add(time.Duration(60-i) * time.Minute))
		if run.Status != "skipped" || run.ScheduledAt.String != want {
			t.Fatalf("retained skip %d = %+v, want occurrence %s", i, run, want)
		}
	}
	for _, result := range results {
		stored, err := state.RunByID(result.ID)
		if err != nil || stored.Status != result.Status || !stored.LogPath.Valid {
			t.Fatalf("real result %d not retained: %+v err=%v", result.ID, stored, err)
		}
		log, err := state.ReadRunLog(stored)
		if err != nil || string(log) != result.Status+" output\n" {
			t.Fatalf("real result log %d = %q err=%v", result.ID, log, err)
		}
	}
	if previous, err := state.PreviousStatus(projectID, "poll", runs[0].ID); err != nil || previous != "failed" {
		t.Fatalf("previous result after skips = %q err=%v, want failed", previous, err)
	}
	lastSuccess, found, err := state.LastSuccess(projectID, "poll")
	if err != nil || !found || lastSuccess != results[0].StartedAt {
		t.Fatalf("last success = %q found=%t err=%v, want %q", lastSuccess, found, err, results[0].StartedAt)
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

func TestRunLogTimingCaptureAndTruncation(t *testing.T) {
	t.Run("first-byte time survives split writes and interleaved streams", func(t *testing.T) {
		state, project := testStore(t)
		run := createTestRun(t, state, project, "timed")
		log, err := state.RunLog(project, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		clock := []time.Time{
			time.Date(2026, 10, 19, 12, 0, 0, 123_000_000, time.UTC),
			time.Date(2026, 10, 19, 12, 0, 1, 456_000_000, time.UTC),
			time.Date(2026, 10, 19, 12, 0, 2, 789_000_000, time.UTC),
			time.Date(2026, 10, 19, 12, 0, 3, 987_000_000, time.UTC),
		}
		calls := 0
		log.now = func() time.Time {
			value := clock[calls]
			calls++
			return value
		}
		// These sequential writes model stdout/stderr writes through RunLog's shared
		// serialized writer; the first line is split across writes.
		for _, chunk := range [][]byte{[]byte("partial"), []byte("-stdout\n"), []byte("stderr:\nsecond"), []byte("-tail\n")} {
			if _, err := log.Write(chunk); err != nil {
				t.Fatal(err)
			}
		}
		logBytes, truncated, err := log.Close()
		if err != nil || truncated {
			t.Fatalf("close timed log: bytes=%d truncated=%t err=%v", logBytes, truncated, err)
		}
		if err := state.SetRunLog(run.ID, log.Path()); err != nil {
			t.Fatal(err)
		}
		run.LogPath = sql.NullString{String: log.Path(), Valid: true}
		data, records, err := state.ReadRunLogWithTiming(run)
		if err != nil {
			t.Fatal(err)
		}
		want := []byte("partial-stdout\nstderr:\nsecond-tail\n")
		if !bytes.Equal(data, want) {
			t.Fatalf("raw bytes changed: got %q want %q", data, want)
		}
		for _, tc := range []struct {
			offset int64
			want   time.Time
		}{{0, clock[0]}, {int64(len("partial-stdout\n")), clock[2]}, {int64(len("partial-stdout\nstderr:\n")), clock[2]}} {
			got, ok := TimingAt(records, tc.offset)
			if !ok || got != tc.want.UnixMilli() {
				t.Fatalf("line offset %d timing=%d covered=%t; want %d", tc.offset, got, ok, tc.want.UnixMilli())
			}
		}
		if _, ok := TimingAt(records, int64(len(data))); ok {
			t.Fatal("coverage extended beyond the raw log size")
		}
		timingInfo, err := os.Stat(filepath.Join(state.dir, log.Path()) + ".timing")
		if err != nil || timingInfo.Mode().Perm() != 0o600 {
			t.Fatalf("timing sidecar mode=%v err=%v; want 0600", timingInfo, err)
		}
	})

	t.Run("tail timing is rebased after an unavailable omission marker", func(t *testing.T) {
		state, project := testStore(t)
		run := createTestRun(t, state, project, "truncated")
		log, err := state.RunLog(project, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		headAt := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
		tailAt := headAt.Add(time.Second)
		calls := 0
		log.now = func() time.Time {
			calls++
			if calls == 1 {
				return headAt
			}
			return tailAt
		}
		head := bytes.Repeat([]byte{'h'}, maxRunLogBytes)
		tail := bytes.Repeat([]byte{'x'}, maxRunLogBytes+3)
		if _, err := log.Write(head); err != nil {
			t.Fatal(err)
		}
		if _, err := log.Write(tail); err != nil {
			t.Fatal(err)
		}
		logBytes, truncated, err := log.Close()
		if err != nil || !truncated {
			t.Fatalf("close truncated log: bytes=%d truncated=%t err=%v", logBytes, truncated, err)
		}
		if err := state.SetRunLog(run.ID, log.Path()); err != nil {
			t.Fatal(err)
		}
		run.LogPath = sql.NullString{String: log.Path(), Valid: true}
		data, records, err := state.ReadRunLogWithTiming(run)
		if err != nil {
			t.Fatal(err)
		}
		marker := []byte("\n[noon: 3 bytes omitted]\n")
		want := append(append(bytes.Clone(head), marker...), tail[len(tail)-maxRunLogBytes:]...)
		if !bytes.Equal(data, want) {
			t.Fatalf("truncated raw log changed: bytes=%d want=%d", len(data), len(want))
		}
		markerStart := int64(maxRunLogBytes + 1)
		tailStart := int64(maxRunLogBytes + len(marker))
		if _, ok := TimingAt(records, markerStart); ok {
			t.Fatal("synthetic omission marker acquired a timestamp")
		}
		if got, ok := TimingAt(records, tailStart); !ok || got != tailAt.UnixMilli() {
			t.Fatalf("rebased retained tail time=%d covered=%t; want %d", got, ok, tailAt.UnixMilli())
		}
	})
}

func TestRunLogTimingFailureDoesNotFailRawWrites(t *testing.T) {
	state, project := testStore(t)
	run := createTestRun(t, state, project, "timing-failure")
	log, err := state.RunLog(project, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := log.timing.Close(); err != nil {
		t.Fatal(err)
	}
	if n, err := log.Write([]byte("second\n")); err != nil || n != len("second\n") {
		t.Fatalf("sidecar failure stopped raw log write: n=%d err=%v", n, err)
	}
	logBytes, truncated, err := log.Close()
	if err != nil || truncated {
		t.Fatalf("sidecar failure affected run close: bytes=%d truncated=%t err=%v", logBytes, truncated, err)
	}
	if err := state.SetRunLog(run.ID, log.Path()); err != nil {
		t.Fatal(err)
	}
	run.LogPath = sql.NullString{String: log.Path(), Valid: true}
	data, records, err := state.ReadRunLogWithTiming(run)
	if err != nil || string(data) != "first\nsecond\n" {
		t.Fatalf("sidecar failure changed raw output: data=%q err=%v", data, err)
	}
	if _, ok := TimingAt(records, 0); !ok {
		t.Fatal("sidecar failure discarded timing captured before it")
	}
	if _, ok := TimingAt(records, int64(len("first\n"))); ok {
		t.Fatal("line written after sidecar failure acquired a timestamp")
	}
}

func TestRunLogReplacesStaleTimingSidecar(t *testing.T) {
	state, project := testStore(t)
	run := createTestRun(t, state, project, "stale")
	dir := filepath.Join(state.dir, "logs", fmt.Sprint(project))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// An orphaned sidecar from an earlier run with the same ID must not time the new log.
	stale := make([]byte, timingRecordSize)
	binary.LittleEndian.PutUint64(stale[8:16], 100)
	binary.LittleEndian.PutUint64(stale[16:24], 1)
	if err := os.WriteFile(timingPath(filepath.Join(dir, fmt.Sprintf("%d.log", run.ID))), stale, 0o600); err != nil {
		t.Fatal(err)
	}
	log, err := state.RunLog(project, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	log.now = func() time.Time { return time.UnixMilli(5000) }
	if _, err := log.Write([]byte("fresh\n")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := log.Close(); err != nil {
		t.Fatal(err)
	}
	run.LogPath = sql.NullString{String: log.Path(), Valid: true}
	_, records, err := state.ReadRunLogWithTiming(run)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := TimingAt(records, 0); !ok || got != 5000 {
		t.Fatalf("new log timing=%d covered=%t; want fresh capture 5000", got, ok)
	}
}

func TestRunLogTimingMissingOrIncompleteSidecar(t *testing.T) {
	state, project := testStore(t)
	run := createTestRun(t, state, project, "incomplete")
	log, err := state.RunLog(project, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	log.now = func() time.Time { return time.UnixMilli(1000) }
	_, _ = log.Write([]byte("known\n"))
	log.now = func() time.Time { return time.UnixMilli(2000) }
	_, _ = log.Write([]byte("unavailable\n"))
	if _, _, err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if err := state.SetRunLog(run.ID, log.Path()); err != nil {
		t.Fatal(err)
	}
	run.LogPath = sql.NullString{String: log.Path(), Valid: true}
	logPath := filepath.Join(state.dir, log.Path())
	sidecar := timingPath(logPath)
	want := []byte("known\nunavailable\n")
	if err := os.Remove(sidecar); err != nil {
		t.Fatal(err)
	}
	data, records, err := state.ReadRunLogWithTiming(run)
	if err != nil || !bytes.Equal(data, want) || len(records) != 0 {
		t.Fatalf("missing sidecar affected raw log: data=%q records=%+v err=%v", data, records, err)
	}
	first := TimingRecord{Start: 0, End: int64(len("known\n")), UnixMilli: 1000}
	encoded := make([]byte, timingRecordSize+5)
	binary.LittleEndian.PutUint64(encoded[0:8], uint64(first.Start))
	binary.LittleEndian.PutUint64(encoded[8:16], uint64(first.End))
	binary.LittleEndian.PutUint64(encoded[16:24], uint64(first.UnixMilli))
	copy(encoded[timingRecordSize:], []byte{1, 2, 3, 4, 5})
	if err := os.WriteFile(sidecar, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	data, records, err = state.ReadRunLogWithTiming(run)
	if err != nil || !bytes.Equal(data, want) || len(records) != 1 {
		t.Fatalf("torn sidecar affected raw log: data=%q records=%+v err=%v", data, records, err)
	}
	if got, ok := TimingAt(records, 0); !ok || got != 1000 {
		t.Fatalf("valid prefix timing=%d covered=%t", got, ok)
	}
	if _, ok := TimingAt(records, int64(len("known\n"))); ok {
		t.Fatal("torn record borrowed the previous line's timestamp")
	}
}

func TestCompleteHookRebasesTimingPastSyntheticMarker(t *testing.T) {
	state, project := testStore(t)
	run := createTestRun(t, state, project, "hook-timing")
	log, err := state.RunLog(project, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("job output\n")); err != nil {
		t.Fatal(err)
	}
	logBytes, truncated, err := log.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SetRunLog(run.ID, log.Path()); err != nil {
		t.Fatal(err)
	}
	exit := 0
	if err := state.FinishRun(run.ID, project, run.Job, Outcome{Status: "succeeded", FinishedAt: time.Now(), ExitCode: &exit, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated, HookEvent: "on_success"}); err != nil {
		t.Fatal(err)
	}
	receipt := time.Date(2026, 10, 19, 15, 0, 0, 123_000_000, time.UTC)
	output := []byte("hook output\n")
	if err := state.CompleteHookWithTiming(run.ID, "on_success", "ok", &exit, output, []TimingRecord{{Start: 0, End: int64(len(output)), UnixMilli: receipt.UnixMilli()}}); err != nil {
		t.Fatal(err)
	}
	stored, err := state.RunByID(run.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, records, err := state.ReadRunLogWithTiming(stored)
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte("[noon: on_success hook exited 0]\n")
	markerStart := int64(len("job output\n"))
	hookStart := markerStart + int64(len(marker))
	if !bytes.Contains(data, append(marker, output...)) {
		t.Fatalf("hook bytes missing from log: %q", data)
	}
	if _, ok := TimingAt(records, markerStart); ok {
		t.Fatal("synthetic hook marker acquired a timestamp")
	}
	if got, ok := TimingAt(records, hookStart); !ok || got != receipt.UnixMilli() {
		t.Fatalf("hook timing=%d covered=%t; want %d", got, ok, receipt.UnixMilli())
	}
}

// Offline temporary-job terminal transitions must keep the normal 50-run
// history and 10-payload limits, including replacement and natural expiry.
func TestTemporaryJobCancellationRetention(t *testing.T) {
	for _, mode := range []string{"remove", "replace", "expire"} {
		t.Run(mode, func(t *testing.T) {
			state, projectID := testStore(t)
			var project string
			if err := state.db.QueryRow("SELECT path FROM projects WHERE id=?", projectID).Scan(&project); err != nil {
				t.Fatal(err)
			}
			start := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
			create := func(now time.Time) TemporaryJob {
				t.Helper()
				value, added, err := state.CreateTemporaryJob(TemporaryJob{ProjectID: projectID, Name: "watch", Definition: []byte("jobs:\n  watch: {run: ['true']}\n"), Digest: "creation", CreatedAt: now, Until: now.Add(time.Minute), RequestedBy: "cli"}, now)
				if err != nil || !added {
					t.Fatalf("create: %+v %t %v", value, added, err)
				}
				return value
			}
			var paths []string
			for i := range 51 {
				now := start.Add(time.Duration(i) * 2 * time.Minute)
				value := create(now)
				queued, err := state.QueueRun(QueueRunInput{ProjectID: projectID, Job: "watch", Digest: "effective", TemporaryJob: true, TemporaryDigest: value.Digest, TemporaryCreatedAt: value.CreatedAt, JobExists: true, JobEnabled: true, Now: now, DueAt: now.Add(30 * time.Second), EventData: []byte(`{"probe":true}`), EventSource: "cli"})
				if err != nil {
					t.Fatal(err)
				}
				paths = append(paths, queued.Run.EventPath.String)
				switch mode {
				case "remove":
					if err := state.DeleteTemporaryJob(project, "watch", now); err != nil {
						t.Fatal(err)
					}
				case "expire":
					if err := state.PurgeTemporaryJobs(value.Until); err != nil {
						t.Fatal(err)
					}
				}
			}
			if mode == "replace" {
				create(start.Add(102 * time.Minute))
			}
			var count int
			if err := state.db.QueryRow("SELECT count(*) FROM runs WHERE project_id=? AND job='watch' AND status='cancelled'", projectID).Scan(&count); err != nil || count != 50 {
				t.Fatalf("terminal retention count=%d err=%v", count, err)
			}
			if err := state.db.QueryRow("SELECT count(*) FROM runs WHERE project_id=? AND job='watch' AND event_path IS NOT NULL", projectID).Scan(&count); err != nil || count != 10 {
				t.Fatalf("payload retention count=%d err=%v", count, err)
			}
			for i, path := range paths {
				_, err := os.Stat(filepath.Join(state.dir, path))
				if i < len(paths)-10 && !os.IsNotExist(err) {
					t.Fatalf("old payload retained: %s %v", path, err)
				}
				if i >= len(paths)-10 && err != nil {
					t.Fatalf("recent payload lost: %s %v", path, err)
				}
			}
		})
	}
}

// A schema-13 database must retain history while concurrent temporary-job
// creators serialize name ownership; offline reads must honor the grace limit.
func TestTemporaryJobs(t *testing.T) {
	dir := t.TempDir()
	legacy, err := sql.Open("sqlite", filepath.Join(dir, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	for i, migration := range migrations[:13] {
		if _, err := legacy.Exec(migration); err != nil {
			t.Fatal(err)
		}
		if _, err := legacy.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := legacy.Exec("INSERT INTO projects(path,created_at,enabled) VALUES('/temporary','2026-10-16T00:00:00.000Z',1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := legacy.Exec("INSERT INTO runs(project_id,job,status,trigger,digest,created_at) VALUES(1,'probe','succeeded','manual','old','2026-10-16T00:00:00.000Z')"); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	state, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	if run, err := state.RunByID(1); err != nil || run.Status != "succeeded" {
		t.Fatalf("migration lost history: %+v %v", run, err)
	}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	type outcome struct {
		created bool
		err     error
	}
	results := make(chan outcome, 10)
	for i := range 10 {
		go func() {
			peer, err := Open(dir)
			if err != nil {
				results <- outcome{err: err}
				return
			}
			defer peer.Close()
			_, created, err := peer.CreateTemporaryJob(TemporaryJob{ProjectID: 1, Name: "probe", Definition: []byte("jobs:\n  probe: {run: ['true']}\n"), Digest: "d", Until: now.Add(time.Hour), CreatedAt: now, RequestedBy: "cli", Key: fmt.Sprint(i)}, now)
			results <- outcome{created, err}
		}()
	}
	created := 0
	for range 10 {
		result := <-results
		if result.created {
			created++
		} else if !errors.Is(result.err, ErrJobExists) {
			t.Fatalf("competing creation: %+v", result)
		}
	}
	values, err := state.TemporaryJobs("/temporary", now)
	if err != nil || created != 1 || len(values) != 1 {
		t.Fatalf("created=%d values=%+v err=%v", created, values, err)
	}
	value := values[0]
	got, added, err := state.CreateTemporaryJob(value, now)
	if err != nil || added || got.Key != value.Key {
		t.Fatalf("keyed duplicate: %+v %v %v", got, added, err)
	}
	input := QueueRunInput{ProjectID: 1, Job: "probe", Digest: "d", TemporaryJob: true, TemporaryDigest: "d", TemporaryCreatedAt: now, JobExists: true, JobEnabled: true, Now: now, DueAt: now.Add(30 * time.Minute)}
	if _, err := state.QueueRun(input); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := state.RequestCancelJob("/temporary", "probe", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := state.QueueRun(input); !errors.Is(err, ErrJobEnded) {
		t.Fatalf("stale snapshot queued cancelled job: %v", err)
	}
	ended := now.Add(time.Minute)
	reader, err := OpenExisting(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	values, err = reader.TemporaryJobs("/temporary", ended.Add(TemporaryJobGrace-time.Millisecond))
	if err != nil || len(values) != 1 || values[0].EndedAt == nil || !values[0].EndedAt.Equal(ended) {
		t.Fatalf("grace: %+v %v", values, err)
	}
	values, err = reader.TemporaryJobs("/temporary", ended.Add(TemporaryJobGrace))
	if err != nil || len(values) != 0 {
		t.Fatalf("offline expired read: %+v %v", values, err)
	}
	if err := state.PurgeTemporaryJobs(ended.Add(TemporaryJobGrace)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := state.db.QueryRow("SELECT count(*) FROM temporary_jobs").Scan(&count); err != nil || count != 0 {
		t.Fatalf("purge rows: %d %v", count, err)
	}
	if known, err := state.WasTemporaryJob("/temporary", "probe"); err != nil || !known {
		t.Fatalf("name history lost: %v %v", known, err)
	}
	if run, err := state.RunByID(1); err != nil || run.Status != "succeeded" {
		t.Fatalf("purge lost history: %+v %v", run, err)
	}
	// Even an identical-definition replacement must reject an older row identity.
	replacement := value
	replacement.CreatedAt = ended.Add(TemporaryJobGrace)
	replacement.Until = replacement.CreatedAt.Add(time.Hour)
	if _, err := state.db.Exec("UPDATE projects SET enabled=0, enabled_at=?, config_file='/old-explicit.yaml' WHERE id=1", FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	if _, err := state.db.Exec("INSERT INTO job_state(project_id, job, schedule_key, next_at) VALUES(1,'other','every 1h|UTC',?)", FormatTime(now)); err != nil {
		t.Fatal(err)
	}
	if _, added, err := state.CreateTemporaryJob(replacement, replacement.CreatedAt); err != nil || !added {
		t.Fatalf("replacement: %v %v", added, err)
	}
	if states, err := state.ScheduleStatesForProject("/temporary"); err != nil || len(states) != 0 {
		t.Fatalf("reactivation retained disabled phase: %+v %v", states, err)
	}
	if approval, err := state.ApprovalForProject("/temporary"); err != nil || !approval.Enabled || approval.EnabledAt.String != FormatTime(replacement.CreatedAt) || approval.ConfigFile.Valid {
		t.Fatalf("temporary add activation: %+v %v", approval, err)
	}
	input.Now, input.DueAt = replacement.CreatedAt, replacement.CreatedAt
	if _, err := state.QueueRun(input); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("stale identity queued replacement: %v", err)
	}
}

// Competing clients must not create two schedules; schema upgrades preserve history.
func TestTemporarySchedules(t *testing.T) {
	dir := t.TempDir()
	legacy, err := sql.Open("sqlite", filepath.Join(dir, databaseName))
	if err != nil {
		t.Fatal(err)
	}
	for i, migration := range migrations[:12] {
		if _, err := legacy.Exec(migration); err != nil {
			t.Fatal(err)
		}
		if _, err := legacy.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := legacy.Exec("INSERT INTO projects(path,created_at,enabled) VALUES('/temporary','2026-10-16T00:00:00.000Z',1)"); err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	state, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	type outcome struct {
		created bool
		err     error
	}
	results := make(chan outcome, 10)
	for i := range 10 {
		go func() {
			peer, err := Open(dir)
			if err != nil {
				results <- outcome{err: err}
				return
			}
			defer peer.Close()
			_, created, _, err := peer.CreateTemporarySchedule(TemporarySchedule{ProjectID: 1, Job: "probe", Schedule: "every 1m", Until: now.Add(time.Hour), NextAt: now.Add(time.Minute), CreatedAt: now, Key: fmt.Sprint(i), Digest: "d", RequestedBy: "cli", EventSource: "cli"}, QueueRunInput{ProjectID: 1, Job: "probe", JobExists: true, JobEnabled: true, Now: now})
			results <- outcome{created, err}
		}()
	}
	created := 0
	for range 10 {
		result := <-results
		if result.created {
			created++
		} else if !errors.Is(result.err, ErrAlreadyScheduled) {
			t.Fatalf("competing creation: %+v", result)
		}
	}
	values, err := state.TemporarySchedules("/temporary")
	if err != nil || created != 1 || len(values) != 1 {
		t.Fatalf("created=%d values=%+v err=%v", created, values, err)
	}
	value := values[0]
	got, newValue, _, err := state.CreateTemporarySchedule(value, QueueRunInput{ProjectID: 1, Job: "probe", JobExists: true, JobEnabled: true, Now: now})
	if err != nil || newValue || got.Key != value.Key || !got.NextAt.Equal(value.NextAt) {
		t.Fatalf("duplicate: %+v %v %v", got, newValue, err)
	}
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
			wantCount := map[string]int{"": 0, "failed": 1, "timed_out": 2, "succeeded": 0}[want]
			if got, count, err := s.PreviousResults(project, "flip", current); err != nil || got != want || count != wantCount {
				t.Fatalf("results=%q/%d err=%v, want %q/%d", got, count, err, want, wantCount)
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
		for _, status := range []string{"cancelled", "interrupted", "skipped", "missed", "expired", "noop", "queued", "running"} {
			insert(project, "flip", status, now.Add(time.Hour))
			check("timed_out")
		}
		// Exclude only the current ID, even if queried after its completion.
		if _, err := s.db.Exec("UPDATE runs SET status='succeeded', finished_at=? WHERE id=?", FormatTime(now.Add(time.Hour)), current); err != nil {
			t.Fatal(err)
		}
		check("timed_out")
		insert(project, "flip", "succeeded", now.Add(2*time.Second))
		check("succeeded")
		insert(project, "flip", "failed", now.Add(3*time.Second))
		check("failed") // the latest success stops the streak
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
		if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) {
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
			`SELECT agent_session_id FROM runs`,
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
		if err != nil || legacy.LogOverflow || legacy.AgentSessionID.Valid {
			t.Fatalf("legacy run = %+v err=%v, want false overflow and null session ID", legacy, err)
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
		if err := writer.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) {
			t.Fatalf("writer user_version=%d err=%v, want latest schema", version, err)
		}
		migrated, err := writer.RunByID(1)
		if err != nil || migrated.LogOverflow || migrated.AgentSessionID.Valid {
			t.Fatalf("migrated legacy run = %+v err=%v, want null session ID", migrated, err)
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

	t.Run("read-only v10 and migrated history have null session IDs", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, databaseName)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		for i, migration := range migrations[:10] {
			if _, err := db.Exec(migration); err != nil {
				t.Fatalf("apply previous migration %d: %v", i+1, err)
			}
			if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", i+1)); err != nil {
				t.Fatal(err)
			}
		}
		created := FormatTime(time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC))
		if _, err := db.Exec(`INSERT INTO projects(path, created_at) VALUES(?, ?)`, "/v10-project", created); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO runs(project_id, job, status, trigger, digest, created_at, started_at)
VALUES(1, 'legacy', 'succeeded', 'manual', 'digest', ?, ?)`, created, created); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO scheduler VALUES(1, 123, 'legacy-start', '1.0.0', ?, ?)", created, created); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}

		reader, err := OpenExisting(dir)
		if err != nil {
			t.Fatal(err)
		}
		record, exists, err := reader.SchedulerInfo()
		if err != nil || !exists || record.Version != "1.0.0" || record.Commit != "" {
			t.Fatalf("legacy scheduler=%+v exists=%v err=%v", record, exists, err)
		}
		legacy, err := reader.RunByID(1)
		if err != nil || legacy.AgentSessionID.Valid {
			t.Fatalf("read-only v10 run = %+v err=%v, want null session ID", legacy, err)
		}
		var version int
		if err := reader.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 10 {
			t.Fatalf("read-only user_version=%d err=%v, want 10", version, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}

		writer, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		record.Commit = "newcommit"
		if err := writer.SetScheduler(record); err != nil {
			t.Fatal(err)
		}
		saved, exists, err := writer.SchedulerInfo()
		if err != nil || !exists || saved != record {
			t.Fatalf("migrated scheduler=%+v exists=%v err=%v", saved, exists, err)
		}
		migrated, err := writer.RunByID(1)
		if err != nil || migrated.AgentSessionID.Valid {
			t.Fatalf("migrated v10 run = %+v err=%v, want null session ID", migrated, err)
		}
		if err := writer.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != len(migrations) {
			t.Fatalf("migrated user_version=%d err=%v, want latest schema", version, err)
		}
	})

	t.Run("newer database is refused without modifying it", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, databaseName)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", len(migrations)+1)); err != nil {
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
		if !errors.As(err, &newer) || newer.Version != len(migrations)+1 {
			t.Fatalf("Open error = %v, want StateTooNewError for next schema", err)
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
		oldTiming := filepath.Join(s.dir, "logs", fmt.Sprint(project), "1.log.timing")
		if _, err := os.Stat(oldTiming); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("pruned run timing sidecar still exists: %v", err)
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
