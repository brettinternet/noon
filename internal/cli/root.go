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
	"time"

	"github.com/brettinternet/noon/internal/config"
	urfave "github.com/urfave/cli/v3"
)

func init() {
	urfave.VersionPrinter = func(cmd *urfave.Command) {
		fmt.Fprintf(cmd.Writer, "%s %s\n", cmd.Name, cmd.Version)
	}
}

// NewRootCommand returns a fresh command tree; no settings are read until an action runs.
func NewRootCommand(build config.BuildOpts, out, errOut io.Writer) *urfave.Command {
	root := &urfave.Command{
		Name:        "noon",
		Usage:       "Repo-local scheduling, without a platform",
		Description: "With no command, show the project dashboard without running jobs, starting services, or opening listeners. Background runs require explicit enablement and a scheduler. Use --user to select personal jobs in your Noon user config.\n\nExamples:\n  noon\n  noon --user\n  noon add tests \"every day at 2am\" -- go test ./...\n  noon run tests\n  noon history -s failed\n  noon logs tests -n 50",
		Version:     fmt.Sprintf("%s (%.7s, built %s)", build.Version, build.Commit, build.BuildTime),
		Writer:      out, ErrWriter: errOut,
		EnableShellCompletion: true,
		ExitErrHandler:        func(context.Context, *urfave.Command, error) {},
		Flags: []urfave.Flag{
			&urfave.BoolFlag{Name: "user", Usage: "Use personal jobs from $XDG_CONFIG_HOME/noon/config.yaml (else ~/.config/noon/config.yaml)"},
			&urfave.StringFlag{Name: "project", Aliases: []string{"C"}, Usage: "Use project `DIR` instead of discovery", Sources: urfave.EnvVars("NOON_PROJECT")},
			&urfave.StringFlag{Name: "config", Usage: "Read only `FILE` (relative to invocation directory)", Sources: urfave.EnvVars("NOON_CONFIG")},
			&urfave.StringFlag{Name: "timezone", Usage: "Override timezone with `TZ` (default Local)", Sources: urfave.EnvVars("NOON_TIMEZONE")},
			&urfave.StringFlag{Name: "state-dir", Usage: "Override per-user state directory with `DIR`", Sources: urfave.EnvVars("NOON_STATE_DIR")},

			&urfave.BoolFlag{Name: "json", Aliases: []string{"j"}, Usage: "Print structured JSON instead of human output"},
			&urfave.StringFlag{Name: "color", Value: "auto", Usage: "Style human output: auto, always, or never", Validator: func(value string) error {
				if value != "auto" && value != "always" && value != "never" {
					return usageError("--color must be auto, always, or never", "")
				}
				return nil
			}},
		},
		Action: status,
		Commands: []*urfave.Command{
			shellCommand(),
			{Name: "version", Usage: "Print version and build information", Action: func(_ context.Context, cmd *urfave.Command) error {
				if err := noArgs(cmd); err != nil {
					return err
				}
				urfave.VersionPrinter(cmd.Root())
				return nil
			}},
			{Name: "run", Usage: "Run one job in the foreground or queue multiple jobs; without names, list runnable jobs", ArgsUsage: "[NAME...]", Flags: []urfave.Flag{
				&urfave.StringFlag{Name: "data", Usage: "Event `JSON`, @FILE, or - for stdin (up to 256 KiB)"},
				&urfave.BoolFlag{Name: "detach", Aliases: []string{"d"}, Usage: "Queue NAME to run as soon as capacity is available"},
				&urfave.StringFlag{Name: "in", Usage: "Queue NAME after `DURATION` (at least 1s)"},
				&urfave.StringFlag{Name: "at", Usage: "Queue NAME at `TIME`: HH:MM, YYYY-MM-DD HH:MM, or RFC 3339"},
				&urfave.StringFlag{Name: "key", Usage: "Idempotency `KEY`; deduplicated while retained (newest 50 terminal runs per job)"},
			}, Action: runJob},
			{Name: "cancel", Usage: "Cancel runs or all active runs of named jobs; without arguments, list active runs", ArgsUsage: "[RUN|NAME...]", Action: cancelRuns},
			{Name: "history", Usage: "Inspect recent runs for this project", ArgsUsage: "[NAME...]", Flags: []urfave.Flag{
				&urfave.IntFlag{Name: "limit", Aliases: []string{"n"}, Value: 20, Usage: "Limit history to `N` runs (1–50)"},
				&urfave.StringSliceFlag{Name: "status", Aliases: []string{"s"}, Usage: "Filter by `STATUS`; repeat or comma-separate values"},
			}, Action: historyRuns},
			{Name: "logs", Usage: "Read a run log, follow it, or print its path", Description: "Without a target, read the latest run. Bounded reads support --json; --follow rejects it. --timestamps / -t prefixes displayed lines with captured receipt times; JSON adds line_times. Resume guidance goes to stderr.\n\nExamples:\n  noon logs tests -n 50\n  noon logs 12 --match FAIL --context 2 --timestamps\n  noon logs 12 --follow -t", ArgsUsage: "[NAME|RUN]", Flags: []urfave.Flag{
				&urfave.BoolFlag{Name: "follow", Aliases: []string{"f"}, Usage: "Follow a running run until it finishes"},
				&urfave.BoolFlag{Name: "timestamps", Aliases: []string{"t"}, Usage: "Prefix displayed lines with captured receipt times"},
				&urfave.BoolFlag{Name: "path", Usage: "Print the absolute log path"},
				&urfave.StringFlag{Name: "section", Value: "all", Usage: "Read `SECTION`: all, job, or hook"},
				&urfave.IntFlag{Name: "tail", Aliases: []string{"n"}, Usage: "Keep the last `N` selected lines (1–10000)"},
				&urfave.Int64Flag{Name: "after-cursor", Aliases: []string{"c"}, Usage: "Read from byte offset `BYTES` and return a resumable cursor"},
				&urfave.StringFlag{Name: "match", Aliases: []string{"m"}, Usage: "Select lines matching Go RE2 `REGEX`"},
				&urfave.IntFlag{Name: "context", Usage: "Include `N` surrounding lines (0–50); requires --match"},
				&urfave.IntFlag{Name: "limit-bytes", Aliases: []string{"b"}, Usage: "Cap emitted log text at `BYTES`"},
				&urfave.StringSliceFlag{Name: "status", Aliases: []string{"s"}, Usage: "Filter by `STATUS`; repeat or comma-separate values"},
			}, Action: runLogs},
			{Name: "enable", Usage: "Enable this project and approve current shared job definitions", Action: enableProject},
			{Name: "disable", Usage: "Disable this project and revoke approvals; keep history", Action: disableProject},
			{Name: "serve", Usage: "Serve enabled schedules; optionally accept signed webhooks and an authenticated API with --listen", Description: "Runs in the foreground until stopped. Uses each enabled project's recorded configuration, including the user project. Accepts --state-dir, --listen, and --api-token-file; rejects --user, --project, --config, --timezone, and --json. No HTTP listener starts by default. --api-token-file requires --listen.\n\nExamples:\n  noon serve\n  noon serve --listen 127.0.0.1:8080\n  noon serve --listen 127.0.0.1:8080 --api-token-file /run/secrets/noon-api-token", Flags: []urfave.Flag{
				&urfave.StringFlag{Name: "listen", Usage: "Listen for signed webhooks at `ADDR`; no listener starts without this flag"},
				&urfave.StringFlag{Name: "api-token-file", Usage: "Read a bearer token from `FILE` and enable the authenticated API and MCP over HTTP; requires --listen"},
			}, Action: serveAction(build)},
			{Name: "mcp", Usage: "Serve MCP tools over stdio", Action: mcpCommand(build.Version)},
			{Name: "tui", Usage: "Open the keyboard-driven jobs, runs, and logs view", Description: "Requires terminal stdin and stdout; --json is not supported. Inspect the current project and safely queue, pause, or cancel work. Log views show local receipt timestamps by default; t toggles them. Keys: j/k or arrows move, enter opens runs, l opens logs, / searches, s changes log sections, f filters runs, r queues, p pauses, c cancels after confirmation, g/G jump through logs, ? shows help, q quits. Never adds, edits, enables, or approves jobs.", Action: tuiAction},
			{Name: "skill", Usage: "Print the shell-only agent skill", Action: showSkill},
			{Name: "service", Usage: "Inspect or manage the per-user scheduler service", Action: serviceStatus, Commands: []*urfave.Command{
				{Name: "install", Usage: "Install or reconcile the scheduler unit and running build", Description: "Changed units and known running-build mismatches restart the service, interrupting active jobs. Matching builds are left running. Unknown build metadata needs explicit service restart. --print is read-only.", Flags: []urfave.Flag{
					&urfave.BoolFlag{Name: "print", Usage: "Print the service unit without installing or starting it"},
				}, Action: func(ctx context.Context, cmd *urfave.Command) error { return installService(ctx, cmd, build) }},
				{Name: "restart", Usage: "Explicitly restart the installed scheduler, interrupting active jobs", Description: "Restart using the installed service definition without rewriting its environment. Active jobs are interrupted, not drained. After a same-path binary upgrade, restart explicitly; use service install for changed executable paths or environment settings. A missing service must be installed first.", Action: restartService},
				{Name: "uninstall", Usage: "Stop and remove the per-user scheduler service", Action: uninstallService},
			}},
			{Name: "status", Usage: "Show the project dashboard without running anything", Action: status},
			{Name: "doctor", Usage: "Diagnose setup with read-only checks; exit 1 if a check fails", Action: func(ctx context.Context, cmd *urfave.Command) error { return doctorAt(ctx, cmd, time.Now(), build) }},
			{Name: "list", Aliases: []string{"ls"}, Usage: "List validated jobs and next scheduled times", Flags: []urfave.Flag{
				&urfave.BoolFlag{Name: "wide", Usage: "Show complete names, schedules, and commands without truncation"},
			}, Action: listJobs},
			{Name: "add", Usage: "Add a scheduled or manual job", Description: "Without arguments, print examples. Writes private .git/noon.yaml by default; --shared writes noon.yaml. With --user, writes the user config.yaml and rejects --shared/--local.\n\nExamples:\n  noon add tests \"every day at 2am\" -- go test ./...\n  noon add lint --sh 'go vet ./...'\n  noon --user add digest \"every day at 8am\" --sh 'date >> digest.txt'\n  noon add review --agent claude --prompt 'Review changes'", ArgsUsage: "NAME [SCHEDULE] [-- ARGV...]", Flags: []urfave.Flag{
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
			{Name: "remove", Aliases: []string{"rm"}, Usage: "Remove a job definition; retain history", Description: "Without NAME, list jobs without removing anything. If both repository files define NAME, choose --shared or --local. With --user, remove edits the user config.yaml.\n\nExample:\n  noon remove tests --local\n  noon --user remove digest", ArgsUsage: "[NAME...]", Flags: []urfave.Flag{&urfave.BoolFlag{Name: "shared", Usage: "Remove from shared noon.yaml"}, &urfave.BoolFlag{Name: "local", Usage: "Remove from private .git/noon.yaml"}}, Action: removeJob},
			{Name: "pause", Usage: "Pause background runs of jobs", ArgsUsage: "[NAME... | --all]", Flags: []urfave.Flag{&urfave.BoolFlag{Name: "all", Aliases: []string{"a"}, Usage: "Pause all currently configured jobs in the selected project"}}, Action: pauseJob},
			{Name: "resume", Usage: "Resume background runs of jobs", ArgsUsage: "[NAME... | --all]", Flags: []urfave.Flag{&urfave.BoolFlag{Name: "all", Aliases: []string{"a"}, Usage: "Resume all currently configured jobs in the selected project"}}, Action: resumeJob},
			{Name: "import", Usage: "Preview or import jobs from another scheduler", Action: importers, Commands: []*urfave.Command{
				{Name: "crontab", Usage: "Preview or import a user crontab", ArgsUsage: "FILE|-", Flags: []urfave.Flag{
					&urfave.BoolFlag{Name: "write", Usage: "Add jobs to private .git/noon.yaml, or to user config.yaml with --user"},
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
				if err := validateUserSelection(selected); err != nil {
					return err
				}
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
	jsonOutput, colorMode := outputRequested(root, args)
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
			style := colorStyle(colorMode, errOut)
			fmt.Fprintf(errOut, "%s %s\n", style.render("noon:", "failed"), humanText(failure.Message))
			if failure.Hint != "" {
				fmt.Fprintf(errOut, "Hint: %s\n", style.render(humanText(failure.Hint), "heading"))
			}
		}
		if failure.Usage {
			return 2
		}
		return 1
	}
	return 0
}

// Inspect output modes on errors, including failures before flag parsing
// reaches --json or --color. Do not mistake a string flag value for an output flag.
func outputRequested(root *urfave.Command, args []string) (bool, string) {
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
	requested, color := false, "auto"
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return requested, color
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
		case name == "color":
			if !hasValue && i+1 < len(args) {
				i++
				value = args[i]
			}
			color = value
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
	return requested, color
}

func validateUserSelection(cmd *urfave.Command) error {
	if !cmd.Bool("user") {
		return nil
	}
	if cmd.IsSet("project") || envSet("NOON_PROJECT") {
		return usageError("--user conflicts with -C/--project and NOON_PROJECT", "Use --user alone to select personal jobs")
	}
	if cmd.IsSet("config") || envSet("NOON_CONFIG") {
		return usageError("--user conflicts with --config and NOON_CONFIG", "Edit the user project's config.yaml directly")
	}
	name := cmd.FullName()
	if name == "noon serve" || name == "noon service" || strings.HasPrefix(name, "noon service ") || name == "noon init" || name == "noon shell" || strings.HasPrefix(name, "noon shell ") {
		return usageError("--user is not supported for "+name, "Use --user with personal job commands")
	}
	return nil
}

func envSet(name string) bool {
	_, set := os.LookupEnv(name)
	return set
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
	values := config.Input{CWD: cwd, UserConfigDir: userDir, User: cmd.Bool("user"), StateDirDefault: filepath.Join(stateHome, "noon"),
		StateDir: setting(cmd, "state-dir")}
	if !shellEntry {
		values.Project, values.File, values.Timezone = setting(cmd, "project"), setting(cmd, "config"), setting(cmd, "timezone")
		if values.File != nil && !values.User {
			project, projectErr := config.ProjectDir(values)
			if projectErr == nil && config.IsUserProject(project, userDir) {
				return config.Input{}, usageError("the user project conflicts with --config", "Use the user project config.yaml without --config")
			}
		}
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
		sessionIDFlag := ""
		if profile.SessionIDFlag != "" {
			sessionIDFlag = ", session_id_flag=" + profile.SessionIDFlag
		}
		promptArgs := ""
		if len(profile.PromptArgs) > 0 {
			quoted := make([]string, len(profile.PromptArgs))
			for i, arg := range profile.PromptArgs {
				quoted[i] = strconv.Quote(arg)
			}
			promptArgs = ", prompt_args=[" + strings.Join(quoted, ", ") + "]"
		}
		fmt.Fprintf(cmd.Writer, "  %s: %s (%s%s%s)\n", cell(name, 80), strings.Join(args, " "), profile.Prompt, sessionIDFlag, promptArgs)
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
	fmt.Fprintln(cmd.Writer, guidance(cmd, "Suggested action: noon config check"))
	return nil
}

func configOutput(cfg config.Config) map[string]any {
	agents := make(map[string][]string, len(cfg.Agents))
	profiles := make(map[string]map[string]any, len(cfg.Agents))
	for name, profile := range cfg.Agents {
		agents[name] = profile.Command
		view := map[string]any{"command": profile.Command, "prompt": profile.Prompt}
		if profile.SessionIDFlag != "" {
			view["session_id_flag"] = profile.SessionIDFlag
		}
		if len(profile.PromptArgs) > 0 {
			view["prompt_args"] = profile.PromptArgs
		}
		profiles[name] = view
	}
	return map[string]any{
		"project": cfg.Project, "timezone": cfg.Timezone, "state_dir": cfg.StateDir,
		"sources": cfg.Sources, "agents": agents, "agent_profiles": profiles, "default_agent": cfg.DefaultAgent,
	}
}

func emit(cmd *urfave.Command, result map[string]any) error {
	result["schema_version"] = 1
	return json.NewEncoder(cmd.Writer).Encode(result)
}
