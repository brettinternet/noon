package app

import (
	"errors"
	"fmt"
	"slices"

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

func MissingRunLogError(id int64) *Error {
	return &Error{Code: "not_found", Message: fmt.Sprintf("log for run %d is unavailable", id), Hint: "Logs are kept for the newest 10 runs per job."}
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
