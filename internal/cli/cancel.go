package cli

import (
	"context"
	"fmt"
	"slices"
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
			fmt.Fprintln(cmd.Writer, guidance(cmd, "No active runs. Suggested action: noon run (list jobs to run)"))
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
		fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("Suggested action: noon cancel %d\n", runs[0].ID)))
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
	jobs := []string{}
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
		jobs = append(jobs, selector)
		found := false
		for _, run := range active {
			if run.Job == selector {
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
				_, ok := configured.Jobs[selector]
				if !ok {
					// Name-only lookup still ends a temporary job that no longer resolves.
					temporary, err := app.TemporaryJobs(configured, time.Now())
					if err != nil {
						return stateError(err)
					}
					ok = slices.ContainsFunc(temporary, func(value store.TemporaryJob) bool { return value.Name == selector })
				}
				if !ok {
					return app.MissingJobError(selector, "cancel", configured.Jobs)
				}
			}
		}
	}
	result := app.CancelResult{Runs: []app.Run{}}
	failures := []targetFailure{}
	selectors := make([]app.CancelJob, 0, len(jobs))
	for _, job := range jobs {
		selectors = append(selectors, app.CancelJob{Project: cfg.Project, Job: job})
	}
	outcomes, err := app.CancelRuns(cfg.StateDir, ids, time.Now(), selectors...)
	if err != nil {
		return stateError(err)
	}
	for _, outcome := range outcomes {
		if outcome.Err != nil {
			target := outcome.Target
			if target == "" {
				target = strconv.FormatInt(outcome.ID, 10)
			}
			failures = append(failures, targetFailure{Target: target, Error: outcome.Err.Error()})
			continue
		}
		result.Runs = append(result.Runs, outcome.Result.Runs...)
		result.Pending = result.Pending || outcome.Result.Pending
		result.EndedTemporaryJobs = append(result.EndedTemporaryJobs, outcome.Result.EndedTemporaryJobs...)
		result.EndedTemporarySchedules = append(result.EndedTemporarySchedules, outcome.Result.EndedTemporarySchedules...)
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
	return finishBatch(cmd, cancellationJSON(result), failures)
}

func cancellationJSON(result app.CancelResult) map[string]any {
	output := map[string]any{"runs": result.Runs, "pending": result.Pending}
	if len(result.EndedTemporaryJobs) > 0 {
		output["ended_temporary_jobs"] = result.EndedTemporaryJobs
	}
	if len(result.EndedTemporarySchedules) > 0 {
		output["ended_temporary_schedules"] = result.EndedTemporarySchedules
	}
	return output
}

func renderCancellation(cmd *urfave.Command, result app.CancelResult) error {
	if cmd.Bool("json") {
		return emit(cmd, cancellationJSON(result))
	}
	for _, job := range result.EndedTemporaryJobs {
		fmt.Fprintf(cmd.Writer, "Ended temporary job %s.\n", job)
	}
	for _, job := range result.EndedTemporarySchedules {
		fmt.Fprintf(cmd.Writer, "Ended temporary schedule for %s.\n", job)
	}
	if len(result.Runs) == 0 && (len(result.EndedTemporarySchedules) > 0 || len(result.EndedTemporaryJobs) > 0) {
		return nil
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
