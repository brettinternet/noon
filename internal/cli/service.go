package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/service"
	urfave "github.com/urfave/cli/v3"
)

const serviceSupervisorHint = "Run noon serve under your own supervisor."

func serviceOptions(cmd *urfave.Command) (service.Config, error) {
	if err := noArgs(cmd); err != nil {
		return service.Config{}, err
	}
	for _, name := range []string{"project", "config", "timezone"} {
		if cmd.IsSet(name) {
			return service.Config{}, usageError(fmt.Sprintf("service does not use --%s or its environment variable", name), "Use --state-dir to select Noon state")
		}
	}
	for _, name := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE"} {
		if _, exists := os.LookupEnv(name); exists {
			return service.Config{}, usageError("service does not use "+name, "Use --state-dir to select Noon state")
		}
	}
	values, err := input(cmd)
	if err != nil {
		return service.Config{}, err
	}
	stateDir, err := config.StateDirectory(values)
	if err != nil {
		return service.Config{}, configError(err)
	}
	options, err := service.CurrentConfig(stateDir)
	if err != nil {
		return service.Config{}, configError(err)
	}
	return options, nil
}

func installService(ctx context.Context, cmd *urfave.Command, build config.BuildOpts) error {
	options, err := serviceOptions(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool("print") {
		unit, err := service.CurrentUnit(options)
		if err != nil {
			return serviceError(err)
		}
		_, err = cmd.Writer.Write(unit.Contents)
		return err
	}
	result, err := app.InstallService(ctx, options, service.ExecRunner{}, build, time.Now())
	if err != nil {
		return serviceError(err)
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"installed": true, "unit_path": result.UnitPath, "changed": result.Changed, "linger_disabled": result.LingerDisabled, "restarted": result.Restarted, "warning": guidance(cmd, result.Warning)})
	}
	fmt.Fprintf(cmd.Writer, "Installed service · %s\n", result.UnitPath)
	if result.Restarted {
		fmt.Fprintln(cmd.Writer, "Restarted outdated scheduler; active jobs were interrupted.")
	}
	if result.Warning != "" {
		fmt.Fprintln(cmd.Writer, guidance(cmd, result.Warning))
	}
	if result.LingerDisabled {
		fmt.Fprintln(cmd.Writer, "Hint: run loginctl enable-linger to keep schedules alive without an interactive login.")
	}
	return nil
}

func restartService(ctx context.Context, cmd *urfave.Command) error {
	options, err := serviceOptions(cmd)
	if err != nil {
		return err
	}
	path, err := app.RestartService(ctx, options, service.ExecRunner{})
	if err != nil {
		return serviceError(err)
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"restarted": true, "unit_path": path})
	}
	fmt.Fprintf(cmd.Writer, "Restarted service · %s\n", path)
	return nil
}

func uninstallService(ctx context.Context, cmd *urfave.Command) error {
	options, err := serviceOptions(cmd)
	if err != nil {
		return err
	}
	path, installed, err := app.UninstallService(ctx, options, service.ExecRunner{})
	if err != nil {
		return serviceError(err)
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"installed": false, "unit_path": path})
	}
	if installed {
		fmt.Fprintf(cmd.Writer, "Uninstalled service · %s\n", path)
	} else {
		fmt.Fprintf(cmd.Writer, "Service not installed · %s\n", path)
	}
	return nil
}

func serviceStatus(ctx context.Context, cmd *urfave.Command) error {
	options, err := serviceOptions(cmd)
	if err != nil {
		return err
	}
	view, err := app.InspectService(ctx, options, service.ExecRunner{}, time.Now())
	if err != nil {
		return serviceError(err)
	}
	if cmd.Bool("json") {
		result := map[string]any{"service": view}
		if view.Drift {
			result["warning"] = driftWarning
			result["hint"] = guidance(cmd, "noon service install")
		}
		return emit(cmd, result)
	}
	installed := "no"
	if view.Installed {
		installed = "yes"
	}
	fmt.Fprintf(cmd.Writer, "Installed:  %s\nUnit:       %s\nManager:    %s\nScheduler:  %s\n", installed, view.UnitPath, view.ManagerState, view.SchedulerState)
	fmt.Fprintf(cmd.Writer, "Build:      version=%q commit=%q\n", view.SchedulerVersion, view.SchedulerCommit)
	if view.SchedulerHeartbeat != nil {
		fmt.Fprintf(cmd.Writer, "Heartbeat:  %s\n", view.SchedulerHeartbeat.Format(time.RFC3339))
	}
	if view.Drift {
		fmt.Fprintf(cmd.Writer, "Warning: service unit differs from current install settings. Suggested action: %s\n", guidance(cmd, "noon service install"))
	}
	return nil
}

const driftWarning = "Installed service unit differs from current install settings."

func serviceGuidance(stateDir string) (string, error) {
	options, err := service.CurrentConfig(stateDir)
	if err != nil {
		return "", configError(err)
	}
	_, installed, err := app.ServiceUnitInstalled(options)
	if service.IsUnsupported(err) {
		return "Suggested action: run noon serve under your own supervisor", nil
	}
	if err != nil {
		return "", serviceError(err)
	}
	if installed {
		return "Scheduler service is installed but offline. Run noon service to inspect manager status.", nil
	}
	return "Suggested action: noon service install", nil
}

func serviceError(err error) error {
	if service.IsUnsupported(err) {
		return &Error{Code: "service_unsupported", Message: err.Error(), Hint: serviceSupervisorHint}
	}
	return &Error{Code: "service", Message: strings.TrimSpace(err.Error())}
}
