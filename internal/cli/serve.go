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

func serveAction(version string) func(context.Context, *urfave.Command) error {
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
		listenAddr := ""
		if cmd.IsSet("listen") {
			listenAddr = cmd.String("listen")
			if strings.TrimSpace(listenAddr) == "" {
				return usageError("--listen must not be empty", "Use --listen ADDR to start a signed webhook listener")
			}
		}
		apiTokenFile := ""
		if cmd.IsSet("api-token-file") {
			apiTokenFile = cmd.String("api-token-file")
			if strings.TrimSpace(apiTokenFile) == "" {
				return usageError("--api-token-file must not be empty", "Pass a file containing a bearer token")
			}
			if listenAddr == "" {
				return usageError("--api-token-file requires --listen", "Pass --listen ADDR to start the HTTP listener")
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
		stateDir, err := config.StateDirectory(values)
		if err != nil {
			return configError(err)
		}
		options := scheduler.ServeOptions{
			StateDir: stateDir, StateDirDefault: values.StateDirDefault,
			UserConfigDir: values.UserConfigDir, Version: version, Environment: os.Environ(), Logger: cmd.ErrWriter, ListenAddr: listenAddr, APITokenFile: apiTokenFile,
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
