package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/schedule"
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
	if modes > 1 {
		return "", usageError("--detach, --in, and --at are mutually exclusive", "Choose one queued run time")
	}
	return mode, nil
}

func queueDueAt(mode, inValue, atValue string, now time.Time, location *time.Location) (time.Time, error) {
	switch mode {
	case "detach":
		return now, nil
	case "in":
		duration, err := schedule.ParseDuration(inValue)
		if err != nil {
			return time.Time{}, err
		}
		if duration < time.Second {
			return time.Time{}, fmt.Errorf("--in must be at least 1s")
		}
		return now.Add(duration), nil
	case "at":
		value := atValue
		var due time.Time
		switch {
		case len(value) == len("15:04"):
			clock, err := time.ParseInLocation("15:04", value, location)
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid --at time %q: use HH:MM, YYYY-MM-DD HH:MM, or RFC 3339", value)
			}
			local := now.In(location)
			due = time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
			if !due.After(now) {
				due = time.Date(local.Year(), local.Month(), local.Day()+1, clock.Hour(), clock.Minute(), 0, 0, location)
			}
		case len(value) == len("2006-01-02 15:04"):
			parsed, err := time.ParseInLocation("2006-01-02 15:04", value, location)
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid --at time %q: use HH:MM, YYYY-MM-DD HH:MM, or RFC 3339", value)
			}
			due = parsed
		default:
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid --at time %q: use HH:MM, YYYY-MM-DD HH:MM, or RFC 3339", value)
			}
			due = parsed
		}
		if due.Before(now) {
			return time.Time{}, fmt.Errorf("--at time %s is in the past", value)
		}
		return due, nil
	default:
		return time.Time{}, fmt.Errorf("missing queued run time")
	}
}

func queueError(err error) error {
	var failure *app.QueueError
	if errors.As(err, &failure) {
		return &Error{Code: failure.Code, Message: failure.Message, Hint: failure.Hint, Usage: failure.Code == "usage"}
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
