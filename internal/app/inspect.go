// Package app exposes operations shared by interface adapters.
package app

import (
	"slices"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/schedule"
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
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	Schedule          *string    `json:"schedule"`
	Splay             string     `json:"splay,omitempty"`
	SplayOffset       string     `json:"splay_offset,omitempty"`
	Jitter            string     `json:"jitter,omitempty"`
	NextAt            *time.Time `json:"next_at"`
	Run               any        `json:"run"`
	Prompt            *string    `json:"prompt"`
	PromptFile        *string    `json:"prompt_file"`
	Agent             string     `json:"agent,omitempty"`
	AgentArgs         []string   `json:"agent_args,omitempty"`
	CWD               string     `json:"cwd"`
	EnvKeys           []string   `json:"env_keys"`
	Timeout           string     `json:"timeout"`
	NoopExitCode      int        `json:"noop_exit_code,omitempty"`
	Overlap           string     `json:"overlap"`
	Missed            string     `json:"missed"`
	Enabled           bool       `json:"enabled"`
	OS                []string   `json:"os,omitempty"`
	Arch              []string   `json:"arch,omitempty"`
	PlatformSupported bool       `json:"platform_supported"`
	Paused            bool       `json:"paused"`
	LastRun           *Run       `json:"last_run"`
	Source            string     `json:"source"`
	Shared            bool       `json:"shared"`
	Overrides         bool       `json:"overrides"`
	Digest            string     `json:"digest"`
	Approval          string     `json:"approval"`
	WebhookPath       string     `json:"webhook_path,omitempty"`
	HasWebhook        bool       `json:"-"`
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
	cfg, err := config.Load(input)
	if err != nil {
		return Inspection{}, err
	}
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
		view := Job{Name: name, Description: job.Description, Run: job.Run, Prompt: job.Prompt, PromptFile: job.PromptFile, Agent: job.Agent, AgentArgs: job.AgentArgs, CWD: job.CWD, EnvKeys: []string{}, Timeout: job.Timeout, NoopExitCode: job.NoopExitCode, Overlap: job.Overlap, Missed: job.Missed, Enabled: job.Enabled, OS: job.OS, Arch: job.Arch, PlatformSupported: job.MatchesPlatform(cfg.Platform), Source: job.Source, Shared: job.Shared, Overrides: job.Overrides, Digest: job.Digest, HasWebhook: job.Webhook != nil}
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
			if job.Enabled && view.PlatformSupported {
				_, next := job.Schedule.NextEffective(now, loc, cfg.Project, name, job.Splay, job.Jitter, time.Time{})
				next = next.In(loc)
				if !next.IsZero() {
					view.NextAt = &next
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
