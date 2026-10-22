package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/brettinternet/noon/internal/config"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func runQueueMode(cmd *urfave.Command) (string, error) {
	modes := 0
	mode := ""
	if cmd.Bool("detach") {
		modes++
		mode = "detach"
	}
	if cmd.IsSet("in") {
		modes++
		mode = "in"
	}
	if cmd.IsSet("at") {
		modes++
		mode = "at"
	}
	if cmd.IsSet("schedule") {
		if modes > 0 {
			return "", usageError("--schedule cannot be combined with --detach, --in, or --at", "Choose a temporary schedule or a queued run")
		}
		mode = "schedule"
	} else if cmd.IsSet("for") || cmd.IsSet("until") {
		return "", usageError("--for and --until require --schedule", "Use noon run NAME --schedule 'every 10m' --for 2h")
	}
	if cmd.IsSet("for") && cmd.IsSet("until") {
		return "", usageError("--for and --until are mutually exclusive", "Choose one end time")
	}
	if modes > 1 {
		return "", usageError("--detach, --in, and --at are mutually exclusive", "Choose one queued run time")
	}
	return mode, nil
}

func queueDueAt(mode, inValue, atValue string, now time.Time, location *time.Location) (time.Time, error) {
	return app.QueueDueAt(mode, inValue, atValue, now, location)
}

func queueError(err error) error {
	var failure *app.QueueError
	if errors.As(err, &failure) {
		return &Error{Code: failure.Code, Message: failure.Message, Hint: failure.Hint, Usage: failure.Code == "usage"}
	}
	var operationError *app.Error
	if errors.As(err, &operationError) {
		return operationError
	}
	return stateError(err)
}

func printQueuedRun(cmd *urfave.Command, queued app.QueuedRun, now time.Time, location *time.Location, dueNow bool) {
	run := queued.Run
	switch {
	case queued.Created:
		due := queueDueDescription(run.ScheduledAt, now, location)
		if dueNow {
			due = "due now"
		}
		fmt.Fprintf(cmd.Writer, "Queued %s as run %d · %s\n", run.Job, run.ID, due)
	case queued.Reason == "queued":
		fmt.Fprintf(cmd.Writer, "%s is already queued as run %d · due now\n", run.Job, run.ID)
	case queued.Reason == "cooldown":
		until := queued.CooldownUntil
		if parsed, err := time.Parse(store.TimestampLayout, until); err == nil {
			until = parsed.In(location).Format("Mon 15:04:05")
		}
		fmt.Fprintf(cmd.Writer, "%s is cooling down until %s (run %d)\n", run.Job, until, run.ID)
	default:
		at := run.FinishedAt
		if at == nil {
			at = run.StartedAt
		}
		if at == nil {
			at = &run.CreatedAt
		}
		fmt.Fprintf(cmd.Writer, "Already requested as run %d (%s %s)\n", run.ID, run.Status, app.RelativeTime(at, now))
	}
	if run.Status == "queued" || run.Status == "running" {
		fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("Follow it: noon logs %d --follow\n", run.ID)))
	} else {
		fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("Read it: noon logs %d\n", run.ID)))
	}
}

// queueJobs validates all names before issuing independent durable requests.
// Eligibility is rechecked by QueueRun; failures do not undo earlier requests.
func queueJobs(cmd *urfave.Command, cfg config.Config, names []string, eventData []byte, dueAt, now time.Time, location *time.Location, dueNow bool) error {
	for _, name := range names {
		job, ok := cfg.Jobs[name]
		if !ok {
			return app.MissingJobError(name, "run", cfg.Jobs)
		}
		if !job.MatchesPlatform(cfg.Platform) {
			return app.UnsupportedPlatformError(job, cfg.Platform)
		}
		if err := app.CheckRecursion(cfg.Project, name, os.Environ()); err != nil {
			return err
		}
	}
	results := []app.QueuedRun{}
	failures := []targetFailure{}
	offline := false
	for _, name := range names {
		result, err := app.QueueRun(cfg, name, cmd.String("key"), dueAt, now, time.Now, dueNow, "cli", app.QueueOptions{EventData: eventData, EventSource: "cli"})
		if err != nil {
			failures = append(failures, targetFailure{Target: name, Error: queueError(err).Error()})
			continue
		}
		results = append(results, result)
		offline = offline || result.Scheduler == "offline"
		if !cmd.Bool("json") {
			printQueuedRun(cmd, result, now, location, dueNow)
		}
	}
	if offline && !cmd.Bool("json") {
		fmt.Fprintln(cmd.Writer, "The scheduler isn't running, so nothing runs until it starts.")
		hint, err := serviceGuidance(cfg.StateDir)
		if err != nil {
			failures = append(failures, targetFailure{Target: "scheduler", Error: err.Error()})
		} else {
			fmt.Fprintln(cmd.Writer, guidance(cmd, hint))
		}
	}
	return finishBatch(cmd, map[string]any{"results": results}, failures)
}

func temporaryJobs(cmd *urfave.Command, cfg config.Config, names []string, data []byte, now time.Time, loc *time.Location) error {
	// Syntax errors must be usage errors even for multi-target requests.
	if _, _, err := app.TemporaryTimes(cmd.String("schedule"), cmd.String("for"), cmd.String("until"), now, loc); err != nil {
		return err
	}
	for _, name := range names {
		job, ok := cfg.Jobs[name]
		if !ok {
			return app.MissingJobError(name, "run", cfg.Jobs)
		}
		if job.Temporary != nil {
			return usageError("temporary jobs cannot have temporary schedules", "Remove and add the temporary job again")
		}
	}
	results := []app.TemporaryResult{}
	failures := []targetFailure{}
	for _, name := range names {
		result, err := app.ScheduleTemporary(cfg, name, cmd.String("schedule"), cmd.String("for"), cmd.String("until"), cmd.String("key"), "cli", "cli", data, now)
		if err != nil {
			if len(names) == 1 {
				return queueError(err)
			}
			failures = append(failures, targetFailure{Target: name, Error: queueError(err).Error()})
			continue
		}
		results = append(results, result)
		if cmd.Bool("json") && len(names) == 1 {
			output := map[string]any{"temporary_schedule": result.TemporarySchedule, "created": result.Created, "scheduler": result.Scheduler}
			if result.Reason != "" {
				output["reason"] = result.Reason
			}
			return emit(cmd, output)
		}
		if !cmd.Bool("json") {
			value := result.TemporarySchedule
			next := store.FormatTime(value.NextAt)
			fmt.Fprintf(cmd.Writer, "Scheduled %s %s until %s · next %s (%s)\n", name, cell(value.Schedule, -1), value.Until.In(loc).Format("Mon 15:04"), value.NextAt.In(loc).Format("Mon 15:04"), app.RelativeTime(&next, now))
		}
	}
	if !cmd.Bool("json") && len(results) > 0 && results[0].Scheduler == "offline" {
		fmt.Fprintln(cmd.Writer, "The scheduler isn't running, so nothing runs until it starts.")
		hint, err := serviceGuidance(cfg.StateDir)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.Writer, guidance(cmd, hint))
	}
	if len(names) == 1 {
		return nil
	}
	return finishBatch(cmd, map[string]any{"results": results}, failures)
}

func queueDueDescription(value *string, now time.Time, location *time.Location) string {
	if value == nil {
		return "due now"
	}
	due, err := time.Parse(store.TimestampLayout, *value)
	if err != nil || !due.After(now) {
		return "due now"
	}
	return fmt.Sprintf("due %s (%s)", due.In(location).Format("Mon 15:04"), app.RelativeTime(value, now))
}
