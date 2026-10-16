package app

import (
	"errors"
	"slices"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

// Enable approves the complete validated snapshot before any background use.
func Enable(cfg config.Config, now time.Time) (Project, []string, error) {
	project := Project{Path: cfg.Project, Timezone: cfg.Timezone, Scheduler: "offline"}
	digests := map[string]string{}
	approved := []string{}
	for name, job := range cfg.Jobs {
		if job.Shared {
			digests[name] = job.Digest
			approved = append(approved, name)
		}
	}
	slices.Sort(approved)
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return project, nil, err
	}
	defer state.Close()
	if err := state.EnableProject(cfg.Project, cfg.ConfigFile, digests, now); err != nil {
		return project, nil, err
	}
	project.Enabled = true
	if record, exists, err := state.SchedulerInfo(); err != nil {
		return project, nil, err
	} else if exists {
		if heartbeat, err := time.Parse(store.TimestampLayout, record.HeartbeatAt); err == nil && now.Sub(heartbeat) >= 0 && now.Sub(heartbeat) < 5*time.Second {
			project.Scheduler = "running"
		}
	}
	return project, approved, nil
}

func Disable(cfg config.Config) (Project, error) {
	project := Project{Path: cfg.Project, Timezone: cfg.Timezone, Scheduler: "offline"}
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return project, err
	}
	defer state.Close()
	return project, state.DisableProject(cfg.Project)
}

// WithApprovals enriches an inspection without creating or migrating state.
func WithApprovals(view Inspection) (Inspection, error) {
	state, err := store.OpenExisting(view.StateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return view, nil
	}
	if err != nil {
		return Inspection{}, err
	}
	defer state.Close()
	approval, err := state.ApprovalForProject(view.Project.Path)
	if err != nil {
		return Inspection{}, err
	}
	view.Project.Enabled = approval.Enabled
	for i := range view.Jobs {
		job := &view.Jobs[i]
		if job.Shared && approval.Enabled && approval.Approvals[job.Name] == job.Digest {
			job.Approval = "approved"
		}
	}
	return view, nil
}
