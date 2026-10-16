package app

import (
	"errors"
	"time"

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
		if job.Schedule == nil {
			continue
		}
		persisted, ok := byJob[job.Name]
		if !ok || persisted.ScheduleKey != *job.Schedule+"|"+view.Project.Timezone {
			continue
		}
		next, err := time.Parse(store.TimestampLayout, persisted.NextAt)
		if err == nil {
			value := next.In(location)
			job.NextAt = &value
		}
	}
	return view, nil
}
