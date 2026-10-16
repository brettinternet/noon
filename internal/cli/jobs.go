package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/brettinternet/bump/internal/app"
	urfave "github.com/urfave/cli/v3"
)

func status(_ context.Context, cmd *urfave.Command) error   { return inspectJobs(cmd, true) }
func listJobs(_ context.Context, cmd *urfave.Command) error { return inspectJobs(cmd, false) }

func inspectJobs(cmd *urfave.Command, dashboard bool) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	now := time.Now()
	view, err := app.Inspect(values, now)
	if err != nil {
		return configError(err)
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"project": view.Project, "jobs": view.Jobs})
	}
	if dashboard {
		fmt.Fprintf(cmd.Writer, "%s · %s · %s\n", filepath.Base(view.Project.Path), view.Project.Path, view.Project.Timezone)
	}
	printJobs(cmd.Writer, view.Jobs, now)
	if dashboard {
		fmt.Fprintln(cmd.Writer, "Next: bump run (list runnable jobs)")
	}
	return nil
}

func printJobs(out io.Writer, jobs []app.Job, now time.Time) {
	if len(jobs) == 0 {
		fmt.Fprintln(out, "No jobs. Add this to bump.yaml:\njobs:\n  tests:\n    schedule: weekdays at 9am\n    run: task test")
		return
	}
	rows := [][4]string{{"JOB", "SCHEDULE", "NEXT", "COMMAND"}}
	widths := [3]int{3, 8, 4}
	overrides := 0
	for _, job := range jobs {
		scheduled, next := "manual", "-"
		if job.Schedule != nil {
			scheduled = *job.Schedule
		}
		if !job.Enabled {
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
		row := [4]string{job.Name, scheduled, next, command}
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
	for i, max := range [3]int{18, 24, 22} {
		if widths[i] > max {
			widths[i] = max
		}
	}
	commandWidth := 80 - widths[0] - widths[1] - widths[2] - 6
	for _, row := range rows {
		fmt.Fprintf(out, "%-*s  %-*s  %-*s  %s\n", widths[0], cell(row[0], widths[0]), widths[1], cell(row[1], widths[1]), widths[2], cell(row[2], widths[2]), cell(row[3], commandWidth))
	}
	fmt.Fprintf(out, "%d jobs · %d overrides\n", len(jobs), overrides)
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
		if (r >= 32 && r < 127 || r == '…') && !unicode.IsControl(r) {
			b.WriteRune(r)
		} else {
			b.WriteString(strconv.QuoteRuneToASCII(r)[1 : len(strconv.QuoteRuneToASCII(r))-1])
		}
	}
	text = b.String()
	if len(text) > width {
		return text[:width-3] + "..."
	}
	return text
}
