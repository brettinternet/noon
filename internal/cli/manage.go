package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/schedule"
	urfave "github.com/urfave/cli/v3"
)

func addJob(_ context.Context, cmd *urfave.Command) error {
	args := cmd.Args().Slice()
	argv, hasArgv := addArgv(cmd)
	if hasArgv {
		args = args[:len(args)-len(argv)]
	}
	if len(args) == 0 && !hasArgv {
		examples := []string{
			`noon add tests "every day at 2am" -- go test ./...`,
			`noon add lint "every 30m" --sh 'golangci-lint run ./...'`,
			`noon add review "weekdays at 9am" --agent claude --prompt "Review open PRs and list risks"`,
			`noon add notes --agent claude --prompt-file prompts/notes.md`,
			`noon add check-ci --in 2h --agent claude --prompt "Check CI and fix failures"`,
		}
		if cmd.Bool("json") {
			return emit(cmd, map[string]any{"examples": examples})
		}
		fmt.Fprintln(cmd.Writer, "Examples:\n  "+strings.Join(examples, "\n  "))
		return nil
	}
	if len(args) < 1 || len(args) > 2 {
		return usageError("add requires NAME and at most one SCHEDULE", "Run noon add --help")
	}
	if setting(cmd, "config") != nil {
		return &Error{Code: "explicit_config", Message: "cannot edit an explicit config file", Hint: "Edit that file directly", Usage: true}
	}
	name := args[0]
	if !validJobName(name) {
		return usageError("invalid job name", "Use a name matching ^[a-z][a-z0-9_-]{0,63}$")
	}
	job := config.Job{Name: name, Enabled: true, Timeout: "1h"}
	if cmd.IsSet("description") {
		job.Description = cmd.String("description")
	}
	if cmd.IsSet("agent") {
		job.Agent = cmd.String("agent")
	}
	if cmd.IsSet("timeout") {
		if cmd.String("timeout") == "none" {
			job.Timeout = "none"
		} else {
			d, e := schedule.ParseDuration(cmd.String("timeout"))
			if e != nil || d < time.Second {
				return usageError("invalid timeout", "Use a duration of at least 1s or none")
			}
			job.Timeout = d.String()
		}
	}
	if len(args) == 2 {
		parsed, parseErr := schedule.Parse(args[1])
		if parseErr != nil {
			return usageError(parseErr.Error(), "Try every day at 9am")
		}
		job.Schedule = &parsed
	}
	actions := 0
	if cmd.IsSet("sh") {
		actions++
		job.Run = cmd.String("sh")
		if cmd.String("sh") == "" {
			return usageError("--sh must not be empty", "Provide a shell command")
		}
	}
	if cmd.IsSet("prompt") {
		actions++
		v := cmd.String("prompt")
		job.Prompt = &v
	}
	if cmd.IsSet("prompt-file") {
		actions++
		v := cmd.String("prompt-file")
		job.PromptFile = &v
	}
	if hasArgv {
		if len(argv) == 0 {
			return usageError("-- requires a command", "Provide ARGV after --")
		}
		actions++
		job.Run = argv
	}
	if actions != 1 {
		return usageError("specify exactly one action: -- ARGV..., --sh STRING, --prompt TEXT, or --prompt-file PATH", "")
	}
	values, e := input(cmd)
	if e != nil {
		return e
	}
	cfg, e := config.Load(values)
	if e != nil {
		return configError(e)
	}
	if job.Prompt != nil || job.PromptFile != nil {
		if job.Agent == "" && cfg.DefaultAgent == "" {
			return usageError("prompt job requires --agent or default_agent", "Use --agent NAME")
		}
		if job.Agent != "" {
			if _, ok := cfg.Agents[job.Agent]; !ok {
				return usageError("unknown agent "+job.Agent, "")
			}
		}
	}
	queued := cmd.IsSet("in") || cmd.IsSet("at")
	if cmd.IsSet("in") && cmd.IsSet("at") {
		return usageError("--in and --at are mutually exclusive", "Choose one queued run time")
	}
	now := time.Now()
	var queueDue time.Time
	if queued {
		location, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			return configError(err)
		}
		mode, inValue, atValue := "in", cmd.String("in"), cmd.String("at")
		if cmd.IsSet("at") {
			mode = "at"
		}
		queueDue, err = queueDueAt(mode, inValue, atValue, now, location)
		if err != nil {
			return usageError(err.Error(), "Use --in DURATION or --at TIME")
		}
	}
	file := filepath.Join(cfg.Project, ".noon.yaml")
	kind := "private"
	mode := os.FileMode(0600)
	if cmd.Bool("shared") {
		file = filepath.Join(cfg.Project, "noon.yaml")
		kind = "shared"
		mode = 0644
	}
	if info, statErr := os.Stat(file); statErr == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	src, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return configError(err)
	}
	oldTargetNames, err := config.JobNames(src)
	if err != nil {
		return &Error{Code: "config_edit", Message: err.Error(), Hint: "Edit the file by hand"}
	}
	_, overrides := cfg.Jobs[name]
	if slices.Contains(oldTargetNames, name) {
		return &Error{Code: "exists", Message: fmt.Sprintf("job %q already exists", name), Hint: "Run noon remove " + name}
	}
	after, err := config.AddJob(src, name, job)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			return &Error{Code: "exists", Message: fmt.Sprintf("job %q already exists", name), Hint: "Run noon remove " + name}
		}
		return &Error{Code: "config_edit", Message: err.Error(), Hint: "Edit the file by hand"}
	}
	candidate, err := config.LoadWithFileOverride(values, file, after)
	if err != nil {
		return &Error{Code: "config_edit", Message: err.Error(), Hint: "Edit the file by hand"}
	}
	newTargetNames, err := config.JobNames(after)
	if err != nil {
		return &Error{Code: "config_edit", Message: err.Error(), Hint: "Edit the file by hand"}
	}
	if !sameNames(newTargetNames, append(append([]string(nil), oldTargetNames...), name)) {
		return editVerificationError("edited job names do not match the requested addition")
	}
	requestedDigest, err := config.DigestJobDefinition(job, cfg.Agents, cfg.DefaultAgent)
	if err != nil {
		return usageError(err.Error(), "")
	}
	addedJob, err := config.JobFromSource(after, name, cfg.Agents, cfg.DefaultAgent)
	if err != nil || addedJob.Digest != requestedDigest {
		return editVerificationError("added job digest differs from the requested definition")
	}
	if err := verifyRawJobsUnchanged(src, after, oldTargetNames, name, cfg); err != nil {
		return editVerificationError(err.Error())
	}
	if err := verifyConfigUnchanged(cfg, candidate, name); err != nil {
		return editVerificationError(err.Error())
	}
	if err = atomicWrite(file, after, mode); err != nil {
		return err
	}
	loadedInput := values
	updated := candidate
	if job.Schedule != nil || queued {
		addedJob.Shared = cmd.Bool("shared")
		if err := app.EnableAddedJob(updated, addedJob, now); err != nil {
			return stateError(err)
		}
	}
	if queued {
		if _, err := app.QueueRun(updated, name, "", queueDue, now, time.Now, false, "cli"); err != nil {
			return queueError(err)
		}
	}
	var view app.Inspection
	if job.Schedule != nil || queued {
		view, err = app.Inspect(loadedInput, now)
		if err != nil {
			return configError(err)
		}
		view, err = app.WithApprovals(view)
		if err != nil {
			return stateError(err)
		}
		view, err = app.WithScheduler(view, now)
		if err != nil {
			return stateError(err)
		}
	}
	scheduleText := "manual"
	if job.Schedule != nil {
		scheduleText = job.Schedule.String()
	}
	if cmd.Bool("json") {
		notes := []string{}
		if overrides {
			notes = append(notes, "Local job "+name+" overrides the shared definition.")
		}
		if !cmd.Bool("shared") && gitWorkTree(cfg.Project) && !ignoredLocalConfig(cfg.Project) {
			notes = append(notes, "Ignore .noon.yaml in Git by adding this line to .gitignore: .noon.yaml")
		}
		var nextAt *time.Time
		if job.Schedule != nil {
			location, _ := time.LoadLocation(updated.Timezone)
			next := job.Schedule.Next(now, location)
			nextAt = &next
		} else if queued {
			nextAt = &queueDue
		}
		needsApproval := []string{}
		for _, item := range view.Jobs {
			if item.Name != name && item.Approval == "needs_approval" {
				needsApproval = append(needsApproval, item.Name)
			}
		}
		next := "noon run " + name
		if (job.Schedule != nil || queued) && view.Project.Scheduler != "running" {
			hint, err := serviceGuidance(updated.StateDir)
			if err != nil {
				return err
			}
			if strings.HasPrefix(hint, "Next: noon service install") {
				next = "noon service install"
			}
		}
		return emit(cmd, map[string]any{"added": name, "file": file, "shared": cmd.Bool("shared"),
			"schedule": scheduleText, "next_at": nextAt, "scheduling_enabled": job.Schedule != nil || queued,
			"notes": notes, "needs_approval": needsApproval, "next": next})
	}
	fmt.Fprintf(cmd.Writer, "Added %s to %s (%s) · %s", name, filepath.Base(file), kind, scheduleText)
	if job.Schedule != nil {
		location, _ := time.LoadLocation(updated.Timezone)
		fmt.Fprintf(cmd.Writer, " · next %s", job.Schedule.Next(now, location).Format("Mon 15:04"))
	} else if queued {
		location, _ := time.LoadLocation(updated.Timezone)
		fmt.Fprintf(cmd.Writer, " · next %s", queueDue.In(location).Format("Mon 15:04"))
	}
	fmt.Fprintln(cmd.Writer)
	if overrides {
		fmt.Fprintf(cmd.Writer, "Local job %s overrides the shared definition.\n", name)
	}
	if !cmd.Bool("shared") && gitWorkTree(cfg.Project) && !ignoredLocalConfig(cfg.Project) {
		fmt.Fprintln(cmd.Writer, "Ignore .noon.yaml in Git by adding this line to .gitignore: .noon.yaml")
	}
	if job.Schedule != nil || queued {
		fmt.Fprintf(cmd.Writer, "Scheduling enabled for %s.\n", view.Project.Path)
		if view.Project.Scheduler == "running" {
			fmt.Fprintln(cmd.Writer, "The scheduler is running.")
		} else {
			fmt.Fprintln(cmd.Writer, "The scheduler isn't running, so nothing runs in the background.")
		}
		if cmd.Bool("shared") {
			needs := 0
			for _, item := range view.Jobs {
				if item.Name != name && item.Approval == "needs_approval" {
					needs++
				}
			}
			if needs > 0 {
				fmt.Fprintf(cmd.Writer, "%d other shared jobs still need approval.\n", needs)
			}
		}
		if view.Project.Scheduler == "running" {
			fmt.Fprintln(cmd.Writer, "Next: noon run "+name)
		} else {
			hint, hintErr := serviceGuidance(updated.StateDir)
			if hintErr != nil {
				return hintErr
			}
			if strings.HasPrefix(hint, "Next: noon service install") {
				fmt.Fprintf(cmd.Writer, "Next: noon service install · noon run %s\n", name)
			} else {
				fmt.Fprintln(cmd.Writer, "Next: noon run "+name)
			}
		}
	} else {
		fmt.Fprintln(cmd.Writer, "Next: noon run "+name)
	}
	return nil
}

func removeJob(_ context.Context, cmd *urfave.Command) error {
	if setting(cmd, "config") != nil {
		return &Error{Code: "explicit_config", Message: "cannot edit an explicit config file", Hint: "Edit that file directly", Usage: true}
	}
	args := cmd.Args().Slice()
	if len(args) == 0 {
		cfg, e := load(cmd)
		if e != nil {
			return e
		}
		names := make([]string, 0, len(cfg.Jobs))
		for name := range cfg.Jobs {
			names = append(names, name)
		}
		slices.Sort(names)
		if cmd.Bool("json") {
			return emit(cmd, map[string]any{"jobs": names})
		}
		for _, name := range names {
			fmt.Fprintln(cmd.Writer, name)
		}
		return nil
	}
	if len(args) != 1 {
		return usageError("remove accepts one NAME", "")
	}
	values, e := input(cmd)
	if e != nil {
		return e
	}
	cfg, e := config.Load(values)
	if e != nil {
		return configError(e)
	}
	job, ok := cfg.Jobs[args[0]]
	if !ok {
		return usageError("job not found", "")
	}
	if cmd.Bool("shared") && cmd.Bool("local") {
		return usageError("--shared and --local are mutually exclusive", "Choose the definition to remove")
	}
	sharedFile, localFile := filepath.Join(cfg.Project, "noon.yaml"), filepath.Join(cfg.Project, ".noon.yaml")
	sharedExists, localExists := hasJobInFile(values, sharedFile, args[0]), hasJobInFile(values, localFile, args[0])
	if sharedExists && localExists && !cmd.Bool("shared") && !cmd.Bool("local") {
		return usageError("job is defined in both noon.yaml and .noon.yaml", "Choose --shared or --local")
	}
	file := job.Source
	if cmd.Bool("shared") {
		file = sharedFile
	}
	if cmd.Bool("local") {
		file = localFile
	}
	src, e := os.ReadFile(file)
	if e != nil {
		return e
	}
	oldTargetNames, e := config.JobNames(src)
	if e != nil {
		return &Error{Code: "config_edit", Message: e.Error(), Hint: "Edit the file by hand"}
	}
	if !slices.Contains(oldTargetNames, args[0]) {
		return usageError("job not found in selected file", "")
	}
	after, e := config.RemoveJob(src, args[0])
	if e != nil {
		return &Error{Code: "config_edit", Message: e.Error(), Hint: "Edit the file by hand"}
	}
	candidate, e := config.LoadWithFileOverride(values, file, after)
	if e != nil {
		return &Error{Code: "config_edit", Message: e.Error(), Hint: "Edit the file by hand"}
	}
	newTargetNames, e := config.JobNames(after)
	if e != nil {
		return &Error{Code: "config_edit", Message: e.Error(), Hint: "Edit the file by hand"}
	}
	want := make([]string, 0, len(oldTargetNames)-1)
	for _, n := range oldTargetNames {
		if n != args[0] {
			want = append(want, n)
		}
	}
	if !sameNames(newTargetNames, want) {
		return editVerificationError("edited job names do not match the requested removal")
	}
	if err := verifyRawJobsUnchanged(src, after, oldTargetNames, args[0], cfg); err != nil {
		return editVerificationError(err.Error())
	}
	if err := verifyConfigUnchanged(cfg, candidate, args[0]); err != nil {
		return editVerificationError(err.Error())
	}
	if err := atomicWrite(file, after, 0); err != nil {
		return err
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"removed": args[0], "file": file})
	}
	return nil
}
func hasJobInFile(_ config.Input, file, name string) bool {
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	names, err := config.JobNames(data)
	return err == nil && slices.Contains(names, name)
}

func pauseJob(ctx context.Context, cmd *urfave.Command) error  { return pauseResume(ctx, cmd, true) }
func resumeJob(ctx context.Context, cmd *urfave.Command) error { return pauseResume(ctx, cmd, false) }
func pauseResume(_ context.Context, cmd *urfave.Command, paused bool) error {
	args := cmd.Args().Slice()
	values, e := input(cmd)
	if e != nil {
		return e
	}
	cfg, e := config.Load(values)
	if e != nil {
		return configError(e)
	}
	if len(args) == 0 {
		names := make([]string, 0, len(cfg.Jobs))
		for name := range cfg.Jobs {
			names = append(names, name)
		}
		slices.Sort(names)
		if cmd.Bool("json") {
			return emit(cmd, map[string]any{"jobs": names})
		}
		for _, name := range names {
			fmt.Fprintln(cmd.Writer, name)
		}
		return nil
	}
	if len(args) != 1 {
		return usageError("expected one job name", "")
	}
	if _, ok := cfg.Jobs[args[0]]; !ok {
		return &Error{Code: "not_found", Message: fmt.Sprintf("job %q not found", args[0]), Hint: "Run noon " + cmd.Name + " to list jobs"}
	}
	if e = app.SetPaused(cfg.StateDir, cfg.Project, args[0], paused, time.Now()); e != nil {
		return stateError(e)
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"job": args[0], "paused": paused})
	}
	if paused {
		fmt.Fprintf(cmd.Writer, "Paused %s. Background runs stop until noon resume %s.\n", args[0], args[0])
	} else {
		fmt.Fprintf(cmd.Writer, "Resumed %s. Its schedule starts from now.\n", args[0])
	}
	return nil
}

// The root retains the subcommand's original tokens, including the delimiter
// that urfave removes from the parsed positional arguments.
func addArgv(cmd *urfave.Command) ([]string, bool) {
	valueFlags := make(map[string]bool)
	for _, command := range cmd.Lineage() {
		for _, flag := range command.Flags {
			if _, boolean := flag.(*urfave.BoolFlag); !boolean {
				for _, name := range flag.Names() {
					valueFlags[name] = true
				}
			}
		}
	}
	raw := cmd.Root().Args().Slice()
	for i := 1; i < len(raw); i++ {
		if raw[i] == "--" {
			return raw[i+1:], true
		}
		if strings.HasPrefix(raw[i], "-") {
			name, _, hasValue := strings.Cut(strings.TrimLeft(raw[i], "-"), "=")
			if valueFlags[name] && !hasValue {
				i++
			}
		}
	}
	return nil, false
}

func validJobName(name string) bool {
	if len(name) < 1 || len(name) > 64 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func gitWorkTree(project string) bool {
	return exec.Command("git", "-C", project, "rev-parse", "--is-inside-work-tree").Run() == nil
}
func ignoredLocalConfig(project string) bool {
	cmd := exec.Command("git", "-C", project, "check-ignore", "-q", ".noon.yaml")
	return cmd.Run() == nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		} else {
			mode = 0600
		}
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".noon-edit-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(data); e == nil {
		e = f.Sync()
	}
	if e == nil && mode != 0 {
		e = f.Chmod(mode)
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	if e = os.Rename(tmp, path); e != nil {
		return e
	}
	d, e := os.Open(filepath.Dir(path))
	if e == nil {
		e = d.Sync()
		_ = d.Close()
	}
	return e
}
