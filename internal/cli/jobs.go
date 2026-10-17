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
	"github.com/brettinternet/noon/internal/config"
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
	style := styleFor(cmd, cmd.Writer)
	if config.IsUserProject(view.Project.Path, values.UserConfigDir) {
		fmt.Fprintf(cmd.Writer, "user · %s\n", view.Project.Path)
	}
	fmt.Fprintf(cmd.Writer, "Scheduler %s\n", style.render(view.Project.Scheduler, view.Project.Scheduler))
	if view.Project.ConfigError != "" {
		fmt.Fprintf(cmd.Writer, "%s %s\n", style.render("Config error:", "failed"), cell(view.Project.ConfigError, 240))
	}
	if len(view.Jobs) == 0 {
		fmt.Fprintln(cmd.Writer, "No jobs. Add one with: "+style.render(humanText(guidance(cmd, `noon add tests "weekdays at 9am" -- task test`)), "heading"))
	} else {
		style.printJobs(cmd.Writer, view.Jobs, now, cmd.Bool("wide"))
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
		fmt.Fprintf(cmd.Writer, "%d shared jobs changed since you enabled this project. Next: %s\n", changed, style.render(humanText(guidance(cmd, "noon enable")), "heading"))
	case !view.Project.Enabled && scheduled:
		fmt.Fprintln(cmd.Writer, "Not enabled for background runs. Next: "+style.render(humanText(guidance(cmd, "noon enable")), "heading"))
	}
	return nil
}

func (s humanStyle) printJobs(out io.Writer, jobs []app.Job, now time.Time, wide bool) {
	rows := [][]tableCell{{{"JOB", "heading"}, {"SCHEDULE", "heading"}, {"NEXT", "heading"}, {"LAST", "heading"}, {"COMMAND", "heading"}}}
	overrides := 0
	for _, job := range jobs {
		scheduled, next := "manual", "-"
		if job.Schedule != nil {
			scheduled = *job.Schedule
		}
		if !job.PlatformSupported {
			next = "other platform"
		} else if job.Paused {
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
		lastRole := ""
		if job.LastRun != nil {
			lastRole = runDisplayStatus(*job.LastRun)
		}
		rows = append(rows, []tableCell{{job.Name, "heading"}, {scheduled, ""}, {next, next}, {lastRunCell(job.LastRun, now), lastRole}, {command, ""}})
		if job.Overrides {
			overrides++
		}
	}
	width := outputWidth(out)
	if wide {
		width = 0
	}
	s.printBoundedRows(out, rows, "  ", width, 0)
	fmt.Fprintf(out, "%d jobs · %d overrides\n", len(jobs), overrides)
}

func lastRunCell(run *app.Run, now time.Time) string {
	if run == nil {
		return "never"
	}
	relative := app.RelativeTime(run.StartedAt, now)
	if run.Status == "running" {
		status := runDisplayStatus(*run)
		if run.StartedAt == nil {
			return status
		}
		started, err := time.Parse(time.RFC3339Nano, *run.StartedAt)
		if err != nil {
			return status
		}
		elapsed := max(time.Duration(0), now.Sub(started))
		if elapsed < time.Minute {
			return fmt.Sprintf("%s %ds", status, max(0, int(elapsed.Round(time.Second)/time.Second)))
		}
		if elapsed < time.Hour {
			return fmt.Sprintf("%s %dm", status, int(elapsed/time.Minute))
		}
		if elapsed < 24*time.Hour {
			return fmt.Sprintf("%s %dh", status, int(elapsed/time.Hour))
		}
		return fmt.Sprintf("%s %dd", status, int(elapsed/(24*time.Hour)))
	}
	if run.Status == "succeeded" {
		return "✓ " + relative + " (" + app.DurationText(run.DurationMS) + ")"
	}
	if run.ExitCode != nil {
		return fmt.Sprintf("✗ exit %d · %s", *run.ExitCode, relative)
	}
	return "✗ " + run.Status + " · " + relative
}

func runDisplayStatus(run app.Run) string {
	if run.Status == "running" && run.Orphaned {
		return "orphaned"
	}
	return run.Status
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
	if width >= 0 && len(runes) > width {
		if width <= 3 {
			return strings.Repeat(".", max(0, width))
		}
		return string(runes[:width-3]) + "..."
	}
	return string(runes)
}
