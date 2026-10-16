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
}

type RunLogInfo struct {
	Bytes     int64 `json:"bytes"`
	Truncated bool  `json:"truncated"`
	Available bool  `json:"available"`
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
	followedRunning := run.Status == "running"
	markerShown := false
	statusCheck := time.Now().Add(time.Second)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if run.LogPath.Valid {
			data, readErr := state.ReadRunLog(run)
			if errors.Is(readErr, store.ErrLogUnavailable) {
				if run.Status != "running" {
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
				if !followedRunning || run.Status != "running" {
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
		} else if run.Status != "running" {
			return store.ErrLogUnavailable
		}
		if run.Status != "running" {
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
