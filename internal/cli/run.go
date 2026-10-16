package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/brettinternet/bump/internal/app"
	"github.com/brettinternet/bump/internal/config"
	"github.com/brettinternet/bump/internal/runner"
	"github.com/brettinternet/bump/internal/store"
	urfave "github.com/urfave/cli/v3"
)

// A completed run has already rendered its result, even when its exit is nonzero.
type runExit int

func (e runExit) Error() string { return fmt.Sprintf("job exited %d", e) }

func runJob(ctx context.Context, cmd *urfave.Command) error {
	if cmd.Args().Len() > 1 {
		return usageError("run accepts only one job name", "Run bump run to list jobs")
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
			fmt.Fprintln(cmd.Writer, "No runnable jobs. Add a job to bump.yaml.")
		}
		for _, job := range jobs {
			fmt.Fprintf(cmd.Writer, "%s  %s\n", job["name"], cell(job["description"], 60))
		}
		if len(jobs) > 0 {
			fmt.Fprintf(cmd.Writer, "Next: bump run %s\n", jobs[0]["name"])
		}
		return nil
	}
	name := cmd.Args().First()
	job, ok := cfg.Jobs[name]
	if !ok {
		hint, best := "Run bump run to list jobs", 3
		for _, candidate := range names {
			if distance := editDistance(name, candidate); distance < best {
				hint, best = "Did you mean bump run "+candidate+"?", distance
			}
		}
		return &Error{Code: "not_found", Message: fmt.Sprintf("job %q not found", name), Hint: hint}
	}
	if !job.Enabled {
		return &Error{Code: "disabled", Message: fmt.Sprintf("job %q is disabled", name), Hint: fmt.Sprintf("Enable it in %q", job.Source)}
	}
	now := time.Now()
	ownerStart, err := runner.ProcessStartIdentity(os.Getpid())
	if err != nil {
		return &Error{Code: "error", Message: fmt.Sprintf("identify Bump process: %v", err)}
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
	run, err := state.CreateRun(store.NewRun{ProjectID: projectID, Job: name, Trigger: "manual", Digest: job.Digest, Now: now, OwnerPID: os.Getpid(), OwnerStart: ownerStart})
	if err != nil {
		return stateError(err)
	}
	log, err := state.RunLog(projectID, run.ID)
	if err != nil {
		code := 125
		finishErr := state.FinishRun(run.ID, projectID, name, store.Outcome{Status: "failed", FinishedAt: time.Now(), ExitCode: &code, Error: err.Error()})
		if finishErr != nil {
			return stateError(errors.Join(err, finishErr))
		}
		return stateError(err)
	}
	if err := state.SetRunLog(run.ID, log.Path()); err != nil {
		logBytes, truncated, closeErr := log.Close()
		code := 125
		finishErr := state.FinishRun(run.ID, projectID, name, store.Outcome{Status: "failed", FinishedAt: time.Now(), ExitCode: &code, Error: errors.Join(err, closeErr).Error(), LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated})
		if finishErr != nil {
			return stateError(errors.Join(err, finishErr))
		}
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
	defer func() { close(done); <-stopped }()
	go func() {
		defer close(stopped)
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
			}
		}
	}()
	spec, startErr := app.RunSpec(cfg.Project, job, os.Environ())
	if startErr == nil {
		spec.Env = setRunEnv(spec.Env, "BUMP_RUN_ID", strconv.FormatInt(run.ID, 10))
		value := ""
		if hasSuccess {
			if at, err := time.Parse(store.TimestampLayout, lastSuccess); err == nil {
				value = at.UTC().Format(time.RFC3339Nano)
			}
		}
		spec.Env = setRunEnv(spec.Env, "BUMP_LAST_SUCCESS_AT", value)
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
	fmt.Fprintf(cmd.ErrWriter, "▶ %s · %s\n", name, cell(command, 240))
	var result runner.Result
	if startErr != nil {
		now := time.Now()
		result = runner.Result{ExitCode: 125, StartError: startErr, StartedAt: now, FinishedAt: now}
	} else {
		result = runner.Run(runCtx, spec)
	}
	status, summary := "succeeded", "✓ "+name+" succeeded in "
	elapsed := result.FinishedAt.Sub(result.StartedAt)
	switch {
	case result.TimedOut:
		status, summary = "timed_out", "✗ "+name+" timed out after "
	case result.Cancelled:
		status, summary = "cancelled", "✗ "+name+" cancelled after "
	case result.ExitCode != 0:
		status, summary = "failed", fmt.Sprintf("✗ %s failed: exit %d after ", name, result.ExitCode)
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
	if err := state.FinishRun(run.ID, projectID, name, store.Outcome{Status: status, FinishedAt: result.FinishedAt, ExitCode: &result.ExitCode, Signal: result.Signal, Error: outcomeError, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: logTruncated}); err != nil {
		return stateError(err)
	}
	if cmd.Bool("json") {
		stored, err := state.RunByID(run.ID)
		if err != nil {
			return stateError(err)
		}
		if err := emit(cmd, map[string]any{"run": app.RunFromStore(stored)}); err != nil {
			return err
		}
	}
	return runExit(result.ExitCode)
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

func stateError(err error) error {
	var newer *store.StateTooNewError
	if errors.As(err, &newer) {
		return &Error{Code: "state_too_new", Message: newer.Error(), Hint: "Upgrade Bump before using this state database."}
	}
	var running *store.AlreadyRunningError
	if errors.As(err, &running) {
		return &Error{Code: "already_running", Message: fmt.Sprintf("job is already running as run %d, started at %s", running.RunID, running.StartedAt), Hint: fmt.Sprintf("Follow it: bump logs %d --follow", running.RunID)}
	}
	return &Error{Code: "error", Message: err.Error()}
}

func editDistance(a, b string) int {
	// Adjacent transpositions count as one edit (tset -> tests).
	var beforePrevious []int
	previous := make([]int, len(b)+1)
	for i := range previous {
		previous[i] = i
	}
	for i := range len(a) {
		row := make([]int, len(b)+1)
		row[0] = i + 1
		for j := range len(b) {
			cost := 0
			if a[i] != b[j] {
				cost = 1
			}
			row[j+1] = min(row[j]+1, previous[j+1]+1, previous[j]+cost)
			if i > 0 && j > 0 && a[i] == b[j-1] && a[i-1] == b[j] {
				row[j+1] = min(row[j+1], beforePrevious[j-1]+1)
			}
		}
		beforePrevious, previous = previous, row
	}
	return previous[len(b)]
}
