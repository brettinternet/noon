package app

import (
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/bump/internal/config"
	"github.com/brettinternet/bump/internal/runner"
)

// RunSpec adapts a validated command job. Reserved metadata wins over all
// inherited values; no shell expansion occurs for argv or environment values.
func RunSpec(project string, job config.Job, inherited []string) runner.Spec {
	spec := runner.Spec{Dir: job.CWD}
	switch run := job.Run.(type) {
	case string:
		spec.Argv = []string{"/bin/sh", "-c", run}
	case []string:
		spec.Argv = append([]string(nil), run...)
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
	return spec
}
