package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
	"golang.org/x/sys/unix"
)

// ServeOptions isolates time and ticks for deterministic loop tests.
type ServeOptions struct {
	StateDir        string
	StateDirDefault string
	UserConfigDir   string
	Version         string
	Environment     []string
	Now             func() time.Time
	Ticks           <-chan time.Time
	KillGrace       time.Duration
	Logger          io.Writer
	IgnoreSignals   bool
	OnTick          func()
}

type SchedulerRunningError struct {
	PID       int
	StartedAt string
}

func (e *SchedulerRunningError) Error() string {
	return fmt.Sprintf("scheduler already running as PID %d, started at %s", e.PID, e.StartedAt)
}

type fileStamp struct {
	path    string
	info    os.FileInfo
	errText string
}

type projectConfig struct {
	project         store.EnabledProject
	config          *config.Config
	configSelection string
	stamps          []fileStamp
	checkedAt       time.Time
}

type runControl struct {
	projectID int64
	cancel    context.CancelFunc
	mu        sync.Mutex
	status    string
}

func (r *runControl) stop(status string) {
	r.mu.Lock()
	r.status = status
	r.mu.Unlock()
	r.cancel()
}

func (r *runControl) stopStatus() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

// Serve supervises every enabled project's approved schedules until cancelled.
func Serve(ctx context.Context, options ServeOptions) error {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logger == nil {
		options.Logger = os.Stderr
	}
	if options.KillGrace <= 0 {
		options.KillGrace = 10 * time.Second
	}
	if options.Environment == nil {
		options.Environment = os.Environ()
	}
	if !filepath.IsAbs(options.StateDir) {
		return fmt.Errorf("state directory must be absolute: %q", options.StateDir)
	}
	state, err := store.Open(options.StateDir)
	if err != nil {
		return err
	}
	defer state.Close()
	lock, err := acquireLock(options.StateDir)
	if err != nil {
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		record, exists, infoErr := state.SchedulerInfo()
		if infoErr != nil {
			return infoErr
		}
		if exists {
			return &SchedulerRunningError{PID: record.PID, StartedAt: record.StartedAt}
		}
		pid, started := readLockOwner(filepath.Join(options.StateDir, "scheduler.lock"))
		return &SchedulerRunningError{PID: pid, StartedAt: started}
	}
	defer lock.close()

	pid := os.Getpid()
	identity, err := runner.ProcessStartIdentity(pid)
	if err != nil {
		return fmt.Errorf("identify scheduler process: %w", err)
	}
	start := options.Now()
	startedAt := store.FormatTime(start)
	if err := lock.writeOwner(pid, identity, startedAt); err != nil {
		return err
	}
	record := store.SchedulerRecord{PID: pid, StartIdentity: identity, Version: options.Version, StartedAt: startedAt, HeartbeatAt: startedAt}
	if err := state.SetScheduler(record); err != nil {
		return fmt.Errorf("record scheduler owner: %w", err)
	}
	owned := true
	defer func() {
		if owned {
			_ = state.ClearScheduler(pid, identity)
		}
	}()
	if err := state.Recover(start, options.KillGrace); err != nil {
		return fmt.Errorf("recover stale runs: %w", err)
	}

	logger := schedulerLogger{writer: options.Logger, now: options.Now}
	configs := map[int64]*projectConfig{}
	initial, err := state.ReadSchedulerSnapshot()
	if err != nil {
		return err
	}
	for _, project := range initial.Projects {
		cached := &projectConfig{project: project}
		configs[project.ID] = cached
		if err := reloadProject(state, cached, options, start, &logger, true); err != nil {
			return err
		}
	}
	logger.event("scheduler started state_dir=%q projects=%d", options.StateDir, len(initial.Projects))

	serveContext, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()
	forceKill := make(chan struct{})
	var forceOnce sync.Once
	signals := make(chan os.Signal, 2)
	if !options.IgnoreSignals {
		signalDone := make(chan struct{})
		signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
		defer func() {
			signal.Stop(signals)
			close(signalDone)
		}()
		go func() {
			count := 0
			for {
				select {
				case <-signalDone:
					return
				case <-signals:
					count++
					if count == 1 {
						cancelServe()
					} else {
						forceOnce.Do(func() { close(forceKill) })
						return
					}
				}
			}
		}()
	}

	var ticker *time.Ticker
	ticks := options.Ticks
	if ticks == nil {
		ticker = time.NewTicker(time.Second)
		defer ticker.Stop()
		ticks = ticker.C
	}
	completed := make(chan int64, 8)
	running := map[int64]*runControl{}
	stopped := false
	defer func() {
		if stopped {
			return
		}
		for _, control := range running {
			control.stop("interrupted")
		}
		for len(running) > 0 {
			id := <-completed
			delete(running, id)
			drainFinished(completed, running)
		}
	}()
	for {
		drainFinished(completed, running)
		select {
		case <-ctx.Done():
			goto shutdown
		case <-serveContext.Done():
			goto shutdown
		case _, ok := <-ticks:
			if !ok {
				goto shutdown
			}
		case id := <-completed:
			delete(running, id)
			continue
		}
		now := options.Now()
		snapshot, err := state.ReadSchedulerSnapshot()
		if err != nil {
			return err
		}
		activeProjects := make(map[int64]bool, len(snapshot.Projects))
		for _, project := range snapshot.Projects {
			activeProjects[project.ID] = true
			cached := configs[project.ID]
			if cached == nil {
				cached = &projectConfig{project: project}
				configs[project.ID] = cached
			}
			selection := configSelection(project)
			selectionChanged := cached.configSelection != selection
			cached.project = project
			if selectionChanged {
				if err := reloadProject(state, cached, options, now, &logger, true); err != nil {
					return err
				}
			} else if !cached.checkedAt.IsZero() && now.Sub(cached.checkedAt) >= 2*time.Second {
				if err := reloadProject(state, cached, options, now, &logger, false); err != nil {
					return err
				}
			} else if cached.config == nil && cached.checkedAt.IsZero() {
				if err := reloadProject(state, cached, options, now, &logger, true); err != nil {
					return err
				}
			}
		}
		for id := range configs {
			if !activeProjects[id] {
				delete(configs, id)
			}
		}
		for _, control := range running {
			if !activeProjects[control.projectID] {
				control.stop("cancelled")
			}
		}

		started, err := state.ScheduleTick(now, pid, identity,
			func(snapshot store.SchedulerSnapshot) (store.SchedulerPlan, error) {
				jobs, err := makeJobInputs(snapshot, configs)
				if err != nil {
					return store.SchedulerPlan{}, err
				}
				states, err := plannerStates(snapshot.States)
				if err != nil {
					return store.SchedulerPlan{}, err
				}
				runs, err := plannerRuns(snapshot.Runs)
				if err != nil {
					return store.SchedulerPlan{}, err
				}
				upsert, remove, create := Due(now, jobs, states, runs)
				plan := store.SchedulerPlan{}
				for _, value := range upsert {
					plan.Upsert = append(plan.Upsert, store.JobStateChange{ProjectID: value.Key.Project, Job: value.Key.Job, ScheduleKey: value.ScheduleKey, NextAt: value.NextAt})
				}
				for _, value := range remove {
					plan.Remove = append(plan.Remove, store.JobStateKey{ProjectID: value.Project, Job: value.Job})
				}
				for _, value := range create {
					cached := configs[value.Key.Project]
					job := cached.config.Jobs[value.Key.Job]
					plan.Create = append(plan.Create, store.ScheduledRunInput{ProjectID: value.Key.Project, Job: value.Key.Job, Status: value.Status, Trigger: value.Trigger, Digest: job.Digest, ScheduledAt: value.ScheduledAt, DueAt: value.DueAt})
				}
				return plan, nil
			},
			func(snapshot store.SchedulerSnapshot) (store.DispatchPlan, error) {
				jobs, err := makeJobInputs(snapshot, configs)
				if err != nil {
					return store.DispatchPlan{}, err
				}
				runs, err := plannerRuns(snapshot.Runs)
				if err != nil {
					return store.DispatchPlan{}, err
				}
				start, expire, cancel := Dispatch(now, jobs, runs)
				return store.DispatchPlan{Start: start, Expire: expire, Cancel: cancel}, nil
			},
		)
		for _, run := range started {
			cached := configs[run.ProjectID]
			if cached == nil || cached.config == nil {
				return fmt.Errorf("scheduled run %d has no validated configuration snapshot", run.ID)
			}
			job, ok := cached.config.Jobs[run.Job]
			if !ok {
				return fmt.Errorf("scheduled run %d refers to missing job %q", run.ID, run.Job)
			}
			control := &runControl{projectID: run.ProjectID}
			runContext, cancel := context.WithCancel(context.Background())
			control.cancel = cancel
			running[run.ID] = control
			logger.event("run started project=%q job=%q run=%d trigger=%s", cached.project.Path, run.Job, run.ID, run.Trigger)
			go executeScheduledRun(runContext, state, run, cached.project.Path, job, options, forceKill, control, &logger, completed)
		}
		if err != nil {
			return err
		}
		if options.OnTick != nil {
			options.OnTick()
		}
	}

shutdown:
	logger.event("scheduler shutting down")
	for _, control := range running {
		control.stop("interrupted")
	}
	for len(running) > 0 {
		id := <-completed
		delete(running, id)
		drainFinished(completed, running)
	}
	if err := state.ClearScheduler(pid, identity); err != nil {
		return fmt.Errorf("clear scheduler record: %w", err)
	}
	owned = false
	stopped = true
	logger.event("scheduler stopped")
	return nil
}

type schedulerLogger struct {
	writer io.Writer
	now    func() time.Time
	mu     sync.Mutex
}

func (l *schedulerLogger) event(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	line = strings.NewReplacer("\n", "\\n", "\r", "\\r").Replace(line)
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = fmt.Fprintf(l.writer, "%s %s\n", l.now().Local().Format("2006-01-02 15:04:05"), line)
}

type schedulerLock struct{ file *os.File }

func (l *schedulerLock) close() {
	_ = unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	_ = l.file.Close()
}

func (l *schedulerLock) writeOwner(pid int, identity, startedAt string) error {
	if err := l.file.Truncate(0); err != nil {
		return fmt.Errorf("write scheduler lock owner: %w", err)
	}
	if _, err := l.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("write scheduler lock owner: %w", err)
	}
	if _, err := fmt.Fprintf(l.file, "%d %s %s\\n", pid, identity, startedAt); err != nil {
		return fmt.Errorf("write scheduler lock owner: %w", err)
	}
	if err := l.file.Sync(); err != nil {
		return fmt.Errorf("sync scheduler lock owner: %w", err)
	}
	return nil
}

func acquireLock(stateDir string) (*schedulerLock, error) {
	path := filepath.Join(stateDir, "scheduler.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open scheduler lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure scheduler lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &schedulerLock{file: file}, nil
}

func readLockOwner(path string) (int, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, "unknown"
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, "unknown"
	}
	pid, _ := strconv.Atoi(fields[0])
	return pid, fields[2]
}

func reloadProject(state *store.Store, cached *projectConfig, options ServeOptions, now time.Time, logger *schedulerLogger, force bool) error {
	paths := configPaths(cached.project, options.UserConfigDir)
	stamps := statPaths(paths)
	changed := force || !sameStamps(cached.stamps, stamps)
	if !changed {
		cached.checkedAt = now
		return nil
	}
	input := config.Input{CWD: cached.project.Path, UserConfigDir: options.UserConfigDir, StateDirDefault: options.StateDirDefault}
	project := cached.project.Path
	input.Project = &project
	if cached.project.ConfigFile.Valid {
		file := cached.project.ConfigFile.String
		input.File = &file
	}
	loaded, err := config.Load(input)
	cached.stamps = stamps
	cached.checkedAt = now
	if err != nil {
		if setErr := state.SetProjectConfigError(cached.project.ID, err.Error()); setErr != nil {
			return fmt.Errorf("record config error for %q: %w", cached.project.Path, setErr)
		}
		logger.event("config error project=%q error=%q", cached.project.Path, err.Error())
		return nil
	}
	cached.config = &loaded
	cached.configSelection = configSelection(cached.project)
	if err := state.SetProjectConfigError(cached.project.ID, ""); err != nil {
		return fmt.Errorf("clear config error for %q: %w", cached.project.Path, err)
	}
	return nil
}

func configSelection(project store.EnabledProject) string {
	if project.ConfigFile.Valid {
		return "explicit:" + project.ConfigFile.String
	}
	return "automatic"
}

func configPaths(project store.EnabledProject, userConfigDir string) []string {
	if project.ConfigFile.Valid {
		return []string{project.Path, project.ConfigFile.String}
	}
	return []string{
		project.Path,
		filepath.Join(userConfigDir, "noon", "config.yaml"),
		filepath.Join(project.Path, "noon.yaml"),
		filepath.Join(project.Path, ".noon.yaml"),
	}
}

func statPaths(paths []string) []fileStamp {
	stamps := make([]fileStamp, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		stamp := fileStamp{path: path, info: info}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			stamp.errText = err.Error()
		}
		stamps = append(stamps, stamp)
	}
	return stamps
}

func sameStamps(a, b []fileStamp) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].path != b[i].path || a[i].errText != b[i].errText || (a[i].info == nil) != (b[i].info == nil) {
			return false
		}
		if a[i].info != nil && (a[i].info.Size() != b[i].info.Size() || !a[i].info.ModTime().Equal(b[i].info.ModTime()) || !os.SameFile(a[i].info, b[i].info)) {
			return false
		}
	}
	return true
}

func makeJobInputs(snapshot store.SchedulerSnapshot, configs map[int64]*projectConfig) ([]JobInput, error) {
	jobs := make([]JobInput, 0)
	for _, project := range snapshot.Projects {
		cached := configs[project.ID]
		if cached == nil || cached.config == nil || cached.configSelection != configSelection(project) {
			continue
		}
		location, err := time.LoadLocation(cached.config.Timezone)
		if err != nil {
			return nil, fmt.Errorf("load timezone %q for %q: %w", cached.config.Timezone, project.Path, err)
		}
		for name, job := range cached.config.Jobs {
			active := job.Enabled && (!job.Shared || project.Approvals[name] == job.Digest)
			jobs = append(jobs, JobInput{Key: JobKey{Project: project.ID, Job: name}, Schedule: job.Schedule, Location: location, Active: active, Missed: job.Missed, Overlap: job.Overlap})
		}
	}
	return jobs, nil
}

func plannerStates(states []store.PersistedJobState) ([]JobState, error) {
	result := make([]JobState, 0, len(states))
	for _, state := range states {
		next, err := time.Parse(store.TimestampLayout, state.NextAt)
		if err != nil {
			return nil, fmt.Errorf("parse next time for project %d job %q: %w", state.ProjectID, state.Job, err)
		}
		result = append(result, JobState{Key: JobKey{Project: state.ProjectID, Job: state.Job}, ScheduleKey: state.ScheduleKey, NextAt: next})
	}
	return result, nil
}

func plannerRuns(runs []store.ScheduledRun) ([]Run, error) {
	result := make([]Run, 0, len(runs))
	for _, stored := range runs {
		var due time.Time
		if stored.DueAt.Valid {
			parsed, err := time.Parse(store.TimestampLayout, stored.DueAt.String)
			if err != nil {
				return nil, fmt.Errorf("parse due time for run %d: %w", stored.ID, err)
			}
			due = parsed
		}
		result = append(result, Run{ID: stored.ID, Key: JobKey{Project: stored.ProjectID, Job: stored.Job}, Status: stored.Status, DueAt: due})
	}
	return result, nil
}

func executeScheduledRun(ctx context.Context, state *store.Store, run store.Run, project string, job config.Job, options ServeOptions, forceKill <-chan struct{}, control *runControl, logger *schedulerLogger, completed chan<- int64) {
	defer func() { completed <- run.ID }()
	fail := func(err error) {
		code := 125
		_ = state.FinishRun(run.ID, run.ProjectID, run.Job, store.Outcome{Status: "failed", FinishedAt: time.Now(), ExitCode: &code, Error: err.Error()})
		logger.event("run finished project=%q job=%q run=%d status=failed error=%q", project, run.Job, run.ID, err.Error())
	}
	log, err := state.RunLog(run.ProjectID, run.ID)
	if err != nil {
		fail(err)
		return
	}
	if err := state.SetRunLog(run.ID, log.Path()); err != nil {
		_, _, _ = log.Close()
		fail(err)
		return
	}
	lastSuccess, hasSuccess, err := state.LastSuccess(run.ProjectID, run.Job)
	if err != nil {
		_, _, _ = log.Close()
		fail(err)
		return
	}
	spec, err := app.RunSpec(project, job, options.Environment)
	if err != nil {
		_, _, _ = log.Close()
		fail(err)
		return
	}
	spec.Env = setEnvironment(spec.Env, "NOON_RUN_ID", strconv.FormatInt(run.ID, 10))
	spec.Env = setEnvironment(spec.Env, "NOON_TRIGGER", run.Trigger)
	scheduledAt := ""
	if run.ScheduledAt.Valid {
		if parsed, err := time.Parse(store.TimestampLayout, run.ScheduledAt.String); err == nil {
			scheduledAt = parsed.UTC().Format(time.RFC3339)
		}
	}
	spec.Env = setEnvironment(spec.Env, "NOON_SCHEDULED_AT", scheduledAt)
	lastSuccessAt := ""
	if hasSuccess {
		if parsed, err := time.Parse(store.TimestampLayout, lastSuccess); err == nil {
			lastSuccessAt = parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	spec.Env = setEnvironment(spec.Env, "NOON_LAST_SUCCESS_AT", lastSuccessAt)
	spec.Log = log
	spec.KillGrace = options.KillGrace
	spec.ForceKill = forceKill
	spec.OnStart = func(_ int, pgid int, leaderStart string) error {
		return state.SetProcessIdentity(run.ID, pgid, leaderStart)
	}
	result := runner.Run(ctx, spec)
	status := "succeeded"
	switch {
	case result.TimedOut:
		status = "timed_out"
	case result.Cancelled:
		status = control.stopStatus()
		if status == "" {
			status = "cancelled"
		}
	case result.ExitCode != 0:
		status = "failed"
	}
	logBytes, truncated, logErr := log.Close()
	errorMessage := ""
	if result.StartError != nil {
		errorMessage = result.StartError.Error()
	}
	if logErr != nil {
		if errorMessage != "" {
			errorMessage += "; "
		}
		errorMessage += "log write failed: " + logErr.Error()
	}
	if err := state.FinishRun(run.ID, run.ProjectID, run.Job, store.Outcome{Status: status, FinishedAt: result.FinishedAt, ExitCode: &result.ExitCode, Signal: result.Signal, Error: errorMessage, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated}); err != nil {
		logger.event("run finish error project=%q job=%q run=%d error=%q", project, run.Job, run.ID, err.Error())
		return
	}
	logger.event("run finished project=%q job=%q run=%d status=%s", project, run.Job, run.ID, status)
}

func setEnvironment(env []string, name, value string) []string {
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if !ok || key != name {
			filtered = append(filtered, entry)
		}
	}
	if value != "" {
		filtered = append(filtered, name+"="+value)
	}
	return filtered
}

func drainFinished(completed <-chan int64, running map[int64]*runControl) {
	for {
		select {
		case id := <-completed:
			delete(running, id)
		default:
			return
		}
	}
}
