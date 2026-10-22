package app

import (
	"errors"
	"time"

	"github.com/brettinternet/noon/internal/schedule"
	"github.com/brettinternet/noon/internal/store"
)

// WithScheduler adds the persisted scheduler status, config diagnostic, and
// interval phase to an inspection without creating or migrating state.
func WithScheduler(view Inspection, now time.Time) (Inspection, error) {
	state, err := store.OpenExisting(view.StateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return view, nil
	}
	if err != nil {
		return Inspection{}, err
	}
	defer state.Close()
	record, exists, err := state.SchedulerInfo()
	if err != nil {
		return Inspection{}, err
	}
	if exists {
		if heartbeat, err := time.Parse(store.TimestampLayout, record.HeartbeatAt); err == nil && now.Sub(heartbeat) >= 0 && now.Sub(heartbeat) < 5*time.Second {
			view.Project.Scheduler = "running"
		}
	}
	message, valid, err := state.ProjectConfigError(view.Project.Path)
	if err != nil {
		return Inspection{}, err
	}
	if valid {
		view.Project.ConfigError = message
	}
	states, err := state.ScheduleStatesForProject(view.Project.Path)
	if err != nil {
		return Inspection{}, err
	}
	location, err := time.LoadLocation(view.Project.Timezone)
	if err != nil {
		return Inspection{}, err
	}
	byJob := make(map[string]store.PersistedJobState, len(states))
	for _, item := range states {
		byJob[item.Job] = item
	}
	for i := range view.Jobs {
		job := &view.Jobs[i]
		if job.Schedule == nil || job.Ended {
			continue
		}
		splay, _ := schedule.ParseDuration(job.Splay)
		jitter, _ := schedule.ParseDuration(job.Jitter)
		persisted, ok := byJob[job.Name]
		if !ok || persisted.ScheduleKey != schedule.StateKey(*job.Schedule, view.Project.Timezone, view.Project.Path, job.Name, splay, jitter) {
			continue
		}
		next, err := time.Parse(store.TimestampLayout, persisted.NextAt)
		if err == nil {
			value := schedule.EffectiveStart(view.Project.Path, job.Name, next, splay, jitter).In(location)
			job.NextAt = nil
			if job.Until == nil || value.Before(*job.Until) {
				job.NextAt = &value
			}
		}
	}
	temporary, err := state.TemporarySchedules(view.Project.Path)
	if err != nil {
		return Inspection{}, err
	}
	for i := range view.Jobs {
		job := &view.Jobs[i]
		if !view.Project.Enabled || !job.Enabled || !job.PlatformSupported || job.Paused || job.Ended || job.Approval == "needs_approval" {
			continue
		}
		for _, value := range temporary {
			if value.Job != job.Name || value.Digest != job.Digest || !now.Before(value.Until) {
				continue
			}
			job.TemporarySchedule = &value
			if value.NextAt.Before(value.Until) && (job.Until == nil || value.NextAt.Before(*job.Until)) && (job.NextAt == nil || value.NextAt.Before(*job.NextAt)) {
				next := value.NextAt.In(location)
				job.NextAt = &next
			}
		}
	}
	return view, nil
}
