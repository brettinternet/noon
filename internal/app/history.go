package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

const maxFollowLogBytes = 1 << 20

// Run is the shared nullable run representation exposed by every client.
type Run struct {
	ID          int64      `json:"id"`
	Job         string     `json:"job"`
	Status      string     `json:"status"`
	Trigger     string     `json:"trigger"`
	ScheduledAt *string    `json:"scheduled_at"`
	CreatedAt   string     `json:"created_at"`
	StartedAt   *string    `json:"started_at"`
	FinishedAt  *string    `json:"finished_at"`
	DurationMS  *int64     `json:"duration_ms"`
	ExitCode    *int       `json:"exit_code"`
	Signal      *string    `json:"signal"`
	Error       *string    `json:"error"`
	Log         RunLogInfo `json:"log"`
	Hook        *RunHook   `json:"hook"`
}

type RunHook struct {
	Event    string `json:"event"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code"`
}

type RunLogInfo struct {
	Bytes     int64 `json:"bytes"`
	Truncated bool  `json:"truncated"`
	Available bool  `json:"available"`
}

type RunsView struct {
	SchemaVersion int   `json:"schema_version"`
	Runs          []Run `json:"runs"`
}

type RunDetail struct {
	SchemaVersion int    `json:"schema_version"`
	Run           Run    `json:"run"`
	Log           string `json:"log"`
}

// RunFromStore maps persisted state to the client-independent run JSON shape.
func RunFromStore(run store.Run) Run {
	result := Run{
		ID: run.ID, Job: run.Job, Status: run.Status, Trigger: run.Trigger,
		CreatedAt: run.CreatedAt,
		Log:       RunLogInfo{Bytes: run.LogBytes, Truncated: run.LogTruncated, Available: run.LogPath.Valid},
	}
	if run.ScheduledAt.Valid {
		result.ScheduledAt = &run.ScheduledAt.String
	}
	if run.StartedAt != "" {
		result.StartedAt = &run.StartedAt
	}
	if run.FinishedAt.Valid {
		result.FinishedAt = &run.FinishedAt.String
	}
	if run.StartedAt != "" && run.FinishedAt.Valid {
		started, startErr := time.Parse(store.TimestampLayout, run.StartedAt)
		finished, finishErr := time.Parse(store.TimestampLayout, run.FinishedAt.String)
		if startErr == nil && finishErr == nil {
			duration := finished.Sub(started).Milliseconds()
			result.DurationMS = &duration
		}
	}
	if run.ExitCode.Valid {
		code := int(run.ExitCode.Int64)
		result.ExitCode = &code
	}
	if run.Signal.Valid {
		result.Signal = &run.Signal.String
	}
	if run.Error.Valid {
		result.Error = &run.Error.String
	}
	if run.HookStatus.Valid {
		event := ""
		switch run.Status {
		case "succeeded":
			event = "on_success"
		case "failed", "timed_out":
			event = "on_failure"
		}
		if event != "" {
			hook := &RunHook{Event: event, Status: run.HookStatus.String}
			if run.HookExitCode.Valid {
				code := int(run.HookExitCode.Int64)
				hook.ExitCode = &code
			}
			result.Hook = hook
		}
	}
	return result
}

// Runs returns newest-first history for a project without creating state.
func Runs(stateDir, project, job string, limit int) ([]Run, error) {
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return []Run{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer state.Close()
	stored, err := state.RunsForProject(project, job, limit)
	if err != nil {
		return nil, err
	}
	runs := make([]Run, len(stored))
	for i, run := range stored {
		runs[i] = RunFromStore(run)
	}
	return runs, nil
}

// ListRuns returns the CLI JSON history view for one project.
func ListRuns(input config.Input, job *string, limit int) (RunsView, error) {
	if limit < 1 || limit > 50 {
		return RunsView{}, UsageError("--limit must be between 1 and 50", "")
	}
	cfg, err := config.Load(input)
	if err != nil {
		return RunsView{}, ConfigError(err)
	}
	jobName := ""
	if job != nil {
		jobName = *job
		if _, ok := cfg.Jobs[jobName]; !ok {
			return RunsView{}, MissingJobError(jobName, "history", cfg.Jobs)
		}
	}
	runs, err := Runs(cfg.StateDir, cfg.Project, jobName, limit)
	if err != nil {
		return RunsView{}, StateError(err)
	}
	return RunsView{SchemaVersion: 1, Runs: runs}, nil
}

// WithLastRuns adds the latest run of each job to an inspection without creating state.
func WithLastRuns(view Inspection, stateDir string) (Inspection, error) {
	if len(view.Jobs) == 0 {
		return view, nil
	}
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return view, nil
	}
	if err != nil {
		return Inspection{}, err
	}
	defer state.Close()
	stored, err := state.LatestRunsForProject(view.Project.Path)
	if err != nil {
		return Inspection{}, err
	}
	latest := make(map[string]Run, len(stored))
	for _, run := range stored {
		latest[run.Job] = RunFromStore(run)
	}
	for i := range view.Jobs {
		if run, ok := latest[view.Jobs[i].Name]; ok {
			view.Jobs[i].LastRun = &run
		}
	}
	return view, nil
}

// RunByID finds a run by its globally unique ID without creating state.
func RunByID(stateDir string, id int64) (Run, error) {
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		return Run{}, err
	}
	defer state.Close()
	stored, err := state.RunByID(id)
	if err != nil {
		return Run{}, err
	}
	return RunFromStore(stored), nil
}

// ReadRunLog retrieves a run and its exact stored bytes without modifying state.
func ReadRunLog(stateDir string, id int64) (Run, []byte, error) {
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		return Run{}, nil, err
	}
	defer state.Close()
	stored, err := state.RunByID(id)
	if err != nil {
		return Run{}, nil, err
	}
	data, err := state.ReadRunLog(stored)
	return RunFromStore(stored), data, err
}

// GetRun optionally waits for a terminal status and returns at most logBytes of log tail.
func GetRun(ctx context.Context, stateDir string, id int64, waitSeconds, logBytes int) (RunDetail, error) {
	if waitSeconds < 0 || waitSeconds > 60 {
		return RunDetail{}, UsageError("wait_seconds must be between 0 and 60", "")
	}
	if logBytes < 0 || logBytes > 65536 {
		return RunDetail{}, UsageError("log_bytes must be between 0 and 65536", "")
	}
	run, err := RunByID(stateDir, id)
	if err != nil {
		if IsMissingRun(err) || NoDatabase(err) {
			return RunDetail{}, RunNotFoundError(id)
		}
		return RunDetail{}, StateError(err)
	}
	if waitSeconds > 0 && (run.Status == "queued" || run.Status == "running") {
		timer := time.NewTimer(time.Duration(waitSeconds) * time.Second)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer timer.Stop()
		defer ticker.Stop()
		wait := true
		for wait && (run.Status == "queued" || run.Status == "running") {
			select {
			case <-ctx.Done():
				return RunDetail{}, ctx.Err()
			case <-timer.C:
				wait = false
			case <-ticker.C:
				run, err = RunByID(stateDir, id)
				if err != nil {
					if IsMissingRun(err) || NoDatabase(err) {
						return RunDetail{}, RunNotFoundError(id)
					}
					return RunDetail{}, StateError(err)
				}
			}
		}
	}
	run, data, err := ReadRunLog(stateDir, id)
	if err != nil {
		if IsMissingRun(err) || NoDatabase(err) {
			return RunDetail{}, RunNotFoundError(id)
		}
		if IsMissingLog(err) {
			run.Log.Available = false
			data = nil
		} else {
			return RunDetail{}, StateError(err)
		}
	}
	if len(data) > logBytes {
		data = data[len(data)-logBytes:]
	}
	log := ValidLogText(data)
	for len(log) > logBytes {
		_, size := utf8.DecodeRuneInString(log)
		log = log[size:]
	}
	return RunDetail{SchemaVersion: 1, Run: run, Log: log}, nil
}

// FollowLog streams live file growth and waits for a running run to finish.
func FollowLog(ctx context.Context, stateDir string, id int64, out io.Writer) error {
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		return err
	}
	defer state.Close()
	run, err := state.RunByID(id)
	if err != nil {
		return err
	}
	var offset int64
	followedRunning := activeStoredRun(run)
	markerShown := false
	statusCheck := time.Now().Add(time.Second)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if run.LogPath.Valid {
			data, readErr := state.ReadRunLog(run)
			if errors.Is(readErr, store.ErrLogUnavailable) {
				if !activeStoredRun(run) {
					return readErr
				}
			} else if readErr != nil {
				return readErr
			} else {
				end := int64(len(data))
				overflow := run.LogOverflow || run.LogTruncated || end > maxFollowLogBytes
				if followedRunning && overflow && !markerShown {
					firstMIB := min(end, int64(maxFollowLogBytes))
					if firstMIB > offset {
						if _, err := io.Copy(out, bytes.NewReader(data[offset:firstMIB])); err != nil {
							return err
						}
						offset = firstMIB
					}
					if offset > 0 && data[offset-1] != '\n' {
						if _, err := io.WriteString(out, "\n"); err != nil {
							return err
						}
					}
					if _, err := fmt.Fprintln(out, "[noon: output past 1 MiB is shown when the run ends]"); err != nil {
						return err
					}
					markerShown = true
				}
				if !followedRunning || !activeStoredRun(run) {
					if end > offset {
						if _, err := io.Copy(out, bytes.NewReader(data[offset:end])); err != nil {
							return err
						}
						offset = end
					}
				} else if !markerShown {
					firstMIB := min(end, int64(maxFollowLogBytes))
					if firstMIB > offset {
						if _, err := io.Copy(out, bytes.NewReader(data[offset:firstMIB])); err != nil {
							return err
						}
						offset = firstMIB
					}
				}
			}
		} else if !activeStoredRun(run) {
			return store.ErrLogUnavailable
		}
		if !activeStoredRun(run) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !time.Now().Before(statusCheck) {
				run, err = state.RunByID(id)
				if err != nil {
					return err
				}
				statusCheck = time.Now().Add(time.Second)
			}
		}
	}
}

func activeStoredRun(run store.Run) bool {
	return run.Status == "running" || (run.HookStatus.Valid && run.HookStatus.String == "running")
}

// IsMissingRun reports a run lookup that did not find a stored row.
func IsMissingRun(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// ValidLogText repairs invalid UTF-8 for JSON string output.
func ValidLogText(data []byte) string { return strings.ToValidUTF8(string(data), "\uFFFD") }

// IsMissingLog reports a run whose retained log is unavailable.
func IsMissingLog(err error) bool { return errors.Is(err, store.ErrLogUnavailable) }

// NoDatabase reports whether inspection found no store.
func NoDatabase(err error) bool { return errors.Is(err, store.ErrNoDatabase) }

// RelativeTime formats a stored run timestamp relative to now.
func RelativeTime(value *string, now time.Time) string {
	if value == nil {
		return "—"
	}
	at, err := time.Parse(store.TimestampLayout, *value)
	if err != nil {
		return "—"
	}
	elapsed := now.Sub(at)
	if elapsed < 0 {
		return "in " + shortDuration(-elapsed)
	}
	if elapsed < time.Minute {
		return "now"
	}
	return shortDuration(elapsed) + " ago"
}

func shortDuration(value time.Duration) string {
	if value < time.Minute {
		return fmt.Sprintf("%ds", max(1, int(value.Round(time.Second)/time.Second)))
	}
	if value < time.Hour {
		return fmt.Sprintf("%dm", int(value/time.Minute))
	}
	if value < 24*time.Hour {
		return fmt.Sprintf("%dh", int(value/time.Hour))
	}
	return fmt.Sprintf("%dd", int(value/(24*time.Hour)))
}

// DurationText formats stored elapsed time compactly for human output.
func DurationText(durationMS *int64) string {
	if durationMS == nil {
		return "—"
	}
	return (time.Duration(*durationMS) * time.Millisecond).Round(time.Second).String()
}
