// Package app exposes operations shared by interface adapters.
package app

import (
	"slices"
	"time"

	"github.com/brettinternet/noon/internal/config"
)

type Project struct {
	Path     string `json:"path"`
	Timezone string `json:"timezone"`
}

// Job exposes environment names, never their values.
type Job struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Schedule    *string    `json:"schedule"`
	NextAt      *time.Time `json:"next_at"`
	Run         any        `json:"run"`
	Prompt      *string    `json:"prompt"`
	PromptFile  *string    `json:"prompt_file"`
	Agent       string     `json:"agent,omitempty"`
	CWD         string     `json:"cwd"`
	EnvKeys     []string   `json:"env_keys"`
	Timeout     string     `json:"timeout"`
	Overlap     string     `json:"overlap"`
	Missed      string     `json:"missed"`
	Enabled     bool       `json:"enabled"`
	LastRun     *Run       `json:"last_run"`
	Source      string     `json:"source"`
	Shared      bool       `json:"shared"`
	Overrides   bool       `json:"overrides"`
	Digest      string     `json:"digest"`
}

type Inspection struct {
	Project  Project `json:"project"`
	Jobs     []Job   `json:"jobs"`
	StateDir string  `json:"-"`
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
	result := Inspection{Project: Project{Path: cfg.Project, Timezone: cfg.Timezone}, Jobs: []Job{}, StateDir: cfg.StateDir}
	names := make([]string, 0, len(cfg.Jobs))
	for name := range cfg.Jobs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		job := cfg.Jobs[name]
		view := Job{Name: name, Description: job.Description, Run: job.Run, Prompt: job.Prompt, PromptFile: job.PromptFile, Agent: job.Agent, CWD: job.CWD, EnvKeys: []string{}, Timeout: job.Timeout, Overlap: job.Overlap, Missed: job.Missed, Enabled: job.Enabled, Source: job.Source, Shared: job.Shared, Overrides: job.Overrides, Digest: job.Digest}
		for key := range job.Env {
			view.EnvKeys = append(view.EnvKeys, key)
		}
		slices.Sort(view.EnvKeys)
		if job.Schedule != nil {
			text := job.Schedule.String()
			view.Schedule = &text
			if job.Enabled {
				next := job.Schedule.Next(now, loc).In(loc)
				if !next.IsZero() {
					view.NextAt = &next
				}
			}
		}
		result.Jobs = append(result.Jobs, view)
	}
	return result, nil
}
