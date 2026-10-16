package app

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/brettinternet/bump/internal/config"
	"github.com/brettinternet/bump/internal/runner"
)

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
	env := make(map[string]string)
	for _, entry := range inherited {
		if key, value, ok := strings.Cut(entry, "="); ok {
			env[key] = value
		}
	}
	for key, value := range job.Env {
		env[key] = value
	}
	env["BUMP_PROJECT"], env["BUMP_JOB"], env["BUMP_TRIGGER"] = project, job.Name, "manual"
	delete(env, "BUMP_RUN_ID")
	delete(env, "BUMP_LAST_SUCCESS_AT")
	if job.Prompt != nil || job.PromptFile != nil {
		env["BUMP_AGENT"] = job.Agent
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		spec.Env = append(spec.Env, key+"="+env[key])
	}
	if job.Timeout != "none" {
		spec.Timeout, _ = time.ParseDuration(job.Timeout)
	}
	return spec, nil
}
