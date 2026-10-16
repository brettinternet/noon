package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/brettinternet/bump/internal/config"
)

func TestLoadJobs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, shared, local, user, want string
		check                           func(*testing.T, config.Config)
	}{
		{name: "all fields", shared: "jobs:\n  tests:\n    description: Tests\n    schedule: weekdays at 9am\n    run: [go, test, '']\n    cwd: subdir\n    env: {TOKEN: secret}\n    timeout: 90s\n    overlap: queue\n    missed: skip\n    enabled: false\n", check: func(t *testing.T, c config.Config) {
			j := c.Jobs["tests"]
			if j.Description != "Tests" || j.Schedule.String() != "weekdays at 09:00" || len(j.Run.([]string)) != 3 || j.CWD != filepath.Join(c.Project, "subdir") || j.Env["TOKEN"] != "secret" || j.Timeout != "1m30s" || j.Overlap != "queue" || j.Missed != "skip" || j.Enabled || !j.Shared || j.Overrides || !filepath.IsAbs(j.Source) {
				t.Fatalf("job: %+v", j)
			}
		}},
		{name: "empty", shared: "jobs: {}", check: func(t *testing.T, c config.Config) {
			if len(c.Jobs) != 0 {
				t.Fatal(c.Jobs)
			}
		}},
		{name: "defaults", shared: "jobs: {tests: {run: 'true'}}", check: func(t *testing.T, c config.Config) {
			j := c.Jobs["tests"]
			if j.Run != "true" || j.Timeout != "1h" || j.Overlap != "skip" || j.Missed != "run_once" || !j.Enabled || j.CWD != c.Project || j.Schedule != nil {
				t.Fatalf("defaults: %+v", j)
			}
		}},
		{name: "whole override", shared: "jobs: {tests: {run: original, env: {TOKEN: secret}, schedule: daily, timeout: 5m}}", local: "jobs: {tests: {run: replacement}}", check: func(t *testing.T, c config.Config) {
			j := c.Jobs["tests"]
			if j.Run != "replacement" || len(j.Env) != 0 || j.Schedule != nil || j.Timeout != "1h" || j.Shared || !j.Overrides || filepath.Base(j.Source) != ".bump.yaml" {
				t.Fatalf("override merged fields: %+v", j)
			}
		}},
		{name: "toggle", shared: "jobs: {tests: {run: original, env: {TOKEN: secret}}}", local: "jobs: {tests: {enabled: false}}", check: func(t *testing.T, c config.Config) {
			j := c.Jobs["tests"]
			if j.Enabled || j.Run != "original" || j.Env["TOKEN"] != "secret" || !j.Shared || !j.Overrides || filepath.Base(j.Source) != ".bump.yaml" {
				t.Fatalf("toggle lost execution provenance: %+v", j)
			}
		}},
		{name: "orphan toggle", local: "jobs: {tests: {enabled: false}}", want: "enabled-only override requires a shared job"},
		{name: "user jobs", user: "jobs: {}", want: "allowed only in bump.yaml, .bump.yaml, or --config"},
		{name: "name", shared: "jobs:\n  1tests: {run: hi}", want: "bump.yaml:2: jobs.1tests:"},
		{name: "unknown field", shared: "jobs:\n  tests:\n    run: hi\n    prompt: nope", want: "bump.yaml:4: jobs.tests.prompt: unknown field"},
		{name: "schedule suggestion", shared: "jobs:\n  tests:\n    schedule: every day\n    run: hi", want: "bump.yaml:3: jobs.tests.schedule: every day needs a time: try every day at 9am"},
		{name: "numeric env", shared: "jobs: {tests: {run: hi, env: {COUNT: 1}}}", want: "jobs.tests.env.COUNT: must be a string (quote numbers)"},
		{name: "reserved env", shared: "jobs: {tests: {run: hi, env: {BUMP_JOB: secret}}}", want: "BUMP_ is reserved"},
		{name: "bad env name", shared: "jobs: {tests: {run: hi, env: {bad-name: secret}}}", want: "invalid or reserved environment name"},
		{name: "null", shared: "jobs: {tests: {run: null}}", want: "jobs.tests.run: must be a string"},
		{name: "alias", shared: "jobs: {tests: {run: &run hi}, other: {run: *run}}", want: "jobs.other.run: must be a string"},
		{name: "duplicate job", shared: "jobs: {tests: {run: hi}, tests: {run: bye}}", want: "jobs.tests: duplicate field"},
		{name: "duplicate field", shared: "jobs: {tests: {run: hi, run: bye}}", want: "jobs.tests.run: duplicate field"},
		{name: "duplicate env", shared: "jobs: {tests: {run: hi, env: {KEY: a, KEY: b}}}", want: "jobs.tests.env.KEY: duplicate field"},
		{name: "empty run", shared: "jobs: {tests: {run: ''}}", want: "jobs.tests.run: must not be empty"},
		{name: "empty argv", shared: "jobs: {tests: {run: []}}", want: "jobs.tests.run: must not be empty"},
		{name: "numeric argv", shared: "jobs: {tests: {run: [echo, 1]}}", want: "jobs.tests.run: must be a string"},
		{name: "timeout zero", shared: "jobs: {tests: {run: hi, timeout: 0s}}", want: "must be at least 1s"},
		{name: "timeout none", shared: "jobs: {tests: {run: hi, timeout: none}}", check: func(t *testing.T, c config.Config) {
			if c.Jobs["tests"].Timeout != "none" {
				t.Fatal(c.Jobs)
			}
		}},
		{name: "overlap", shared: "jobs: {tests: {run: hi, overlap: parallel}}", want: "must be skip or queue"},
		{name: "missed", shared: "jobs: {tests: {run: hi, missed: burst}}", want: "must be run_once or skip"},
		{name: "enabled", shared: "jobs: {tests: {run: hi, enabled: 'false'}}", want: "must be a boolean"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := canonical(t, t.TempDir())
			user := filepath.Join(root, "user")
			if tc.shared != "" {
				put(t, filepath.Join(root, "bump.yaml"), tc.shared)
			}
			if tc.local != "" {
				put(t, filepath.Join(root, ".bump.yaml"), tc.local)
			}
			if tc.user != "" {
				put(t, filepath.Join(user, "bump/config.yaml"), tc.user)
			}
			cfg, err := config.Load(config.Input{CWD: root, UserConfigDir: user})
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("want %q, got %v", tc.want, err)
				}
				if strings.Contains(err.Error(), "secret") {
					t.Fatalf("error leaked environment value: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
	t.Run("explicit file is shared", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		file := filepath.Join(root, "custom.yaml")
		put(t, file, "jobs: {tests: {run: hi}}")
		cfg, err := config.Load(config.Input{CWD: root, File: &file})
		if err != nil {
			t.Fatal(err)
		}
		if !cfg.Jobs["tests"].Shared || cfg.Jobs["tests"].Source != file {
			t.Fatal(cfg.Jobs)
		}
	})
}

func TestJobDigest(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	load := func(body, local string) string {
		t.Helper()
		put(t, filepath.Join(root, "bump.yaml"), "jobs:\n  tests:\n"+body)
		put(t, filepath.Join(root, ".bump.yaml"), local)
		cfg, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Jobs["tests"].Digest
	}
	base := "    run: echo hi\n    schedule: weekdays at 9am\n    env: {A: a, B: b}\n    cwd: subdir\n    timeout: 1h\n"
	digest := load(base, "{}")
	if len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		t.Fatal(digest)
	}
	equivalent := "    timeout: 60m\n    cwd: subdir\n    env: {B: b, A: a}\n    schedule: WEEKDAYS AT 09:00\n    run: echo hi # comment\n    description: changed\n    enabled: false\n"
	if load(equivalent, "{}") != digest || load(base, "jobs: {tests: {enabled: false}}") != digest {
		t.Fatal("cosmetic edits/toggle changed digest")
	}
	for _, change := range [][2]string{{"echo hi", "echo bye"}, {"9am", "10am"}, {"A: a", "A: changed"}, {"subdir", "other"}, {"1h", "2h"}} {
		if load(strings.Replace(base, change[0], change[1], 1), "{}") == digest {
			t.Fatalf("%v did not change digest", change)
		}
	}
	// Unset fields must stay omitted; explicit defaults participate in approval.
	if load("    run: hi\n", "{}") == load("    run: hi\n    timeout: 1h\n", "{}") {
		t.Fatal("unset field included in digest")
	}
}
