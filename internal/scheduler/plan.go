// Package scheduler plans scheduled work without performing I/O or reading a clock.
package scheduler

import (
	"cmp"
	"slices"
	"time"

	"github.com/brettinternet/noon/internal/schedule"
)

type JobKey struct {
	Project int64
	Job     string
}

// JobInput is a validated job. Active includes project opt-in, approval and pause
// state. Nil Location means UTC, as in schedule.Next.
type JobInput struct {
	Key         JobKey
	Schedule    *schedule.Schedule // nil means manual
	ProjectPath string
	Splay       time.Duration
	Jitter      time.Duration
	Until       *time.Time
	Location    *time.Location
	Active      bool
	Missed      string // run_once | skip
	Overlap     string // skip | queue
}

type JobState struct {
	Key         JobKey
	ScheduleKey string
	NextAt      time.Time
}

type Run struct {
	ID     int64
	Key    JobKey
	Status string
	DueAt  time.Time
}

type NewRun struct {
	Key                JobKey
	Status, Trigger    string
	ScheduledAt, DueAt time.Time
}

// Due returns state changes and at most one occurrence per scheduled job.
// Inputs are snapshots with unique job/state keys and run IDs, and are never
// mutated. Callers must apply the returned changes atomically before replanning.
// Reactivation commands must delete state so reactivation behaves as first sight.
func Due(now time.Time, jobs []JobInput, states []JobState, runs []Run) (upsert []JobState, remove []JobKey, create []NewRun) {
	previous := make(map[JobKey]JobState, len(states))
	for _, state := range states {
		previous[state.Key] = state
	}
	running, queued := map[JobKey]bool{}, map[JobKey]bool{}
	for _, run := range runs {
		if run.Status == "running" {
			running[run.Key] = true
		} else if run.Status == "queued" && !run.DueAt.After(now) {
			queued[run.Key] = true
		}
	}
	retained := make(map[JobKey]bool, len(jobs))
	for _, job := range jobs {
		if !job.Active || job.Schedule == nil || (job.Until != nil && !now.Before(*job.Until)) {
			continue
		}
		retained[job.Key] = true
		loc := job.Location
		if loc == nil {
			loc = time.UTC
		}
		key := schedule.StateKey(job.Schedule.String(), loc.String(), job.ProjectPath, job.Key.Job, job.Splay, job.Jitter)
		state, exists := previous[job.Key]
		if !exists || state.ScheduleKey != key {
			nominal, _ := job.Schedule.NextEffective(now, loc, job.ProjectPath, job.Key.Job, job.Splay, job.Jitter, time.Time{})
			upsert = append(upsert, JobState{job.Key, key, nominal})
			continue
		}
		effective := schedule.EffectiveStart(job.ProjectPath, job.Key.Job, state.NextAt, job.Splay, job.Jitter)
		if job.Until != nil && !effective.Before(*job.Until) {
			continue
		}
		if effective.After(now) {
			continue
		}
		run := NewRun{job.Key, "queued", "schedule", state.NextAt, effective}
		if now.Sub(effective) > time.Minute {
			run.DueAt = now
			if job.Missed == "skip" {
				run.Status = "missed"
			} else {
				run.Trigger = "catch_up"
			}
		}
		if run.Status == "queued" && (queued[job.Key] || (running[job.Key] && job.Overlap != "queue")) {
			run.Status = "skipped"
		}
		create = append(create, run)
		if run.Trigger == "catch_up" || run.Status == "missed" {
			intervalPhase := time.Time{}
			if _, interval := job.Schedule.Interval(); interval {
				intervalPhase = state.NextAt
			}
			state.NextAt, _ = job.Schedule.NextEffective(now, loc, job.ProjectPath, job.Key.Job, job.Splay, job.Jitter, intervalPhase)
		} else {
			state.NextAt = job.Schedule.Next(state.NextAt, loc)
		}
		upsert = append(upsert, state)
	}
	for _, state := range states {
		if !retained[state.Key] {
			remove = append(remove, state.Key)
		}
	}
	return
}

// Dispatch cancels ineligible queued work, expires old work, and fills capacity
// in (DueAt, ID) order. All running work counts, including foreground runs and
// runs whose jobs have since been removed or disabled. Cancellation takes
// precedence over expiry; neither operation stops an already-running process.
func Dispatch(now time.Time, jobs []JobInput, runs []Run) (start, expire, cancel []int64) {
	active := make(map[JobKey]bool, len(jobs))
	for _, job := range jobs {
		active[job.Key] = job.Active && (job.Until == nil || now.Before(*job.Until))
	}
	projects := map[int64]bool{}
	running := 0
	var pending []Run
	for _, run := range runs {
		switch run.Status {
		case "running":
			projects[run.Key.Project] = true
			running++
		case "queued":
			pending = append(pending, run)
		}
	}
	slices.SortFunc(pending, func(a, b Run) int {
		if order := a.DueAt.Compare(b.DueAt); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	for _, run := range pending {
		switch {
		case !active[run.Key]:
			cancel = append(cancel, run.ID)
		case now.Sub(run.DueAt) > time.Hour:
			expire = append(expire, run.ID)
		case !run.DueAt.After(now) && !projects[run.Key.Project] && running < 4:
			start = append(start, run.ID)
			projects[run.Key.Project] = true
			running++
		}
	}
	return
}
