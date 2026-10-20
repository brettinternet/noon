package app

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
)

// NewAgentSessionID returns a cryptographically random RFC 4122 version 4 UUID.
func NewAgentSessionID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate agent session ID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(value[0:4]), binary.BigEndian.Uint16(value[4:6]),
		binary.BigEndian.Uint16(value[6:8]), binary.BigEndian.Uint16(value[8:10]),
		value[10:16]), nil
}

// RunSpec adapts a validated command job. Reserved metadata wins over all
// inherited values; no shell expansion occurs for argv or environment values.
func RunSpec(project string, job config.Job, inherited []string) (runner.Spec, error) {
	spec := runner.Spec{Dir: job.CWD}
	if job.Prompt != nil || job.PromptFile != nil {
		var prompt []byte
		if job.Prompt != nil {
			prompt = []byte(*job.Prompt)
		} else {
			// Nonblocking open prevents a FIFO from hanging before runner cancellation.
			file, err := os.OpenFile(filepath.Join(project, *job.PromptFile), os.O_RDONLY|syscall.O_NONBLOCK, 0)
			if err != nil {
				return runner.Spec{}, fmt.Errorf("prompt_file %q: %w", *job.PromptFile, err)
			}
			info, err := file.Stat()
			if err != nil || !info.Mode().IsRegular() {
				_ = file.Close()
				if err != nil {
					return runner.Spec{}, fmt.Errorf("prompt_file %q: %w", *job.PromptFile, err)
				}
				return runner.Spec{}, fmt.Errorf("prompt_file %q: must be a regular file", *job.PromptFile)
			}
			prompt, err = io.ReadAll(io.LimitReader(file, config.MaxPromptBytes+1))
			closeErr := file.Close()
			if err != nil {
				return runner.Spec{}, fmt.Errorf("prompt_file %q: %w", *job.PromptFile, err)
			}
			if closeErr != nil {
				return runner.Spec{}, fmt.Errorf("prompt_file %q: %w", *job.PromptFile, closeErr)
			}
			if len(prompt) > config.MaxPromptBytes {
				return runner.Spec{}, fmt.Errorf("prompt_file %q: exceeds 64 KiB", *job.PromptFile)
			}
		}
		spec.Argv = append([]string(nil), job.AgentCommand...)
		spec.Argv = append(spec.Argv, job.AgentArgs...)
		spec.Argv = append(spec.Argv, job.AgentPromptArgs...)
		if job.PromptMode == "argument" {
			spec.Argv = append(spec.Argv, string(prompt))
		} else {
			spec.Stdin = bytes.NewReader(prompt)
		}
	} else {
		switch run := job.Run.(type) {
		case string:
			spec.Argv = []string{"/bin/sh", "-c", run}
		case []string:
			spec.Argv = append([]string(nil), run...)
		}
	}
	inherited = slices.Clone(inherited)
	for _, path := range job.EnvFile {
		values, err := readEnvFile(project, path)
		if err != nil {
			return runner.Spec{}, err
		}
		for key, value := range values {
			inherited = append(inherited, key+"="+value)
		}
	}
	spec.Env = JobEnvironment(project, job, inherited)
	if job.Timeout != "none" {
		spec.Timeout, _ = time.ParseDuration(job.Timeout)
	}
	return spec, nil
}

// readEnvFile uses a bounded, nonblocking read so special files cannot hang startup.
func readEnvFile(project, configuredPath string) (map[string]string, error) {
	values, err := config.ReadEnvFile(project, configuredPath)
	if err != nil {
		return nil, fmt.Errorf("env_file %q: %w", configuredPath, err)
	}
	return values, nil
}

// RunSpecForExecution adds the assigned session ID to a prompt profile that
// supports caller-chosen IDs. RunSpec remains read-only for doctor and inspection.
func RunSpecForExecution(project string, job config.Job, inherited []string, sessionID string) (runner.Spec, error) {
	spec, err := RunSpec(project, job, inherited)
	if err != nil {
		return runner.Spec{}, err
	}
	if job.AgentSessionIDFlag == "" {
		return spec, nil
	}
	if sessionID == "" {
		return runner.Spec{}, fmt.Errorf("agent session ID was not assigned")
	}
	commandLength := len(job.AgentCommand)
	if commandLength > len(spec.Argv) {
		return runner.Spec{}, fmt.Errorf("agent command is missing from run specification")
	}
	argv := make([]string, 0, len(spec.Argv)+2)
	argv = append(argv, spec.Argv[:commandLength]...)
	argv = append(argv, job.AgentSessionIDFlag, sessionID)
	argv = append(argv, spec.Argv[commandLength:]...)
	spec.Argv = argv
	spec.Env = setEnvironment(spec.Env, "NOON_AGENT_SESSION_ID", sessionID)
	return spec, nil
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

// JobEnvironment overlays validated job values and reserved Noon metadata on the
// inherited process environment, returning a stable, duplicate-free environment.
func JobEnvironment(project string, job config.Job, inherited []string) []string {
	env := make(map[string]string)
	for _, entry := range inherited {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	for key, value := range job.Env {
		env[key] = value
	}
	env["NOON_PROJECT"], env["NOON_JOB"], env["NOON_TRIGGER"] = project, job.Name, "manual"
	// Run and hook metadata from a parent run (e.g. a hook calling noon run) must not leak.
	for _, key := range []string{"NOON_RUN_ID", "NOON_LAST_SUCCESS_AT", "NOON_DEPTH", "NOON_EVENT_FILE",
		"NOON_AGENT", "NOON_AGENT_SESSION_ID", "NOON_SCHEDULED_AT", "NOON_STATUS", "NOON_PREVIOUS_STATUS",
		"NOON_CONSECUTIVE_FAILURES", "NOON_EXIT_CODE", "NOON_LOG_FILE"} {
		delete(env, key)
	}
	if job.Prompt != nil || job.PromptFile != nil {
		env["NOON_AGENT"] = job.Agent
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+env[key])
	}
	return result
}
