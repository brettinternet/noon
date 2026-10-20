package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/config"
	process "github.com/brettinternet/noon/internal/runner"
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
	SchedulerVersion   string     `json:"scheduler_version"`
	SchedulerCommit    string     `json:"scheduler_commit"`
}

func InstallService(ctx context.Context, options service.Config, runner service.Runner, build config.BuildOpts, now time.Time) (service.InstallResult, error) {
	// Read before install: a stopped service starting up must not be restarted again.
	record, exists, err := schedulerRecord(options.StateDir)
	if err != nil {
		return service.InstallResult{}, err
	}
	result, err := service.Install(ctx, options, runner)
	if err != nil || result.Changed {
		return result, err
	}
	if !exists || !schedulerLive(record, now) {
		// Install may have just started the current build; an older one may also be hung.
		result.Warning = "No live scheduler was found before install; if an older build is still running, use noon service restart."
		return result, nil
	}
	mismatch, known := compareSchedulerBuild(record, build)
	if !mismatch {
		if !known {
			result.Warning = "Scheduler build metadata is unknown; use noon service restart explicitly after an upgrade."
		}
		return result, nil
	}
	unit, err := service.CurrentUnit(options)
	if err != nil {
		return result, err
	}
	pid, err := service.RunningPID(ctx, unit, runner)
	if err != nil {
		return result, err
	}
	if pid <= 0 || pid != record.PID {
		result.Warning = "Scheduler is not identified as service-owned; restart its supervisor explicitly after an upgrade."
		return result, nil
	}
	// Check identity again after the manager lookup to reject exited/reused PIDs.
	if !schedulerLive(record, now) {
		return result, nil
	}
	if err := service.Restart(ctx, unit, runner); err != nil {
		return result, err
	}
	result.Restarted = true
	return result, nil
}

func schedulerRecord(stateDir string) (store.SchedulerRecord, bool, error) {
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return store.SchedulerRecord{}, false, nil
	}
	if err != nil {
		return store.SchedulerRecord{}, false, err
	}
	defer state.Close()
	return state.SchedulerInfo()
}

func schedulerLive(record store.SchedulerRecord, now time.Time) bool {
	heartbeat, err := time.Parse(store.TimestampLayout, record.HeartbeatAt)
	if err != nil || now.Sub(heartbeat) < 0 || now.Sub(heartbeat) >= 5*time.Second {
		return false
	}
	alive, err := process.ProcessOwnerAlive(record.PID, record.StartIdentity)
	return err == nil && alive
}

// A known differing field proves mismatch even if the other field is unavailable.
// Matching requires both fields to be known; development labels prove nothing.
func compareSchedulerBuild(record store.SchedulerRecord, build config.BuildOpts) (mismatch, known bool) {
	knownValue := func(value string) bool {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "unknown", "dev", "development", "test", "(devel)":
			return false
		default:
			return true
		}
	}
	versionKnown := knownValue(record.Version) && knownValue(build.Version)
	commitKnown := knownValue(record.Commit) && knownValue(build.Commit)
	return versionKnown && record.Version != build.Version || commitKnown && record.Commit != build.Commit, versionKnown && commitKnown
}

func schedulerBuildMessage(record store.SchedulerRecord, build config.BuildOpts) string {
	return fmt.Sprintf("Scheduler version=%q commit=%q; invoking binary version=%q commit=%q", record.Version, record.Commit, build.Version, build.Commit)
}

func RestartService(ctx context.Context, config service.Config, runner service.Runner) (string, error) {
	unit, err := service.CurrentUnit(config)
	if err != nil {
		return "", err
	}
	return unit.Path, service.Restart(ctx, unit, runner)
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
	view.SchedulerVersion = record.Version
	view.SchedulerCommit = record.Commit
	if age := now.Sub(heartbeat); age >= 0 && age < 5*time.Second {
		view.SchedulerState = "running"
	}
	return view, nil
}
