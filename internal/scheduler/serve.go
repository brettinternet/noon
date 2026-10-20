package scheduler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/brettinternet/noon/internal/app"
	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/httpapi"
	noonmcp "github.com/brettinternet/noon/internal/mcp"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
	protocol "github.com/modelcontextprotocol/go-sdk/mcp"
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
	ListenAddr      string
	APITokenFile    string
	Platform        *config.Platform
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
	project            store.EnabledProject
	config             *config.Config
	webhookSecrets     map[string][]byte
	webhookConfigError string
	configSelection    string
	stamps             []fileStamp
	checkedAt          time.Time
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
	var apiToken []byte
	if options.APITokenFile != "" {
		if options.ListenAddr == "" {
			return errors.New("API token file requires an HTTP listener")
		}
		var err error
		apiToken, err = readAPITokenFile(options.APITokenFile)
		if err != nil {
			return err
		}
	}
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
	options.Environment = append([]string(nil), options.Environment...)
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
	var webhookHandler *httpapi.Handler
	var httpServer *http.Server
	var httpErrors <-chan error
	var stopHTTP func()
	// Configs of registered but not-enabled projects, reloaded only when their files change.
	inactiveConfigs := map[int64]*projectConfig{}
	if options.ListenAddr != "" {
		projects, err := webhookProjects(state, initial.Projects, configs, inactiveConfigs, options)
		if err != nil {
			return err
		}
		webhookHandler = httpapi.NewHandler(projects)
		var listenerHandler http.Handler = webhookHandler
		if len(apiToken) > 0 {
			apiInput := config.Input{CWD: options.StateDir, UserConfigDir: options.UserConfigDir, StateDirDefault: options.StateDirDefault, StateDir: &options.StateDir, Platform: options.Platform}
			apiHandler := httpapi.NewAPIHandler(apiInput, options.StateDir)
			mcpHandler := protocol.NewStreamableHTTPHandler(
				func(*http.Request) *protocol.Server { return noonmcp.NewHTTPServer(apiInput, options.Version) },
				// Bearer authentication protects this endpoint against DNS rebinding.
				// Permit the public Host preserved by a reverse proxy on loopback.
				&protocol.StreamableHTTPOptions{Stateless: true, MaxRequestBodyBytes: httpapi.MaxAPIRequestBytes, DisableLocalhostProtection: true},
			)
			listenerHandler = httpapi.NewListenerHandler(webhookHandler, apiHandler, mcpHandler, apiToken)
		}
		listener, err := net.Listen("tcp", options.ListenAddr)
		if err != nil {
			return fmt.Errorf("listen for webhooks on %q: %w", options.ListenAddr, err)
		}
		address := httpapi.ListenerAddress(listener)
		httpServer = httpapi.NewServer(listenerHandler)
		done := make(chan struct{})
		failures := make(chan error, 1)
		go func() {
			if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				failures <- err
			}
			close(done)
		}()
		httpErrors = failures
		if len(apiToken) > 0 {
			logger.event("webhook and API listener listening address=%s", address)
		} else {
			logger.event("webhook listener listening address=%s", address)
		}
		if !httpapi.IsLoopbackAddress(address) {
			if len(apiToken) > 0 {
				logger.event("WARNING: API/webhook listener uses a non-loopback address; API requests need a bearer token and webhooks need signatures")
			} else {
				logger.event("WARNING: webhook listener uses a non-loopback address; requests still require signatures")
			}
		}
		var stopOnce sync.Once
		stopHTTP = func() {
			stopOnce.Do(func() {
				webhookHandler.SetHealthy(false)
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := httpServer.Shutdown(shutdown); err != nil {
					_ = httpServer.Close()
				}
				select {
				case <-done:
				case <-shutdown.Done():
					_ = httpServer.Close()
					<-done
				}
			})
		}
		defer stopHTTP()
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
		case err := <-httpErrors:
			return fmt.Errorf("HTTP listener server: %w", err)
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
		if webhookHandler != nil {
			projects, err := webhookProjects(state, snapshot.Projects, configs, inactiveConfigs, options)
			if err != nil {
				return err
			}
			webhookHandler.ReplaceProjects(projects)
		}
		for id, control := range running {
			if !activeProjects[control.projectID] {
				control.stop("cancelled")
				continue
			}
			requested, err := state.CancellationRequested(id)
			if err != nil {
				return fmt.Errorf("check cancellation for run %d: %w", id, err)
			}
			if requested {
				control.stop("cancelled")
			}
		}

		started, err := state.ScheduleTick(now, pid, identity,
			func(snapshot store.SchedulerSnapshot) (store.SchedulerPlan, error) {
				jobs, err := makeJobInputs(snapshot, configs)
				if err != nil {
					return store.SchedulerPlan{}, err
				}
				snapshot = withoutUnloaded(snapshot, configs)
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
				runs, err := plannerRuns(withoutUnloaded(snapshot, configs).Runs)
				if err != nil {
					return store.DispatchPlan{}, err
				}
				start, expire, cancel := Dispatch(now, jobs, runs)
				sessionIDs := make(map[int64]string)
				for _, id := range start {
					var queued store.ScheduledRun
					for _, candidate := range snapshot.Runs {
						if candidate.ID == id {
							queued = candidate
							break
						}
					}
					if queued.ID == 0 {
						return store.DispatchPlan{}, fmt.Errorf("dispatch selected unknown run %d", id)
					}
					if queued.AgentSessionID.Valid {
						sessionIDs[id] = queued.AgentSessionID.String
						continue
					}
					cached := configs[queued.ProjectID]
					if cached == nil || cached.config == nil {
						continue
					}
					job, exists := cached.config.Jobs[queued.Job]
					if !exists || job.AgentSessionIDFlag == "" {
						continue
					}
					sessionID, err := app.NewAgentSessionID()
					if err != nil {
						return store.DispatchPlan{}, err
					}
					sessionIDs[id] = sessionID
				}
				return store.DispatchPlan{Start: start, Expire: expire, Cancel: cancel, AgentSessionIDs: sessionIDs}, nil
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
		if webhookHandler != nil {
			webhookHandler.SetHealthy(true)
		}
		if options.OnTick != nil {
			options.OnTick()
		}
	}

shutdown:
	logger.event("scheduler shutting down")
	if stopHTTP != nil {
		stopHTTP()
	}
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

func readAPITokenFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read API token file %q: %w", path, err)
	}
	value := string(data)
	if strings.HasSuffix(value, "\r\n") {
		value = strings.TrimSuffix(value, "\r\n")
	} else {
		value = strings.TrimSuffix(value, "\n")
	}
	if len([]byte(value)) < 32 {
		return nil, errors.New("API token must be at least 32 bytes")
	}
	return []byte(value), nil
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
	if _, err := fmt.Fprintf(l.file, "%d %s %s\n", pid, identity, startedAt); err != nil {
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
	input := config.Input{CWD: cached.project.Path, UserConfigDir: options.UserConfigDir, StateDirDefault: options.StateDirDefault, Platform: options.Platform}
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
	cached.webhookSecrets, cached.webhookConfigError = webhookSecretSnapshots(loaded, options.Environment)
	if err := state.SetProjectConfigError(cached.project.ID, cached.webhookConfigError); err != nil {
		return fmt.Errorf("record config error for %q: %w", cached.project.Path, err)
	}
	if cached.webhookConfigError != "" {
		logger.event("config error project=%q error=%q", cached.project.Path, cached.webhookConfigError)
	}
	return nil
}

func webhookSecretSnapshots(cfg config.Config, environment []string) (map[string][]byte, string) {
	secrets := map[string][]byte{}
	names := make([]string, 0, len(cfg.Jobs))
	for name := range cfg.Jobs {
		names = append(names, name)
	}
	slices.Sort(names)
	var missing []string
	for _, name := range names {
		webhook := cfg.Jobs[name].Webhook
		if webhook == nil {
			continue
		}
		value, found := "", false
		for _, entry := range environment {
			key, candidate, ok := strings.Cut(entry, "=")
			if ok && key == webhook.SecretEnv {
				value, found = candidate, true
			}
		}
		if !found || value == "" {
			missing = append(missing, fmt.Sprintf("job %q webhook secret environment variable %q is missing or empty", name, webhook.SecretEnv))
			continue
		}
		secrets[name] = []byte(value)
	}
	return secrets, strings.Join(missing, "; ")
}

func webhookProjects(state *store.Store, enabled []store.EnabledProject, configs, inactive map[int64]*projectConfig, options ServeOptions) ([]httpapi.WebhookProject, error) {
	records, err := state.ProjectRecords()
	if err != nil {
		return nil, fmt.Errorf("read webhook project records: %w", err)
	}
	active := make(map[int64]store.EnabledProject, len(enabled))
	for _, project := range enabled {
		active[project.ID] = project
	}
	projects := make([]httpapi.WebhookProject, 0, len(records))
	seen := make(map[int64]bool)
	for _, record := range records {
		var loaded config.Config
		var projectState store.EnabledProject
		var secrets map[string][]byte
		if record.Enabled {
			var exists bool
			projectState, exists = active[record.ID]
			if !exists {
				continue
			}
			cached := loadedConfig(projectState, configs)
			if cached == nil {
				continue
			}
			loaded = *cached.config
			secrets = cached.webhookSecrets
		} else {
			projectState = store.EnabledProject{ID: record.ID, Path: record.Path, ConfigFile: record.ConfigFile}
			seen[record.ID] = true
			cached := inactive[record.ID]
			stamps := statPaths(configPaths(projectState, options.UserConfigDir))
			if cached == nil || cached.project.Path != record.Path || cached.configSelection != configSelection(projectState) || !sameStamps(cached.stamps, stamps) {
				cached = &projectConfig{project: projectState, configSelection: configSelection(projectState), stamps: stamps}
				inactive[record.ID] = cached
				project := record.Path
				input := config.Input{CWD: project, UserConfigDir: options.UserConfigDir, StateDirDefault: options.StateDirDefault, Project: &project, Platform: options.Platform}
				if record.ConfigFile.Valid {
					file := record.ConfigFile.String
					input.File = &file
				}
				if loaded, err := config.Load(input); err == nil {
					cached.config = &loaded
					cached.webhookSecrets, _ = webhookSecretSnapshots(loaded, options.Environment)
				}
			}
			if cached.config == nil {
				continue
			}
			loaded = *cached.config
			secrets = cached.webhookSecrets
		}
		// Requests must use the same state store as this scheduler, including overrides.
		loaded.StateDir = options.StateDir
		jobs := make(map[string]httpapi.WebhookJob, len(loaded.Jobs))
		for name, definition := range loaded.Jobs {
			jobs[name] = httpapi.WebhookJob{
				Definition: definition, Secret: secrets[name], Paused: projectState.Paused[name],
				Approved: !definition.Shared || projectState.Approvals[name] == definition.Digest,
			}
		}
		projects = append(projects, httpapi.WebhookProject{ID: record.ID, Config: loaded, Enabled: record.Enabled, Jobs: jobs})
	}
	for id := range inactive {
		if !seen[id] {
			delete(inactive, id)
		}
	}
	return projects, nil
}

func configSelection(project store.EnabledProject) string {
	if project.ConfigFile.Valid {
		return "explicit:" + project.ConfigFile.String
	}
	return "automatic"
}

func configPaths(project store.EnabledProject, userConfigDir string) []string {
	var paths []string
	input := config.Input{CWD: project.Path, UserConfigDir: userConfigDir, Project: &project.Path}
	if config.IsUserProject(project.Path, userConfigDir) {
		paths = append(paths, filepath.Join(project.Path, "config.yaml"))
	} else if project.ConfigFile.Valid {
		// Loading validates the user config even when an explicit file supplies jobs.
		input.File = &project.ConfigFile.String
		paths = append(paths, project.Path, project.ConfigFile.String, filepath.Join(userConfigDir, "noon", "config.yaml"))
	} else {
		paths = append(paths,
			project.Path,
			filepath.Join(userConfigDir, "noon", "config.yaml"),
			filepath.Join(project.Path, "noon.yaml"),
		)
		// A broken .git fails the reload itself, so it needs no stamp here.
		if local, err := config.LocalPath(project.Path); err == nil && local != "" {
			paths = append(paths, local)
		}
	}
	return append(paths, config.VarsFilePaths(input)...)
}

func statPaths(paths []string) []fileStamp {
	stamps := make([]fileStamp, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			// A dangling symlink is invalid config, unlike an absent optional
			// file. Stamp the link so removing it triggers recovery.
			info, err = os.Lstat(path)
		}
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
		if a[i].info != nil && (a[i].info.Size() != b[i].info.Size() || a[i].info.Mode() != b[i].info.Mode() || !a[i].info.ModTime().Equal(b[i].info.ModTime()) || !os.SameFile(a[i].info, b[i].info)) {
			return false
		}
	}
	return true
}

// loadedConfig returns the project's validated snapshot for its current selection.
func loadedConfig(project store.EnabledProject, configs map[int64]*projectConfig) *projectConfig {
	cached := configs[project.ID]
	if cached == nil || cached.config == nil || cached.configSelection != configSelection(project) {
		return nil
	}
	return cached
}

// withoutUnloaded hides enabled projects that have no valid configuration yet
// from the planners, so a transient config error neither resets their schedule
// watermarks nor cancels their queued runs. Running runs still count toward caps.
func withoutUnloaded(snapshot store.SchedulerSnapshot, configs map[int64]*projectConfig) store.SchedulerSnapshot {
	unloaded := map[int64]bool{}
	for _, project := range snapshot.Projects {
		if loadedConfig(project, configs) == nil {
			unloaded[project.ID] = true
		}
	}
	if len(unloaded) == 0 {
		return snapshot
	}
	filtered := store.SchedulerSnapshot{Projects: snapshot.Projects}
	for _, state := range snapshot.States {
		if !unloaded[state.ProjectID] {
			filtered.States = append(filtered.States, state)
		}
	}
	for _, run := range snapshot.Runs {
		if run.Status == "running" || !unloaded[run.ProjectID] {
			filtered.Runs = append(filtered.Runs, run)
		}
	}
	return filtered
}

func makeJobInputs(snapshot store.SchedulerSnapshot, configs map[int64]*projectConfig) ([]JobInput, error) {
	jobs := make([]JobInput, 0)
	for _, project := range snapshot.Projects {
		cached := loadedConfig(project, configs)
		if cached == nil {
			continue
		}
		location, err := time.LoadLocation(cached.config.Timezone)
		if err != nil {
			return nil, fmt.Errorf("load timezone %q for %q: %w", cached.config.Timezone, project.Path, err)
		}
		for name, job := range cached.config.Jobs {
			active := job.Enabled && job.MatchesPlatform(cached.config.Platform) && !project.Paused[name] && (!job.Shared || project.Approvals[name] == job.Digest)
			jobs = append(jobs, JobInput{Key: JobKey{Project: project.ID, Job: name}, Schedule: job.Schedule, ProjectPath: project.Path, Splay: job.Splay, Jitter: job.Jitter, Location: location, Active: active, Missed: job.Missed, Overlap: job.Overlap})
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
	fail := func(err error, runLog *store.RunLog) {
		code := 125
		outcome := store.Outcome{Status: "failed", FinishedAt: time.Now(), ExitCode: &code, Error: err.Error()}
		if runLog != nil {
			outcome.LogBytes, outcome.LogTruncated, _ = runLog.Close()
			outcome.LogPath = runLog.Path()
		}
		_, finishErr := app.FinishRunWithHook(state, run, project, job, app.JobEnvironment(project, job, options.Environment), outcome)
		if finishErr != nil {
			logger.event("run finish error project=%q job=%q run=%d error=%q", project, run.Job, run.ID, finishErr.Error())
			return
		}
		logger.event("run finished project=%q job=%q run=%d status=failed error=%q", project, run.Job, run.ID, err.Error())
	}
	log, err := state.RunLog(run.ProjectID, run.ID)
	if err != nil {
		fail(err, nil)
		return
	}
	if err := state.SetRunLog(run.ID, log.Path()); err != nil {
		fail(err, log)
		return
	}
	lastSuccess, hasSuccess, err := state.LastSuccess(run.ProjectID, run.Job)
	if err != nil {
		fail(err, log)
		return
	}
	spec, err := app.RunSpecForExecution(project, job, options.Environment, run.AgentSessionID.String)
	if err != nil {
		fail(err, log)
		return
	}
	spec.Env = setEnvironment(spec.Env, "NOON_RUN_ID", strconv.FormatInt(run.ID, 10))
	spec.Env = setEnvironment(spec.Env, "NOON_DEPTH", strconv.Itoa(run.Depth+1))
	if eventPath, ok := state.EventFilePath(run); ok {
		spec.Env = setEnvironment(spec.Env, "NOON_EVENT_FILE", eventPath)
	}
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
	cancelRequested, cancelCheckErr := state.CancellationRequested(run.ID)
	if cancelRequested {
		result.Cancelled = true
		result.ExitCode = 130
	}
	status := "succeeded"
	switch {
	case result.TimedOut:
		status = "timed_out"
	case result.Cancelled:
		status = control.stopStatus()
		if status == "" {
			status = "cancelled"
		}
	case job.NoopExitCode != 0 && result.StartError == nil && result.ExitCode == job.NoopExitCode:
		status = "noop"
	case result.ExitCode != 0:
		status = "failed"
	}
	logBytes, truncated, logErr := log.Close()
	errorMessage := ""
	if result.StartError != nil {
		errorMessage = result.StartError.Error()
	}
	if cancelCheckErr != nil {
		status = "failed"
		result.ExitCode = 125
		errorMessage = "check cancellation request: " + cancelCheckErr.Error()
	}
	if logErr != nil {
		if errorMessage != "" {
			errorMessage += "; "
		}
		errorMessage += "log write failed: " + logErr.Error()
	}
	hookResult, finishErr := app.FinishRunWithHook(state, run, project, job, spec.Env, store.Outcome{Status: status, FinishedAt: result.FinishedAt, ExitCode: &result.ExitCode, Signal: result.Signal, Error: errorMessage, LogPath: log.Path(), LogBytes: logBytes, LogTruncated: truncated})
	if finishErr != nil {
		logger.event("run finish error project=%q job=%q run=%d error=%q", project, run.Job, run.ID, finishErr.Error())
		return
	}
	if hookResult != nil {
		logger.event("run finished project=%q job=%q run=%d status=%s hook=%s", project, run.Job, run.ID, status, hookResult.Status)
	} else {
		logger.event("run finished project=%q job=%q run=%d status=%s", project, run.Job, run.ID, status)
	}
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
