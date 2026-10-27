package app

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

// LoadJobs resolves every job source through the same merge operation.
func LoadJobs(input config.Input, now time.Time) (config.Config, error) {
	cfg, err := config.Load(input)
	if err == nil {
		err = MergeTemporaryJobs(&cfg, now, true)
	}
	return cfg, err
}

// MergeTemporaryJobs refreshes state-backed jobs without changing config files.
// A hand-written config definition always wins. Cached callers must disable
// shadow deletion, then reload current config before retrying a collision.
func MergeTemporaryJobs(cfg *config.Config, now time.Time, deleteShadowed bool) error {
	values, err := TemporaryJobs(*cfg, now)
	if err != nil {
		return err
	}
	return MergeTemporaryRows(cfg, values, now, deleteShadowed)
}

// TemporaryJobs reads retained definitions without resolving them, so remove
// and cancel still work after a definition stops resolving.
func TemporaryJobs(cfg config.Config, now time.Time) ([]store.TemporaryJob, error) {
	state, err := store.OpenExisting(cfg.StateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	values, err := state.TemporaryJobs(cfg.Project, now)
	return values, errors.Join(err, state.Close())
}

// MergeTemporaryRows merges rows already read for cfg's project. A row that
// cannot merge is skipped and reported, so the rest of cfg stays usable.
func MergeTemporaryRows(cfg *config.Config, values []store.TemporaryJob, now time.Time, deleteShadowed bool) error {
	cfg.Jobs = maps.Clone(cfg.Jobs)
	for name, job := range cfg.Jobs {
		if job.Source == "temporary" {
			delete(cfg.Jobs, name)
		}
	}
	if len(values) == 0 {
		return nil
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return err
	}
	var errs []error
	for _, value := range values {
		if _, exists := cfg.Jobs[value.Name]; exists {
			if !deleteShadowed {
				errs = append(errs, fmt.Errorf("temporary job %q requires current config before shadow deletion", value.Name))
			} else if err := RemoveTemporaryJob(*cfg, value.Name, now); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		job, err := config.JobFromSource(value.Definition, value.Name, cfg.Agents, cfg.DefaultAgent, cfg.OnSuccess, cfg.OnFailure, false, cfg.Project, cfg.Timezone)
		if err != nil {
			errs = append(errs, fmt.Errorf("temporary job %q no longer resolves: %w; run noon remove %s", value.Name, err, value.Name))
			continue
		}
		job.Source, job.Shared = "temporary", false
		job.Temporary = &config.TemporaryJob{DefinitionDigest: value.Digest, CreatedAt: value.CreatedAt.In(loc), RequestedBy: value.RequestedBy, Key: value.Key, RemovesAt: value.End().Add(store.TemporaryJobGrace).In(loc)}
		if value.EndedAt != nil {
			at := value.EndedAt.In(loc)
			job.Temporary.EndedAt = &at
		}
		cfg.Jobs[value.Name] = job
	}
	return errors.Join(errs...)
}

func RemoveTemporaryJob(cfg config.Config, name string, now time.Time) error {
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return err
	}
	defer state.Close()
	return state.DeleteTemporaryJob(cfg.Project, name, now)
}

type AddedTemporaryJob struct {
	SchemaVersion int    `json:"schema_version"`
	Job           Job    `json:"job"`
	Created       bool   `json:"created"`
	Reason        string `json:"reason,omitempty"`
	Scheduler     string `json:"scheduler"`
}

// AddTemporaryJob is called only by the CLI: no remote adapter exposes creation.
func AddTemporaryJob(cfg config.Config, requested config.Job, key string, due *time.Time, now time.Time) (AddedTemporaryJob, error) {
	fail := func(message string) (AddedTemporaryJob, error) {
		return AddedTemporaryJob{}, UsageError(message, "Use noon add NAME SCHEDULE --temporary --for 2h --sh COMMAND")
	}
	if requested.Until == nil || !requested.Until.After(now) {
		return fail("a temporary job requires an end after now")
	}
	if requested.Until.Sub(now) > MaxTemporaryLifetime {
		return fail("temporary jobs must end within 7 days (including minute rounding)")
	}
	if key != "" && !ValidIdempotencyKey(key) {
		return fail("invalid key: use 1–200 characters of [A-Za-z0-9._:-]")
	}
	if requested.Schedule == nil && requested.ScheduleExpression == "" && due == nil {
		return fail("a temporary job requires a schedule, --in, or --at")
	}
	if existing, ok := cfg.Jobs[requested.Name]; ok && existing.Source != "temporary" {
		return AddedTemporaryJob{}, &Error{Code: "exists", Message: fmt.Sprintf("job %q already exists", requested.Name), Hint: "noon remove " + requested.Name}
	}
	src, err := config.AddJob(nil, requested.Name, requested)
	if err != nil {
		return fail(err.Error())
	}
	job, err := config.JobFromSource(src, requested.Name, cfg.Agents, cfg.DefaultAgent, cfg.OnSuccess, cfg.OnFailure, false, cfg.Project, cfg.Timezone)
	if err != nil {
		return fail(err.Error())
	}
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return AddedTemporaryJob{}, err
	}
	occurs := due != nil && due.Before(*job.Until)
	if job.Schedule != nil {
		next := job.Schedule.Next(now, loc)
		occurs = occurs || !next.IsZero() && next.Before(*job.Until)
	}
	if !occurs || due != nil && !due.Before(*job.Until) {
		return fail("job has no occurrence or queued run before its end")
	}
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return AddedTemporaryJob{}, err
	}
	id, err := state.RegisterProject(cfg.Project, now)
	if err != nil {
		state.Close()
		return AddedTemporaryJob{}, err
	}
	_, created, err := state.CreateTemporaryJob(store.TemporaryJob{ProjectID: id, Name: job.Name, Definition: src, Digest: job.Digest, Until: *job.Until, CreatedAt: now, RequestedBy: "cli", Key: key}, now)
	closeErr := state.Close()
	if errors.Is(err, store.ErrJobExists) {
		return AddedTemporaryJob{}, &Error{Code: "exists", Message: fmt.Sprintf("job %q already exists", job.Name), Hint: "noon remove " + job.Name}
	}
	if err != nil || closeErr != nil {
		return AddedTemporaryJob{}, errors.Join(err, closeErr)
	}
	if err := MergeTemporaryJobs(&cfg, now, true); err != nil {
		return AddedTemporaryJob{}, err
	}
	if due != nil && created {
		if _, err := QueueRun(cfg, job.Name, "", *due, now, nil, false, "cli"); err != nil {
			return AddedTemporaryJob{}, err
		}
	}
	view, err := inspectConfig(cfg, now)
	if err == nil {
		view, err = WithApprovals(view)
	}
	if err == nil {
		view, err = WithLastRuns(view, cfg.StateDir)
	}
	if err == nil {
		view, err = WithScheduler(view, now)
	}
	if err != nil {
		return AddedTemporaryJob{}, err
	}
	result := AddedTemporaryJob{SchemaVersion: 1, Created: created, Scheduler: view.Project.Scheduler}
	if !created {
		result.Reason = "duplicate"
	}
	for _, item := range view.Jobs {
		if item.Name == job.Name {
			result.Job = item
		}
	}
	return result, nil
}

func EnableAddedJob(cfg config.Config, job config.Job, now time.Time) error {
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return err
	}
	defer state.Close()
	var explicit *string
	if cfg.ConfigFile != nil {
		explicit = cfg.ConfigFile
	}
	return state.EnableAddedJob(cfg.Project, explicit, job.Name, job.Digest, job.Shared, now)
}
func SetPaused(stateDir, project, name string, paused bool, now time.Time) error {
	_, err := SetJobsPaused(stateDir, project, []string{name}, paused, now)
	return err
}

func SetJobsPaused(stateDir, project string, names []string, paused bool, now time.Time) (int, error) {
	if paused {
		return PauseJobs(stateDir, project, names, store.PauseWindow{}, now)
	}
	return withPauses(stateDir, project, names, now, func(state *store.Store) (int, error) {
		return state.SetJobsPaused(project, names, false, now)
	})
}

// PauseJobs records a validated window; the zero window is open-ended.
func PauseJobs(stateDir, project string, names []string, window store.PauseWindow, now time.Time) (int, error) {
	return withPauses(stateDir, project, names, now, func(state *store.Store) (int, error) {
		return state.PauseJobs(project, names, window, now)
	})
}

func withPauses(stateDir, project string, names []string, now time.Time, change func(*store.Store) (int, error)) (int, error) {
	if len(names) == 0 {
		return 0, nil
	}
	state, err := store.Open(stateDir)
	if err != nil {
		return 0, err
	}
	defer state.Close()
	if _, err := state.RegisterProject(project, now); err != nil {
		return 0, err
	}
	return change(state)
}

// ResolvePauseWindow resolves pause times once in loc. For and until are
// exclusive, from requires one of them, and for counts from the start. Empty
// values mean an open-ended pause that starts now.
func ResolvePauseWindow(fromValue, forValue, untilValue string, now time.Time, loc *time.Location) (store.PauseWindow, error) {
	var window store.PauseWindow
	hint := `Use --for 8h, or --from "2026-11-07 22:00" --until "2026-11-08 06:00"`
	switch {
	case forValue != "" && untilValue != "":
		return window, UsageError("cannot combine --for with --until", hint)
	case forValue == "" && untilValue == "":
		if fromValue != "" {
			return window, UsageError("--from requires --for or --until", hint)
		}
		return window, nil
	}
	base := now
	if fromValue != "" {
		start, err := ResolveTime(fromValue, now, loc)
		if err != nil {
			return window, UsageError(fmt.Sprintf("invalid from time %q: use HH:MM, YYYY-MM-DD HH:MM, or RFC 3339", fromValue), hint)
		}
		if !start.After(now) {
			return window, UsageError("from must be after now", hint)
		}
		start = start.In(loc)
		window.Start, base = &start, start
	}
	end, err := ResolveEnd(forValue, untilValue, base, loc)
	if err != nil {
		message := err.Error()
		if window.Start != nil {
			message = strings.Replace(message, "after now", "after from", 1)
		}
		return window, UsageError(message, hint)
	}
	window.End = &end
	return window, nil
}
