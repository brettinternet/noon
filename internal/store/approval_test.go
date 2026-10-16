package store

import (
	"path/filepath"
	"testing"
	"time"
)

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
