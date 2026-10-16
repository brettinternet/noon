package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/store"
)

// Error describes a shared operation failure independently of its adapter.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Usage   bool   `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func ConfigError(err error) *Error {
	return &Error{Code: "config", Message: err.Error()}
}

func UsageError(message, hint string) *Error {
	return &Error{Code: "usage", Message: message, Hint: hint, Usage: true}
}

const validRunStatusNames = "queued, running, succeeded, failed, timed_out, cancelled, interrupted, skipped, missed, expired"

var validRunStatuses = map[string]struct{}{
	"queued": {}, "running": {}, "succeeded": {}, "failed": {}, "timed_out": {},
	"cancelled": {}, "interrupted": {}, "skipped": {}, "missed": {}, "expired": {},
}

// ValidateRunStatuses checks status filters shared by CLI and MCP.
func ValidateRunStatuses(statuses []string) error {
	for _, status := range statuses {
		if _, ok := validRunStatuses[status]; !ok {
			return UsageError(fmt.Sprintf("invalid run status %q", status), "Valid statuses: "+validRunStatusNames)
		}
	}
	return nil
}

// StateError is the CLI and MCP mapping for durable-state failures.
func StateError(err error) *Error {
	var newer *store.StateTooNewError
	if errors.As(err, &newer) {
		return &Error{Code: "state_too_new", Message: newer.Error(), Hint: "Upgrade Noon before using this state database."}
	}
	var running *store.AlreadyRunningError
	if errors.As(err, &running) {
		return &Error{Code: "already_running", Message: fmt.Sprintf("job is already running as run %d, started at %s", running.RunID, running.StartedAt), Hint: fmt.Sprintf("Follow it: noon logs %d --follow", running.RunID)}
	}
	return &Error{Code: "error", Message: err.Error()}
}

// MissingJobError keeps job lookup diagnostics consistent across adapters.
func MissingJobError(name, command string, jobs map[string]config.Job) *Error {
	hint, best := "Run noon list to see available jobs.", 3
	if command == "run" {
		hint = "Run noon run to list jobs"
	}
	names := make([]string, 0, len(jobs))
	for candidate := range jobs {
		names = append(names, candidate)
	}
	slices.Sort(names)
	for _, candidate := range names {
		if distance := editDistance(name, candidate); distance < best {
			hint, best = fmt.Sprintf("Did you mean noon %s %s?", command, candidate), distance
		}
	}
	return &Error{Code: "not_found", Message: fmt.Sprintf("job %q not found", name), Hint: hint}
}

func RunNotFoundError(id int64) *Error {
	return &Error{Code: "not_found", Message: fmt.Sprintf("run %d not found", id), Hint: "Run noon history to list recent runs."}
}

func MatchingJobRunNotFoundError(job string, statuses []string) *Error {
	message := fmt.Sprintf("no matching run for job %q", job)
	if len(statuses) > 0 {
		message = fmt.Sprintf("no runs of job %q match --status %s", job, strings.Join(statuses, ","))
	}
	return &Error{Code: "not_found", Message: message, Hint: "Run noon history to inspect retained runs."}
}

func MissingRunLogError(run Run) *Error {
	if run.Status == "queued" {
		return &Error{Code: "not_found", Message: fmt.Sprintf("run %d is queued and has not started", run.ID), Hint: fmt.Sprintf("Follow it: noon logs %d --follow", run.ID)}
	}
	if run.StartedAt == nil && run.Status != "running" {
		return &Error{Code: "not_found", Message: fmt.Sprintf("run %d was %s and never started, so it has no log", run.ID, run.Status)}
	}
	return &Error{Code: "not_found", Message: fmt.Sprintf("log for run %d is unavailable", run.ID), Hint: "Logs are kept for the newest 10 runs per job."}
}

func OrphanedRunError(id int64) *Error {
	return &Error{Code: "orphaned", Message: fmt.Sprintf("run %d's owner process exited without recording a result", id), Hint: fmt.Sprintf("Record it: noon cancel %d", id)}
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
