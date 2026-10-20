package scheduler

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/schedule"
)

func TestPlanDue(t *testing.T) {
	base := time.Date(2026, 10, 1, 1, 0, 0, 0, time.UTC)
	hourly, err := schedule.Parse("hourly")
	if err != nil {
		t.Fatal(err)
	}
	interval, err := schedule.Parse("every 6h")
	if err != nil {
		t.Fatal(err)
	}
	key := JobKey{Project: 1, Job: "tests"}
	job := JobInput{Key: key, Schedule: &hourly, Location: time.UTC, Active: true, Missed: "run_once", Overlap: "skip"}
	state := JobState{Key: key, ScheduleKey: "@hourly|UTC", NextAt: base}
	changed := job
	changed.Schedule = &interval
	zoned := job
	zoned.Location = time.FixedZone("east", 2*60*60)
	inactive := job
	inactive.Active = false
	manual := job
	manual.Schedule = nil
	missed := job
	missed.Missed = "skip"
	queue := job
	queue.Overlap = "queue"
	intervalState := JobState{Key: key, ScheduleKey: "every 6h|UTC", NextAt: base}
	running := Run{ID: 1, Key: key, Status: "running", DueAt: base}
	pending := Run{ID: 2, Key: key, Status: "queued", DueAt: base}
	future := Run{ID: 3, Key: key, Status: "queued", DueAt: base.Add(time.Hour)}
	newRun := func(status, trigger string, due time.Time) []NewRun {
		return []NewRun{{Key: key, Status: status, Trigger: trigger, ScheduledAt: base, DueAt: due}}
	}
	cases := []struct {
		name        string
		now         time.Time
		jobs        []JobInput
		states      []JobState
		runs        []Run
		next        time.Time
		scheduleKey string
		remove      []JobKey
		create      []NewRun
	}{
		{name: "first sight", now: base, jobs: []JobInput{job}, next: base.Add(time.Hour)},
		{name: "before due", now: base.Add(-time.Second), jobs: []JobInput{job}, states: []JobState{state}},
		{name: "on time", now: base, jobs: []JobInput{job}, states: []JobState{state}, next: base.Add(time.Hour), create: newRun("queued", "schedule", base)},
		{name: "59 seconds", now: base.Add(59 * time.Second), jobs: []JobInput{job}, states: []JobState{state}, next: base.Add(time.Hour), create: newRun("queued", "schedule", base)},
		{name: "60 seconds inclusive", now: base.Add(time.Minute), jobs: []JobInput{job}, states: []JobState{state}, next: base.Add(time.Hour), create: newRun("queued", "schedule", base)},
		{name: "61 seconds catch up", now: base.Add(61 * time.Second), jobs: []JobInput{job}, states: []JobState{state}, next: base.Add(time.Hour), create: newRun("queued", "catch_up", base.Add(61*time.Second))},
		{name: "missed skip even while running", now: base.Add(61 * time.Second), jobs: []JobInput{missed}, states: []JobState{state}, runs: []Run{running}, next: base.Add(time.Hour), create: newRun("missed", "schedule", base.Add(61*time.Second))},
		{name: "three days asleep", now: base.Add(72*time.Hour + 30*time.Minute), jobs: []JobInput{job}, states: []JobState{state}, next: base.Add(73 * time.Hour), create: newRun("queued", "catch_up", base.Add(72*time.Hour+30*time.Minute))},
		{name: "interval phase", now: base.Add(13*time.Hour + 30*time.Minute), jobs: []JobInput{changed}, states: []JobState{intervalState}, next: base.Add(18 * time.Hour), scheduleKey: "every 6h|UTC", create: newRun("queued", "catch_up", base.Add(13*time.Hour+30*time.Minute))},
		{name: "schedule change", now: base.Add(time.Hour), jobs: []JobInput{changed}, states: []JobState{state}, next: base.Add(7 * time.Hour), scheduleKey: "every 6h|UTC"},
		{name: "timezone change", now: base, jobs: []JobInput{zoned}, states: []JobState{state}, next: base.Add(time.Hour).In(zoned.Location), scheduleKey: "@hourly|east"},
		{name: "inactive", now: base, jobs: []JobInput{inactive}, states: []JobState{state}, remove: []JobKey{key}},
		{name: "manual", now: base, jobs: []JobInput{manual}, states: []JobState{state}, remove: []JobKey{key}},
		{name: "removed", now: base, states: []JobState{state}, remove: []JobKey{key}},
		{name: "reactivated with cleared state", now: base.Add(72 * time.Hour), jobs: []JobInput{job}, next: base.Add(73 * time.Hour)},
		{name: "overlap skip", now: base, jobs: []JobInput{job}, states: []JobState{state}, runs: []Run{running}, next: base.Add(time.Hour), create: newRun("skipped", "schedule", base)},
		{name: "overlap catch up skip", now: base.Add(61 * time.Second), jobs: []JobInput{job}, states: []JobState{state}, runs: []Run{running}, next: base.Add(time.Hour), create: newRun("skipped", "catch_up", base.Add(61*time.Second))},
		{name: "overlap queue", now: base, jobs: []JobInput{queue}, states: []JobState{state}, runs: []Run{running}, next: base.Add(time.Hour), create: newRun("queued", "schedule", base)},
		{name: "one pending while running", now: base, jobs: []JobInput{queue}, states: []JobState{state}, runs: []Run{running, pending}, next: base.Add(time.Hour), create: newRun("skipped", "schedule", base)},
		{name: "due queued suppresses", now: base, jobs: []JobInput{job}, states: []JobState{state}, runs: []Run{pending}, next: base.Add(time.Hour), create: newRun("skipped", "schedule", base)},
		{name: "past queued suppresses catch up", now: base.Add(61 * time.Second), jobs: []JobInput{queue}, states: []JobState{state}, runs: []Run{pending}, next: base.Add(time.Hour), create: newRun("skipped", "catch_up", base.Add(61*time.Second))},
		{name: "future one shot", now: base, jobs: []JobInput{job}, states: []JobState{state}, runs: []Run{future}, next: base.Add(time.Hour), create: newRun("queued", "schedule", base)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobs, states, runs := slices.Clone(tc.jobs), slices.Clone(tc.states), slices.Clone(tc.runs)
			upsert, remove, create := Due(tc.now, tc.jobs, tc.states, tc.runs)
			var want []JobState
			if !tc.next.IsZero() {
				sk := tc.scheduleKey
				if sk == "" {
					sk = "@hourly|UTC"
				}
				want = []JobState{{Key: key, ScheduleKey: sk, NextAt: tc.next}}
			}
			if !reflect.DeepEqual(upsert, want) || !reflect.DeepEqual(remove, tc.remove) || !reflect.DeepEqual(create, tc.create) {
				t.Fatalf("got (%+v, %+v, %+v); want (%+v, %+v, %+v)", upsert, remove, create, want, tc.remove, tc.create)
			}
			if !reflect.DeepEqual(jobs, tc.jobs) || !reflect.DeepEqual(states, tc.states) || !reflect.DeepEqual(runs, tc.runs) {
				t.Fatal("inputs mutated")
			}
			if len(upsert) > 0 {
				_, _, again := Due(tc.now, tc.jobs, upsert, tc.runs)
				if len(again) != 0 {
					t.Fatalf("replanning creates duplicate runs: %+v", again)
				}
			}
		})
	}
}

func TestSplayedDue(t *testing.T) {
	nominal := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	daily, err := schedule.Parse("every day at 9am")
	if err != nil {
		t.Fatal(err)
	}
	job := JobInput{Key: JobKey{1, "triage"}, ProjectPath: "/repo", Splay: 10 * time.Minute, Schedule: &daily, Active: true, Missed: "run_once"}
	jobs := []JobInput{job}
	effective := nominal.Add(487 * time.Second)
	// First sight between nominal and effective must not discard today's run.
	states, _, runs := Due(nominal.Add(time.Minute), jobs, nil, nil)
	if len(states) != 1 || !states[0].NextAt.Equal(nominal) || len(runs) != 0 {
		t.Fatalf("initial state = %+v, runs = %+v", states, runs)
	}
	for _, tc := range []struct {
		name            string
		now             time.Time
		status, trigger string
		due             time.Time
	}{
		{"before", effective.Add(-time.Second), "", "", time.Time{}},
		{"on time", effective, "queued", "schedule", effective},
		{"window boundary", effective.Add(time.Minute), "queued", "schedule", effective},
		{"missed", effective.Add(72 * time.Hour), "queued", "catch_up", effective.Add(72 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated, _, created := Due(tc.now, jobs, states, nil)
			if tc.status == "" {
				if len(created) != 0 || len(updated) != 0 {
					t.Fatalf("premature run: %+v", created)
				}
				return
			}
			if len(created) != 1 || created[0].Status != tc.status || created[0].Trigger != tc.trigger || !created[0].ScheduledAt.Equal(nominal) || !created[0].DueAt.Equal(tc.due) {
				t.Fatalf("created = %+v", created)
			}
			if len(updated) != 1 || !updated[0].NextAt.Add(487*time.Second).After(tc.now) {
				t.Fatalf("next = %+v", updated)
			}
			_, _, again := Due(tc.now, jobs, updated, nil)
			if len(again) != 0 {
				t.Fatalf("duplicate catch-up: %+v", again)
			}
		})
	}
	skip := job
	skip.Missed = "skip"
	_, _, skipped := Due(effective.Add(61*time.Second), []JobInput{skip}, states, nil)
	if len(skipped) != 1 || skipped[0].Status != "missed" || !skipped[0].ScheduledAt.Equal(nominal) {
		t.Fatalf("missed: %+v", skipped)
	}
	_, _, overlap := Due(effective, jobs, states, []Run{{ID: 1, Key: job.Key, Status: "running"}})
	if len(overlap) != 1 || overlap[0].Status != "skipped" {
		t.Fatalf("overlap: %+v", overlap)
	}
	for _, change := range []string{"splay", "path", "name"} {
		changed := job
		switch change {
		case "splay":
			changed.Splay = time.Hour
		case "path":
			changed.ProjectPath = "/moved"
		case "name":
			changed.Key.Job = "renamed"
		}
		updated, _, created := Due(effective.Add(72*time.Hour), []JobInput{changed}, states, nil)
		if len(created) != 0 || len(updated) != 1 || updated[0].ScheduleKey == states[0].ScheduleKey {
			t.Fatalf("%s did not reset: %+v %+v", change, updated, created)
		}
	}
	plain := job
	plain.Splay = 0
	unchanged, _, _ := Due(nominal, []JobInput{plain}, nil, nil)
	if len(unchanged) != 1 || unchanged[0].ScheduleKey != "every day at 09:00|UTC" {
		t.Fatalf("legacy key changed: %+v", unchanged)
	}
}

func TestPlanDispatch(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 30, 0, 0, time.UTC)
	a, b, c, d, e := JobKey{1, "a"}, JobKey{2, "b"}, JobKey{3, "c"}, JobKey{4, "d"}, JobKey{5, "e"}
	other := JobKey{1, "other"}
	jobs := []JobInput{{Key: a, Active: true}, {Key: b, Active: true}, {Key: c, Active: true}, {Key: d, Active: true}, {Key: e, Active: true}, {Key: other, Active: true}, {Key: JobKey{6, "inactive"}}}
	run := func(id int64, key JobKey, status string, offset time.Duration) Run {
		return Run{ID: id, Key: key, Status: status, DueAt: now.Add(offset)}
	}
	cases := []struct {
		name                  string
		runs                  []Run
		start, expire, cancel []int64
	}{
		{name: "FIFO and ID tie break", runs: []Run{run(4, c, "queued", 0), run(3, b, "queued", 0), run(2, a, "queued", -time.Minute)}, start: []int64{2, 3, 4}},
		{name: "one per project", runs: []Run{run(2, other, "queued", 0), run(1, a, "queued", 0)}, start: []int64{1}},
		{name: "four across five projects", runs: []Run{run(5, e, "queued", 0), run(4, d, "queued", 0), run(3, c, "queued", 0), run(2, b, "queued", 0), run(1, a, "queued", 0)}, start: []int64{1, 2, 3, 4}},
		// Run has no trigger field: foreground/manual running work is indistinguishable.
		{name: "foreground blocks project and consumes global slot", runs: []Run{run(9, other, "running", 0), run(1, a, "queued", 0), run(2, b, "queued", 0), run(3, c, "queued", 0), run(4, d, "queued", 0), run(5, e, "queued", 0)}, start: []int64{2, 3, 4}},
		{name: "same job waits", runs: []Run{run(1, a, "queued", 0), run(9, a, "running", 0)}},
		{name: "expired beyond hour but boundary starts", runs: []Run{run(1, a, "queued", -time.Hour-time.Nanosecond), run(2, a, "queued", -time.Hour)}, start: []int64{2}, expire: []int64{1}},
		{name: "cancel inactive and gone before expiry", runs: []Run{run(1, JobKey{6, "inactive"}, "queued", -2*time.Hour), run(2, JobKey{7, "gone"}, "queued", time.Hour)}, cancel: []int64{1, 2}},
		{name: "future waits", runs: []Run{run(1, a, "queued", time.Nanosecond)}},
		{name: "removed running job still blocks project", runs: []Run{run(9, JobKey{1, "gone"}, "running", -2*time.Hour), run(1, a, "queued", 0)}},
		{name: "terminal runs consume no capacity", runs: []Run{run(9, a, "succeeded", 0), run(1, a, "queued", 0)}, start: []int64{1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := slices.Clone(tc.runs)
			start, expire, cancel := Dispatch(now, jobs, tc.runs)
			if !slices.Equal(start, tc.start) || !slices.Equal(expire, tc.expire) || !slices.Equal(cancel, tc.cancel) {
				t.Fatalf("got (%v, %v, %v); want (%v, %v, %v)", start, expire, cancel, tc.start, tc.expire, tc.cancel)
			}
			if !reflect.DeepEqual(before, tc.runs) {
				t.Fatal("inputs mutated")
			}
		})
	}
}
