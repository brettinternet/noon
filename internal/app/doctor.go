package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/runner"
	"github.com/brettinternet/noon/internal/service"
	"github.com/brettinternet/noon/internal/store"
	"golang.org/x/sys/unix"
)

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}
type DoctorView struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

// Doctor checks setup without executing jobs, creating state, or fixing anything.
func Doctor(ctx context.Context, input config.Input, now time.Time) DoctorView {
	result := DoctorView{OK: true, Checks: []Check{}}
	add := func(name, status, message, hint string) {
		result.Checks = append(result.Checks, Check{name, status, message, hint})
		if status == "fail" {
			result.OK = false
		}
	}
	check := func(name, message, hint string, err error) {
		if err != nil {
			add(name, "fail", err.Error(), hint)
		} else {
			add(name, "ok", message, "")
		}
	}
	cfg, configErr := config.Load(input)
	check("config", "Configuration valid", "Fix configuration; run noon config check", configErr)
	if configErr == nil {
		_, err := time.LoadLocation(cfg.Timezone)
		check("timezone", cfg.Timezone, "Set a valid IANA timezone", err)
	} else if strings.Contains(configErr.Error(), "timezone") {
		add("timezone", "fail", configErr.Error(), "Set a valid IANA timezone")
	}
	view, inspectErr := inspectionForDiagnostics(input, now)
	if inspectErr != nil {
		add("state", "fail", inspectErr.Error(), "Fix configuration so the state directory can be resolved")
	} else {
		check("state_directory", "State location is writable", "Choose a writable --state-dir or fix directory permissions", writableState(view.StateDir))
		state, err := store.OpenExisting(view.StateDir)
		if errors.Is(err, store.ErrNoDatabase) {
			add("database", "ok", "No database yet", "")
		} else {
			check("database", "Database schema compatible", "Check state permissions or upgrade Noon; preserve the database", err)
			if err == nil {
				_ = state.Close()
			}
		}
		if err == nil || errors.Is(err, store.ErrNoDatabase) {
			enriched, err := WithApprovals(view)
			if err == nil {
				enriched, err = WithScheduler(enriched, now)
			}
			if err != nil {
				add("scheduler", "fail", err.Error(), "Inspect the state database")
			} else {
				scheduled, pending := false, false
				for _, job := range enriched.Jobs {
					scheduled = scheduled || job.Schedule != nil
					pending = pending || job.Enabled && job.Approval == "needs_approval"
				}
				if scheduled && !enriched.Project.Enabled {
					add("enabled", "warn", "Project is not enabled", "noon enable")
				} else {
					add("enabled", "ok", "Project enabled or no scheduled jobs", "")
				}
				if pending {
					add("approvals", "warn", "Shared jobs need approval", "Review jobs, then noon enable")
				} else {
					add("approvals", "ok", "No approvals pending", "")
				}
				if enriched.Project.ConfigError != "" && enriched.Project.ConfigError != view.Project.ConfigError {
					add("scheduler_config", "fail", enriched.Project.ConfigError, "Fix the scheduler's recorded configuration")
				}
				if enriched.Project.Enabled && enriched.Project.Scheduler != "running" {
					add("scheduler", "warn", "Scheduler heartbeat is absent or stale", "noon service install, or noon serve")
				} else {
					add("scheduler", "ok", "Scheduler running or project not enabled", "")
				}
			}
		}
		options, err := service.CurrentConfig(view.StateDir)
		if err == nil {
			_, installed, serviceErr := service.IsInstalled(options)
			if !service.IsUnsupported(serviceErr) {
				if serviceErr != nil {
					add("service", "warn", serviceErr.Error(), "Inspect noon service")
				} else if !installed {
					add("service", "warn", "Per-user service is not installed", "noon service install")
				} else {
					add("service", "ok", "Per-user service installed", "")
				}
			}
		} else {
			add("service", "warn", err.Error(), "Inspect noon service")
		}
	}
	if configErr == nil {
		names := make([]string, 0, len(cfg.Jobs))
		for name := range cfg.Jobs {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			job := cfg.Jobs[name]
			// A disabled job cannot run, and a local tombstone has no action or cwd.
			if !job.Enabled || job.Run == nil && job.Prompt == nil && job.PromptFile == nil {
				continue
			}
			info, err := os.Stat(job.CWD)
			if err == nil && !info.IsDir() {
				err = fmt.Errorf("%s is not a directory", job.CWD)
			}
			check("job "+name+" cwd", job.CWD, "Create the job cwd or correct its configuration", err)
			if job.PromptFile != nil {
				_, err := RunSpec(cfg.Project, job, nil)
				check("job "+name+" prompt_file", *job.PromptFile, "Provide a readable regular prompt file of at most 64 KiB", err)
				// Resolve the command independently, even when the prompt file is broken.
				empty := ""
				job.PromptFile = nil
				job.Prompt = &empty
			}
			spec, err := RunSpec(cfg.Project, job, os.Environ())
			if err == nil && len(spec.Argv) > 0 {
				_, err = runner.ResolveExecutable(spec.Argv[0], spec.Env, spec.Dir)
				check("job "+name+" command", spec.Argv[0], "Install "+spec.Argv[0]+" or correct the job PATH/command", err)
			} else if err != nil {
				check("job "+name+" command", "", "Correct the job command", err)
			}
		}
	}
	return result
}

func writableState(path string) error {
	for {
		info, err := os.Stat(path)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", path)
			}
			if err := unix.Access(path, unix.W_OK|unix.X_OK); err != nil {
				return fmt.Errorf("%s is not writable/searchable: %w", path, err)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return err
		}
		path = parent
	}
}
