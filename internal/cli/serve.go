package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/scheduler"
	"github.com/brettinternet/noon/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func serveAction(build config.BuildOpts) func(context.Context, *urfave.Command) error {
	return func(ctx context.Context, cmd *urfave.Command) error {
		if cmd.Args().Len() != 0 {
			return usageError("serve accepts no arguments", "Run noon serve")
		}
		for _, name := range []string{"project", "config", "timezone"} {
			if cmd.IsSet(name) {
				return usageError(fmt.Sprintf("serve does not accept --%s or its environment variable", name), "Use the project and config selection recorded by noon enable")
			}
		}
		if cmd.IsSet("json") {
			return usageError("serve does not support --json or NOON_JSON", "Run noon status to inspect scheduler state")
		}
		for _, name := range []string{"listen", "api-token-file"} {
			if cmd.IsSet(name) && strings.TrimSpace(cmd.String(name)) == "" {
				return usageError("--"+name+" must not be empty", "Pass a value or omit the flag to use user config")
			}
		}
		for _, name := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE", "NOON_JSON"} {
			if _, exists := os.LookupEnv(name); exists {
				return usageError("serve does not accept "+name, "Use the project and config selection recorded by noon enable")
			}
		}
		values, err := input(cmd)
		if err != nil {
			return err
		}
		stateDir, settings, err := config.ServeSettings(values)
		if err != nil {
			return configError(err)
		}
		// Each flag overrides its user config setting.
		if cmd.IsSet("listen") {
			settings.Listen = cmd.String("listen")
		}
		if cmd.IsSet("api-token-file") {
			settings.APITokenFile = cmd.String("api-token-file")
		}
		if settings.APITokenFile != "" && settings.Listen == "" {
			return usageError("an API token file (--api-token-file or serve.api_token_file) requires a listen address", "Pass --listen ADDR or set serve.listen in user config")
		}
		options := scheduler.ServeOptions{
			StateDir: stateDir, StateDirDefault: values.StateDirDefault,
			UserConfigDir: values.UserConfigDir, Version: build.Version, Commit: build.Commit, Environment: os.Environ(), Logger: cmd.ErrWriter, ListenAddr: settings.Listen, APITokenFile: settings.APITokenFile,
		}
		if err := scheduler.Serve(ctx, options); err != nil {
			var running *scheduler.SchedulerRunningError
			if errors.As(err, &running) {
				return &Error{Code: "scheduler_running", Message: "scheduler_running: " + running.Error()}
			}
			var newer *store.StateTooNewError
			if errors.As(err, &newer) {
				fmt.Fprintf(cmd.ErrWriter, "noon: %s\nHint: Upgrade Noon before using this state database.\n", newer.Error())
				return runExit(75)
			}
			return &Error{Code: "error", Message: strings.TrimSpace(err.Error())}
		}
		return nil
	}
}
