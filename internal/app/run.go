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
	spec.Env = JobEnvironment(project, job, inherited)
	if job.Timeout != "none" {
		spec.Timeout, _ = time.ParseDuration(job.Timeout)
	}
	return spec, nil
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
	delete(env, "NOON_RUN_ID")
	delete(env, "NOON_LAST_SUCCESS_AT")
	delete(env, "NOON_DEPTH")
	delete(env, "NOON_EVENT_FILE")
	delete(env, "NOON_AGENT_SESSION_ID")
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
