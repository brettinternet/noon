package scheduler

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

func TestServeQueued(t *testing.T) {
	t.Run("starts only when due and retains idempotency across restart", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "ran")
		fixture := newServeFixture(t, fmt.Sprintf("jobs:\n  tests:\n    run: 'touch %s'\n", marker))
		defer fixture.stop(t)
		due := serveTestStart.Add(time.Minute)
		queued := createQueuedRun(t, fixture, "tests", due, "restart-key")
		fixture.step(t, serveTestStart)
		if _, err := os.Stat(marker); !os.IsNotExist(err) || fixture.runByID(t, queued.ID).Status != "queued" {
			t.Fatal("future queued run started before its due time")
		}

		fixture.stop(t)
		fixture.start(t)
		fixture.step(t, serveTestStart.Add(30*time.Second))
		state, err := store.Open(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		duplicate, err := state.QueueRun(store.QueueRunInput{
			ProjectID: fixture.projectID(t), Job: "tests", Digest: "test", IdempotencyKey: "restart-key",
			RequestedBy: "cli", DueAt: due.Add(time.Hour), Now: serveTestStart.Add(30 * time.Second),
			JobExists: true, JobEnabled: true,
		})
		_ = state.Close()
		if err != nil || duplicate.Created || duplicate.Reason != "duplicate" || duplicate.Run.ID != queued.ID {
			t.Fatalf("restart key result: %+v err=%v", duplicate, err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatal("queued run started before due time after restart")
		}
		fixture.step(t, due)
		waitFor(t, func() bool { _, err := os.Stat(marker); return err == nil })
		if run := fixture.runByID(t, queued.ID); run.Status != "running" && run.Status != "succeeded" {
			t.Fatalf("due run status = %s", run.Status)
		}
	})

	t.Run("config invalid at startup keeps queued runs and watermarks", func(t *testing.T) {
		fixture := newServeFixture(t, "jobs:\n  tests:\n    schedule: every 1h\n    run: 'true'\n")
		defer fixture.stop(t)
		fixture.step(t, serveTestStart)
		queued := createQueuedRun(t, fixture, "tests", serveTestStart.Add(24*time.Hour), "")
		fixture.stop(t)
		fixture.writeConfig(t, "jobs: [broken\n")
		fixture.start(t)
		fixture.step(t, serveTestStart.Add(time.Second))
		if fixture.configError(t) == "" {
			t.Fatal("invalid config was not recorded")
		}
		if run := fixture.runByID(t, queued.ID); run.Status != "queued" {
			t.Fatalf("future queued run status = %s, want queued", run.Status)
		}
		if len(fixture.states(t)) != 1 {
			t.Fatal("schedule watermark was dropped while config was invalid")
		}
	})

	t.Run("cancelled queued run never starts", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "should-not-run")
		fixture := newServeFixture(t, fmt.Sprintf("jobs:\n  tests:\n    run: 'touch %s'\n", marker))
		defer fixture.stop(t)
		queued := createQueuedRun(t, fixture, "tests", serveTestStart, "")
		state, err := store.Open(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		cancelled, err := state.RequestCancelRun(queued.ID, serveTestStart)
		_ = state.Close()
		if err != nil || cancelled.Status != "cancelled" {
			t.Fatalf("cancel queued run: %+v err=%v", cancelled, err)
		}
		fixture.step(t, serveTestStart)
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("cancelled queued command ran: %v", err)
		}
		if run := fixture.runByID(t, queued.ID); run.Status != "cancelled" || run.StartedAt != "" {
			t.Fatalf("cancelled queued row: %+v", run)
		}
	})

	t.Run("running cancellation kills the process group", func(t *testing.T) {
		dir := t.TempDir()
		pidFile := filepath.Join(dir, "child.pid")
		command := fmt.Sprintf(`trap '' TERM; sleep 30 & child=$!; echo "$child" > %s; wait "$child"`, shellQuote(pidFile))
		fixture := newServeFixture(t, "jobs:\n  tests:\n    run: "+strconv.Quote(command)+"\n")
		defer fixture.stop(t)
		queued := createQueuedRun(t, fixture, "tests", serveTestStart, "")
		fixture.step(t, serveTestStart)
		waitFor(t, func() bool {
			data, err := os.ReadFile(pidFile)
			return err == nil && strings.HasSuffix(string(data), "\n")
		})
		pidData, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatal(err)
		}
		var childPID int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(pidData)), "%d", &childPID); err != nil {
			t.Fatal(err)
		}
		identity, err := runner.ProcessStartIdentity(childPID)
		if err != nil {
			t.Fatal(err)
		}
		state, err := store.Open(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		requested, err := state.RequestCancelRun(queued.ID, serveTestStart)
		_ = state.Close()
		if err != nil || requested.Status != "running" || !requested.CancelRequestedAt.Valid {
			t.Fatalf("running cancellation request: %+v err=%v", requested, err)
		}
		fixture.step(t, serveTestStart.Add(time.Second))
		waitFor(t, func() bool { return fixture.runByID(t, queued.ID).Status == "cancelled" })
		waitFor(t, func() bool {
			alive, err := runner.ProcessOwnerAlive(childPID, identity)
			return err != nil || !alive
		})
		if run := fixture.runByID(t, queued.ID); run.Status != "cancelled" {
			t.Fatalf("running cancellation status: %+v", run)
		}
	})

	t.Run("FIFO, project and global caps, and expiry use persisted queued rows", func(t *testing.T) {
		fixture := newServeFixture(t, "")
		defer fixture.stop(t)
		state, err := store.Open(fixture.stateDir)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		var expiredID, firstProjectID int64
		for i := 1; i <= 6; i++ {
			project := filepath.Join(fixture.root, fmt.Sprintf("queued-%d", i))
			if err := os.MkdirAll(filepath.Join(project, ".git"), 0o700); err != nil {
				t.Fatal(err)
			}
			project, err = filepath.EvalSymlinks(project)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, ".git", "noon.yaml"), []byte("jobs:\n  tests:\n    run: 'sleep 30'\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := state.EnableProject(project, nil, nil, serveTestStart); err != nil {
				t.Fatal(err)
			}
			projectID, err := state.RegisterProject(project, serveTestStart)
			if err != nil {
				t.Fatal(err)
			}
			if i == 1 {
				firstProjectID = projectID
			}
			if i == 6 {
				run, err := state.QueueRun(store.QueueRunInput{ProjectID: projectID, Job: "tests", Digest: "test", RequestedBy: "test", DueAt: serveTestStart.Add(-time.Hour - time.Second), Now: serveTestStart, JobExists: true, JobEnabled: true})
				if err != nil || !run.Created {
					t.Fatalf("seed expired queued run: %+v err=%v", run, err)
				}
				expiredID = run.Run.ID
			}
		}
		// Insert in reverse due-time order so IDs disagree with dispatch FIFO.
		for i := 5; i >= 1; i-- {
			projectID := int64(i + 1) // Project IDs follow registration order after the fixture project.
			run, err := state.QueueRun(store.QueueRunInput{
				ProjectID: projectID, Job: "tests", Digest: "test", RequestedBy: "test",
				DueAt: serveTestStart.Add(-time.Duration(6-i) * time.Minute), Now: serveTestStart,
				JobExists: true, JobEnabled: true,
			})
			if err != nil || !run.Created {
				t.Fatalf("seed queued project %d: %+v err=%v", projectID, run, err)
			}
			ids = append(ids, run.Run.ID)
		}
		projectRun, err := state.QueueRun(store.QueueRunInput{
			ProjectID: firstProjectID, Job: "tests", Digest: "test", RequestedBy: "test",
			DueAt: serveTestStart.Add(-30 * time.Second), Now: serveTestStart,
			JobExists: true, JobEnabled: true,
		})
		if err != nil || !projectRun.Created {
			t.Fatalf("seed second queued run for same project: %+v err=%v", projectRun, err)
		}
		if err := state.Close(); err != nil {
			t.Fatal(err)
		}

		// Keep launched work alive so the persisted reservation set reflects the
		// four global slots. The serialized scheduler start events expose FIFO order.
		fixture.stop(t)
		events := &bytes.Buffer{}
		fixture.logger = events
		fixture.start(t)
		fixture.step(t, serveTestStart)
		startedIDs := []int64{}
		queuedIDs := []int64{}
		for _, id := range ids {
			run := fixture.runByID(t, id)
			if run.Status == "running" {
				startedIDs = append(startedIDs, id)
			} else if run.Status == "queued" {
				queuedIDs = append(queuedIDs, id)
			}
		}
		if len(startedIDs) != 4 || len(queuedIDs) != 1 {
			t.Fatalf("capacity reservations: running=%v queued=%v", startedIDs, queuedIDs)
		}
		wantOrder := []int64{ids[4], ids[3], ids[2], ids[1]}
		var gotOrder []int64
		for _, line := range strings.Split(events.String(), "\n") {
			if !strings.Contains(line, "run started") {
				continue
			}
			at := strings.Index(line, "run=")
			if at < 0 {
				continue
			}
			fields := strings.Fields(line[at+len("run="):])
			id, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			gotOrder = append(gotOrder, id)
		}
		if fmt.Sprint(gotOrder) != fmt.Sprint(wantOrder) {
			t.Fatalf("dispatch start order=%v, want FIFO %v; events=%s", gotOrder, wantOrder, events.String())
		}
		for _, id := range wantOrder {
			if fixture.runByID(t, id).Status != "running" {
				t.Fatalf("FIFO reservation skipped earlier due run %d: %+v", id, fixture.runByID(t, id))
			}
		}
		if fixture.runByID(t, ids[0]).Status != "queued" {
			t.Fatalf("fifth due run did not wait for global capacity: %+v", fixture.runByID(t, ids[0]))
		}
		if fixture.runByID(t, projectRun.Run.ID).Status != "queued" {
			t.Fatalf("second run for active project bypassed project cap: %+v", fixture.runByID(t, projectRun.Run.ID))
		}
		if run := fixture.runByID(t, expiredID); run.Status != "expired" {
			t.Fatalf("overdue queued row status = %s, want expired", run.Status)
		}
	})
}

func createQueuedRun(t *testing.T, fixture *serveFixture, job string, due time.Time, key string) store.Run {
	t.Helper()
	state, err := store.Open(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	result, err := state.QueueRun(store.QueueRunInput{
		ProjectID: fixture.projectID(t), Job: job, Digest: "test", IdempotencyKey: key,
		RequestedBy: "test", DueAt: due, Now: serveTestStart,
		JobExists: true, JobEnabled: true,
	})
	_ = state.Close()
	if err != nil || !result.Created {
		t.Fatalf("create queued run: %+v err=%v", result, err)
	}
	return result.Run
}
