package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	urfave "github.com/urfave/cli/v3"
)

func historyRuns(_ context.Context, cmd *urfave.Command) error {
	if cmd.Args().Len() > 1 {
		return usageError("history accepts at most one job name", "Run noon history [NAME]")
	}
	limit := cmd.Int("limit")
	values, err := input(cmd)
	if err != nil {
		return err
	}
	job := ""
	var jobFilter *string
	if cmd.Args().Len() == 1 {
		job = cmd.Args().First()
		jobFilter = &job
	}
	history, err := app.ListRuns(values, jobFilter, limit)
	if err != nil {
		return err
	}
	runs := history.Runs
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"runs": runs})
	}
	if len(runs) == 0 {
		if job != "" {
			fmt.Fprintf(cmd.Writer, "No runs of %s yet. Next: noon run %s\n", job, job)
		} else {
			fmt.Fprintln(cmd.Writer, "No runs yet.")
		}
		return nil
	}
	printHistory(cmd.Writer, runs, time.Now())
	return nil
}

func printHistory(out io.Writer, runs []app.Run, now time.Time) {
	rows := make([][7]string, 1, len(runs)+1)
	rows[0] = [7]string{"RUN", "JOB", "STATUS", "TRIGGER", "STARTED", "DURATION", "EXIT"}
	widths := [7]int{3, 3, 6, 7, 7, 8, 4}
	for _, run := range runs {
		exit := "—"
		if run.ExitCode != nil {
			exit = strconv.Itoa(*run.ExitCode)
		}
		row := [7]string{
			strconv.FormatInt(run.ID, 10), run.Job, run.Status, run.Trigger,
			app.RelativeTime(run.StartedAt, now), app.DurationText(run.DurationMS), exit,
		}
		rows = append(rows, row)
		for i, value := range row {
			if n := len([]rune(value)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	for i, maxWidth := range [7]int{8, 18, 14, 12, 12, 12, 8} {
		widths[i] = min(widths[i], maxWidth)
	}
	for _, row := range rows {
		fmt.Fprintf(out, "%-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %s\n",
			widths[0], cell(row[0], widths[0]), widths[1], cell(row[1], widths[1]),
			widths[2], cell(row[2], widths[2]), widths[3], cell(row[3], widths[3]),
			widths[4], cell(row[4], widths[4]), widths[5], cell(row[5], widths[5]), cell(row[6], widths[6]))
	}
}

func runLogs(ctx context.Context, cmd *urfave.Command) error {
	if cmd.Args().Len() > 1 {
		return usageError("logs accepts at most one job name or run ID", "Run noon logs [NAME|RUN]")
	}
	follow := cmd.Bool("follow")
	if follow && cmd.Bool("json") {
		return usageError("logs --follow cannot be used with --json", "Run noon logs without --follow to get JSON")
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(values)
	if err != nil {
		return configError(err)
	}
	selector := ""
	if cmd.Args().Len() == 1 {
		selector = cmd.Args().First()
	}
	var run app.Run
	if selector != "" {
		if id, parseErr := strconv.ParseInt(selector, 10, 64); parseErr == nil {
			run, err = app.RunByID(cfg.StateDir, id)
			if app.NoDatabase(err) {
				return noRuns(cmd, "")
			}
			if app.IsMissingRun(err) {
				return app.RunNotFoundError(id)
			}
			if err != nil {
				return stateError(err)
			}
		} else {
			if _, ok := cfg.Jobs[selector]; !ok {
				return app.MissingJobError(selector, "logs", cfg.Jobs)
			}
			runs, runsErr := app.Runs(cfg.StateDir, cfg.Project, selector, 1)
			if runsErr != nil {
				return stateError(runsErr)
			}
			if len(runs) == 0 {
				return noRuns(cmd, selector)
			}
			run = runs[0]
		}
	} else {
		runs, runsErr := app.Runs(cfg.StateDir, cfg.Project, "", 1)
		if runsErr != nil {
			return stateError(runsErr)
		}
		if len(runs) == 0 {
			return noRuns(cmd, "")
		}
		run = runs[0]
	}
	if follow {
		if err := app.FollowLog(ctx, cfg.StateDir, run.ID, cmd.Writer); err != nil {
			return logReadError(run.ID, err)
		}
		return nil
	}
	if !run.Log.Available {
		return prunedLogError(run.ID)
	}
	_, data, err := app.ReadRunLog(cfg.StateDir, run.ID)
	if err != nil {
		return logReadError(run.ID, err)
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"run": run, "log": app.ValidLogText(data)})
	}
	_, err = cmd.Writer.Write(data)
	return err
}

func noRuns(cmd *urfave.Command, job string) error {
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"run": nil, "log": ""})
	}
	if job != "" {
		fmt.Fprintf(cmd.Writer, "No runs of %s yet. Next: noon run %s\n", job, job)
	} else {
		fmt.Fprintln(cmd.Writer, "No runs yet.")
	}
	return nil
}

func logReadError(id int64, err error) error {
	if app.IsMissingLog(err) {
		return prunedLogError(id)
	}
	return stateError(err)
}

func prunedLogError(id int64) error { return app.MissingRunLogError(id) }
