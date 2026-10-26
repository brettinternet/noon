package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/schedule"
	"github.com/brettinternet/noon/internal/store"
	urfave "github.com/urfave/cli/v3"
)

func addJob(_ context.Context, cmd *urfave.Command) error {
	if cmd.Bool("user") && (cmd.Bool("shared") || cmd.Bool("local")) {
		return usageError("--user add conflicts with --shared and --local", "Use --user to select the user config file")
	}
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
		for i := range examples {
			examples[i] = guidance(cmd, examples[i])
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
	if cmd.IsSet("key") && !cmd.Bool("temporary") {
		return usageError("--key requires --temporary", "")
	}
	if cmd.Bool("temporary") && cmd.Bool("shared") {
		return usageError("--temporary conflicts with --shared", "")
	}
	if cmd.Bool("temporary") && !cmd.IsSet("for") && !cmd.IsSet("until") {
		return usageError("--temporary requires --for or --until", "")
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
		if strings.Contains(cmd.String("timeout"), "$") {
			job.Timeout = cmd.String("timeout")
		} else if cmd.String("timeout") == "none" {
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
		if strings.Contains(args[1], "$") {
			job.ScheduleExpression = args[1]
		} else {
			parsed, parseErr := schedule.Parse(args[1])
			if parseErr != nil {
				return usageError(parseErr.Error(), "Try every day at 9am")
			}
			job.Schedule = &parsed
		}
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
	if cmd.IsSet("for") && cmd.IsSet("until") {
		return usageError("--for and --until are mutually exclusive", "Choose one job end time")
	}
	if cmd.IsSet("for") || cmd.IsSet("until") {
		location, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			return configError(err)
		}
		until, err := app.ResolveEnd(cmd.String("for"), cmd.String("until"), now, location)
		if err != nil {
			return usageError(err.Error(), "Use --for 2h, or --until HH:MM, YYYY-MM-DD HH:MM, or RFC 3339")
		}
		job.Until = &until
	}
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
		queueDue, err = app.QueueDueAt(mode, inValue, atValue, now, location)
		if err != nil {
			return usageError(err.Error(), "Use --in DURATION or --at TIME")
		}
	}
	merged := cfg
	if err := app.MergeTemporaryJobs(&merged, now, true); err != nil {
		return stateError(err)
	}
	if cmd.Bool("temporary") {
		var due *time.Time
		if queued {
			due = &queueDue
		}
		result, err := app.AddTemporaryJob(merged, job, cmd.String("key"), due, now)
		if err != nil {
			return queueError(err)
		}
		if cmd.Bool("json") {
			output := map[string]any{"job": result.Job, "created": result.Created, "scheduler": result.Scheduler}
			if result.Reason != "" {
				output["reason"] = result.Reason
			}
			return emit(cmd, output)
		}
		scheduleText := "manual"
		if result.Job.Schedule != nil {
			scheduleText = *result.Job.Schedule
		}
		verb := "Added temporary job"
		if !result.Created {
			verb = "Already added temporary job"
		}
		fmt.Fprintf(cmd.Writer, "%s %s (%s until %s)", verb, name, scheduleText, result.Job.Until.Format("Mon 15:04"))
		if result.Job.NextAt != nil {
			next := store.FormatTime(*result.Job.NextAt)
			fmt.Fprintf(cmd.Writer, " · next %s (%s)", result.Job.NextAt.Format("Mon 15:04"), app.RelativeTime(&next, now))
		}
		fmt.Fprintln(cmd.Writer)
		if result.Scheduler == "offline" {
			fmt.Fprintln(cmd.Writer, "The scheduler isn't running, so nothing runs until it starts.")
			hint, err := serviceGuidance(cfg.StateDir)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.Writer, guidance(cmd, hint))
		}
		return nil
	}
	if existing, ok := merged.Jobs[name]; ok && existing.Temporary != nil {
		return &Error{Code: "exists", Message: fmt.Sprintf("job %q already exists", name), Hint: "noon remove " + name}
	}
	userProject := config.IsUserProject(cfg.Project, values.UserConfigDir)
	if userProject && (cmd.Bool("shared") || cmd.Bool("local")) {
		return usageError("user jobs do not use --shared or --local", "Select the user project with --user")
	}
	file, kind, mode := filepath.Join(cfg.Project, "noon.yaml"), "shared", os.FileMode(0644)
	if userProject {
		file, kind, mode = filepath.Join(cfg.Project, "config.yaml"), "user", 0600
	} else if !cmd.Bool("shared") {
		var err error
		if file, err = privateFile(cfg.Project); err != nil {
			return err
		}
		kind, mode = "private", 0600
	}
	if err := refuseSymlink(file); err != nil {
		return err
	}
	if info, statErr := os.Stat(file); statErr == nil {
		mode = info.Mode().Perm()
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	if userProject {
		mode = 0600
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
	if !jobHasInterpolation(job) {
		requestedDigest, err := config.DigestJobDefinition(job, cfg.Agents, cfg.DefaultAgent)
		if err != nil {
			return usageError(err.Error(), "")
		}
		rawAddedJob, err := config.JobFromSource(after, name, cfg.Agents, cfg.DefaultAgent, nil, nil, false, cfg.Project, "")
		if err != nil || rawAddedJob.Digest != requestedDigest {
			return editVerificationError("added job digest differs from the requested definition")
		}
	}
	addedJob, err := config.JobFromSource(after, name, cfg.Agents, cfg.DefaultAgent, cfg.OnSuccess, cfg.OnFailure, cfg.ConfigFile != nil, cfg.Project, cfg.Timezone)
	if err != nil {
		return editVerificationError("added job could not inherit configured hook defaults")
	}
	if err := verifyRawJobsUnchanged(src, after, oldTargetNames, name, cfg); err != nil {
		return editVerificationError(err.Error())
	}
	if err := verifyConfigUnchanged(cfg, candidate, name); err != nil {
		return editVerificationError(err.Error())
	}
	if userProject {
		if err := ensureUserConfigDir(cfg.Project); err != nil {
			return err
		}
	}
	if err = atomicWrite(file, after, mode); err != nil {
		return err
	}
	loadedInput := values
	updated := candidate
	scheduled := addedJob.Schedule != nil
	if scheduled || queued {
		addedJob.Shared = cmd.Bool("shared") && !userProject
		if err := app.EnableAddedJob(updated, addedJob, now); err != nil {
			return stateError(err)
		}
	}
	if queued {
		if _, err := app.QueueRun(updated, name, "", queueDue, now, time.Now, false, "cli", app.QueueOptions{}); err != nil {
			return queueError(err)
		}
	}
	var view app.Inspection
	if scheduled || queued {
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
	if scheduled {
		scheduleText = addedJob.Schedule.String()
	}
	if cmd.Bool("json") {
		notes := []string{}
		if overrides {
			notes = append(notes, "Local job "+name+" overrides the shared definition.")
		}
		var nextAt *time.Time
		if scheduled {
			location, _ := time.LoadLocation(updated.Timezone)
			_, next := addedJob.Schedule.NextEffective(now, location, updated.Project, name, addedJob.Splay, addedJob.Jitter, time.Time{})
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
		if (scheduled || queued) && view.Project.Scheduler != "running" {
			hint, err := serviceGuidance(updated.StateDir)
			if err != nil {
				return err
			}
			if strings.HasPrefix(hint, "Suggested action: noon service install") {
				next = "noon service install"
			}
		}
		return emit(cmd, map[string]any{"added": name, "file": file, "shared": cmd.Bool("shared") && !userProject,
			"schedule": scheduleText, "next_at": nextAt, "scheduling_enabled": scheduled || queued,
			"notes": notes, "needs_approval": needsApproval, "next": guidance(cmd, next)})
	}
	fmt.Fprintf(cmd.Writer, "Added %s to %s (%s) · %s", name, displayPath(cfg.Project, file), kind, scheduleText)
	if scheduled {
		location, _ := time.LoadLocation(updated.Timezone)
		_, next := addedJob.Schedule.NextEffective(now, location, updated.Project, name, addedJob.Splay, addedJob.Jitter, time.Time{})
		fmt.Fprintf(cmd.Writer, " · next %s", next.Format("Mon 15:04"))
	} else if queued {
		location, _ := time.LoadLocation(updated.Timezone)
		fmt.Fprintf(cmd.Writer, " · next %s", queueDue.In(location).Format("Mon 15:04"))
	}
	fmt.Fprintln(cmd.Writer)
	if overrides {
		fmt.Fprintf(cmd.Writer, "Local job %s overrides the shared definition.\n", name)
	}
	if scheduled || queued {
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
			fmt.Fprintln(cmd.Writer, guidance(cmd, "Suggested action: noon run "+name))
		} else {
			hint, hintErr := serviceGuidance(updated.StateDir)
			if hintErr != nil {
				return hintErr
			}
			if strings.HasPrefix(hint, "Suggested action: noon service install") {
				fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("Suggested action: noon service install · noon run %s\n", name)))
			} else {
				fmt.Fprintln(cmd.Writer, guidance(cmd, "Suggested action: noon run "+name))
			}
		}
	} else {
		fmt.Fprintln(cmd.Writer, guidance(cmd, "Suggested action: noon run "+name))
	}
	return nil
}

func jobHasInterpolation(job config.Job) bool {
	return job.ScheduleExpression != "" || strings.Contains(job.Timeout, "$") ||
		job.PromptFile != nil && strings.Contains(*job.PromptFile, "$")
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
		printJobCandidates(cmd, names)
		return nil
	}
	values, e := input(cmd)
	if e != nil {
		return e
	}
	cfg, e := config.Load(values)
	if e != nil {
		return configError(e)
	}
	now := time.Now()
	retained, e := app.TemporaryJobs(cfg, now)
	if e != nil {
		return stateError(e)
	}
	if cmd.Bool("shared") && cmd.Bool("local") {
		return usageError("--shared and --local are mutually exclusive", "Choose the definition to remove")
	}
	userProject := config.IsUserProject(cfg.Project, values.UserConfigDir)
	if userProject && (cmd.Bool("shared") || cmd.Bool("local")) {
		return usageError("user jobs do not use --shared or --local", "Select the user project with --user")
	}
	sharedFile, localFile := filepath.Join(cfg.Project, "noon.yaml"), ""
	if userProject {
		sharedFile = filepath.Join(cfg.Project, "config.yaml")
	} else if localFile, e = config.LocalPath(cfg.Project); e != nil {
		return configError(e)
	}
	if cmd.Bool("local") && localFile == "" {
		return usageError("no private config outside a Git checkout", "Choose --shared")
	}
	args = uniqueTargets(args)
	removed := []map[string]string{}
	// Resolve temporary jobs by name only, so one that no longer resolves can
	// still be removed. A config definition wins and deletes its shadow.
	var temporary []string
	for _, value := range retained {
		if _, configured := cfg.Jobs[value.Name]; configured {
			if err := app.RemoveTemporaryJob(cfg, value.Name, now); err != nil {
				return stateError(err)
			}
		} else if slices.Contains(args, value.Name) {
			if cmd.Bool("shared") || cmd.Bool("local") {
				return usageError("temporary jobs have no config file", "Remove without --shared or --local")
			}
			temporary = append(temporary, value.Name)
		}
	}
	args = slices.DeleteFunc(args, func(name string) bool { return slices.Contains(temporary, name) })
	byFile := map[string][]string{}
	files := []string{}
	for _, name := range args {
		job, ok := cfg.Jobs[name]
		if !ok {
			return app.MissingJobError(name, "remove", cfg.Jobs)
		}
		sharedExists, localExists := hasJobInFile(values, sharedFile, name), localFile != "" && hasJobInFile(values, localFile, name)
		if sharedExists && localExists && !cmd.Bool("shared") && !cmd.Bool("local") {
			return usageError("job is defined in both noon.yaml and "+displayPath(cfg.Project, localFile), "Choose --shared or --local")
		}
		file := job.Source
		if cmd.Bool("shared") {
			file = sharedFile
		}
		if cmd.Bool("local") {
			file = localFile
		}
		if _, ok := byFile[file]; !ok {
			files = append(files, file)
		}
		byFile[file] = append(byFile[file], name)
	}
	// Prepare and verify every file before writing any of them.
	prepared := map[string][]byte{}
	for _, file := range files {
		if err := refuseSymlink(file); err != nil {
			return err
		}
		src, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		before := cfg
		for _, name := range byFile[file] {
			oldNames, err := config.JobNames(src)
			if err != nil {
				return editVerificationError(err.Error())
			}
			if !slices.Contains(oldNames, name) {
				return usageError("job "+name+" not found in selected file", "")
			}
			after, err := config.RemoveJob(src, name)
			if err != nil {
				return editVerificationError(err.Error())
			}
			candidate, err := config.LoadWithFileOverride(values, file, after)
			if err != nil {
				return editVerificationError(err.Error())
			}
			newNames, err := config.JobNames(after)
			if err != nil {
				return editVerificationError(err.Error())
			}
			want := slices.DeleteFunc(slices.Clone(oldNames), func(n string) bool { return n == name })
			if !sameNames(newNames, want) {
				return editVerificationError("edited job names do not match the requested removal")
			}
			if err := verifyRawJobsUnchanged(src, after, oldNames, name, before); err != nil {
				return editVerificationError(err.Error())
			}
			if err := verifyConfigUnchanged(before, candidate, name); err != nil {
				return editVerificationError(err.Error())
			}
			src, before = after, candidate
		}
		prepared[file] = src
	}
	for _, name := range temporary {
		if err := app.RemoveTemporaryJob(cfg, name, now); err != nil {
			return stateError(err)
		}
		removed = append(removed, map[string]string{"removed": name, "file": "temporary"})
		if !cmd.Bool("json") {
			fmt.Fprintf(cmd.Writer, "Removed temporary job %s. History is retained.\n", name)
		}
	}
	failures := []targetFailure{}
	for _, file := range files {
		if err := app.EndTemporarySchedules(cfg.StateDir, cfg.Project, byFile[file]); err != nil {
			return stateError(err)
		}
		if err := atomicWrite(file, prepared[file], 0); err != nil {
			for _, name := range byFile[file] {
				failures = append(failures, targetFailure{Target: name, Error: err.Error()})
			}
			continue
		}
		for _, name := range byFile[file] {
			removed = append(removed, map[string]string{"removed": name, "file": file})
			if !cmd.Bool("json") {
				fmt.Fprintf(cmd.Writer, "Removed %s from %s. History is retained.\n", name, file)
			}
		}
	}
	if cmd.Args().Len() == 1 && len(failures) == 0 {
		if cmd.Bool("json") {
			return emit(cmd, map[string]any{"removed": removed[0]["removed"], "file": removed[0]["file"]})
		}
		return nil
	}
	return finishBatch(cmd, map[string]any{"removed": removed}, failures)
}
func printJobCandidates(cmd *urfave.Command, names []string) {
	if len(names) == 0 {
		fmt.Fprintln(cmd.Writer, guidance(cmd, "No jobs. Suggested action: noon add tests -- go test ./..."))
		return
	}
	fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("Choose a job: noon %s NAME\n", cmd.Name)))
	for _, name := range names {
		fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("  noon %s %s\n", cmd.Name, name)))
	}
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
	if cmd.Bool("all") && len(args) != 0 {
		return usageError("cannot combine a job name with --all", "")
	}
	values, e := input(cmd)
	if e != nil {
		return e
	}
	cfg, e := app.LoadJobs(values, time.Now())
	if e != nil {
		return configError(e)
	}
	if len(args) == 0 {
		names := make([]string, 0, len(cfg.Jobs))
		for name := range cfg.Jobs {
			names = append(names, name)
		}
		slices.Sort(names)
		if cmd.Bool("all") {
			changed, err := app.SetJobsPaused(cfg.StateDir, cfg.Project, names, paused, time.Now())
			if err != nil {
				return stateError(err)
			}
			if cmd.Bool("json") {
				return emit(cmd, map[string]any{"jobs": names, "paused": paused, "changed": changed})
			}
			if paused {
				fmt.Fprintf(cmd.Writer, "Paused %d jobs (%d configured). Queued runs cancelled; running work continues.\n", changed, len(names))
			} else {
				fmt.Fprintf(cmd.Writer, "Resumed %d jobs (%d configured). Schedules start from now.\n", changed, len(names))
			}
			return nil
		}
		if cmd.Bool("json") {
			return emit(cmd, map[string]any{"jobs": names})
		}
		printJobCandidates(cmd, names)
		return nil
	}
	args = uniqueTargets(args)
	for _, name := range args {
		if _, ok := cfg.Jobs[name]; !ok {
			return &Error{Code: "not_found", Message: fmt.Sprintf("job %q not found", name), Hint: "Run noon " + cmd.Name + " to list jobs"}
		}
	}
	changed, e := app.SetJobsPaused(cfg.StateDir, cfg.Project, args, paused, time.Now())
	if e != nil {
		return stateError(e)
	}
	if cmd.Args().Len() > 1 {
		if cmd.Bool("json") {
			slices.Sort(args)
			return emit(cmd, map[string]any{"jobs": args, "paused": paused, "changed": changed})
		}
		fmt.Fprintf(cmd.Writer, "%s: %s (%d changed).\n", cmd.Name, strings.Join(args, ", "), changed)
		return nil
	}
	if cmd.Bool("json") {
		return emit(cmd, map[string]any{"job": args[0], "paused": paused})
	}
	if paused {
		fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("Paused %s. Background runs stop until noon resume %s.\n", args[0], args[0])))
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

// privateFile returns the project's private config path. Private jobs skip
// approval, so they live only where repository content cannot place them.
func ensureUserConfigDir(project string) error {
	if err := os.MkdirAll(project, 0o700); err != nil {
		return err
	}
	return os.Chmod(project, 0o700)
}

func privateFile(project string) (string, error) {
	path, err := config.LocalPath(project)
	if err != nil {
		return "", configError(err)
	}
	if path == "" {
		return "", usageError("private jobs need a Git checkout; they live in .git/noon.yaml", "Use --shared to write noon.yaml")
	}
	return path, nil
}

// displayPath shortens paths inside the project, such as .git/noon.yaml.
func displayPath(project, path string) string {
	if rel, err := filepath.Rel(project, path); err == nil && filepath.IsLocal(rel) {
		return rel
	}
	return path
}

// refuseSymlink keeps edits from replacing a link, which may be shared by other checkouts.
func refuseSymlink(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	target, _ := os.Readlink(path)
	return &Error{Code: "config_edit", Message: fmt.Sprintf("%s is a symlink to %s; Noon does not edit symlinked config", path, target), Hint: "Edit the link target by hand, or use noon add --temporary for a bounded follow-up"}
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := refuseSymlink(path); err != nil {
		return err
	}
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
