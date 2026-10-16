package store

import (
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
	if err := state.DisableProject(path); err != nil {
		t.Fatal(err)
	}
	enable(map[string]string{"b": "two"}, now.Add(4*time.Hour))
	snapshot, err = state.ApprovalForProject(path)
	if err != nil || snapshot.EnabledAt.String != FormatTime(now.Add(4*time.Hour)) || len(snapshot.Approvals) != 1 || stamp("b") != FormatTime(now.Add(4*time.Hour)) {
		t.Fatalf("re-enable: %+v %v", snapshot, err)
	}
}
