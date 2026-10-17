// Package cli adapts Noon's core operations to a human and machine CLI.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/brettinternet/noon/internal/config"
	urfave "github.com/urfave/cli/v3"
)

// NewRootCommand returns a fresh command tree; no settings are read until an action runs.
func NewRootCommand(build config.BuildOpts, out, errOut io.Writer) *urfave.Command {
	root := &urfave.Command{
		Name:        "noon",
		Usage:       "Repo-local scheduling, without a platform",
		Description: "With no command, show the project dashboard without running jobs, starting services, or opening listeners. Background runs require explicit enablement and a scheduler.\n\nExamples:\n  noon\n  noon add tests \"every day at 2am\" -- go test ./...\n  noon run tests\n  noon history -s failed\n  noon logs tests -n 50",
		Version:     fmt.Sprintf("%s (commit %s, built %s)", build.Version, build.Commit, build.BuildTime),
		Writer:      out, ErrWriter: errOut,
		EnableShellCompletion: true,
		ExitErrHandler:        func(context.Context, *urfave.Command, error) {},
		Flags: []urfave.Flag{
			&urfave.StringFlag{Name: "project", Aliases: []string{"C"}, Usage: "Use project `DIR` instead of discovery", Sources: urfave.EnvVars("NOON_PROJECT")},
			&urfave.StringFlag{Name: "config", Usage: "Read only `FILE` (relative to invocation directory)", Sources: urfave.EnvVars("NOON_CONFIG")},
			&urfave.StringFlag{Name: "timezone", Usage: "Override timezone with `TZ` (default Local)", Sources: urfave.EnvVars("NOON_TIMEZONE")},
			&urfave.StringFlag{Name: "state-dir", Usage: "Override per-user state directory with `DIR`", Sources: urfave.EnvVars("NOON_STATE_DIR")},

			&urfave.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "Print structured JSON instead of human output"},
		},
		Action: status,
		Commands: []*urfave.Command{
			shellCommand(),
			{Name: "run", Usage: "Run NAME in the foreground; without a name, list runnable jobs", ArgsUsage: "[NAME]", Flags: []urfave.Flag{
				&urfave.StringFlag{Name: "data", Usage: "Event `JSON`, @FILE, or - for stdin (up to 256 KiB)"},
				&urfave.BoolFlag{Name: "detach", Aliases: []string{"d"}, Usage: "Queue NAME to run as soon as capacity is available"},
				&urfave.StringFlag{Name: "in", Usage: "Queue NAME after `DURATION` (at least 1s)"},
				&urfave.StringFlag{Name: "at", Usage: "Queue NAME at `TIME`: HH:MM, YYYY-MM-DD HH:MM, or RFC 3339"},
				&urfave.StringFlag{Name: "key", Usage: "Idempotency `KEY`; deduplicated while retained (newest 50 terminal runs per job)"},
			}, Action: runJob},
			{Name: "cancel", Usage: "Cancel a run or all active runs of a job; without an argument, list active runs", ArgsUsage: "[RUN|NAME]", Action: cancelRuns},
			{Name: "history", Usage: "Inspect recent runs for this project", ArgsUsage: "[NAME]", Flags: []urfave.Flag{
				&urfave.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 20, Usage: "Limit history to `N` runs (1–50)"},
				&urfave.StringSliceFlag{Name: "status", Aliases: []string{"s"}, Usage: "Filter by `STATUS`; repeat or comma-separate values"},
			}, Action: historyRuns},
			{Name: "logs", Usage: "Read a run log, follow it, or print its path", Description: "Without a target, read the latest run. Bounded reads support --json; --follow rejects it. Resume guidance goes to stderr.\n\nExamples:\n  noon logs tests -n 50\n  noon logs 12 --match FAIL --context 2\n  noon logs 12 --follow", ArgsUsage: "[NAME|RUN]", Flags: []urfave.Flag{
				&urfave.BoolFlag{Name: "follow", Aliases: []string{"f"}, Usage: "Follow a running run until it finishes"},
				&urfave.BoolFlag{Name: "path", Usage: "Print the absolute log path"},
				&urfave.StringFlag{Name: "section", Value: "all", Usage: "Read `SECTION`: all, job, or hook"},
				&urfave.IntFlag{Name: "tail", Aliases: []string{"n"}, Usage: "Keep the last `N` selected lines (1–10000)"},
				&urfave.Int64Flag{Name: "after-cursor", Aliases: []string{"c"}, Usage: "Read from byte offset `BYTES` and return a resumable cursor"},
				&urfave.StringFlag{Name: "match", Aliases: []string{"m"}, Usage: "Select lines matching Go RE2 `REGEX`"},
				&urfave.IntFlag{Name: "context", Usage: "Include `N` surrounding lines (0–50); requires --match"},
				&urfave.IntFlag{Name: "limit-bytes", Aliases: []string{"b"}, Usage: "Cap emitted log text at `BYTES`"},
				&urfave.StringSliceFlag{Name: "status", Aliases: []string{"s"}, Usage: "Filter by `STATUS`; repeat or comma-separate values"},
			}, Action: runLogs},
			{Name: "enable", Usage: "Enable this checkout and approve current shared job definitions", Action: enableProject},
			{Name: "disable", Usage: "Disable this checkout and revoke approvals; keep history", Action: disableProject},
			{Name: "serve", Usage: "Serve enabled schedules; optionally accept signed webhooks with --listen", Description: "Runs in the foreground until stopped. Uses each enabled project's recorded configuration. Accepts --state-dir and --listen; rejects --project, --config, --timezone, and --json. No HTTP listener starts by default.\n\nExamples:\n  noon serve\n  noon serve --listen 127.0.0.1:8080", Flags: []urfave.Flag{
				&urfave.StringFlag{Name: "listen", Usage: "Listen for signed webhooks at `ADDR`; no listener starts without this flag"},
			}, Action: serveAction(build.Version)},
			{Name: "mcp", Usage: "Serve MCP tools over stdio", Action: mcpCommand(build.Version)},
			{Name: "tui", Usage: "Open the keyboard-driven jobs, runs, and logs view", Description: "Requires terminal stdin and stdout; --json is not supported. Inspect the current project and safely queue, pause, or cancel work. Keys: j/k or arrows move, enter opens runs, l opens logs, / searches, s changes log sections, f filters runs, r queues, p pauses, c cancels after confirmation, g/G jump through logs, ? shows help, q quits. Never adds, edits, enables, or approves jobs.", Action: tuiAction},
			{Name: "skill", Usage: "Print the shell-only agent skill", Action: showSkill},
			{Name: "service", Usage: "Inspect or manage the per-user scheduler service", Action: serviceStatus, Commands: []*urfave.Command{
				{Name: "install", Usage: "Install and start the per-user scheduler service", Flags: []urfave.Flag{
					&urfave.BoolFlag{Name: "print", Usage: "Print the service unit without installing or starting it"},
				}, Action: installService},
				{Name: "uninstall", Usage: "Stop and remove the per-user scheduler service", Action: uninstallService},
			}},
			{Name: "status", Usage: "Show the project dashboard without running anything", Action: status},
			{Name: "doctor", Usage: "Diagnose setup with read-only checks; exit 1 if a check fails", Action: doctor},
			{Name: "list", Aliases: []string{"ls"}, Usage: "List validated jobs and next scheduled times", Action: listJobs},
			{Name: "add", Usage: "Add a scheduled or manual job", Description: "Without arguments, print examples. Writes private .git/noon.yaml by default; --shared writes noon.yaml.\n\nExamples:\n  noon add tests \"every day at 2am\" -- go test ./...\n  noon add lint --sh 'go vet ./...'\n  noon add review --agent claude --prompt \"Review changes\"", ArgsUsage: "NAME [SCHEDULE] [-- ARGV...]", Flags: []urfave.Flag{
				&urfave.StringFlag{Name: "sh", Usage: "Run `STRING` through /bin/sh -c"},
				&urfave.StringFlag{Name: "prompt", Aliases: []string{"p"}, Usage: "Create an agent-prompt job with `TEXT`"},
				&urfave.StringFlag{Name: "prompt-file", Usage: "Read the prompt from `PATH` at run time"},
				&urfave.StringFlag{Name: "agent", Aliases: []string{"a"}, Usage: "Use agent profile `NAME`"},
				&urfave.StringFlag{Name: "description", Usage: "Set the job description to `TEXT`"},
				&urfave.StringFlag{Name: "timeout", Aliases: []string{"t"}, Usage: "Set the run timeout to `DURATION`"},
				&urfave.BoolFlag{Name: "shared", Usage: "Write noon.yaml instead of private .git/noon.yaml"},
				&urfave.StringFlag{Name: "in", Usage: "Queue a run after `DURATION`"},
				&urfave.StringFlag{Name: "at", Usage: "Queue a run at `TIME`"},
			}, Action: addJob},
			{Name: "remove", Aliases: []string{"rm"}, Usage: "Remove a job definition; retain history", Description: "Without NAME, list jobs without removing anything. If both files define NAME, choose --shared or --local.\n\nExample:\n  noon remove tests --local", ArgsUsage: "[NAME]", Flags: []urfave.Flag{&urfave.BoolFlag{Name: "shared", Usage: "Remove from shared noon.yaml"}, &urfave.BoolFlag{Name: "local", Usage: "Remove from private .git/noon.yaml"}}, Action: removeJob},
			{Name: "pause", Usage: "Pause background runs of a job", ArgsUsage: "[NAME]", Action: pauseJob},
			{Name: "resume", Usage: "Resume background runs of a job", ArgsUsage: "[NAME]", Action: resumeJob},
			{Name: "import", Usage: "Preview or import jobs from another scheduler", Action: importers, Commands: []*urfave.Command{
				{Name: "crontab", Usage: "Preview or import a user crontab", ArgsUsage: "FILE|-", Flags: []urfave.Flag{
					&urfave.BoolFlag{Name: "write", Usage: "Add all imported jobs to private .git/noon.yaml"},
				}, Action: importCrontab},
			}},
			{Name: "init", Usage: "Create noon.yaml without overwriting existing files", Flags: []urfave.Flag{
				&urfave.BoolFlag{Name: "local", Usage: "Create private .git/noon.yaml instead; Git cannot commit it"},
			}, Action: initialize},
			{Name: "config", Usage: "Show effective configuration (default) or validate it", Action: showConfig, Commands: []*urfave.Command{
				{Name: "show", Usage: "Show effective settings and loaded file paths", Action: showConfig},
				{Name: "check", Usage: "Validate configuration without changing anything", Action: checkConfig},
			}},
		},
	}
	_ = root.Walk(func(cmd *urfave.Command) error {
		// urfave adds a help/h subcommand to every command; on leaves it would capture job names.
		cmd.HideHelpCommand = len(cmd.Commands) == 0
		cmd.OnUsageError = func(_ context.Context, selected *urfave.Command, err error, _ bool) error {
			return usageError(err.Error(), flagUsageHint(selected, err))
		}
		action := cmd.Action
		if action != nil {
			cmd.Action = func(ctx context.Context, selected *urfave.Command) error {
				if len(selected.Commands) > 0 {
					if err := noArgs(selected); err != nil {
						return err
					}
				}
				err := action(ctx, selected)
				var failure *Error
				if errors.As(err, &failure) {
					copy := *failure
					copy.Hint = guidance(selected, copy.Hint)
					if !failure.Usage {
						return &copy
					}
					hint := commandHelp(selected)
					if !strings.Contains(copy.Hint, hint) {
						if copy.Hint != "" {
							copy.Hint += ". "
						}
						copy.Hint += hint
					}
					return &copy
				}
				return err
			}
		}
		return nil
	})
	defaultCompletion(root)
	return root
}

// Run owns error rendering so framework errors cannot exit the calling process.
// JSON actions emit one object; help/version/completion retain standard CLI text.
func Run(ctx context.Context, args []string, build config.BuildOpts, out, errOut io.Writer) int {
	args = shellCompletionArgs(args)
	root := NewRootCommand(build, out, errOut)
	configureCompletion(root, args)
	jsonOutput := jsonRequested(root, args)
	if err := root.Run(ctx, args); err != nil {
		var exit runExit
		if errors.As(err, &exit) {
			return int(exit)
		}
		var failure *Error
		if !errors.As(err, &failure) {
			failure = &Error{Code: "error", Message: err.Error()}
		}
		if jsonOutput {
			_ = json.NewEncoder(out).Encode(map[string]any{"schema_version": 1, "error": failure})
		} else {
			fmt.Fprintf(errOut, "noon: %s\n", failure.Message)
			if failure.Hint != "" {
				fmt.Fprintf(errOut, "Hint: %s\n", failure.Hint)
			}
		}
		if failure.Usage {
			return 2
		}
		return 1
	}
	return 0
}

// Inspect only output mode on errors, including failures before flag parsing
// reaches --json. Do not mistake a string flag's value for an output flag.
func jsonRequested(root *urfave.Command, args []string) bool {
	valueFlags := make(map[string]bool)
	includeFlags := func(cmd *urfave.Command) {
		for _, flag := range cmd.Flags {
			_, boolean := flag.(*urfave.BoolFlag)
			for _, name := range flag.Names() {
				valueFlags[name] = !boolean
			}
		}
	}
	selected := root
	includeFlags(selected)
	positional := false
	requested := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return requested
		}
		if !strings.HasPrefix(arg, "-") {
			if next := selected.Command(arg); !positional && next != nil {
				selected = next
				includeFlags(selected)
			} else {
				positional = true
			}
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
		switch {
		case valueFlags[name]:
			if !hasValue {
				i++
			}
		case name == "json" || name == "j":
			if !hasValue {
				requested = true
			} else {
				requested, _ = strconv.ParseBool(value)
			}
		}
	}
	return requested
}

func input(cmd *urfave.Command) (config.Input, error) {
	return invocationInput(cmd, false)
}

func invocationInput(cmd *urfave.Command, shellEntry bool) (config.Input, error) {
	for _, name := range []string{"project", "config", "timezone", "state-dir"} {
		if shellEntry && name != "state-dir" {
			continue
		}
		if value := setting(cmd, name); value != nil && strings.TrimSpace(*value) == "" {
			return config.Input{}, usageError(fmt.Sprintf("--%s or its environment variable must not be empty", name), "")
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return config.Input{}, err
	}
	userDir := os.Getenv("XDG_CONFIG_HOME")
	if userDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return config.Input{}, err
		}
		userDir = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(userDir) {
		return config.Input{}, errors.New("XDG_CONFIG_HOME must be absolute")
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return config.Input{}, err
		}
		stateHome = filepath.Join(home, ".local", "state")
	} else if !filepath.IsAbs(stateHome) {
		return config.Input{}, errors.New("XDG_STATE_HOME must be absolute")
	}
	values := config.Input{CWD: cwd, UserConfigDir: userDir, StateDirDefault: filepath.Join(stateHome, "noon"),
		StateDir: setting(cmd, "state-dir")}
	if !shellEntry {
		values.Project, values.File, values.Timezone = setting(cmd, "project"), setting(cmd, "config"), setting(cmd, "timezone")
	}
	return values, nil
}

func setting(cmd *urfave.Command, name string) *string {
	if !cmd.IsSet(name) {
		return nil
	}
	value := cmd.String(name)
	return &value
}

func load(cmd *urfave.Command) (config.Config, error) {
	if err := noArgs(cmd); err != nil {
		return config.Config{}, err
	}
	values, err := input(cmd)
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Load(values)
	return cfg, configError(err)
}

func noArgs(cmd *urfave.Command) error {
	if cmd.Args().Len() != 0 {
		names := []string{}
		for _, child := range cmd.VisibleCommands() {
			names = append(names, child.Names()...)
		}
		return usageError(fmt.Sprintf("unexpected argument %q", cmd.Args().First()), commandHelp(cmd)+nearbyHint(cmd.Args().First(), names, cmd.FullName()+" "))
	}
	return nil
}

func showConfig(_ context.Context, cmd *urfave.Command) error {
	cfg, err := load(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"config": configOutput(cfg)})
	}
	fmt.Fprintf(cmd.Writer, "Project:       %s\nTimezone:      %s\nState dir:     %s\n", cfg.Project, cfg.Timezone, cfg.StateDir)
	if cfg.DefaultAgent == "" {
		fmt.Fprintln(cmd.Writer, "Default agent: (none)")
	} else {
		fmt.Fprintf(cmd.Writer, "Default agent: %s\n", cell(cfg.DefaultAgent, 80))
	}
	fmt.Fprintln(cmd.Writer, "Files (low → high precedence):")
	if len(cfg.Sources) == 0 {
		fmt.Fprintln(cmd.Writer, "  none; using defaults and invocation overrides")
	}
	for _, path := range cfg.Sources {
		fmt.Fprintf(cmd.Writer, "  %s\n", path)
	}
	fmt.Fprintln(cmd.Writer, "Agents:")
	names := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		profile := cfg.Agents[name]
		args := make([]string, len(profile.Command))
		for i, arg := range profile.Command {
			args[i] = strconv.Quote(arg)
		}
		fmt.Fprintf(cmd.Writer, "  %s: %s (%s)\n", cell(name, 80), strings.Join(args, " "), profile.Prompt)
	}
	fmt.Fprintln(cmd.Writer, "CLI flags override environment variables; both override files.")
	return nil
}

func checkConfig(_ context.Context, cmd *urfave.Command) error {
	cfg, err := load(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"valid": true, "config": configOutput(cfg), "jobs": len(cfg.Jobs)})
	}
	fmt.Fprintf(cmd.Writer, "Configuration valid · %s · %d jobs\n", cfg.Project, len(cfg.Jobs))
	return nil
}

func initialize(_ context.Context, cmd *urfave.Command) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	for _, name := range []string{"config", "timezone", "state-dir"} {
		if cmd.IsSet(name) {
			return usageError(fmt.Sprintf("init does not use --%s or its environment variable", name), "Edit the generated file instead")
		}
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	project, err := config.ProjectDir(values)
	if err != nil {
		return configError(err)
	}
	path, mode := filepath.Join(project, "noon.yaml"), os.FileMode(0o644)
	if cmd.Bool("local") {
		if path, err = privateFile(project); err != nil {
			return err
		}
		mode = 0o600
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	created := true
	if errors.Is(err, os.ErrExist) {
		created = false
	} else if err != nil {
		return err
	} else {
		_, writeErr := io.WriteString(file, "# Noon settings. Listing jobs does not execute them.\nversion: 1\n# timezone: Local\n# jobs:\n#   tests:\n#     schedule: weekdays at 9am\n#     run: task test\n")
		closeErr := file.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"path": path, "created": created})
	}
	if created {
		fmt.Fprintf(cmd.Writer, "Created %s\n", path)
	} else {
		fmt.Fprintf(cmd.Writer, "Kept existing %s (not overwritten)\n", path)
	}
	fmt.Fprintln(cmd.Writer, guidance(cmd, "Next: noon config check"))
	return nil
}

func configOutput(cfg config.Config) map[string]any {
	agents := make(map[string][]string, len(cfg.Agents))
	for name, profile := range cfg.Agents {
		agents[name] = profile.Command
	}
	return map[string]any{
		"project": cfg.Project, "timezone": cfg.Timezone, "state_dir": cfg.StateDir,
		"sources": cfg.Sources, "agents": agents, "default_agent": cfg.DefaultAgent,
	}
}

func emit(cmd *urfave.Command, result map[string]any) error {
	result["schema_version"] = 1
	return json.NewEncoder(cmd.Writer).Encode(result)
}
