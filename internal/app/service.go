package app

import (
	"context"
	"errors"
	"time"

	"github.com/brettinternet/noon/internal/service"
	"github.com/brettinternet/noon/internal/store"
)

type ServiceView struct {
	Installed          bool       `json:"installed"`
	UnitPath           string     `json:"unit_path"`
	ManagerState       string     `json:"manager_state"`
	SchedulerState     string     `json:"scheduler_state"`
	SchedulerHeartbeat *time.Time `json:"scheduler_heartbeat,omitempty"`
	Drift              bool       `json:"drift"`
}

func InstallService(ctx context.Context, config service.Config, runner service.Runner) (service.InstallResult, error) {
	return service.Install(ctx, config, runner)
}

func UninstallService(ctx context.Context, config service.Config, runner service.Runner) (string, bool, error) {
	return service.Uninstall(ctx, config, runner)
}

func ServiceUnitInstalled(config service.Config) (string, bool, error) {
	return service.IsInstalled(config)
}

func InspectService(ctx context.Context, config service.Config, runner service.Runner, now time.Time) (ServiceView, error) {
	manager, err := service.Inspect(ctx, config, runner)
	if err != nil {
		return ServiceView{}, err
	}
	view := ServiceView{
		Installed: manager.Installed, UnitPath: manager.UnitPath, ManagerState: manager.ManagerState,
		SchedulerState: "offline", Drift: manager.Drift,
	}
	state, err := store.OpenExisting(config.StateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return view, nil
	}
	if err != nil {
		return ServiceView{}, err
	}
	defer state.Close()
	record, exists, err := state.SchedulerInfo()
	if err != nil {
		return ServiceView{}, err
	}
	if !exists {
		return view, nil
	}
	heartbeat, err := time.Parse(store.TimestampLayout, record.HeartbeatAt)
	if err != nil {
		return view, nil
	}
	view.SchedulerHeartbeat = &heartbeat
	if age := now.Sub(heartbeat); age >= 0 && age < 5*time.Second {
		view.SchedulerState = "running"
	}
	return view, nil
}
