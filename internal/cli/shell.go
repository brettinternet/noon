package cli

import (
	"context"
	"embed"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	urfave "github.com/urfave/cli/v3"
)

//go:embed shell/bash.sh shell/zsh.sh shell/fish.fish
var shellScripts embed.FS

func shellCommand() *urfave.Command {
	return &urfave.Command{
		Name: "shell", Usage: "Set up Bash, Zsh, or Fish completion and optional project-entry jobs",
		Description: "Run noon shell init bash|zsh|fish for completion-only setup. Add --on-enter JOB to queue that job when entering an enabled project. Source the output manually; no startup files are changed and no scheduler is started.",
		Action: func(ctx context.Context, cmd *urfave.Command) error {
			if err := noArgs(cmd); err != nil {
				return err
			}
			if cmd.Bool("json") {
				return emit(cmd, map[string]any{"usage": "noon shell init bash|zsh|fish", "next": "noon shell init", "description": cmd.Description})
			}
			return urfave.ShowSubcommandHelp(cmd)
		},
		Commands: []*urfave.Command{
			{Name: "init", Usage: "Print sourceable completion; optionally queue JOB on project entry", ArgsUsage: "bash|zsh|fish [--on-enter JOB]",
				Flags: []urfave.Flag{&urfave.StringFlag{Name: "on-enter", Usage: "Queue this existing job on entry to an enabled project"}}, Action: shellInit},
			{Name: "entry", Hidden: true, Flags: []urfave.Flag{&urfave.BoolFlag{Name: "record"}}, Action: shellEntry},
		},
	}
}

func supportedShell(shell string) bool { return shell == "bash" || shell == "zsh" || shell == "fish" }

var shellJobName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func shellInit(_ context.Context, cmd *urfave.Command) error {
	if cmd.Args().Len() == 0 && !cmd.IsSet("on-enter") {
		if cmd.Bool("json") {
			return emit(cmd, map[string]any{"shells": []string{"bash", "zsh", "fish"}})
		}
		fmt.Fprintln(cmd.Writer, `Choose your shell; source the output explicitly:
  Bash (~/.bashrc): eval "$(noon shell init bash)"
  Zsh (~/.zshrc): eval "$(noon shell init zsh)"
  Fish (~/.config/fish/config.fish): noon shell init fish | source
No startup files are changed. No shell is selected automatically.`)
		return nil
	}
	if cmd.Args().Len() != 1 || !supportedShell(cmd.Args().First()) || cmd.Bool("json") {
		return usageError("use noon shell init bash|zsh|fish [--on-enter JOB] (without --json)", "")
	}
	job := cmd.String("on-enter")
	if cmd.IsSet("on-enter") && !shellJobName.MatchString(job) {
		return usageError("--on-enter requires a valid job name", "")
	}
	shell := cmd.Args().First()
	extension := ".sh"
	if shell == "fish" {
		extension = ".fish"
	}
	script, err := shellScripts.ReadFile("shell/" + shell + extension)
	if err != nil {
		return err
	}
	// Job names have a restrictive grammar; all dynamic paths are passed as
	// quoted argv by the scripts, never substituted into source text.
	_, err = io.WriteString(cmd.Writer, strings.ReplaceAll(string(script), "@JOB@", job))
	return err
}

func shellEntry(_ context.Context, cmd *urfave.Command) error {
	args := cmd.Args().Slice()
	if len(args) != 5 || !supportedShell(args[0]) || !shellJobName.MatchString(args[1]) || cmd.Bool("json") {
		return usageError("shell entry requires SHELL JOB PREVIOUS_PROJECT PREVIOUS_DIRECTORY DIRECTORY", "Use noon shell init instead")
	}
	// Ignore selection overrides without unsetting the actual process env:
	// QueueRun must still see NOON_PROJECT/NOON_JOB for its recursion guard.
	values, err := invocationInput(cmd, true)
	if err != nil {
		return err
	}
	token, err := app.EnterProject(values, app.ShellEntry{Shell: args[0], Job: args[1], PreviousProject: args[2], PreviousDirectory: args[3], Directory: args[4], RecordOnly: cmd.Bool("record")}, time.Now(), time.Now)
	fmt.Fprintln(cmd.Writer, token)
	if err != nil {
		return queueError(err)
	}
	return nil
}

// Completion uses urfave's completion-only dispatch, but examines the original
// argv because parsed Args cannot distinguish a consumed flag value from a
// positional selector. Every shell passes the current (possibly empty) token.
func configureCompletion(root *urfave.Command, argv []string) {
	_ = root.Walk(func(cmd *urfave.Command) error {
		cmd.ShellComplete = func(_ context.Context, active *urfave.Command) {
			completeShell(root, active, argv)
		}
		return nil
	})
}

const (
	completionPathFile = "__NOON_COMPLETE_PATH_FILE__"
	completionPathDir  = "__NOON_COMPLETE_PATH_DIR__"
)

type pendingCompletionValue struct {
	flag urfave.Flag
	name string
}

func completeShell(root, active *urfave.Command, argv []string) {
	if len(argv) < 2 {
		return
	}
	args := argv[1:]
	if args[len(args)-1] == "--generate-shell-completion" {
		args = args[:len(args)-1]
	}
	if len(args) == 0 {
		args = []string{""}
	}
	current := args[len(args)-1]
	selected, position, pending, afterBoundary, valid := completionPosition(root, args[:len(args)-1])
	if !valid || afterBoundary {
		return
	}
	if pending.flag != nil {
		completeFlagValue(active, pending, current, args)
		return
	}
	if strings.HasPrefix(current, "-") {
		name, value, equal := strings.Cut(strings.TrimLeft(current, "-"), "=")
		if equal {
			flag := completionFlag(selected, name)
			if flag == nil {
				return
			}
			if _, boolean := flag.(*urfave.BoolFlag); !boolean {
				completeFlagValue(active, pendingCompletionValue{flag: flag, name: name}, value, args)
				return
			}
		}
		for _, command := range selected.Lineage() {
			for _, flag := range command.VisibleFlags() {
				for _, name := range flag.Names() {
					leader := "--"
					if len(name) == 1 {
						leader = "-"
					}
					printCompletionCandidate(active, leader+name, current)
				}
			}
		}
		return
	}
	if position != 0 {
		switch selected.FullName() {
		case "noon pause", "noon resume", "noon remove", "noon history", "noon cancel":
		case "noon run":
			mode, err := runQueueMode(active)
			if err != nil || mode == "" {
				return
			}
		default:
			return
		}
	}
	for _, command := range selected.Commands {
		if !command.Hidden {
			for _, name := range command.Names() {
				printCompletionCandidate(active, name, current)
			}
		}
	}
	switch selected.FullName() {
	case "noon shell init":
		for _, shell := range []string{"bash", "zsh", "fish"} {
			printCompletionCandidate(active, shell, current)
		}
	case "noon import crontab":
		fmt.Fprintln(active.Writer, completionPathFile)
		printCompletionCandidate(active, "-", current)
	case "noon run", "noon pause", "noon resume", "noon remove", "noon history", "noon logs", "noon cancel":
		cfg, ok := completionConfig(active, args)
		if !ok {
			return
		}
		for _, name := range sortedMapKeys(cfg.Jobs) {
			printCompletionCandidate(active, name, current)
		}
	}
}

func completionPosition(root *urfave.Command, args []string) (*urfave.Command, int, pendingCompletionValue, bool, bool) {
	selected := root
	position := 0
	pending := pendingCompletionValue{}
	afterBoundary := false
	for _, arg := range args {
		if afterBoundary {
			continue
		}
		if pending.flag != nil {
			pending = pendingCompletionValue{}
			continue
		}
		if arg == "--" {
			afterBoundary = true
			continue
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			name, _, equal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			flag := completionFlag(selected, name)
			if flag == nil {
				return selected, position, pendingCompletionValue{}, afterBoundary, false
			}
			_, boolean := flag.(*urfave.BoolFlag)
			if !boolean && !equal {
				pending = pendingCompletionValue{flag: flag, name: name}
			}
			continue
		}
		if position == 0 {
			if next := selected.Command(arg); next != nil {
				selected = next
				continue
			}
		}
		position++
	}
	return selected, position, pending, afterBoundary, true
}

func completionFlag(cmd *urfave.Command, name string) urfave.Flag {
	for _, command := range cmd.Lineage() {
		for _, flag := range command.Flags {
			for _, candidate := range flag.Names() {
				if candidate == name {
					return flag
				}
			}
		}
	}
	return nil
}

func printCompletionCandidate(cmd *urfave.Command, candidate, prefix string) {
	if strings.HasPrefix(candidate, prefix) {
		fmt.Fprintln(cmd.Writer, candidate)
	}
}

func completeFlagValue(active *urfave.Command, pending pendingCompletionValue, prefix string, args []string) {
	name := pending.name
	if names := pending.flag.Names(); len(names) != 0 {
		name = names[0]
	}
	switch name {
	case "project":
		fmt.Fprintln(active.Writer, completionPathDir)
	case "config":
		fmt.Fprintln(active.Writer, completionPathFile)
	case "prompt-file":
		fmt.Fprintln(active.Writer, completionPathFile)
	case "color":
		completeValues(active.Writer, []string{"auto", "always", "never"}, prefix)
	case "status":
		completeStatusValues(active.Writer, prefix)
	case "section":
		completeValues(active.Writer, []string{"all", "job", "hook"}, prefix)
	case "agent":
		if cfg, ok := completionConfig(active, args); ok {
			completeValues(active.Writer, sortedMapKeys(cfg.Agents), prefix)
		}
	case "on-enter":
		if cfg, ok := completionConfig(active, args); ok {
			completeValues(active.Writer, sortedMapKeys(cfg.Jobs), prefix)
		}
	}
}

func completeValues(out io.Writer, values []string, prefix string) {
	for _, candidate := range values {
		if strings.HasPrefix(candidate, prefix) {
			fmt.Fprintln(out, candidate)
		}
	}
}

func completeStatusValues(out io.Writer, prefix string) {
	split := strings.LastIndex(prefix, ",") + 1
	lead, last := prefix[:split], prefix[split:]
	for _, status := range app.RunStatuses {
		if strings.HasPrefix(status, last) {
			fmt.Fprintln(out, lead+status)
		}
	}
}

func completionConfig(cmd *urfave.Command, args []string) (config.Config, bool) {
	values, err := input(cmd)
	if err != nil {
		return config.Config{}, false
	}
	if userSelectedForCompletion(args) {
		values.User = true
		values.Project, values.File = nil, nil
	}
	cfg, err := app.LoadJobs(values, time.Now())
	return cfg, err == nil
}

func userSelectedForCompletion(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--user" {
			return true
		}
		if value, ok := strings.CutPrefix(arg, "--user="); ok {
			selected, err := strconv.ParseBool(value)
			return err == nil && selected
		}
	}
	return false
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// os.Args is the default for callers that run NewRootCommand directly; Run
// replaces it with its explicit argv, without mutating process-global state.
func defaultCompletion(root *urfave.Command) { configureCompletion(root, os.Args) }

// Readline retains shell quoting in COMP_WORDS. Remove syntax, never evaluate
// it: even a command substitution in a completion request is just path data.
// This adapter is gated by the completion marker, so it cannot affect actions.
func shellCompletionArgs(args []string) []string {
	if len(args) < 2 || args[len(args)-1] != "--generate-shell-completion" || os.Getenv("NOON_COMPLETION_SHELL") != "bash" {
		return args
	}
	result := []string{args[0]}
	for _, word := range args[1 : len(args)-1] {
		word = unquoteCompletionWord(word)
		// Readline treats '=' as a word break even in --config=FILE.
		if word == "=" && len(result) > 1 && strings.HasPrefix(result[len(result)-1], "-") {
			result[len(result)-1] += "="
		} else if len(result) > 1 && strings.HasPrefix(result[len(result)-1], "-") && strings.HasSuffix(result[len(result)-1], "=") {
			result[len(result)-1] += word
		} else {
			result = append(result, word)
		}
	}
	return append(result, "--generate-shell-completion")
}

func unquoteCompletionWord(word string) string {
	var out strings.Builder
	var quote byte
	for i := 0; i < len(word); i++ {
		c := word[i]
		switch {
		case quote == '\'' && c != '\'':
			out.WriteByte(c)
		case quote == 0 && c == '$' && i+1 < len(word) && word[i+1] == '\'':
			quote = 'a' // Bash ANSI-C quoting, without variable/command expansion.
			i++
		case quote == 'a' && c == '\'':
			quote = 0
		case quote == 'a' && c == '\\' && i+1 < len(word):
			if word[i+1] == 'e' || word[i+1] == 'E' {
				out.WriteByte(27)
				i++
			} else if value, _, tail, err := strconv.UnquoteChar(word[i:], '\''); err == nil {
				out.WriteRune(value)
				i = len(word) - len(tail) - 1
			} else {
				out.WriteByte(c)
			}
		case c == '\\' && quote != '\'' && i+1 < len(word):
			next := word[i+1]
			if quote != '"' || strings.ContainsRune("$`\"\\\n", rune(next)) {
				i++
				if next != '\n' {
					out.WriteByte(next)
				}
			} else {
				out.WriteByte(c)
			}
		case (c == '\'' || c == '"') && quote == 0:
			quote = c
		case c == quote && (quote == '\'' || quote == '"'):
			quote = 0
		default:
			out.WriteByte(c)
		}
	}
	return out.String()
}
