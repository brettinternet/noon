package store

import (
	"path/filepath"
	"testing"
	"time"
)

// A later job failure must roll back earlier pauses and queued cancellations.
func TestBulkPauseAtomicity(t *testing.T) {
	state, id := testStore(t)
	var project string
	if err := state.db.QueryRow("SELECT path FROM projects WHERE id=?", id).Scan(&project); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	for _, status := range []string{"queued", "running"} {
		if _, err := state.db.Exec("INSERT INTO runs(project_id,job,status,trigger,digest,created_at) VALUES(?, 'a', ?, 'manual', 'test', ?)", id, status, FormatTime(now)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := state.db.Exec("CREATE TRIGGER reject_pause BEFORE INSERT ON pauses WHEN NEW.job='b' BEGIN SELECT RAISE(ABORT, 'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if n, err := state.SetJobsPaused(project, []string{"a", "b"}, true, now); err == nil || n != 0 {
		t.Fatalf("expected rollback: %d %v", n, err)
	}
	paused, err := state.PausedJobs(project)
	if err != nil || len(paused) != 0 {
		t.Fatalf("partial pause: %v %v", paused, err)
	}
	var queued int
	if err := state.db.QueryRow("SELECT count(*) FROM runs WHERE status='queued'").Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("queue not rolled back: %d %v", queued, err)
	}
	if _, err := state.db.Exec("DROP TRIGGER reject_pause"); err != nil {
		t.Fatal(err)
	}
	if n, err := state.SetJobsPaused(project, []string{"a", "b"}, true, now); err != nil || n != 2 {
		t.Fatalf("bulk pause: %d %v", n, err)
	}
	var cancelled, running int
	if err := state.db.QueryRow("SELECT count(*) FROM runs WHERE status='cancelled'").Scan(&cancelled); err != nil {
		t.Fatal(err)
	}
	if err := state.db.QueryRow("SELECT count(*) FROM runs WHERE status='running'").Scan(&running); err != nil || cancelled != 1 || running != 1 {
		t.Fatalf("run transitions: cancelled=%d running=%d err=%v", cancelled, running, err)
	}
}

// Guards timestamp idempotency and atomic replacement of the authorization set.
func TestApprovalTransitions(t *testing.T) {
	state, projectID := testStore(t)
	var path string
	if err := state.db.QueryRow(`SELECT path FROM projects WHERE id=?`, projectID).Scan(&path); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	enable := func(digests map[string]string, at time.Time) {
		t.Helper()
		if err := state.EnableProject(path, nil, digests, at); err != nil {
			t.Fatal(err)
		}
	}
	stamp := func(job string) string {
		t.Helper()
		var value string
		if err := state.db.QueryRow(`SELECT approved_at FROM approvals WHERE project_id=? AND job=?`, projectID, job).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	enable(map[string]string{"a": "one", "b": "two"}, now)
	enable(map[string]string{"a": "one", "b": "two"}, now.Add(time.Hour))
	snapshot, err := state.ApprovalForProject(path)
	if err != nil || snapshot.EnabledAt.String != FormatTime(now) || stamp("a") != FormatTime(now) || stamp("b") != FormatTime(now) {
		t.Fatalf("idempotency: %+v %v", snapshot, err)
	}
	enable(map[string]string{"a": "changed", "b": "two"}, now.Add(2*time.Hour))
	if stamp("a") != FormatTime(now.Add(2*time.Hour)) || stamp("b") != FormatTime(now) {
		t.Fatal("changed digest timestamps")
	}
	// A database failure after updating the project must roll the whole operation back.
	if _, err := state.db.Exec(`CREATE TRIGGER reject_approval BEFORE INSERT ON approvals WHEN NEW.job='fail' BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	explicit := "/explicit.yaml"
	if err := state.EnableProject(path, &explicit, map[string]string{"fail": "digest"}, now.Add(3*time.Hour)); err == nil {
		t.Fatal("expected transactional failure")
	}
	snapshot, err = state.ApprovalForProject(path)
	if err != nil || snapshot.ConfigFile.Valid || len(snapshot.Approvals) != 2 || snapshot.Approvals["a"] != "changed" {
		t.Fatalf("partial enable persisted: %+v %v", snapshot, err)
	}
	if err := state.DisableProject(path, now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	enable(map[string]string{"b": "two"}, now.Add(4*time.Hour))
	snapshot, err = state.ApprovalForProject(path)
	if err != nil || snapshot.EnabledAt.String != FormatTime(now.Add(4*time.Hour)) || len(snapshot.Approvals) != 1 || stamp("b") != FormatTime(now.Add(4*time.Hour)) {
		t.Fatalf("re-enable: %+v %v", snapshot, err)
	}
}

// Prevents ordinary/idempotent enable from shifting interval phase while still
// ensuring re-approval and disable/re-enable cannot produce stale catch-up runs.
func TestEnableScheduleStateResets(t *testing.T) {
	state, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	project := t.TempDir()
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	if err := state.EnableProject(project, nil, map[string]string{"a": "one", "b": "two"}, now); err != nil {
		t.Fatal(err)
	}
	seed := func(job string) {
		t.Helper()
		_, err := state.db.Exec(`INSERT INTO job_state(project_id, job, schedule_key, next_at) SELECT id, ?, 'every 1h|UTC', ? FROM projects WHERE path=?`, job, FormatTime(now.Add(time.Hour)), project)
		if err != nil {
			t.Fatal(err)
		}
	}
	present := func(job string) bool {
		t.Helper()
		var count int
		if err := state.db.QueryRow(`SELECT count(*) FROM job_state WHERE project_id=(SELECT id FROM projects WHERE path=?) AND job=?`, project, job).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count == 1
	}
	seed("a")
	seed("b")
	if err := state.EnableProject(project, nil, map[string]string{"a": "one", "b": "two"}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if !present("a") || !present("b") {
		t.Fatal("idempotent enable reset interval phase")
	}
	seed("c")
	if err := state.EnableProject(project, nil, map[string]string{"a": "changed", "b": "two", "c": "new"}, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if present("a") || present("c") || !present("b") {
		t.Fatal("approval update did not reset exactly the affected schedule states")
	}
	seed("a")
	seed("c")
	if err := state.DisableProject(project, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := state.EnableProject(project, nil, map[string]string{"a": "changed", "b": "two", "c": "new"}, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if present("a") || present("b") || present("c") {
		t.Fatal("re-enabling a disabled project retained schedule state")
	}
}

// Adding a shared job preserves existing approvals and clears only its stale
// schedule watermark in the same transaction.
func TestEnableAddedJobIsSelectiveAndResetsState(t *testing.T) {
	state, err := Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	project := t.TempDir()
	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	if err := state.EnableProject(project, nil, map[string]string{"existing": "approved"}, now); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := state.db.QueryRow(`SELECT id FROM projects WHERE path=?`, project).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"existing", "added", "local"} {
		if _, err := state.db.Exec(`INSERT INTO job_state(project_id,job,schedule_key,next_at) VALUES(?,?,?,?)`, id, name, "every 1h|UTC", FormatTime(now.Add(time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.EnableAddedJob(project, nil, "added", "new-digest", true, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := state.EnableAddedJob(project, nil, "local", "local-digest", false, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	approval, err := state.ApprovalForProject(project)
	if err != nil || !approval.Enabled || approval.Approvals["existing"] != "approved" || approval.Approvals["added"] != "new-digest" || approval.Approvals["local"] != "" {
		t.Fatalf("selective approval: %+v %v", approval, err)
	}
	var existing, added, local int
	if err := state.db.QueryRow(`SELECT count(*) FROM job_state WHERE project_id=? AND job='existing'`, id).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if err := state.db.QueryRow(`SELECT count(*) FROM job_state WHERE project_id=? AND job='added'`, id).Scan(&added); err != nil {
		t.Fatal(err)
	}
	if err := state.db.QueryRow(`SELECT count(*) FROM job_state WHERE project_id=? AND job='local'`, id).Scan(&local); err != nil {
		t.Fatal(err)
	}
	if existing != 1 || added != 0 || local != 0 {
		t.Fatalf("job state rows existing=%d added=%d local=%d", existing, added, local)
	}

	// Re-enabling a disabled project through add resets every watermark.
	if err := state.DisableProject(project, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := state.EnableAddedJob(project, nil, "another", "another-digest", false, now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := state.db.QueryRow(`SELECT count(*) FROM job_state WHERE project_id=?`, id).Scan(&existing); err != nil {
		t.Fatal(err)
	}
	if existing != 0 {
		t.Fatalf("re-enable kept %d stale job state rows", existing)
	}
}
