package cli

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func cancelRuns(_ context.Context, cmd *urfave.Command) error {
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
			fmt.Fprintln(cmd.Writer, guidance(cmd, "No active runs. Next: noon run (list jobs to run)"))
			return nil
		}
		style := styleFor(cmd, cmd.Writer)
		for _, run := range runs {
			status := runDisplayStatus(run)
			fmt.Fprintf(cmd.Writer, "run %d · %s · %s", run.ID, run.Job, style.render(status, status))
			if run.Status == "queued" {
				fmt.Fprintf(cmd.Writer, " · %s", queuedRunDue(run.ScheduledAt, time.Now(), cfg.Timezone))
			}
			fmt.Fprintln(cmd.Writer)
		}
		fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("Next: noon cancel %d\n", runs[0].ID)))
		return nil
	}

	// Resolve every selector before cancellation; names snapshot current active runs.
	active, err := app.ActiveRuns(cfg.StateDir, cfg.Project)
	if err != nil {
		return stateError(err)
	}
	ids := []int64{}
	seen := map[int64]bool{}
	add := func(id int64) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, selector := range uniqueTargets(cmd.Args().Slice()) {
		if selector == "" {
			return usageError("cancel targets must not be empty", "Use a run ID or job name")
		}
		if id, parseErr := strconv.ParseInt(selector, 10, 64); parseErr == nil {
			if _, err := app.RunByID(cfg.StateDir, id); err != nil {
				if app.IsMissingRun(err) || app.NoDatabase(err) {
					return app.RunNotFoundError(id)
				}
				return stateError(err)
			}
			add(id)
			continue
		}
		found := false
		for _, run := range active {
			if run.Job == selector {
				add(run.ID)
				found = true
			}
		}
		if !found {
			retained, err := app.Runs(cfg.StateDir, cfg.Project, selector, 1)
			if err != nil {
				return stateError(err)
			}
			if len(retained) == 0 {
				configured, err := config.Load(values)
				if err != nil {
					return configError(err)
				}
				if _, ok := configured.Jobs[selector]; !ok {
					return app.MissingJobError(selector, "cancel", configured.Jobs)
				}
			}
		}
	}
	result := app.CancelResult{Runs: []app.Run{}}
	failures := []targetFailure{}
	outcomes, err := app.CancelRuns(cfg.StateDir, ids, time.Now())
	if err != nil {
		return stateError(err)
	}
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			failures = append(failures, targetFailure{Target: strconv.FormatInt(outcome.ID, 10), Error: outcome.Err.Error()})
			continue
		}
		result.Runs = append(result.Runs, outcome.Result.Runs...)
		result.Pending = result.Pending || outcome.Result.Pending
		result.AlreadyFinished = outcome.Result.AlreadyFinished
	}
	if cmd.Args().Len() == 1 && len(failures) == 0 {
		return renderCancellation(cmd, result)
	}
	if !cmd.Bool("json") {
		if err := renderCancellation(cmd, result); err != nil {
			return err
		}
	}
	return finishBatch(cmd, map[string]any{"runs": result.Runs, "pending": result.Pending}, failures)
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
