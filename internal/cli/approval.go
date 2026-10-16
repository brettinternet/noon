package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	urfave "github.com/urfave/cli/v3"
)

func enableProject(_ context.Context, cmd *urfave.Command) error {
	cfg, err := load(cmd)
	if err != nil {
		return err
	}
	project, approved, err := app.Enable(cfg, time.Now())
	if err != nil {
		return stateError(err)
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"project": project, "approved": approved})
	}
	fmt.Fprintf(cmd.Writer, "Enabled %s · %d jobs · approved %d shared jobs\n", project.Path, len(cfg.Jobs), len(approved))
	fmt.Fprintln(cmd.Writer, "Nothing runs in the background until a scheduler serves this project.")
	return nil
}

func disableProject(_ context.Context, cmd *urfave.Command) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	cfg, err := config.LoadForDisable(values)
	if err != nil {
		return configError(err)
	}
	project, err := app.Disable(cfg)
	if err != nil {
		return stateError(err)
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"project": project})
	}
	fmt.Fprintf(cmd.Writer, "Disabled %s · history kept\n", project.Path)
	return nil
}
