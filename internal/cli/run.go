package cli

import (
	"context"
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
	spec.Stdout, spec.Stderr, spec.ForceKill = cmd.Writer, cmd.ErrWriter, force
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
	if cmd.Bool("json") {
		if err := emit(cmd, map[string]any{"run": map[string]any{
			"job": name, "status": status, "exit_code": result.ExitCode, "signal": result.Signal,
			"started_at": result.StartedAt, "finished_at": result.FinishedAt, "duration_ms": elapsed.Milliseconds(),
		}}); err != nil {
			return err
		}
	}
	return runExit(result.ExitCode)
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
