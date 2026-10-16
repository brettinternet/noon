package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func cancelRuns(_ context.Context, cmd *urfave.Command) error {
	if cmd.Args().Len() > 1 {
		return usageError("cancel accepts one run ID or job name", "Run noon cancel [RUN|NAME]")
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	cfg, err := config.LoadForDisable(values)
	if err != nil {
		return configError(err)
	}
	if cmd.Args().Len() == 0 {
		runs, err := app.ActiveRuns(cfg.StateDir, cfg.Project)
		if err != nil {
			return stateError(err)
		}
		if cmd.Bool("json") {
			return emit(cmd, map[string]any{"runs": runs})
		}
		if len(runs) == 0 {
			fmt.Fprintln(cmd.Writer, "No active runs.")
			return nil
		}
		for _, run := range runs {
			fmt.Fprintf(cmd.Writer, "run %d · %s · %s", run.ID, run.Job, run.Status)
			if run.Status == "queued" {
				fmt.Fprintf(cmd.Writer, " · %s", queuedRunDue(run.ScheduledAt, time.Now(), cfg.Timezone))
			}
			fmt.Fprintln(cmd.Writer)
		}
		fmt.Fprintf(cmd.Writer, "Next: noon cancel %d\n", runs[0].ID)
		return nil
	}

	selector := cmd.Args().First()
	if id, parseErr := strconv.ParseInt(selector, 10, 64); parseErr == nil {
		result, err := app.Cancel(cfg.StateDir, cfg.Project, &id, "", time.Now())
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, store.ErrNoDatabase) {
			return app.RunNotFoundError(id)
		}
		if err != nil {
			return stateError(err)
		}
		return renderCancellation(cmd, result)
	}
	result, err := app.Cancel(cfg.StateDir, cfg.Project, nil, selector, time.Now())
	if errors.Is(err, store.ErrNoDatabase) {
		result = app.CancelResult{Runs: []app.Run{}}
	} else if err != nil {
		return stateError(err)
	}
	return renderCancellation(cmd, result)
}

func renderCancellation(cmd *urfave.Command, result app.CancelResult) error {
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"runs": result.Runs, "pending": result.Pending})
	}
	if len(result.Runs) == 0 {
		fmt.Fprintln(cmd.Writer, "No active runs to cancel.")
		return nil
	}
	if result.AlreadyFinished && len(result.Runs) == 1 {
		fmt.Fprintf(cmd.Writer, "Run %d is already %s.\n", result.Runs[0].ID, result.Runs[0].Status)
		return nil
	}
	for _, run := range result.Runs {
		switch {
		case result.Pending && run.Status == "running":
			fmt.Fprintf(cmd.Writer, "Cancellation request recorded for run %d; it is still running.\n", run.ID)
		case run.Status == "cancelled" && run.StartedAt == nil:
			fmt.Fprintf(cmd.Writer, "Cancelled queued run %d.\n", run.ID)
		case run.Status == "cancelled":
			fmt.Fprintf(cmd.Writer, "Cancelled run %d.\n", run.ID)
		default:
			fmt.Fprintf(cmd.Writer, "Run %d is already %s.\n", run.ID, run.Status)
		}
	}
	return nil
}

func queuedRunDue(scheduledAt *string, now time.Time, timezone string) string {
	if scheduledAt == nil {
		return "due now"
	}
	due, err := time.Parse(store.TimestampLayout, *scheduledAt)
	if err != nil || !due.After(now) {
		return "due now"
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		location = time.Local
	}
	return "due " + due.In(location).Format("15:04")
}
