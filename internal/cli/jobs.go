package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/brettinternet/noon/internal/app"
	urfave "github.com/urfave/cli/v3"
)

func listJobs(_ context.Context, cmd *urfave.Command) error { return inspectJobs(cmd) }

func inspectJobs(cmd *urfave.Command) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	now := time.Now()
	view, err := app.ListJobs(values, now)
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"project": view.Project, "jobs": view.Jobs})
	}
	fmt.Fprintf(cmd.Writer, "Scheduler %s\n", view.Project.Scheduler)
	if view.Project.ConfigError != "" {
		fmt.Fprintf(cmd.Writer, "Config error: %s\n", cell(view.Project.ConfigError, 240))
	}
	if len(view.Jobs) == 0 {
		fmt.Fprintln(cmd.Writer, guidance(cmd, `No jobs. Add one with: noon add tests "weekdays at 9am" -- task test`))
	} else {
		printJobs(cmd.Writer, view.Jobs, now)
	}
	changed, scheduled := 0, false
	for _, job := range view.Jobs {
		if job.Approval == "needs_approval" {
			changed++
		}
		if job.Schedule != nil {
			scheduled = true
		}
	}
	switch {
	case view.Project.Enabled && changed > 0:
		fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("%d shared jobs changed since you enabled this project. Next: noon enable\n", changed)))
	case !view.Project.Enabled && scheduled:
		fmt.Fprintln(cmd.Writer, guidance(cmd, "Not enabled for background runs. Next: noon enable"))
	}
	return nil
}

func printJobs(out io.Writer, jobs []app.Job, now time.Time) {
	rows := [][5]string{{"JOB", "SCHEDULE", "NEXT", "LAST", "COMMAND"}}
	widths := [4]int{3, 8, 4, 4}
	overrides := 0
	for _, job := range jobs {
		scheduled, next := "manual", "-"
		if job.Schedule != nil {
			scheduled = *job.Schedule
		}
		if job.Paused {
			next = "paused"
		} else if job.Approval == "needs_approval" {
			next = "needs approval"
		} else if !job.Enabled {
			next = "disabled"
		} else if job.NextAt != nil {
			until := job.NextAt.Sub(now)
			unit := time.Minute
			suffix := "m"
			if until >= time.Hour {
				unit, suffix = time.Hour, "h"
			}
			amount := int64(until / unit)
			if amount < 1 {
				amount = 1
			}
			next = fmt.Sprintf("%s (in %d%s)", job.NextAt.Format("Mon 15:04"), amount, suffix)
		}
		command := ""
		if job.Prompt != nil || job.PromptFile != nil {
			command = promptDescription(job.Agent, job.Prompt, job.PromptFile)
		} else {
			switch run := job.Run.(type) {
			case string:
				command = run
			case []string:
				quoted := make([]string, len(run))
				for i, arg := range run {
					quoted[i] = strconv.Quote(arg)
				}
				command = strings.Join(quoted, " ")
			}
		}
		row := [5]string{job.Name, scheduled, next, lastRunCell(job.LastRun, now), command}
		rows = append(rows, row)
		for i := range widths {
			if n := len([]rune(row[i])); n > widths[i] {
				widths[i] = n
			}
		}
		if job.Overrides {
			overrides++
		}
	}
	// Bound earlier columns as well, leaving room for a command on an 80-column terminal.
	for i, max := range [4]int{16, 20, 18, 24} {
		if widths[i] > max {
			widths[i] = max
		}
	}
	commandWidth := max(12, 80-widths[0]-widths[1]-widths[2]-widths[3]-8)
	for _, row := range rows {
		fmt.Fprintf(out, "%-*s  %-*s  %-*s  %-*s  %s\n", widths[0], cell(row[0], widths[0]), widths[1], cell(row[1], widths[1]), widths[2], cell(row[2], widths[2]), widths[3], cell(row[3], widths[3]), cell(row[4], commandWidth))
	}
	fmt.Fprintf(out, "%d jobs · %d overrides\n", len(jobs), overrides)
}

func lastRunCell(run *app.Run, now time.Time) string {
	if run == nil {
		return "never"
	}
	relative := app.RelativeTime(run.StartedAt, now)
	if run.Status == "running" {
		if run.StartedAt == nil {
			return "running"
		}
		started, err := time.Parse(time.RFC3339Nano, *run.StartedAt)
		if err != nil {
			return "running"
		}
		elapsed := max(time.Duration(0), now.Sub(started))
		if elapsed < time.Minute {
			return fmt.Sprintf("running %ds", max(0, int(elapsed.Round(time.Second)/time.Second)))
		}
		if elapsed < time.Hour {
			return fmt.Sprintf("running %dm", int(elapsed/time.Minute))
		}
		if elapsed < 24*time.Hour {
			return fmt.Sprintf("running %dh", int(elapsed/time.Hour))
		}
		return fmt.Sprintf("running %dd", int(elapsed/(24*time.Hour)))
	}
	if run.Status == "succeeded" {
		return "✓ " + relative + " (" + app.DurationText(run.DurationMS) + ")"
	}
	if run.ExitCode != nil {
		return fmt.Sprintf("✗ exit %d · %s", *run.ExitCode, relative)
	}
	return "✗ " + run.Status + " · " + relative
}

// Render control and non-ASCII characters as escapes so terminal controls and
// wide glyphs cannot defeat the table's column budget.
func promptDescription(agent string, prompt, promptFile *string) string {
	if promptFile != nil {
		return agent + ": " + *promptFile
	}
	firstLine, _, _ := strings.Cut(*prompt, "\n")
	return agent + ": " + firstLine + "…"
}

func cell(text string, width int) string {
	var b strings.Builder
	for _, r := range text {
		if (r >= 32 && r < 127 || r == '…' || r == '—' || r == '✓' || r == '✗' || r == '·') && !unicode.IsControl(r) {
			b.WriteRune(r)
		} else {
			b.WriteString(strconv.QuoteRuneToASCII(r)[1 : len(strconv.QuoteRuneToASCII(r))-1])
		}
	}
	runes := []rune(b.String())
	if len(runes) > width {
		if width <= 3 {
			return strings.Repeat(".", max(0, width))
		}
		return string(runes[:width-3]) + "..."
	}
	return string(runes)
}
