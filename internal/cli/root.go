// Package cli adapts Bump's core operations to a human and machine CLI.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/brettinternet/bump/internal/config"
	urfave "github.com/urfave/cli/v3"
)

// NewRootCommand returns a fresh command tree; no settings are read until an action runs.
func NewRootCommand(build config.BuildOpts, out, errOut io.Writer) *urfave.Command {
	root := &urfave.Command{
		Name:        "bump",
		Usage:       "Repo-local scheduling, without a platform",
		Description: "Bootstrap: inspect configuration and initialize a project. Job execution and scheduling are not implemented yet.",
		Version:     fmt.Sprintf("%s (commit %s, built %s)", build.Version, build.Commit, build.BuildTime),
		Writer:      out, ErrWriter: errOut,
		EnableShellCompletion: true,
		ExitErrHandler:        func(context.Context, *urfave.Command, error) {},
		Flags: []urfave.Flag{
			&urfave.StringFlag{Name: "project", Aliases: []string{"C"}, Usage: "Use this project directory instead of discovery", Sources: urfave.EnvVars("BUMP_PROJECT")},
			&urfave.StringFlag{Name: "config", Usage: "Read only this configuration file (relative to invocation directory)", Sources: urfave.EnvVars("BUMP_CONFIG")},
			&urfave.StringFlag{Name: "timezone", Usage: "Override timezone (default Local)", Sources: urfave.EnvVars("BUMP_TIMEZONE")},
			&urfave.StringFlag{Name: "state-dir", Usage: "Override state directory (default PROJECT/.bump)", Sources: urfave.EnvVars("BUMP_STATE_DIR")},
			&urfave.BoolFlag{Name: "json", Usage: "Print structured JSON instead of human output"},
		},
		Action: status,
		Commands: []*urfave.Command{
			{Name: "status", Usage: "Show project configuration and bootstrap status", Action: status},
			{Name: "init", Usage: "Create bump.yaml without overwriting existing files", Flags: []urfave.Flag{
				&urfave.BoolFlag{Name: "local", Usage: "Create private .bump.yaml instead; add it to .gitignore"},
			}, Action: initialize},
			{Name: "config", Usage: "Show effective configuration (default) or validate it", Action: showConfig, Commands: []*urfave.Command{
				{Name: "show", Usage: "Show effective settings and loaded file paths", Action: showConfig},
				{Name: "check", Usage: "Validate configuration without changing anything", Action: checkConfig},
			}},
		},
	}
	_ = root.Walk(func(cmd *urfave.Command) error {
		cmd.OnUsageError = func(_ context.Context, _ *urfave.Command, err error, _ bool) error { return err }
		return nil
	})
	return root
}

// Run owns error rendering so framework errors cannot exit the calling process.
// JSON actions emit one object; help/version/completion retain standard CLI text.
func Run(ctx context.Context, args []string, build config.BuildOpts, out, errOut io.Writer) int {
	root := NewRootCommand(build, out, errOut)
	if err := root.Run(ctx, args); err != nil {
		if jsonRequested(args) {
			_ = json.NewEncoder(out).Encode(map[string]any{"schema_version": 1, "error": err.Error()})
		} else {
			fmt.Fprintf(errOut, "bump: %v\n", err)
		}
		return 1
	}
	return 0
}

// Inspect only output mode on errors, including failures before flag parsing
// reaches --json. Do not mistake a string flag's value for an output flag.
func jsonRequested(args []string) bool {
	requested := false
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return requested
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
		switch name {
		case "project", "C", "config", "timezone", "state-dir":
			if !hasValue {
				i++
			}
		case "json":
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
	return config.Input{CWD: cwd, UserConfigDir: userDir,
		Project: setting(cmd, "project"), File: setting(cmd, "config"),
		Timezone: setting(cmd, "timezone"), StateDir: setting(cmd, "state-dir")}, nil
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
	return config.Load(values)
}

func noArgs(cmd *urfave.Command) error {
	if cmd.Args().Len() != 0 {
		return fmt.Errorf("unexpected argument %q; run bump --help", cmd.Args().First())
	}
	return nil
}

func status(_ context.Context, cmd *urfave.Command) error {
	cfg, err := load(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"status": "bootstrap", "config": cfg})
	}
	fmt.Fprintf(cmd.Writer, "Bump · %s\nTimezone: %s\nConfig files: %d\n", cfg.Project, cfg.Timezone, len(cfg.Sources))
	fmt.Fprintln(cmd.Writer, "Scheduling is not implemented yet. No jobs or services were started.")
	fmt.Fprintln(cmd.Writer, "Next: bump init · bump config · bump --help")
	return nil
}

func showConfig(_ context.Context, cmd *urfave.Command) error {
	cfg, err := load(cmd)
	if err != nil {
		return err
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"config": cfg})
	}
	fmt.Fprintf(cmd.Writer, "Project:   %s\nTimezone:  %s\nState dir: %s\n", cfg.Project, cfg.Timezone, cfg.StateDir)
	fmt.Fprintln(cmd.Writer, "Files (low → high precedence):")
	if len(cfg.Sources) == 0 {
		fmt.Fprintln(cmd.Writer, "  none; using defaults and invocation overrides")
	}
	for _, path := range cfg.Sources {
		fmt.Fprintf(cmd.Writer, "  %s\n", path)
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
		return emit(cmd, map[string]any{"valid": true, "config": cfg})
	}
	fmt.Fprintf(cmd.Writer, "Configuration valid · %s\n", cfg.Project)
	return nil
}

func initialize(_ context.Context, cmd *urfave.Command) error {
	if err := noArgs(cmd); err != nil {
		return err
	}
	for _, name := range []string{"config", "timezone", "state-dir"} {
		if cmd.IsSet(name) {
			return fmt.Errorf("init does not use --%s or its environment variable; edit the generated file instead", name)
		}
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	project, err := config.ProjectDir(values)
	if err != nil {
		return err
	}
	name := "bump.yaml"
	mode := os.FileMode(0o644)
	if cmd.Bool("local") {
		name, mode = ".bump.yaml", 0o600
	}
	path := filepath.Join(project, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	created := true
	if errors.Is(err, os.ErrExist) {
		created = false
	} else if err != nil {
		return err
	} else {
		_, writeErr := io.WriteString(file, "# Bump bootstrap settings. Jobs will be added in a later release.\n# Ignore .bump.yaml and .bump/ in your repository.\nversion: 1\n# timezone: Local\n")
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
	fmt.Fprintln(cmd.Writer, "Ignore .bump.yaml and .bump/ in Git. Next: bump config check")
	return nil
}

func emit(cmd *urfave.Command, result map[string]any) error {
	result["schema_version"] = 1
	return json.NewEncoder(cmd.Writer).Encode(result)
}
