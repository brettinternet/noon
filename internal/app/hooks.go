package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/store"
)

const maxHookOutputBytes = 64 << 10

// HookResult describes the lifecycle command selected for a completed run.
type HookResult struct {
	Event, Status string
	ExitCode      *int
}

type hookPayload struct {
	SchemaVersion int `json:"schema_version"`
	Project       struct {
		Path string `json:"path"`
		Name string `json:"name"`
	} `json:"project"`
	Run Run `json:"run"`
}

// FinishRunWithHook records the run's terminal result and, when configured,
// executes its matching hook after that durable transition.
func FinishRunWithHook(state *store.Store, run store.Run, project string, job config.Job, hookEnvironment []string, outcome store.Outcome) (*HookResult, error) {
	event, command := matchingHook(outcome.Status, job)
	if command != nil {
		outcome.HookEvent = event
	}
	if err := state.FinishRun(run.ID, run.ProjectID, run.Job, outcome); err != nil {
		return nil, err
	}
	if command == nil {
		return nil, nil
	}
	stored, err := state.RunByID(run.ID)
	if err != nil {
		return nil, err
	}
	payload := hookPayload{SchemaVersion: 1, Run: RunFromStore(state, stored)}
	payload.Project.Path = project
	payload.Project.Name = filepath.Base(project)
	stdin, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode %s hook input: %w", event, err)
	}
	if hookEnvironment == nil {
		hookEnvironment = JobEnvironment(project, job, os.Environ())
	}
	env := setHookEnvironment(hookEnvironment, "NOON_PROJECT", project)
	env = setHookEnvironment(env, "NOON_JOB", job.Name)
	env = setHookEnvironment(env, "NOON_RUN_ID", strconv.FormatInt(stored.ID, 10))
	env = setHookEnvironment(env, "NOON_DEPTH", strconv.Itoa(stored.Depth+1))
	if eventPath, ok := state.EventFilePath(stored); ok {
		env = setHookEnvironment(env, "NOON_EVENT_FILE", eventPath)
	} else {
		env = setHookEnvironment(env, "NOON_EVENT_FILE", "")
	}
	env = setHookEnvironment(env, "NOON_TRIGGER", stored.Trigger)
	env = setHookEnvironment(env, "NOON_STATUS", stored.Status)
	exitCode := 125
	if stored.ExitCode.Valid {
		exitCode = int(stored.ExitCode.Int64)
	}
	env = setHookEnvironment(env, "NOON_EXIT_CODE", strconv.Itoa(exitCode))
	logPath, hasLog := state.RunLogFilePath(stored)
	if hasLog {
		env = setHookEnvironment(env, "NOON_LOG_FILE", logPath)
	} else {
		env = setHookEnvironment(env, "NOON_LOG_FILE", "")
	}

	capture := &hookCapture{}
	result := runner.Run(context.Background(), runner.Spec{
		Argv: hookArgv(command), Dir: job.CWD, Env: env, Stdin: bytes.NewReader(stdin),
		Stdout: capture, Stderr: capture, Timeout: 30 * time.Second, KillGrace: 5 * time.Second,
	})
	status := "failed"
	hookExitCode := result.ExitCode
	switch {
	case result.TimedOut:
		status = "timed_out"
	case result.StartError == nil && result.ExitCode == 0:
		status = "ok"
	}
	hookResult := &HookResult{Event: event, Status: status, ExitCode: &hookExitCode}
	if err := state.CompleteHook(stored.ID, event, status, &hookExitCode, capture.Bytes()); err != nil {
		return hookResult, err
	}
	return hookResult, nil
}

func matchingHook(status string, job config.Job) (string, any) {
	switch status {
	case "succeeded":
		if hasHook(job.OnSuccess) {
			return "on_success", job.OnSuccess
		}
	case "failed", "timed_out":
		if hasHook(job.OnFailure) {
			return "on_failure", job.OnFailure
		}
	}
	return "", nil
}

func hasHook(command any) bool {
	switch value := command.(type) {
	case string:
		return value != ""
	case []string:
		return len(value) > 0
	default:
		return false
	}
}

func hookArgv(command any) []string {
	switch value := command.(type) {
	case string:
		return []string{"/bin/sh", "-c", value}
	case []string:
		return append([]string(nil), value...)
	default:
		return nil
	}
}

func setHookEnvironment(env []string, name, value string) []string {
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

type hookCapture struct{ data []byte }

func (w *hookCapture) Write(p []byte) (int, error) {
	if remaining := maxHookOutputBytes - len(w.data); remaining > 0 {
		w.data = append(w.data, p[:min(len(p), remaining)]...)
	}
	return len(p), nil
}

func (w *hookCapture) Bytes() []byte { return slices.Clone(w.data) }
