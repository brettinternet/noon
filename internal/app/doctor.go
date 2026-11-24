package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
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
func Doctor(ctx context.Context, input config.Input, now time.Time, build config.BuildOpts) DoctorView {
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
	cfg, configErr := LoadJobs(input, now)
	check("config", "Configuration valid", "Fix configuration; run noon config check", configErr)
	if configErr == nil {
		_, err := time.LoadLocation(cfg.Timezone)
		check("timezone", cfg.Timezone, "Set a valid IANA timezone", err)
		for _, varsFile := range cfg.VarsFiles {
			if varsFile.Present {
				add("vars_file", "ok", varsFile.Configured, "")
			} else {
				add("vars_file", "warn", varsFile.Configured+" (missing; using expression defaults)", "Create it or correct vars_file")
			}
		}
		listenerChecks(cfg.Serve, add)
	} else if strings.Contains(configErr.Error(), "timezone") {
		add("timezone", "fail", configErr.Error(), "Set a valid IANA timezone")
	}
	// Scheduled jobs inherit the installed unit's PATH, not this shell's.
	jobEnv, schedulerPath, unitPath := os.Environ(), "", false
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
		if record, exists, err := schedulerRecord(view.StateDir); err == nil && exists && schedulerLive(record, now) {
			mismatch, known := compareSchedulerBuild(record, build)
			hint := "Run noon service install to reconcile a service-owned scheduler, or restart its foreground supervisor; restarts interrupt active jobs"
			if mismatch {
				add("scheduler_version_mismatch", "warn", schedulerBuildMessage(record, build), hint)
			} else if !known {
				add("scheduler_build_unknown", "warn", schedulerBuildMessage(record, build), "Build metadata is unknown; run noon service restart explicitly for an installed service, or restart its foreground supervisor")
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
					if path, installed, err := service.InstalledPath(options); err != nil {
						add("service_path", "warn", err.Error(), "noon service install")
					} else if installed {
						jobEnv, schedulerPath, unitPath = schedulerEnvironment(path), path, true
					}
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
			// A disabled or non-matching job cannot run, and a local tombstone has no action or cwd.
			if !job.Enabled || !job.MatchesPlatform(cfg.Platform) || job.Run == nil && job.Prompt == nil && job.PromptFile == nil {
				continue
			}
			info, err := os.Stat(job.CWD)
			if err == nil && !info.IsDir() {
				err = fmt.Errorf("%s is not a directory", job.CWD)
			}
			check("job "+name+" cwd", job.CWD, "Create the job cwd or correct its configuration", err)
			inherited := slices.Clone(jobEnv)
			for _, path := range job.EnvFile {
				values, err := readEnvFile(cfg.Project, path)
				check("job "+name+" env_file", path, "Provide a valid readable regular env file of at most 64 KiB", err)
				for key, value := range values {
					inherited = append(inherited, key+"="+value)
				}
			}
			// Each file is read once; prompt and command checks stay independent of failures.
			job.EnvFile = nil
			if job.PromptFile != nil {
				_, err := RunSpec(cfg.Project, job, nil)
				check("job "+name+" prompt_file", *job.PromptFile, "Provide a readable regular prompt file of at most 64 KiB", err)
				// Resolve the command independently, even when the prompt file is broken.
				empty := ""
				job.PromptFile = nil
				job.Prompt = &empty
			}
			spec, err := RunSpec(cfg.Project, job, inherited)
			if err == nil && len(spec.Argv) > 0 {
				_, err = runner.ResolveExecutable(spec.Argv[0], spec.Env, spec.Dir)
				hint := "Install " + spec.Argv[0] + " or correct the job PATH/command"
				if unitPath && errors.Is(err, exec.ErrNotFound) && environmentValue(spec.Env, "PATH") == schedulerPath {
					err = fmt.Errorf("%s not found on the scheduler's PATH", spec.Argv[0])
					hint = "Re-run noon service install from a shell where it resolves, or set PATH in the job's env"
				}
				check("job "+name+" command", spec.Argv[0], hint, err)
			} else if err != nil {
				check("job "+name+" command", "", "Correct the job command", err)
			}
		}
	}
	return result
}

// listenerChecks reviews user-config listener settings without opening a port.
// Messages name the token file, never its contents.
func listenerChecks(serve config.Serve, add func(name, status, message, hint string)) {
	if serve.Listen != "" {
		if loopback(serve.Listen) {
			add("listener", "ok", serve.Listen, "")
		} else {
			add("listener", "warn", serve.Listen+" is not a loopback address", "Bind 127.0.0.1, or keep the path to it private (loopback, a tailnet, or a VPN)")
		}
	}
	_, _, err := config.ReadServeTokens(serve)
	reused := errors.Is(err, config.ErrTokenReuse)
	for _, token := range []struct{ name, path string }{{"api_token_file", serve.APITokenFile}, {"web_token_file", serve.WebTokenFile}} {
		if token.path == "" {
			continue
		}
		if _, err := config.ReadTokenFile(token.path); err != nil {
			add(token.name, "fail", err.Error(), "umask 077; openssl rand -base64 32 > "+token.path)
			continue
		}
		if reused && token.name == "web_token_file" {
			add(token.name, "fail", config.ErrTokenReuse.Error(), "umask 077; openssl rand -base64 32 > "+token.path)
			continue
		}
		info, err := os.Stat(token.path)
		if err == nil && info.Mode().Perm()&0o077 != 0 {
			add(token.name, "warn", token.path+" is accessible by group or others", "chmod 600 "+token.path)
			continue
		}
		add(token.name, "ok", token.path, "")
	}
}

func loopback(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
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

// MissingCommand returns a job's executable when the environment scheduled runs
// inherit cannot find it, or "". unitPath reports whether that environment uses
// an installed service unit's PATH. Shell-string jobs check only the shell.
func MissingCommand(project string, job config.Job, stateDir string) (missing string, unitPath bool) {
	env := os.Environ()
	if options, err := service.CurrentConfig(stateDir); err == nil {
		if path, installed, err := service.InstalledPath(options); err == nil && installed {
			env, unitPath = schedulerEnvironment(path), true
		}
	}
	if job.PromptFile != nil {
		empty := ""
		job.PromptFile, job.Prompt = nil, &empty
	}
	spec, err := RunSpec(project, job, env)
	if err != nil || len(spec.Argv) == 0 {
		return "", unitPath
	}
	if _, err := runner.ResolveExecutable(spec.Argv[0], spec.Env, spec.Dir); errors.Is(err, exec.ErrNotFound) {
		return spec.Argv[0], unitPath
	}
	return "", unitPath
}

func schedulerEnvironment(path string) []string {
	return setEnvironment(os.Environ(), "PATH", path)
}

func environmentValue(env []string, name string) string {
	value := ""
	for _, entry := range env {
		if key, v, ok := strings.Cut(entry, "="); ok && key == name {
			value = v
		}
	}
	return value
}
