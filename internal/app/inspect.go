// Package app exposes operations shared by interface adapters.
package app

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/schedule"
	"github.com/brettinternet/noon/internal/store"
)

type Project struct {
	ID          int64  `json:"-"`
	Path        string `json:"path"`
	Timezone    string `json:"timezone"`
	Enabled     bool   `json:"enabled"`
	Scheduler   string `json:"scheduler"`
	ConfigError string `json:"config_error,omitempty"`
}

// Job exposes environment names, never their values.
type Job struct {
	TemporarySchedule   *store.TemporarySchedule `json:"temporary_schedule,omitempty"`
	Temporary           *config.TemporaryJob     `json:"temporary,omitempty"`
	Name                string                   `json:"name"`
	Description         string                   `json:"description"`
	Schedule            *string                  `json:"schedule"`
	Splay               string                   `json:"splay,omitempty"`
	SplayOffset         string                   `json:"splay_offset,omitempty"`
	Jitter              string                   `json:"jitter,omitempty"`
	NextAt              *time.Time               `json:"next_at,omitempty"`
	Until               *time.Time               `json:"until,omitempty"`
	Ended               bool                     `json:"ended"`
	Run                 any                      `json:"run"`
	Prompt              *string                  `json:"prompt"`
	PromptFile          *string                  `json:"prompt_file"`
	EnvFile             []string                 `json:"env_file"`
	Agent               string                   `json:"agent,omitempty"`
	AgentArgs           []string                 `json:"agent_args,omitempty"`
	CWD                 string                   `json:"cwd"`
	EnvKeys             []string                 `json:"env_keys"`
	Timeout             string                   `json:"timeout"`
	NoopExitCode        int                      `json:"noop_exit_code,omitempty"`
	Overlap             string                   `json:"overlap"`
	Parallel            bool                     `json:"parallel,omitempty"`
	Missed              string                   `json:"missed"`
	Enabled             bool                     `json:"enabled"`
	OS                  []string                 `json:"os,omitempty"`
	Arch                []string                 `json:"arch,omitempty"`
	PlatformSupported   bool                     `json:"platform_supported"`
	Paused              bool                     `json:"paused"`
	PauseStartsAt       *time.Time               `json:"pause_starts_at,omitempty"`
	PauseEndsAt         *time.Time               `json:"pause_ends_at,omitempty"`
	PausedBy            string                   `json:"paused_by,omitempty"`
	PausedAt            *time.Time               `json:"-"`
	LastRun             *Run                     `json:"last_run"`
	ConsecutiveFailures int                      `json:"consecutive_failures,omitempty"`
	FailingSince        *time.Time               `json:"failing_since,omitempty"`
	Source              string                   `json:"source"`
	Shared              bool                     `json:"shared"`
	Overrides           bool                     `json:"overrides"`
	Digest              string                   `json:"digest"`
	Approval            string                   `json:"approval"`
	WebhookPath         string                   `json:"webhook_path,omitempty"`
	HasWebhook          bool                     `json:"-"`
	// nextNominal is the schedule occurrence behind NextAt, before splay and
	// jitter, so Upcoming can chain later occurrences exactly as the scheduler does.
	nextNominal time.Time
}

type Inspection struct {
	SchemaVersion int     `json:"schema_version"`
	Project       Project `json:"project"`
	Jobs          []Job   `json:"jobs"`
	StateDir      string  `json:"-"`
}

// Inspect validates the effective configuration without executing or writing.
// Until a scheduler persists interval phase, intervals are relative to now.
func Inspect(input config.Input, now time.Time) (Inspection, error) {
	cfg, err := LoadJobs(input, now)
	if err != nil {
		return Inspection{}, err
	}
	return inspectConfig(cfg, now)
}

func inspectConfig(cfg config.Config, now time.Time) (Inspection, error) {
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		return Inspection{}, err
	}
	result := Inspection{SchemaVersion: 1, Project: Project{Path: cfg.Project, Timezone: cfg.Timezone, Scheduler: "offline"}, Jobs: []Job{}, StateDir: cfg.StateDir}
	names := make([]string, 0, len(cfg.Jobs))
	for name := range cfg.Jobs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		job := cfg.Jobs[name]
		view := Job{Temporary: job.Temporary, Name: name, Description: job.Description, Run: job.Run, Prompt: job.Prompt, PromptFile: job.PromptFile, EnvFile: job.EnvFile, Agent: job.Agent, AgentArgs: job.AgentArgs, CWD: job.CWD, EnvKeys: []string{}, Timeout: job.Timeout, NoopExitCode: job.NoopExitCode, Overlap: job.Overlap, Parallel: job.Parallel, Missed: job.Missed, Enabled: job.Enabled, OS: job.OS, Arch: job.Arch, PlatformSupported: job.MatchesPlatform(cfg.Platform), Source: job.Source, Shared: job.Shared, Overrides: job.Overrides, Digest: job.Digest, HasWebhook: job.Webhook != nil}
		view.Ended = job.Ended(now)
		if job.Until != nil {
			until := job.Until.In(loc)
			view.Until = &until
		}
		view.Approval = "not_required"
		if job.Shared {
			view.Approval = "needs_approval"
		}
		for key := range job.Env {
			view.EnvKeys = append(view.EnvKeys, key)
		}
		slices.Sort(view.EnvKeys)
		if job.Splay > 0 {
			view.Splay = schedule.FormatDuration(job.Splay)
			view.SplayOffset = schedule.SplayOffset(cfg.Project, name, job.Splay).String()
		}
		if job.Jitter > 0 {
			view.Jitter = schedule.FormatDuration(job.Jitter)
		}
		if job.Schedule != nil {
			text := job.Schedule.String()
			view.Schedule = &text
			if job.Enabled && view.PlatformSupported && !view.Ended {
				nominal, next := job.Schedule.NextEffective(now, loc, cfg.Project, name, job.Splay, job.Jitter, time.Time{})
				next = next.In(loc)
				if !next.IsZero() && (job.Until == nil || next.Before(*job.Until)) {
					view.NextAt, view.nextNominal = &next, nominal
				}
			}
		}
		result.Jobs = append(result.Jobs, view)
	}
	return result, nil
}

// ListJobs returns the complete JSON list view shared by CLI and MCP.
func ListJobs(input config.Input, now time.Time) (Inspection, error) {
	view, err := Inspect(input, now)
	if err != nil {
		return Inspection{}, ConfigError(err)
	}
	view, err = WithApprovals(view)
	if err != nil {
		return Inspection{}, StateError(err)
	}
	view, err = WithLastRuns(view, view.StateDir)
	if err != nil {
		return Inspection{}, StateError(err)
	}
	view, err = WithScheduler(view, now)
	if err != nil {
		return Inspection{}, StateError(err)
	}
	return view, nil
}

// Failing reports a failure streak on a job that is still expected to run.
func (job Job) Failing() bool {
	return job.ConsecutiveFailures > 0 && job.Enabled && !job.Paused && !job.Ended
}

// PauseStatus is the NEXT text for an active pause, or empty.
func (job Job) PauseStatus() string {
	if !job.Paused {
		return ""
	}
	if job.PauseEndsAt != nil {
		return "paused until " + job.PauseEndsAt.Format("Mon 15:04")
	}
	return "paused"
}

// Occurrence is one planned start; Job is empty for a schedule preview.
type Occurrence struct {
	Job string    `json:"job,omitempty"`
	At  time.Time `json:"at"`
}

// Upcoming lists planned starts from a ListJobs view. Without names it returns
// the next n across all jobs; with names, the next n of each. Results are in
// time order, ties by job name. Each job starts at its NextAt and follows the
// scheduler: splay and jitter apply, a pending pause window is skipped, and
// nothing starts at or after until. Jobs without a NextAt and paused, disabled,
// other-platform, or unapproved jobs contribute nothing. Temporary schedules are excluded.
func Upcoming(view Inspection, names []string, n int) ([]Occurrence, error) {
	if n < 1 || n > 100 {
		return nil, UsageError("--limit must be between 1 and 100", "")
	}
	loc, err := time.LoadLocation(view.Project.Timezone)
	if err != nil {
		return nil, ConfigError(err)
	}
	selected := view.Jobs
	if len(names) > 0 {
		selected = make([]Job, 0, len(names))
		for _, name := range names {
			i := slices.IndexFunc(view.Jobs, func(job Job) bool { return job.Name == name })
			if i < 0 {
				hint, best := "Run noon list to see available jobs", 3
				for _, job := range view.Jobs {
					if distance := editDistance(name, job.Name); distance < best {
						hint, best = "Did you mean noon next "+job.Name+"?", distance
					}
				}
				return nil, UsageError(fmt.Sprintf("job %q not found", name), hint)
			}
			if view.Jobs[i].Schedule == nil {
				return nil, UsageError(fmt.Sprintf("job %q has no schedule", name), "Run noon list to see scheduled jobs")
			}
			selected = append(selected, view.Jobs[i])
		}
	}
	var result []Occurrence
	for _, job := range selected {
		if job.Schedule == nil || job.nextNominal.IsZero() || !job.Enabled || !job.PlatformSupported || job.Paused || job.Approval == "needs_approval" {
			continue
		}
		parsed, err := schedule.Parse(*job.Schedule)
		if err != nil {
			return nil, ConfigError(err)
		}
		splay, _ := schedule.ParseDuration(job.Splay)
		jitter, _ := schedule.ParseDuration(job.Jitter)
		p := plan{schedule: parsed, loc: loc, project: view.Project.Path, name: job.Name, splay: splay, jitter: jitter, until: job.Until, pauseStart: job.PauseStartsAt, pauseEnd: job.PauseEndsAt}
		for _, at := range p.upcoming(job.nextNominal, n) {
			result = append(result, Occurrence{Job: job.Name, At: at})
		}
	}
	slices.SortStableFunc(result, func(a, b Occurrence) int {
		if c := a.At.Compare(b.At); c != 0 {
			return c
		}
		return strings.Compare(a.Job, b.Job)
	})
	if len(names) == 0 && len(result) > n {
		result = result[:n]
	}
	return result, nil
}

// PreviewSchedule lists the next n occurrences of s after now in loc, without
// splay or jitter.
func PreviewSchedule(s schedule.Schedule, loc *time.Location, now time.Time, n int) ([]Occurrence, error) {
	if n < 1 || n > 100 {
		return nil, UsageError("--limit must be between 1 and 100", "")
	}
	nominal, _ := s.NextEffective(now, loc, "", "", 0, 0, time.Time{})
	p := plan{schedule: s, loc: loc}
	result := []Occurrence{}
	for _, at := range p.upcoming(nominal, n) {
		result = append(result, Occurrence{At: at})
	}
	return result, nil
}

type plan struct {
	schedule             schedule.Schedule
	loc                  *time.Location
	project, name        string
	splay, jitter        time.Duration
	until                *time.Time
	pauseStart, pauseEnd *time.Time
}

// upcoming chains nominal occurrences as the scheduler advances them:
// intervals keep their phase and calendar schedules use wall-clock rules.
func (p plan) upcoming(nominal time.Time, n int) []time.Time {
	var result []time.Time
	for len(result) < n && !nominal.IsZero() {
		at := schedule.EffectiveStart(p.project, p.name, nominal, p.splay, p.jitter).In(p.loc)
		if p.until != nil && !at.Before(*p.until) {
			break
		}
		if p.pauseStart != nil && p.pauseEnd != nil && !at.Before(*p.pauseStart) && at.Before(*p.pauseEnd) {
			nominal, _ = p.schedule.AfterPause(*p.pauseEnd, p.loc, p.project, p.name, p.splay, p.jitter)
			continue
		}
		result = append(result, at)
		if interval, ok := p.schedule.Interval(); ok {
			nominal = nominal.Add(interval)
		} else {
			nominal = p.schedule.Next(nominal, p.loc)
		}
	}
	return result
}
