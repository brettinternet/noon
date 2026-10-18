package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
	urfave "github.com/urfave/cli/v3"
)

// A completed run has already rendered its result, even when its exit is nonzero.
type runExit int

func (e runExit) Error() string { return fmt.Sprintf("job exited %d", e) }

func runJob(ctx context.Context, cmd *urfave.Command) error {
	if cmd.Args().Len() > 1 {
		return usageError("run accepts only one job name", "Run noon run to list jobs")
	}
	queueMode, err := runQueueMode(cmd)
	if err != nil {
		return err
	}
	if cmd.Args().Len() == 0 && queueMode != "" {
		return usageError("queued run options require a job name", "Run noon run to list jobs")
	}
	if cmd.IsSet("data") && cmd.Args().Len() == 0 {
		return usageError("--data requires a job name", "Run noon run NAME --data JSON")
	}
	if cmd.IsSet("key") && queueMode == "" {
		return usageError("--key requires --detach, --in, or --at", "Use --key with a queued run")
	}
	if cmd.IsSet("key") && !app.ValidIdempotencyKey(cmd.String("key")) {
		return usageError("--key must be 1–200 characters of [A-Za-z0-9._:-]", "Choose a key using letters, digits, dot, underscore, colon, or hyphen")
	}
	var eventData []byte
	if cmd.IsSet("data") {
		eventData, err = readEventData(cmd, cmd.String("data"))
		if err != nil {
			return err
		}
	}
	values, err := input(cmd)
	if err != nil {
		return err
	}
	cfg, err := config.Load(values)
	if err != nil {
		return configError(err)
	}
	names := make([]string, 0, len(cfg.Jobs))
	for name := range cfg.Jobs {
		names = append(names, name)
	}
	slices.Sort(names)
	if cmd.Args().Len() == 0 {
		jobs := []map[string]string{}
		for _, name := range names {
			job := cfg.Jobs[name]
			if job.Enabled {
				jobs = append(jobs, map[string]string{"name": name, "description": job.Description})
			}
		}
		if cmd.Bool("json") {
			return emit(cmd, map[string]any{"jobs": jobs})
		}
		if len(jobs) == 0 {
			fmt.Fprintln(cmd.Writer, guidance(cmd, "No runnable jobs. Next: noon add tests -- go test ./..."))
		}
		for _, job := range jobs {
			fmt.Fprintf(cmd.Writer, "%s  %s\n", job["name"], cell(job["description"], 60))
		}
		if len(jobs) > 0 {
			fmt.Fprint(cmd.Writer, guidance(cmd, fmt.Sprintf("Next: noon run %s\n", jobs[0]["name"])))
		}
		return nil
	}
	name := cmd.Args().First()
	if queueMode != "" {
		now := time.Now()
		location, err := time.LoadLocation(cfg.Timezone)
		if err != nil {
			return configError(err)
		}
		dueAt, err := queueDueAt(queueMode, cmd.String("in"), cmd.String("at"), now, location)
		if err != nil {
			return usageError(err.Error(), "Use --in 2h or --at '2026-10-16 09:00'")
		}
		result, err := app.QueueRun(cfg, name, cmd.String("key"), dueAt, now, time.Now, queueMode == "detach", "cli", app.QueueOptions{EventData: eventData, EventSource: "cli"})
		if err != nil {
			return queueError(err)
		}
		if cmd.Bool("json") {
			output := map[string]any{"run": result.Run, "created": result.Created, "scheduler": result.Scheduler}
			if !result.Created {
				output["reason"] = result.Reason
			}
			if result.CooldownUntil != "" {
				output["cooldown_until"] = result.CooldownUntil
			}
			return emit(cmd, output)
		}
		printQueuedRun(cmd, result, now, location, queueMode == "detach")
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
	job, ok := cfg.Jobs[name]
	if !ok {
		return app.MissingJobError(name, "run", cfg.Jobs)
	}
	if !job.MatchesPlatform(cfg.Platform) {
		return app.UnsupportedPlatformError(job, cfg.Platform)
	}
	if !job.Enabled {
		return &Error{Code: "disabled", Message: fmt.Sprintf("job %q is disabled", name), Hint: fmt.Sprintf("Enable it in %q", job.Source)}
	}
	if err := app.CheckRecursion(cfg.Project, name, os.Environ()); err != nil {
		return err
	}
	now := time.Now()
	ownerStart, err := runner.ProcessStartIdentity(os.Getpid())
	if err != nil {
		return &Error{Code: "error", Message: fmt.Sprintf("identify Noon process: %v", err)}
	}
	state, err := store.Open(cfg.StateDir)
	if err != nil {
		return stateError(err)
	}
	defer state.Close()
	projectID, err := state.RegisterProject(cfg.Project, now)
	if err != nil {
		return stateError(err)
	}
	if err := state.Recover(now, 10*time.Second); err != nil {
		return stateError(err)
	}
	lastSuccess, hasSuccess, err := state.LastSuccess(projectID, name)
	if err != nil {
		return stateError(err)
	}
	sessionID := ""
	if job.AgentSessionIDFlag != "" {
		sessionID, err = app.NewAgentSessionID()
		if err != nil {
			return &Error{Code: "error", Message: err.Error()}
		}
	}
	run, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: name, Trigger: "manual", Digest: job.Digest, Now: now, OwnerPID: os.Getpid(), OwnerStart: ownerStart, Depth: app.RequestDepth(os.Environ()), EventData: eventData, EventSource: "cli", AgentSessionID: sessionID})
	if err != nil {
		return stateError(err)
	}
	log, err := state.RunLog(projectID, run.ID)
	if err != nil {
		code := 125
		hook, finishErr := app.FinishRunWithHook(state, run, cfg.Project, job, app.JobEnvironment(cfg.Project, job, os.Environ()), store.Outcome{Status: "failed", FinishedAt: time.Now(), ExitCode: &code, Error: err.Error()})
		if finishErr != nil {
			return stateError(errors.Join(err, finishErr))
		}
		printHookResult(cmd.ErrWriter, hook)
		return stateError(err)
	}
	if err := state.SetRunLog(run.ID, log.Path()); err != nil {
		logBytes, truncated, closeErr := log.Close()
		code := 125
		hook, finishErr := app.FinishRunWithHook(state, run, cfg.Project, job, app.JobEnvironment(cfg.Project, job, os.Environ()), store.Outcome{Status: "failed", FinishedAt: time.Now(), ExitCode: &code, Error: errors.Join(err, closeErr).Error(), LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated})
		if finishErr != nil {
			return stateError(errors.Join(err, closeErr, finishErr))
		}
		printHookResult(cmd.ErrWriter, hook)
		return stateError(err)
	}
	// Register before starting the child. Count OS signals separately from parent
	// cancellation so NotifyContext delivery order cannot turn the first into KILL.
	signals := make(chan os.Signal, 2)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(signals)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	force := make(chan struct{})
	done := make(chan struct{})
	stopped := make(chan struct{})
	pollErrors := make(chan error, 1)
	defer func() { close(done); <-stopped }()
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		tickerC := ticker.C
		count := 0
		for {
			select {
			case <-done:
				return
			case <-signals:
				count++
				if count == 1 {
					cancel()
				} else {
					close(force)
					return
				}
			case <-tickerC:
				requested, err := state.CancellationRequested(run.ID)
				if err != nil {
					pollErrors <- err
					cancel()
					ticker.Stop()
					tickerC = nil
					continue
				}
				if requested {
					cancel()
					ticker.Stop()
					tickerC = nil
				}
			}
		}
	}()
	spec, startErr := app.RunSpecForExecution(cfg.Project, job, os.Environ(), run.AgentSessionID.String)
	if startErr == nil {
		spec.Env = setRunEnv(spec.Env, "NOON_RUN_ID", strconv.FormatInt(run.ID, 10))
		spec.Env = setRunEnv(spec.Env, "NOON_DEPTH", strconv.Itoa(run.Depth+1))
		if eventPath, ok := state.EventFilePath(run); ok {
			spec.Env = setRunEnv(spec.Env, "NOON_EVENT_FILE", eventPath)
		}
		value := ""
		if hasSuccess {
			if at, err := time.Parse(store.TimestampLayout, lastSuccess); err == nil {
				value = at.UTC().Format(time.RFC3339Nano)
			}
		}
		spec.Env = setRunEnv(spec.Env, "NOON_LAST_SUCCESS_AT", value)
	}
	spec.Stdout, spec.Stderr, spec.ForceKill = cmd.Writer, cmd.ErrWriter, force
	spec.Log = log
	spec.OnStart = func(_ int, pgid int, leaderStart string) error {
		return state.SetProcessIdentity(run.ID, pgid, leaderStart)
	}
	if cmd.Bool("json") {
		spec.Stdout = cmd.ErrWriter
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
	style := styleFor(cmd, cmd.ErrWriter)
	displayName := style.render(cell(name, -1), "heading")
	fmt.Fprintf(cmd.ErrWriter, "%s %s · run %d · %s\n", style.render("▶", "running"), displayName, run.ID, cell(command, 240))
	var result runner.Result
	if startErr != nil {
		now := time.Now()
		result = runner.Result{ExitCode: 125, StartError: startErr, StartedAt: now, FinishedAt: now}
	} else {
		result = runner.Run(runCtx, spec)
	}
	var cancellationPollError error
	select {
	case cancellationPollError = <-pollErrors:
	default:
	}
	requested, requestErr := state.CancellationRequested(run.ID)
	if requestErr != nil && cancellationPollError == nil {
		cancellationPollError = requestErr
	}
	if requested {
		result.Cancelled = true
		result.ExitCode = 130
	}
	status, summary := "succeeded", style.render("✓", "succeeded")+" "+displayName+" succeeded in "
	elapsed := result.FinishedAt.Sub(result.StartedAt)
	switch {
	case result.TimedOut:
		status, summary = "timed_out", style.render("✗", "timed_out")+" "+displayName+" "+style.render("timed out", "timed_out")+" after "
	case result.Cancelled:
		status, summary = "cancelled", style.render("✗", "cancelled")+" "+displayName+" "+style.render("cancelled", "cancelled")+" after "
	case job.NoopExitCode != 0 && result.StartError == nil && result.ExitCode == job.NoopExitCode:
		status, summary = "noop", fmt.Sprintf("%s %s noop: exit %d after ", style.render("–", "noop"), displayName, result.ExitCode)
	case result.ExitCode != 0:
		status, summary = "failed", fmt.Sprintf("%s %s %s: exit %d after ", style.render("✗", "failed"), displayName, style.render("failed", "failed"), result.ExitCode)
	}
	detail := ""
	if result.StartError != nil {
		detail = fmt.Sprintf(" (%s)", strconv.Quote(result.StartError.Error()))
	}
	fmt.Fprintf(cmd.ErrWriter, "%s%s%s\n", summary, elapsed.Round(time.Millisecond), detail)
	logBytes, logTruncated, logErr := log.Close()
	outcomeError := ""
	if result.StartError != nil {
		outcomeError = result.StartError.Error()
	}
	if logErr != nil {
		if outcomeError != "" {
			outcomeError += "; "
		}
		outcomeError += "log write failed: " + logErr.Error()
		fmt.Fprintf(cmd.ErrWriter, "run log failed: %v\n", logErr)
	}
	if cancellationPollError != nil {
		if outcomeError != "" {
			outcomeError += "; "
		}
		outcomeError += "cancellation poll failed: " + cancellationPollError.Error()
	}
	hookResult, finishErr := app.FinishRunWithHook(state, run, cfg.Project, job, spec.Env, store.Outcome{Status: status, FinishedAt: result.FinishedAt, ExitCode: &result.ExitCode, Signal: result.Signal, Error: outcomeError, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: logTruncated})
	if finishErr != nil {
		if hookResult == nil {
			return stateError(finishErr)
		}
		fmt.Fprintf(cmd.ErrWriter, "hook recording failed: %v\n", finishErr)
	}
	printHookResult(cmd.ErrWriter, hookResult)
	if cancellationPollError != nil {
		return stateError(cancellationPollError)
	}
	if cmd.Bool("json") {
		stored, err := state.RunByID(run.ID)
		if err != nil {
			return stateError(err)
		}
		if err := emit(cmd, map[string]any{"run": app.RunFromStore(state, stored)}); err != nil {
			return err
		}
	}
	if status == "noop" {
		return runExit(0)
	}
	return runExit(result.ExitCode)
}

func printHookResult(out io.Writer, result *app.HookResult) {
	if result == nil {
		return
	}
	detail := result.Status
	switch result.Status {
	case "ok":
		detail = "ok"
	case "failed":
		if result.ExitCode != nil {
			detail = fmt.Sprintf("exit %d", *result.ExitCode)
		}
	case "timed_out":
		detail = "timed out"
	}
	fmt.Fprintf(out, "%s hook: %s\n", result.Event, detail)
}

func readEventData(cmd *urfave.Command, value string) ([]byte, error) {
	var reader io.Reader
	if value == "-" {
		reader = cmd.Reader
	} else if strings.HasPrefix(value, "@") {
		path := strings.TrimPrefix(value, "@")
		if path == "" {
			return nil, usageError("--data @FILE requires a file path", "Pass a JSON value, @FILE, or - for stdin")
		}
		file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err != nil {
			return nil, usageError(fmt.Sprintf("read --data file %q: %v", path, err), "Pass a readable regular file")
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			_ = file.Close()
			if err != nil {
				return nil, usageError(fmt.Sprintf("read --data file %q: %v", path, err), "Pass a readable regular file")
			}
			return nil, usageError(fmt.Sprintf("--data file %q must be a regular file", path), "Pass a readable regular file")
		}
		reader = file
		defer file.Close()
	} else {
		data := []byte(value)
		if err := app.ValidateEventData(data); err != nil {
			return nil, err
		}
		return data, nil
	}
	data, err := io.ReadAll(io.LimitReader(reader, store.MaxEventBytes+1))
	if err != nil {
		return nil, usageError(fmt.Sprintf("read --data: %v", err), "Pass a readable JSON input")
	}
	if err := app.ValidateEventData(data); err != nil {
		return nil, err
	}
	return data, nil
}

func setRunEnv(env []string, key, value string) []string {
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || name != key {
			filtered = append(filtered, entry)
		}
	}
	if value != "" {
		filtered = append(filtered, key+"="+value)
	}
	return filtered
}

func stateError(err error) error { return app.StateError(err) }
