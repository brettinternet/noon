package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

const maxFollowLogBytes = 1 << 20

// Run is the shared nullable run representation exposed by every client.
type Run struct {
	ID                int64      `json:"id"`
	Project           string     `json:"project"`
	Job               string     `json:"job"`
	Status            string     `json:"status"`
	Orphaned          bool       `json:"orphaned"`
	Trigger           string     `json:"trigger"`
	Digest            string     `json:"digest"`
	RequestedBy       *string    `json:"requested_by"`
	IdempotencyKey    *string    `json:"idempotency_key"`
	DueAt             *string    `json:"due_at"`
	CancelRequestedAt *string    `json:"cancel_requested_at"`
	EventFile         *string    `json:"event_file"`
	Depth             int        `json:"depth"`
	ScheduledAt       *string    `json:"scheduled_at"`
	CreatedAt         string     `json:"created_at"`
	StartedAt         *string    `json:"started_at"`
	FinishedAt        *string    `json:"finished_at"`
	DurationMS        *int64     `json:"duration_ms"`
	ExitCode          *int       `json:"exit_code"`
	Signal            *string    `json:"signal"`
	Error             *string    `json:"error"`
	Log               RunLogInfo `json:"log"`
	Hook              *RunHook   `json:"hook"`
}

type RunHook struct {
	Event    string `json:"event"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code"`
}

type RunLogInfo struct {
	Path       *string `json:"path"`
	Bytes      int64   `json:"bytes"`
	Truncated  bool    `json:"truncated"`
	Available  bool    `json:"available"`
	HookOffset *int64  `json:"hook_offset"`
}

type RunsView struct {
	SchemaVersion int   `json:"schema_version"`
	Runs          []Run `json:"runs"`
}

type RunDetail struct {
	Orphaned      bool   `json:"orphaned"`
	SchemaVersion int    `json:"schema_version"`
	Run           Run    `json:"run"`
	Log           string `json:"log"`
	Next          int64  `json:"next"`
	Size          int64  `json:"size"`
	Truncated     bool   `json:"truncated"`
	MatchedLines  *int   `json:"matched_lines"`
}

// RunFromStore maps persisted state to the client-independent run JSON shape and inspects its owner.
func RunFromStore(state *store.Store, run store.Run) Run {
	current, orphaned, err := inspectStoredRun(state, run.ID)
	if err == nil {
		run = current
	}
	result := mapRunFromStore(state, run)
	result.Orphaned = orphaned
	return result
}

func mapRunFromStore(state *store.Store, run store.Run) Run {
	result := Run{
		ID: run.ID, Project: run.ProjectPath, Job: run.Job, Status: run.Status, Trigger: run.Trigger,
		Digest: run.Digest, Depth: run.Depth, CreatedAt: run.CreatedAt,
		Log: RunLogInfo{Bytes: run.LogBytes, Truncated: run.LogTruncated, Available: run.LogPath.Valid},
	}
	if path, ok := state.RunLogFilePath(run); ok {
		result.Log.Path = &path
	}
	if run.HookLogOffset.Valid {
		offset := run.HookLogOffset.Int64
		result.Log.HookOffset = &offset
	}
	if path, ok := state.EventFilePath(run); ok {
		result.EventFile = &path
	}
	if run.RequestedBy.Valid {
		result.RequestedBy = &run.RequestedBy.String
	}
	if run.IdempotencyKey.Valid {
		result.IdempotencyKey = &run.IdempotencyKey.String
	}
	if run.DueAt.Valid {
		result.DueAt = &run.DueAt.String
	}
	if run.CancelRequestedAt.Valid {
		result.CancelRequestedAt = &run.CancelRequestedAt.String
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
	return RunsWithStatuses(stateDir, project, job, nil, limit)
}

// RunsWithStatuses returns newest-first history filtered by status before the limit.
func RunsWithStatuses(stateDir, project, job string, statuses []string, limit int) ([]Run, error) {
	if err := ValidateRunStatuses(statuses); err != nil {
		return nil, err
	}
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return []Run{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer state.Close()
	stored, err := state.RunsForProjectFiltered(project, job, statuses, false, limit)
	if err != nil {
		return nil, err
	}
	runs := make([]Run, len(stored))
	for i, run := range stored {
		mapped, err := inspectAndMapRun(state, run)
		if err != nil {
			return nil, err
		}
		runs[i] = mapped
	}
	return runs, nil
}

// LatestRun returns the newest matching run for a project without creating state.
func LatestRun(stateDir, project, job string, statuses []string) (Run, bool, error) {
	if err := ValidateRunStatuses(statuses); err != nil {
		return Run{}, false, err
	}
	state, err := store.OpenExisting(stateDir)
	if errors.Is(err, store.ErrNoDatabase) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, err
	}
	defer state.Close()
	stored, err := state.RunsForProjectFiltered(project, job, statuses, len(statuses) == 0, 1)
	if err != nil {
		return Run{}, false, err
	}
	if len(stored) == 0 {
		return Run{}, false, nil
	}
	run, err := inspectAndMapRun(state, stored[0])
	if err != nil {
		return Run{}, false, err
	}
	return run, true, nil
}

// ListRuns returns the CLI JSON history view for one project.
func ListRuns(input config.Input, job *string, statuses []string, limit int) (RunsView, error) {
	if limit < 1 || limit > 50 {
		return RunsView{}, UsageError("--limit must be between 1 and 50", "")
	}
	if err := ValidateRunStatuses(statuses); err != nil {
		return RunsView{}, err
	}
	cfg, err := config.Load(input)
	if err != nil {
		return RunsView{}, ConfigError(err)
	}
	jobName := ""
	if job != nil {
		jobName = *job
		if jobName == "" {
			return RunsView{}, MissingJobError(jobName, "history", cfg.Jobs)
		}
	}
	runs, err := RunsWithStatuses(cfg.StateDir, cfg.Project, jobName, statuses, limit)
	if err != nil {
		return RunsView{}, StateError(err)
	}
	if job != nil && len(runs) == 0 {
		if _, configured := cfg.Jobs[jobName]; !configured {
			retained, historyErr := Runs(cfg.StateDir, cfg.Project, jobName, 1)
			if historyErr != nil {
				return RunsView{}, StateError(historyErr)
			}
			if len(retained) == 0 {
				return RunsView{}, MissingJobError(jobName, "history", cfg.Jobs)
			}
		}
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
		mapped, err := inspectAndMapRun(state, run)
		if err != nil {
			return Inspection{}, err
		}
		latest[run.Job] = mapped
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
	return inspectAndMapRun(state, stored)
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
	mapped, err := inspectAndMapRun(state, stored)
	if err != nil {
		return Run{}, nil, err
	}
	data, err := state.ReadRunLog(stored)
	return mapped, data, err
}

// GetRun optionally waits for a terminal status and returns one bounded log selection.
func GetRun(ctx context.Context, stateDir string, id int64, waitSeconds int, query LogQuery) (RunDetail, error) {
	if err := ctx.Err(); err != nil {
		return RunDetail{}, err
	}
	if waitSeconds < 0 || waitSeconds > 60 {
		return RunDetail{}, UsageError("wait_seconds must be between 0 and 60", "")
	}
	if query.LimitBytes == nil || *query.LimitBytes < 0 || *query.LimitBytes > 65536 {
		return RunDetail{}, UsageError("log_bytes must be between 0 and 65536", "")
	}
	if err := ValidateLogQuery(query); err != nil {
		return RunDetail{}, err
	}
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		if NoDatabase(err) {
			return RunDetail{}, RunNotFoundError(id)
		}
		return RunDetail{}, StateError(err)
	}
	defer state.Close()
	run, orphaned, err := inspectStoredRun(state, id)
	if err != nil {
		if IsMissingRun(err) || NoDatabase(err) {
			return RunDetail{}, RunNotFoundError(id)
		}
		return RunDetail{}, StateError(err)
	}
	if waitSeconds > 0 && !orphaned && (run.Status == "queued" || activeStoredRun(run)) {
		timer := time.NewTimer(time.Duration(waitSeconds) * time.Second)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer timer.Stop()
		defer ticker.Stop()
		wait := true
		for wait && !orphaned && (run.Status == "queued" || activeStoredRun(run)) {
			select {
			case <-ctx.Done():
				return RunDetail{}, ctx.Err()
			case <-timer.C:
				wait = false
			case <-ticker.C:
				run, orphaned, err = inspectStoredRun(state, id)
				if err != nil {
					if IsMissingRun(err) || NoDatabase(err) {
						return RunDetail{}, RunNotFoundError(id)
					}
					return RunDetail{}, StateError(err)
				}
			}
		}
	}
	view, selection, err := ReadRunLogSelection(stateDir, id, query)
	if err != nil {
		if IsMissingRun(err) || NoDatabase(err) {
			return RunDetail{}, RunNotFoundError(id)
		}
		if failure, ok := err.(*Error); ok && failure.Usage {
			return RunDetail{}, failure
		}
		return RunDetail{}, StateError(err)
	}
	// Cancellation can arrive while reading a run that became terminal before
	// the wait loop, so it must also be observed on the non-waiting path.
	if err := ctx.Err(); err != nil {
		return RunDetail{}, err
	}
	run, orphaned, err = inspectStoredRun(state, id)
	if err != nil {
		if IsMissingRun(err) || NoDatabase(err) {
			return RunDetail{}, RunNotFoundError(id)
		}
		return RunDetail{}, StateError(err)
	}
	logInfo := view.Log
	view = mapRunFromStore(state, run)
	view.Log = logInfo
	view.Orphaned = orphaned
	return RunDetail{
		SchemaVersion: 1, Run: view, Orphaned: orphaned, Log: ValidLogText(selection.Bytes), Next: selection.Next,
		Size: selection.Size, Truncated: selection.Truncated, MatchedLines: selection.MatchedLines,
	}, nil
}

// FollowLog waits for queued runs, then streams live growth until completion or owner exit.
func FollowLog(ctx context.Context, stateDir string, id int64, after *int64, tail *int, out, notices io.Writer) error {
	state, err := store.OpenExisting(stateDir)
	if err != nil {
		return err
	}
	defer state.Close()
	run, orphaned, err := inspectStoredRun(state, id)
	if err != nil {
		return err
	}
	if run.Status == "queued" {
		if _, err := fmt.Fprintf(notices, "noon: run %d is queued; waiting for it to start\n", id); err != nil {
			return err
		}
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for run.Status == "queued" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				run, orphaned, err = inspectStoredRun(state, id)
				if err != nil {
					return err
				}
			}
		}
	}
	var initial []byte
	if run.LogPath.Valid {
		initial, err = state.ReadRunLog(run)
		if errors.Is(err, store.ErrLogUnavailable) {
			initial = nil
		} else if err != nil {
			return err
		}
	}
	offset, err := FollowLogStart(initial, after, tail)
	if err != nil {
		return err
	}
	followedRunning := activeStoredRun(run)
	markerShown := false
	statusCheck := time.Now().Add(time.Second)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if run.LogPath.Valid {
			data, readErr := state.ReadRunLog(run)
			if errors.Is(readErr, store.ErrLogUnavailable) {
				if !activeStoredRun(run) || orphaned {
					if orphaned {
						return OrphanedRunError(id)
					}
					return MissingRunLogError(RunFromStore(state, run))
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
				if !followedRunning || !activeStoredRun(run) || orphaned {
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
			return MissingRunLogError(RunFromStore(state, run))
		}
		if orphaned {
			return OrphanedRunError(id)
		}
		if !activeStoredRun(run) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !time.Now().Before(statusCheck) {
				run, orphaned, err = inspectStoredRun(state, id)
				if err != nil {
					return err
				}
				statusCheck = time.Now().Add(time.Second)
			}
		}
	}
}

// inspectAndMapRun adds the read-only owner check to a stored run.
func inspectAndMapRun(state *store.Store, run store.Run) (Run, error) {
	current, orphaned, err := inspectStoredRun(state, run.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return mapRunFromStore(state, run), nil
	}
	if err != nil {
		return Run{}, err
	}
	mapped := mapRunFromStore(state, current)
	mapped.Orphaned = orphaned
	return mapped, nil
}

// inspectStoredRun checks the same owner identity as recovery, but never writes.
// Re-read after a failed identity check so a normal finish is not called orphaned.
func inspectStoredRun(state *store.Store, id int64) (store.Run, bool, error) {
	run, err := state.RunByID(id)
	if err != nil || !activeStoredRun(run) {
		return run, false, err
	}
	alive, err := runner.ProcessOwnerAlive(int(run.OwnerPID.Int64), run.OwnerStart.String)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return run, false, err
	}
	if alive {
		return run, false, nil
	}
	run, err = state.RunByID(id)
	return run, err == nil && activeStoredRun(run), err
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
