package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/charmbracelet/x/term"
	urfave "github.com/urfave/cli/v3"
)

func status(_ context.Context, cmd *urfave.Command) error { return dashboardAt(cmd, time.Now()) }
func dashboardAt(cmd *urfave.Command, now time.Time) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	// An invalid invocation override is still a command error, not a persisted
	// project diagnostic. Keep the standard CLI error envelope for bad flags.
	if values.Timezone != nil {
		if _, err := time.LoadLocation(*values.Timezone); err != nil {
			return configError(err)
		}
	}
	view, err := app.Dashboard(values, now)
	if err != nil {
		var configuration *app.DashboardConfigError
		if errors.As(err, &configuration) {
			return configError(err)
		}
		return stateError(err)
	}
	view.Next = guidance(cmd, view.Next)
	for i := range view.Notices {
		// Only this notice embeds commands; its other text is run IDs and validated job names.
		if view.Notices[i].Code == "orphaned" {
			view.Notices[i].Message = guidance(cmd, view.Notices[i].Message)
		}
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"project": view.Project, "scheduler": view.Scheduler, "service": view.Service, "jobs": view.Jobs, "queued": view.Queued, "notices": view.Notices, "next": view.Next})
	}
	style := styleFor(cmd, cmd.Writer)
	style.printBoundedRows(cmd.Writer, [][]tableCell{{{view.Project.Name, "heading"}, {view.Project.Path, ""}, {"scheduler " + view.Scheduler.State, view.Scheduler.State}, {view.Project.Timezone, ""}}}, " · ", outputWidth(cmd.Writer))
	rows := [][]tableCell{{{"JOB", "heading"}, {"SCHEDULE", "heading"}, {"NEXT", "heading"}, {"LAST", "heading"}}}
	for _, job := range view.Jobs {
		schedule, next := "manual", "-"
		if job.Schedule != nil {
			schedule = *job.Schedule
		}
		switch {
		case !job.PlatformSupported:
			next = "other platform"
		case job.Paused:
			next = "paused"
		case !job.Enabled:
			next = "disabled"
		case job.Approval == "needs_approval":
			next = "needs approval"
		case job.Schedule != nil && !view.Project.Enabled:
			next = "disabled"
		case job.NextAt != nil:
			next = job.NextAt.Format("Mon 15:04")
		}
		lastRole := ""
		if job.LastRun != nil {
			lastRole = runDisplayStatus(*job.LastRun)
		}
		rows = append(rows, []tableCell{{job.Name, "heading"}, {schedule, ""}, {next, next}, {lastRunCell(job.LastRun, now), lastRole}})
	}
	style.printBoundedRows(cmd.Writer, rows, "  ", outputWidth(cmd.Writer), 0)
	for _, notice := range view.Notices[:min(3, len(view.Notices))] {
		if notice.Code == "orphaned" {
			for _, line := range wrapCell(notice.Message, outputWidth(cmd.Writer)) {
				fmt.Fprintln(cmd.Writer, style.render(line, "orphaned"))
			}
			continue
		}
		message := cell(notice.Message, outputWidth(cmd.Writer))
		if notice.Code == "config_error" {
			message = style.render(message, "config_error")
		}
		fmt.Fprintln(cmd.Writer, message)
	}
	fmt.Fprintln(cmd.Writer, "Next: "+style.render(humanText(view.Next), "heading"))
	return nil
}

// Redirected output has a deterministic budget, independent of shell settings.
func wrapCell(text string, width int) []string {
	width = max(1, width)
	words := strings.Fields(cell(text, -1))
	var lines []string
	line := ""
	for _, word := range words {
		for len([]rune(word)) > width {
			if line != "" {
				lines = append(lines, line)
				line = ""
			}
			runes := []rune(word)
			lines = append(lines, string(runes[:width]))
			word = string(runes[width:])
		}
		if line != "" && len([]rune(line))+1+len([]rune(word)) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func outputWidth(out io.Writer) int {
	if file, ok := out.(interface{ Fd() uintptr }); ok {
		if width, _, err := term.GetSize(file.Fd()); err == nil && width > 0 {
			return width
		}
	}
	return 80
}

// Shrink descriptive columns, never identifiers. A zero budget means full width.
// Very long identifiers may exceed the budget rather than becoming ambiguous.
func (s humanStyle) printBoundedRows(out io.Writer, rows [][]tableCell, separator string, budget int, identifiers ...int) {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, value := range row {
			widths[i] = max(widths[i], len([]rune(cell(value.text, -1))))
		}
	}
	for budget > 0 {
		total, widest := (len(widths)-1)*len([]rune(separator)), -1
		for i, width := range widths {
			total += width
			if !slices.Contains(identifiers, i) && width > 4 && (widest == -1 || width > widths[widest]) {
				widest = i
			}
		}
		if total <= budget || widest == -1 {
			break
		}
		widths[widest]--
	}
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, value := range row {
			text := cell(value.text, widths[i])
			cells[i] = s.render(text, value.role)
			if i < len(row)-1 {
				cells[i] += strings.Repeat(" ", widths[i]-len([]rune(text)))
			}
		}
		fmt.Fprintln(out, strings.TrimRight(strings.Join(cells, separator), " "))
	}
}

func doctor(ctx context.Context, cmd *urfave.Command) error { return doctorAt(ctx, cmd, time.Now()) }
func doctorAt(ctx context.Context, cmd *urfave.Command, now time.Time) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	result := app.Doctor(ctx, values, now)
	for i := range result.Checks {
		result.Checks[i].Hint = guidance(cmd, result.Checks[i].Hint)
	}
	if cmd.Bool("json") {
		if err := emit(cmd, map[string]any{"ok": result.OK, "checks": result.Checks}); err != nil {
			return err
		}
	} else {
		style := styleFor(cmd, cmd.Writer)
		for _, check := range result.Checks {
			fmt.Fprintf(cmd.Writer, "%s %s: %s", style.render(check.Status, check.Status), style.render(cell(check.Name, 120), "heading"), cell(check.Message, 240))
			if check.Hint != "" {
				fmt.Fprintf(cmd.Writer, " — %s", style.render(humanText(check.Hint), "heading"))
			}
			fmt.Fprintln(cmd.Writer)
		}
	}
	if !result.OK {
		return runExit(1)
	}
	return nil
}
