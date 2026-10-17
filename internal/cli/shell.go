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
	token, err := app.EnterProject(values, app.ShellEntry{Shell: args[0], Job: args[1], PreviousProject: args[2], PreviousDirectory: args[3], Directory: args[4], RecordOnly: cmd.Bool("record")}, time.Now())
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
	prefix := args[len(args)-1]
	selected := root
	position := 0
	var expectingValue bool
	for _, arg := range args[:len(args)-1] {
		if expectingValue {
			expectingValue = false
			continue
		}
		if arg == "--" {
			return
		}
		if strings.HasPrefix(arg, "-") {
			name, _, equal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
			flag := completionFlag(selected, name)
			if flag == nil {
				return
			}
			_, boolean := flag.(*urfave.BoolFlag)
			expectingValue = !boolean && !equal
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
	if expectingValue {
		return
	}
	printCandidate := func(candidate string) {
		if strings.HasPrefix(candidate, prefix) {
			fmt.Fprintln(active.Writer, candidate)
		}
	}
	if strings.HasPrefix(prefix, "-") {
		for _, command := range selected.Lineage() {
			for _, flag := range command.VisibleFlags() {
				for _, name := range flag.Names() {
					leader := "--"
					if len(name) == 1 {
						leader = "-"
					}
					printCandidate(leader + name)
				}
			}
		}
		return
	}
	if position != 0 {
		return
	}
	for _, command := range selected.Commands {
		if !command.Hidden {
			for _, name := range command.Names() {
				printCandidate(name)
			}
		}
	}
	switch selected.Name {
	case "run", "pause", "resume", "remove", "history", "logs", "cancel":
		values, err := input(active)
		if err != nil {
			return
		}
		cfg, err := config.Load(values)
		if err != nil {
			return
		}
		names := make([]string, 0, len(cfg.Jobs))
		for name := range cfg.Jobs {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			printCandidate(name)
		}
	}
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
