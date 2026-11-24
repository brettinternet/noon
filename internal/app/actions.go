package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

// Actions on enabled projects for the web viewer. Disabled and unknown
// projects, runs, and jobs share the not-found error, as in the remote API.

// QueueEnabledJob queues an approved job of an enabled project exactly as the
// remote API does, except that a paused job is refused: the viewer offers
// Resume instead of a manual override. mode is now, in, or at.
func QueueEnabledJob(input config.Input, stateDir string, id int64, job, mode, inValue, atValue, requestedBy, source string, now time.Time) (QueuedRun, error) {
	cfg, err := enabledConfig(input, stateDir, id, now)
	if err != nil {
		return QueuedRun{}, err
	}
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return QueuedRun{}, ConfigError(err)
	}
	switch mode {
	case "now":
		mode, inValue, atValue = "detach", "", ""
	case "in":
		atValue = ""
	case "at":
		inValue = ""
	default:
		return QueuedRun{}, UsageError("when must be now, in, or at", "")
	}
	dueAt, err := QueueDueAt(mode, inValue, atValue, now, location)
	if err != nil {
		return QueuedRun{}, UsageError(err.Error(), `Use in "2h" or at "2026-10-16 09:00"`)
	}
	return QueueRun(cfg, job, "", dueAt, now, time.Now, mode == "detach", requestedBy, QueueOptions{EventSource: source, RequireUnpaused: true})
}

// PauseResult reports a pause or resume. With NeedsConfirm, nothing changed:
// the pause covers the whole project or ends the temporary schedules of
// EndsTemporary, which resume never restores.
type PauseResult struct {
	Jobs          []string
	Changed       int
	NeedsConfirm  bool
	EndsTemporary []string
}

// PauseEnabledJobs pauses job, or every configured job when job is empty, in
// an enabled project. window is 1h, 1d, tomorrow (the next 09:00 in the
// project's timezone), or open (until resumed).
func PauseEnabledJobs(input config.Input, stateDir string, id int64, job, window, requestedBy string, confirmed bool, now time.Time) (PauseResult, error) {
	cfg, names, err := enabledJobs(input, stateDir, id, job, now)
	if err != nil {
		return PauseResult{}, err
	}
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return PauseResult{}, ConfigError(err)
	}
	var forValue, untilValue string
	switch window {
	case "1h":
		forValue = "1h"
	case "1d":
		forValue = "24h"
	case "tomorrow":
		untilValue = "09:00"
	case "open":
	default:
		return PauseResult{}, UsageError("window must be 1h, 1d, tomorrow, or open", "")
	}
	resolved, err := ResolvePauseWindow("", forValue, untilValue, now, location)
	if err != nil {
		return PauseResult{}, err
	}
	result := PauseResult{Jobs: names}
	if !confirmed {
		result.EndsTemporary, err = activeTemporarySchedules(cfg, names, now)
		if err != nil {
			return PauseResult{}, StateError(err)
		}
		if job == "" || len(result.EndsTemporary) > 0 {
			result.NeedsConfirm = true
			return result, nil
		}
	}
	result.Changed, err = PauseJobs(cfg.StateDir, cfg.Project, names, resolved, requestedBy, now)
	if err != nil {
		return PauseResult{}, StateError(err)
	}
	return result, nil
}

// ResumeEnabledJobs resumes job, or every configured job when job is empty, in
// an enabled project.
func ResumeEnabledJobs(input config.Input, stateDir string, id int64, job string, now time.Time) (PauseResult, error) {
	cfg, names, err := enabledJobs(input, stateDir, id, job, now)
	if err != nil {
		return PauseResult{}, err
	}
	changed, err := SetJobsPaused(cfg.StateDir, cfg.Project, names, false, now)
	if err != nil {
		return PauseResult{}, StateError(err)
	}
	return PauseResult{Jobs: names, Changed: changed}, nil
}

// CancelEnabledRun cancels a run of an enabled project. Unless confirmed, a
// running run is left alone and needsConfirm is true.
func CancelEnabledRun(stateDir string, id int64, confirmed bool, now time.Time) (result CancelResult, needsConfirm bool, err error) {
	if _, err := EnabledRunProject(stateDir, id); err != nil {
		return CancelResult{}, false, err
	}
	run, err := RunByID(stateDir, id)
	if err != nil {
		if IsMissingRun(err) || NoDatabase(err) {
			return CancelResult{}, false, RunNotFoundError(id)
		}
		return CancelResult{}, false, StateError(err)
	}
	if run.Status == "running" && !confirmed {
		return CancelResult{Runs: []Run{run}}, true, nil
	}
	result, err = Cancel(stateDir, "", &id, "", now)
	if IsMissingRun(err) || NoDatabase(err) {
		err = RunNotFoundError(id)
	}
	return result, false, err
}

func enabledConfig(input config.Input, stateDir string, id int64, now time.Time) (config.Config, error) {
	values, err := EnabledProjectInput(input, stateDir, id)
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := LoadJobs(values, now)
	if err != nil {
		return config.Config{}, ConfigError(err)
	}
	return cfg, nil
}

// enabledJobs resolves job, or every configured job when job is empty, like
// pause --all.
func enabledJobs(input config.Input, stateDir string, id int64, job string, now time.Time) (config.Config, []string, error) {
	cfg, err := enabledConfig(input, stateDir, id, now)
	if err != nil {
		return config.Config{}, nil, err
	}
	if job != "" {
		if _, ok := cfg.Jobs[job]; !ok {
			return config.Config{}, nil, &Error{Code: "not_found", Message: fmt.Sprintf("job %q not found", job)}
		}
		return cfg, []string{job}, nil
	}
	names := make([]string, 0, len(cfg.Jobs))
	for name := range cfg.Jobs {
		names = append(names, name)
	}
	slices.Sort(names)
	return cfg, names, nil
}

// activeTemporarySchedules names the selected jobs with an unexpired temporary
// schedule, which a pause deletes.
func activeTemporarySchedules(cfg config.Config, names []string, now time.Time) ([]string, error) {
	state, err := store.OpenExisting(cfg.StateDir)
	if NoDatabase(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer state.Close()
	values, err := state.TemporarySchedules(cfg.Project)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, value := range values {
		if slices.Contains(names, value.Job) && now.Before(value.Until) && !slices.Contains(result, value.Job) {
			result = append(result, value.Job)
		}
	}
	slices.Sort(result)
	return result, nil
}
