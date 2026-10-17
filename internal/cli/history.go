package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
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
	statuses := cmd.StringSlice("status")
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
	history, err := app.ListRuns(values, jobFilter, statuses, limit)
	if err != nil {
		return err
	}
	runs := history.Runs
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"runs": runs})
	}
	style := styleFor(cmd, cmd.Writer)
	if len(runs) == 0 {
		if len(statuses) > 0 {
			if job != "" {
				fmt.Fprintf(cmd.Writer, "No runs of %s match --status %s.\n", job, strings.Join(statuses, ","))
			} else {
				fmt.Fprintf(cmd.Writer, "No runs match --status %s.\n", strings.Join(statuses, ","))
			}
		} else if job != "" {
			fmt.Fprintf(cmd.Writer, "No runs of %s yet. Next: %s\n", cell(job, -1), style.render(humanText(guidance(cmd, "noon run")+" "+shellArgument(job)), "heading"))
		} else {
			fmt.Fprintln(cmd.Writer, "No runs yet. Next: "+style.render(humanText(guidance(cmd, "noon run")), "heading")+" (list jobs to run)")
		}
		return nil
	}
	style.printHistory(cmd.Writer, runs, time.Now())
	return nil
}

func (s humanStyle) printHistory(out io.Writer, runs []app.Run, now time.Time) {
	rows := make([][]tableCell, 1, len(runs)+1)
	rows[0] = []tableCell{{"RUN", "heading"}, {"JOB", "heading"}, {"STATUS", "heading"}, {"TRIGGER", "heading"}, {"STARTED", "heading"}, {"DURATION", "heading"}, {"EXIT", "heading"}}
	for _, run := range runs {
		exit := "—"
		if run.ExitCode != nil {
			exit = strconv.Itoa(*run.ExitCode)
		}
		row := []tableCell{
			{strconv.FormatInt(run.ID, 10), ""}, {run.Job, "heading"}, {run.Status, run.Status}, {run.Trigger, ""},
			{app.RelativeTime(run.StartedAt, now), ""}, {app.DurationText(run.DurationMS), ""}, {exit, ""},
		}
		rows = append(rows, row)
	}
	s.printBoundedRows(out, rows, "  ", outputWidth(out), 0, 1)
}

func runLogs(ctx context.Context, cmd *urfave.Command) error {
	if cmd.Args().Len() > 1 {
		return usageError("logs accepts at most one job name or run ID", "Run noon logs [NAME|RUN]")
	}
	follow := cmd.Bool("follow")
	pathOnly := cmd.Bool("path")
	statuses := cmd.StringSlice("status")
	statusSet := cmd.IsSet("status")
	if err := app.ValidateRunStatuses(statuses); err != nil {
		return err
	}
	sectionSet := cmd.IsSet("section")
	tailSet := cmd.IsSet("tail")
	afterSet := cmd.IsSet("after-cursor")
	matchSet := cmd.IsSet("match")
	contextSet := cmd.IsSet("context")
	limitSet := cmd.IsSet("limit-bytes")
	hasSelection := sectionSet || tailSet || afterSet || matchSet || contextSet || limitSet
	if pathOnly && (follow || cmd.Bool("json")) {
		return usageError("logs --path cannot be used with --json or --follow", "Run noon logs [NAME|RUN] --json and read .run.log.path")
	}
	if pathOnly && hasSelection {
		return usageError("logs --path cannot be used with log selection flags", "Read the selected log with noon logs [NAME|RUN]")
	}
	if follow && cmd.Bool("json") {
		return usageError("logs --follow cannot be used with --json", "Run noon logs without --follow to get JSON")
	}
	if follow && sectionSet {
		return usageError("logs --section cannot be used with --follow", "Read a section after the run finishes")
	}
	if follow && (matchSet || contextSet || limitSet) {
		return usageError("logs --follow cannot be used with --match, --context, or --limit-bytes", "Use --tail or --after-cursor to choose where following starts")
	}
	var query app.LogQuery
	query.Section = cmd.String("section")
	if sectionSet && query.Section == "" {
		return usageError("section must be all, job, or hook", "")
	}
	if tailSet {
		value := cmd.Int("tail")
		query.Tail = &value
	}
	if afterSet {
		value := cmd.Int64("after-cursor")
		query.After = &value
	}
	if matchSet {
		value := cmd.String("match")
		query.Match = &value
	}
	if contextSet {
		value := cmd.Int("context")
		query.Context = &value
	}
	if limitSet {
		value := cmd.Int("limit-bytes")
		query.LimitBytes = &value
	}
	query.RepairUTF8 = cmd.Bool("json")
	query.IncludeActivePartial = !hasSelection && !follow
	if err := app.ValidateLogQuery(query); err != nil {
		return err
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
			if statusSet {
				return usageError("logs --status cannot be combined with a run ID", "Run noon logs [NAME] --status STATUS or select a numeric RUN without --status")
			}
			run, err = app.RunByID(cfg.StateDir, id)
			if app.NoDatabase(err) {
				return noRuns(cmd, "", statuses)
			}
			if app.IsMissingRun(err) {
				return app.RunNotFoundError(id)
			}
			if err != nil {
				return stateError(err)
			}
		} else {
			var found bool
			run, found, err = app.LatestRun(cfg.StateDir, cfg.Project, selector, statuses)
			if err != nil {
				return stateError(err)
			}
			if !found {
				if _, configured := cfg.Jobs[selector]; !configured {
					retained, historyErr := app.Runs(cfg.StateDir, cfg.Project, selector, 1)
					if historyErr != nil {
						return stateError(historyErr)
					}
					if len(retained) == 0 {
						return app.MissingJobError(selector, "logs", cfg.Jobs)
					}
				}
				return noRuns(cmd, selector, statuses)
			}
		}
	} else {
		var found bool
		run, found, err = app.LatestRun(cfg.StateDir, cfg.Project, "", statuses)
		if err != nil {
			return stateError(err)
		}
		if !found {
			return noRuns(cmd, "", statuses)
		}
	}
	if pathOnly {
		if !run.Log.Available || run.Log.Path == nil {
			return app.MissingRunLogError(run)
		}
		if _, _, err := app.ReadRunLog(cfg.StateDir, run.ID); err != nil {
			return logReadError(run, err)
		}
		_, err := fmt.Fprintln(cmd.Writer, *run.Log.Path)
		return err
	}
	if follow {
		if err := app.FollowLog(ctx, cfg.StateDir, run.ID, query.After, query.Tail, cmd.Writer, cmd.ErrWriter); err != nil {
			return logReadError(run, err)
		}
		return nil
	}
	run, selection, err := app.ReadRunLogSelection(cfg.StateDir, run.ID, query)
	if err != nil {
		return logReadError(run, err)
	}
	if !run.Log.Available {
		return app.MissingRunLogError(run)
	}
	resume := guidance(cmd, "noon logs") + fmt.Sprintf(" %d", run.ID)
	if sectionSet {
		resume += " --section " + shellArgument(query.Section)
	}
	resume += fmt.Sprintf(" --after-cursor %d", selection.Next)
	for _, name := range []string{"match", "context", "limit-bytes"} {
		if cmd.IsSet(name) {
			resume += " --" + name + " " + shellArgument(fmt.Sprint(cmd.Value(name)))
		}
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{
			"run": run, "log": app.ValidLogText(selection.Bytes), "next": selection.Next, "resume": resume,
			"size": selection.Size, "truncated": selection.Truncated, "matched_lines": selection.MatchedLines,
		})
	}
	if _, err := cmd.Writer.Write(selection.Bytes); err != nil {
		return err
	}
	if hasSelection {
		_, err = fmt.Fprintln(cmd.ErrWriter, "next: "+resume)
	}
	return err
}

func noRuns(cmd *urfave.Command, job string, statuses []string) error {
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"run": nil, "log": ""})
	}
	if len(statuses) > 0 {
		if job != "" {
			fmt.Fprintf(cmd.Writer, "No runs of %s match --status %s.\n", job, strings.Join(statuses, ","))
		} else {
			fmt.Fprintf(cmd.Writer, "No runs match --status %s.\n", strings.Join(statuses, ","))
		}
	} else if job != "" {
		fmt.Fprintf(cmd.Writer, "No runs of %s yet. Next: %s %s\n", job, guidance(cmd, "noon run"), shellArgument(job))
	} else {
		fmt.Fprintln(cmd.Writer, guidance(cmd, "No runs yet. Next: noon run (list jobs to run)"))
	}
	return nil
}

func logReadError(run app.Run, err error) error {
	if failure, ok := err.(*app.Error); ok {
		return failure
	}
	if app.IsMissingLog(err) {
		return app.MissingRunLogError(run)
	}
	return stateError(err)
}
