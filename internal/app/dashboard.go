package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/service"
	"github.com/brettinternet/noon/internal/store"
)

type DashboardProject struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	Timezone    string `json:"timezone"`
	Enabled     bool   `json:"enabled"`
	ConfigError string `json:"config_error"`
}
type SchedulerView struct {
	State       string     `json:"state"`
	PID         int        `json:"pid"`
	HeartbeatAt *time.Time `json:"heartbeat_at"`
}
type DashboardService struct {
	Installed bool `json:"installed"`
}
type Notice struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type DashboardView struct {
	SchemaVersion int              `json:"schema_version"`
	Project       DashboardProject `json:"project"`
	Scheduler     SchedulerView    `json:"scheduler"`
	Service       DashboardService `json:"service"`
	Jobs          []Job            `json:"jobs"`
	Queued        []Run            `json:"queued"`
	Notices       []Notice         `json:"notices"`
	Next          string           `json:"next"`
}

// inspectionForDiagnostics preserves a config failure while resolving enough
// context to inspect existing state. It never uses invalid jobs for execution.
func inspectionForDiagnostics(input config.Input, now time.Time) (Inspection, error) {
	view, err := Inspect(input, now)
	if err == nil {
		return view, nil
	}
	message := err.Error()
	input.Timezone = nil
	cfg, err := config.LoadForDisable(input)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{Project: Project{Path: cfg.Project, Timezone: cfg.Timezone, Scheduler: "offline", ConfigError: message}, StateDir: cfg.StateDir, Jobs: []Job{}}, nil
}

// DashboardConfigError identifies configuration failures that prevent even
// resolving the dashboard's project/state context.
type DashboardConfigError struct{ Err error }

func (e *DashboardConfigError) Error() string { return e.Err.Error() }
func (e *DashboardConfigError) Unwrap() error { return e.Err }

// Dashboard builds the complete read-only project dashboard for all adapters.
func Dashboard(input config.Input, now time.Time) (DashboardView, error) {
	view, err := inspectionForDiagnostics(input, now)
	if err != nil {
		return DashboardView{}, &DashboardConfigError{Err: err}
	}
	configError := view.Project.ConfigError
	view, err = WithApprovals(view)
	if err != nil {
		return DashboardView{}, err
	}
	view, err = WithLastRuns(view, view.StateDir)
	if err != nil {
		return DashboardView{}, err
	}
	view, err = WithScheduler(view, now)
	if err != nil {
		return DashboardView{}, err
	}
	if configError != "" {
		view.Project.ConfigError = configError
	}
	result := DashboardView{
		SchemaVersion: 1,
		Project:       DashboardProject{Path: view.Project.Path, Name: filepath.Base(view.Project.Path), Timezone: view.Project.Timezone, Enabled: view.Project.Enabled, ConfigError: view.Project.ConfigError},
		Scheduler:     SchedulerView{State: view.Project.Scheduler}, Jobs: view.Jobs, Notices: []Notice{},
	}
	result.Queued, err = QueuedRuns(view.StateDir, view.Project.Path)
	if err != nil {
		return DashboardView{}, err
	}
	state, err := store.OpenExisting(view.StateDir)
	if err != nil && !errors.Is(err, store.ErrNoDatabase) {
		return DashboardView{}, err
	}
	if err == nil {
		defer state.Close()
		record, exists, err := state.SchedulerInfo()
		if err != nil {
			return DashboardView{}, err
		}
		if exists {
			result.Scheduler.PID = record.PID
			if at, err := time.Parse(store.TimestampLayout, record.HeartbeatAt); err == nil {
				result.Scheduler.HeartbeatAt = &at
			}
		}
	}
	options, err := service.CurrentConfig(view.StateDir)
	if err != nil {
		return DashboardView{}, err
	}
	_, result.Service.Installed, err = service.IsInstalled(options)
	if err != nil && !service.IsUnsupported(err) {
		return DashboardView{}, err
	}
	scheduled, pending := false, 0
	for _, job := range result.Jobs {
		if job.Schedule != nil {
			scheduled = true
		}
		if job.Enabled && job.Approval == "needs_approval" {
			pending++
		}
	}
	add := func(code, message string) { result.Notices = append(result.Notices, Notice{code, message}) }
	if result.Project.ConfigError != "" {
		add("config_error", "Config error: "+result.Project.ConfigError)
	}
	if !result.Project.Enabled && scheduled {
		add("not_enabled", "Not enabled for background runs.")
	}
	if result.Project.Enabled && pending > 0 {
		add("needs_approval", fmt.Sprintf("%d shared jobs need approval.", pending))
	}
	if result.Project.Enabled && result.Scheduler.State == "offline" {
		add("scheduler_offline", "Scheduler offline; background work will wait.")
	}
	if len(result.Queued) > 0 {
		jobs := make([]string, 0, len(result.Queued))
		for _, run := range result.Queued {
			jobs = append(jobs, run.Job+" due "+RelativeTime(run.ScheduledAt, now))
		}
		add("queued", fmt.Sprintf("%d queued: %s", len(jobs), strings.Join(jobs, ", ")))
	}
	switch {
	case result.Project.ConfigError != "":
		result.Next = "noon config check"
	case !result.Project.Enabled && scheduled || result.Project.Enabled && pending > 0:
		result.Next = "noon enable"
	case result.Project.Enabled && result.Scheduler.State == "offline":
		result.Next = "noon service install"
		if result.Service.Installed || service.IsUnsupported(err) {
			result.Next = "noon serve"
		}
	default:
		result.Next = "noon add"
		for _, job := range result.Jobs {
			if job.Enabled {
				result.Next = "noon run " + job.Name
				break
			}
		}
	}
	return result, nil
}
