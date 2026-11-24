package app

import (
	"bytes"
	"cmp"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/schedule"
	"github.com/brettinternet/noon/internal/store"
)

const (
	overviewRecentRuns = 20
	upcomingWindow     = 24 * time.Hour
	upcomingLimit      = 50
)

// OverviewProject is one enabled project with its state and recent runs.
// State is ok, failing, needs_approval, paused, missing, or config_error.
type OverviewProject struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Timezone    string     `json:"timezone,omitempty"`
	State       string     `json:"state"`
	ConfigError string     `json:"config_error,omitempty"`
	NextAt      *time.Time `json:"next_at,omitempty"`
	Recent      []Run      `json:"recent"`
	// Command is the CLI command that resolves State, run from anywhere.
	Command string `json:"command,omitempty"`
}

// FailingJob is a job whose latest run failed, timed out, or was interrupted.
type FailingJob struct {
	ProjectID int64  `json:"project_id"`
	Project   string `json:"project"`
	Job       string `json:"job"`
	Run       Run    `json:"run"`
}

// UpcomingRun is one planned start. A job that starts more than once an hour
// takes one row whose Every names its schedule.
type UpcomingRun struct {
	ProjectID int64     `json:"project_id"`
	Project   string    `json:"project"`
	Timezone  string    `json:"timezone"`
	Job       string    `json:"job"`
	At        time.Time `json:"at"`
	Jitter    string    `json:"jitter,omitempty"`
	Every     string    `json:"every,omitempty"`
}

// OverviewView spans every enabled project the scheduler serves.
type OverviewView struct {
	Projects []OverviewProject `json:"projects"`
	Failing  []FailingJob      `json:"failing"`
	Upcoming []UpcomingRun     `json:"upcoming"`
}

// Overview reads every enabled project's state, recent runs, failing jobs, and
// next day of scheduled starts without creating state.
func Overview(input config.Input, now time.Time) (OverviewView, error) {
	stateDir, dashboards, _, err := enabledDashboards(input, now)
	if err != nil {
		return OverviewView{}, err
	}
	result := OverviewView{Projects: []OverviewProject{}, Failing: []FailingJob{}, Upcoming: []UpcomingRun{}}
	for _, entry := range dashboards {
		status := entry.status
		project := OverviewProject{ID: status.ID, Name: status.Name, Path: status.Path, State: status.State, ConfigError: status.ConfigError, NextAt: status.NextAt}
		switch project.State {
		case "missing":
			project.Command = ProjectCommand(project.Path, status.configFile, "noon disable")
		case "config_error":
			project.Command = ProjectCommand(project.Path, status.configFile, "noon config check")
		}
		project.Recent, err = Runs(stateDir, project.Path, "", overviewRecentRuns)
		if err != nil && !NoDatabase(err) {
			return OverviewView{}, StateError(err)
		}
		if view := entry.dashboard; view != nil && project.State != "config_error" {
			project.Timezone = view.Project.Timezone
			if project.State == "ok" {
				project.State = quietState(view.Jobs)
			}
			if project.State == "needs_approval" {
				project.Command = ProjectCommand(project.Path, status.configFile, "noon enable")
			}
			for _, job := range view.Jobs {
				if job.LastRun != nil && FailedStatus(job.LastRun.Status) {
					result.Failing = append(result.Failing, FailingJob{ProjectID: project.ID, Project: project.Name, Job: job.Name, Run: *job.LastRun})
				}
			}
			result.Upcoming = append(result.Upcoming, upcomingDay(project, view.Jobs, now)...)
		}
		result.Projects = append(result.Projects, project)
	}
	slices.SortStableFunc(result.Failing, func(a, b FailingJob) int { return cmp.Compare(b.Run.ID, a.Run.ID) })
	result.Upcoming = sortUpcoming(result.Upcoming)
	return result, nil
}

// FailedStatus reports a run that ended badly.
func FailedStatus(status string) bool {
	return status == "failed" || status == "timed_out" || status == "interrupted"
}

// quietState refines an ok project: pending approval outranks a full pause.
func quietState(jobs []Job) string {
	enabled, paused := 0, 0
	for _, job := range jobs {
		if !job.Enabled || job.Ended {
			continue
		}
		if job.Approval == "needs_approval" {
			return "needs_approval"
		}
		enabled++
		if job.Paused {
			paused++
		}
	}
	if enabled > 0 && paused == enabled {
		return "paused"
	}
	return "ok"
}

// upcomingDay lists a project's starts within a day of now, following the
// Upcoming rules. More than 24 starts in the window collapse to one row.
func upcomingDay(project OverviewProject, jobs []Job, now time.Time) []UpcomingRun {
	loc, err := time.LoadLocation(project.Timezone)
	if err != nil {
		return nil
	}
	end := now.Add(upcomingWindow)
	var result []UpcomingRun
	for _, job := range jobs {
		if job.Schedule == nil || job.nextNominal.IsZero() || !job.Enabled || !job.PlatformSupported || job.Paused || job.Approval == "needs_approval" {
			continue
		}
		parsed, err := schedule.Parse(*job.Schedule)
		if err != nil {
			continue
		}
		splay, _ := schedule.ParseDuration(job.Splay)
		jitter, _ := schedule.ParseDuration(job.Jitter)
		p := plan{schedule: parsed, loc: loc, project: project.Path, name: job.Name, splay: splay, jitter: jitter, until: job.Until, pauseStart: job.PauseStartsAt, pauseEnd: job.PauseEndsAt}
		var starts []time.Time
		for _, at := range p.upcoming(job.nextNominal, 25) {
			if at.Before(end) {
				starts = append(starts, at)
			}
		}
		row := UpcomingRun{ProjectID: project.ID, Project: project.Name, Timezone: project.Timezone, Job: job.Name, Jitter: job.Jitter}
		if len(starts) > 24 {
			row.At, row.Every = starts[0], *job.Schedule
			result = append(result, row)
			continue
		}
		for _, at := range starts {
			row.At = at
			result = append(result, row)
		}
	}
	return result
}

// sortUpcoming orders rows by time, then project and job, and keeps the first 50.
func sortUpcoming(rows []UpcomingRun) []UpcomingRun {
	slices.SortStableFunc(rows, func(a, b UpcomingRun) int {
		return cmp.Or(a.At.Compare(b.At), strings.Compare(a.Project, b.Project), strings.Compare(a.Job, b.Job))
	})
	if len(rows) > upcomingLimit {
		rows = rows[:upcomingLimit]
	}
	return rows
}

// ProjectView is one enabled project's jobs and filtered recent runs.
type ProjectView struct {
	Project   OverviewProject `json:"project"`
	Dashboard DashboardView   `json:"dashboard"`
	Runs      []Run           `json:"runs"`
}

// EnabledProject reads one enabled project for an adapter. Disabled and
// unknown projects share the not-found error.
func EnabledProject(input config.Input, id int64, job string, statuses []string, limit int, now time.Time) (ProjectView, error) {
	if err := ValidateRunStatuses(statuses); err != nil {
		return ProjectView{}, err
	}
	stateDir, dashboards, _, err := enabledDashboards(input, now)
	if err != nil {
		return ProjectView{}, err
	}
	for _, entry := range dashboards {
		if entry.status.ID != id {
			continue
		}
		result := ProjectView{Project: OverviewProject{ID: id, Name: entry.status.Name, Path: entry.status.Path, State: entry.status.State, ConfigError: entry.status.ConfigError, NextAt: entry.status.NextAt}}
		if entry.dashboard != nil {
			result.Dashboard = *entry.dashboard
			result.Project.Timezone = entry.dashboard.Project.Timezone
			if result.Project.State == "ok" {
				result.Project.State = quietState(entry.dashboard.Jobs)
			}
			if result.Project.State == "needs_approval" {
				result.Project.Command = ProjectCommand(result.Project.Path, entry.status.configFile, "noon enable")
			}
		}
		result.Runs, err = RunsWithStatuses(stateDir, entry.status.Path, job, statuses, limit)
		if err != nil && !NoDatabase(err) {
			return ProjectView{}, StateError(err)
		}
		return result, nil
	}
	return ProjectView{}, projectNotFound()
}

// RunPageView is one run of an enabled project with its prompt and resume command.
type RunPageView struct {
	Project  ProjectSummary `json:"project"`
	Timezone string         `json:"timezone"`
	Run      Run            `json:"run"`
	Prompt   string         `json:"prompt,omitempty"`
	Resume   string         `json:"resume,omitempty"`
}

// EnabledRun reads a run that belongs to an enabled project.
func EnabledRun(input config.Input, stateDir string, id int64, now time.Time) (RunPageView, error) {
	project, err := EnabledRunProject(stateDir, id)
	if err != nil {
		return RunPageView{}, err
	}
	run, err := RunByID(stateDir, id)
	if err != nil {
		if IsMissingRun(err) || NoDatabase(err) {
			return RunPageView{}, RunNotFoundError(id)
		}
		return RunPageView{}, StateError(err)
	}
	result := RunPageView{Project: project, Timezone: "Local", Run: run}
	if config.IsUserProject(project.Path, input.UserConfigDir) {
		result.Project.Name = "user"
	}
	values, err := EnabledProjectInput(input, stateDir, project.ID)
	if err != nil {
		return RunPageView{}, err
	}
	// A broken config still shows the run; only prompt and resume need it.
	if cfg, err := LoadJobs(values, now); err == nil {
		result.Timezone = cfg.Timezone
		if job, ok := cfg.Jobs[run.Job]; ok {
			if job.Prompt != nil {
				result.Prompt = *job.Prompt
			} else if job.PromptFile != nil {
				result.Prompt = *job.PromptFile
			}
			if run.AgentSessionID != nil {
				result.Resume = AgentResumeCommand(job, *run.AgentSessionID, true)
			}
		}
	}
	return result, nil
}

// LogLine is one log line, numbered from the start of the log.
type LogLine struct {
	Number int64
	Text   []byte
}

// LogPage is a window of whole log lines ending at End. Earlier, when set, is
// the cursor of the window before it.
type LogPage struct {
	Lines   []LogLine
	Start   int64
	End     int64
	Earlier *int64
}

// RunLogPage reads at most limit bytes of whole lines ending before the before
// cursor, or at the end of the log. An active run's unfinished last line is
// left for the stream.
func RunLogPage(stateDir string, id int64, before *int64, limit int) (LogPage, error) {
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		return LogPage{}, err
	}
	defer state.Close()
	stored, err := state.RunByID(id)
	if err != nil {
		return LogPage{}, err
	}
	data, err := state.ReadRunLog(stored)
	if errors.Is(err, store.ErrLogUnavailable) {
		return LogPage{}, nil
	}
	if err != nil {
		return LogPage{}, err
	}
	end := int64(len(data))
	if activeStoredRun(stored) {
		end = int64(bytes.LastIndexByte(data, '\n') + 1)
	}
	if before != nil {
		if *before < 0 || *before > end {
			return LogPage{}, UsageError("before must be a log cursor", "")
		}
		end = logLineStart(data, *before)
	}
	start := max(end-int64(limit), 0)
	// A line straddling the limit is shown whole.
	start = logLineStart(data, start)
	page := LogPage{Start: start, End: end}
	if start > 0 {
		page.Earlier = &start
	}
	number := int64(bytes.Count(data[:start], []byte{'\n'})) + 1
	for offset := start; offset < end; number++ {
		next := int64(bytes.IndexByte(data[offset:end], '\n'))
		if next < 0 {
			page.Lines = append(page.Lines, LogLine{Number: number, Text: data[offset:end]})
			break
		}
		page.Lines = append(page.Lines, LogLine{Number: number, Text: data[offset : offset+next]})
		offset += next + 1
	}
	return page, nil
}
