package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
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
	// People at a terminal browse runnable jobs in the TUI; scripts and agents keep the run command.
	if !cmd.Bool("json") && strings.HasPrefix(view.Next, "noon run ") && terminal(os.Stdin) && terminalWriter(cmd.Writer) {
		view.Next = "noon tui"
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
	if view.Scheduler.HeartbeatAt != nil {
		fmt.Fprintf(cmd.Writer, "Scheduler build: version=%q commit=%q\n", view.Scheduler.Version, view.Scheduler.Commit)
	}
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
			next = job.PauseStatus()
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
		rows = append(rows, []tableCell{{job.Name, "heading"}, {schedule, ""}, {next, nextRole(job, next)}, {lastRunCell(job.LastRun, now), lastRole}})
	}
	style.printBoundedRows(cmd.Writer, rows, "  ", outputWidth(cmd.Writer), 0)
	notices := slices.Clone(view.Notices[:min(3, len(view.Notices))])
	for _, notice := range view.Notices[min(3, len(view.Notices)):] {
		if notice.Code == "temporary_schedule" || notice.Code == "temporary_job" {
			notices = append(notices, notice)
		}
	}
	for _, notice := range notices {
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
	fmt.Fprintln(cmd.Writer, "Suggested action: "+style.render(humanText(view.Next), "heading"))
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

func doctorAt(ctx context.Context, cmd *urfave.Command, now time.Time, build config.BuildOpts) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	result := app.Doctor(ctx, values, now, build)
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

func listProjects(_ context.Context, cmd *urfave.Command) error { return projectsAt(cmd, time.Now()) }

// projectsAt ignores -C: it lists every enabled project in the selected state.
func projectsAt(cmd *urfave.Command, now time.Time) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	view, err := app.ProjectStatuses(values, now)
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	canonicalHome, _ := filepath.EvalSymlinks(home)
	// Abbreviate $HOME for people; a path elsewhere stays absolute.
	abbreviate := func(path string) (string, bool) {
		for _, prefix := range []string{home, canonicalHome} {
			if prefix != "" && prefix != "/" && strings.HasPrefix(path, prefix+string(filepath.Separator)) {
				return strings.TrimPrefix(path, prefix+string(filepath.Separator)), true
			}
		}
		return path, false
	}
	next := func(human bool) string {
		if view.NextProject == "" {
			return guidance(cmd, view.Next)
		}
		project := shellArgument(view.NextProject)
		if rest, ok := abbreviate(view.NextProject); human && ok {
			project = "~/" + shellArgument(rest)
		}
		// Check the same recorded config the row reflects.
		if view.NextConfig != "" {
			project += " --config " + shellArgument(view.NextConfig)
		}
		command := "noon -C " + project + strings.TrimPrefix(view.Next, "noon")
		// NOON_STATE_DIR carries over to the suggested command; a differing flag does not.
		if stateDir := setting(cmd, "state-dir"); stateDir != nil && *stateDir != os.Getenv("NOON_STATE_DIR") {
			if absolute, err := filepath.Abs(*stateDir); err == nil {
				command += " --state-dir " + shellArgument(absolute)
			}
		}
		return command
	}
	if cmd.Bool("json") {
		result := map[string]any{"projects": view.Projects}
		if view.Next != "" {
			result["next"] = next(false)
		}
		return emit(cmd, result)
	}
	if len(view.Projects) == 0 {
		fmt.Fprintln(cmd.Writer, "No enabled projects.")
		return nil
	}
	style := styleFor(cmd, cmd.Writer)
	rows := [][]tableCell{{{"PROJECT", "heading"}, {"PATH", "heading"}, {"STATE", "heading"}, {"NEXT", "heading"}}}
	for _, project := range view.Projects {
		path, ok := abbreviate(project.Path)
		if ok {
			path = "~/" + path
		}
		state, role := "ok", "ok"
		switch project.State {
		case "missing":
			state, role = "missing checkout", "failed"
		case "config_error":
			state, role = "config error", "config_error"
		case "failing":
			state, role = project.Failing[0]+" failing", "failed"
			if len(project.Failing) > 1 {
				state = fmt.Sprintf("%d jobs failing", len(project.Failing))
			}
		}
		nextAt := "-"
		if project.NextAt != nil {
			nextAt = project.NextAt.Format("Mon 15:04")
		}
		rows = append(rows, []tableCell{{project.Name, "heading"}, {path, ""}, {state, role}, {nextAt, ""}})
	}
	style.printBoundedRows(cmd.Writer, rows, "  ", outputWidth(cmd.Writer), 0, 1)
	if view.Next != "" {
		fmt.Fprintln(cmd.Writer, "Suggested action: "+style.render(humanText(next(true)), "heading"))
	}
	return nil
}
