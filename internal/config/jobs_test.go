package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brettinternet/noon/internal/config"
	"github.com/google/jsonschema-go/jsonschema"
)

// End dates must keep their absolute meaning through DST and config layering.
func TestUntilParsing(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "noon.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{"2099-01-01 00:00", float64(12), []any{"x"}} {
		err := resolved.Validate(map[string]any{"jobs": map[string]any{"probe": map[string]any{"run": "true", "until": value}}})
		_, isString := value.(string)
		if (err == nil) != isString {
			t.Fatalf("until schema value %v: %v", value, err)
		}
	}
	root := t.TempDir()
	for _, tc := range []struct{ value, want string }{
		{"2099-01-01 00:00", "2099-01-01T00:00:00-07:00"},
		{"2000-01-01 00:00", "2000-01-01T00:00:00-07:00"},
		{"2026-11-01 01:30", "2026-11-01T01:30:00-06:00"},
		{"2026-03-08 02:30", "2026-03-08T03:00:00-06:00"},
		{"2026-11-01T01:30:00-07:00", "2026-11-01T01:30:00-07:00"},
	} {
		put(t, filepath.Join(root, "noon.yaml"), "timezone: UTC\njobs:\n  probe:\n    run: 'true'\n    until: "+tc.value+"\n")
		put(t, filepath.Join(root, ".git", "noon.yaml"), "timezone: America/Denver\n")
		cfg, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatal(err)
		}
		job := cfg.Jobs["probe"]
		if got := job.Until.Format(time.RFC3339); got != tc.want {
			t.Fatalf("%s = %s, want %s", tc.value, got, tc.want)
		}
		if job.Ended(job.Until.Add(-time.Nanosecond)) || !job.Ended(*job.Until) {
			t.Fatal("until is not an exclusive execution boundary")
		}
		utc := "UTC"
		overridden, err := config.Load(config.Input{CWD: root, Timezone: &utc})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(tc.value, "T") && overridden.Jobs["probe"].Until.Location().String() != "UTC" {
			t.Fatal("CLI timezone override was ignored")
		}
	}
	put(t, filepath.Join(root, "noon.yaml"), "jobs: {probe: {run: 'true', until: 0001-01-01T00:00:00Z}}\n")
	utc := "UTC"
	zeroEnd, err := config.Load(config.Input{CWD: root, Timezone: &utc})
	if err != nil || zeroEnd.Jobs["probe"].Until == nil || !zeroEnd.Jobs["probe"].Until.IsZero() || !zeroEnd.Jobs["probe"].Ended(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("explicit zero end treated as absent: %+v %v", zeroEnd.Jobs["probe"], err)
	}
	for _, value := range []string{"tomorrow", "'17:00'", "2h", "2026-13-01 00:00", "[x]", "null", "12"} {
		put(t, filepath.Join(root, "noon.yaml"), "jobs:\n  probe:\n    run: 'true'\n    until: "+value+"\n")
		_, err := config.Load(config.Input{CWD: root})
		if err == nil || !strings.Contains(err.Error(), "noon.yaml:4: jobs.probe.until:") {
			t.Fatalf("%s diagnostic = %v", value, err)
		}
	}
}

func TestVarsFileSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "noon.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{".noon.vars", "/tmp/noon.vars"} {
		if err := resolved.Validate(map[string]any{"vars_file": value}); err != nil {
			t.Errorf("schema rejected vars_file %v: %v", value, err)
		}
	}
	for _, value := range []any{"", " ", float64(1), []any{".noon.vars"}, nil, map[string]any{"path": ".noon.vars"}} {
		if err := resolved.Validate(map[string]any{"vars_file": value}); err == nil {
			t.Errorf("schema accepted vars_file %v", value)
		}
	}
}

func TestNoopExitCodeSchemaRange(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "noon.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	instance := func(value any) map[string]any {
		return map[string]any{"jobs": map[string]any{"poll": map[string]any{"run": "true", "noop_exit_code": value}}}
	}
	for _, code := range []float64{1, 124} {
		if err := resolved.Validate(instance(code)); err != nil {
			t.Errorf("schema rejected valid noop_exit_code %v: %v", code, err)
		}
	}
	for _, code := range []any{float64(0), float64(125), float64(128), float64(256), "78"} {
		if err := resolved.Validate(instance(code)); err == nil {
			t.Errorf("schema accepted invalid noop_exit_code %v", code)
		}
	}
	if err := resolved.Validate(map[string]any{"jobs": map[string]any{"review": map[string]any{"prompt": "review"}}}); err != nil {
		t.Fatalf("schema rejected control prompt job: %v", err)
	}
	if err := resolved.Validate(map[string]any{"jobs": map[string]any{"review": map[string]any{"prompt": "review", "noop_exit_code": float64(78)}}}); err == nil {
		t.Fatal("schema accepted noop_exit_code on a prompt job")
	}
}

func TestJitterSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "noon.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	instance := func(jitter any) map[string]any {
		return map[string]any{"jobs": map[string]any{"digest": map[string]any{"schedule": "every day at 9am", "run": "true", "jitter": jitter}}}
	}
	if err := resolved.Validate(instance("20m")); err != nil {
		t.Fatalf("schema rejected valid jitter string: %v", err)
	}
	for _, value := range []any{float64(20), []any{"20m"}} {
		if err := resolved.Validate(instance(value)); err == nil {
			t.Errorf("schema accepted non-string jitter %v", value)
		}
	}
}

func TestAgentArgsSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "noon.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	instance := func(job map[string]any) map[string]any {
		return map[string]any{"jobs": map[string]any{"review": job}}
	}
	for _, job := range []map[string]any{
		{"prompt": "review", "agent": "claude", "agent_args": []any{"--model", "haiku", ""}},
		{"prompt_file": "prompts/review.md", "agent": "codex", "agent_args": []any{"-s", "read-only"}},
	} {
		if err := resolved.Validate(instance(job)); err != nil {
			t.Errorf("schema rejected valid agent_args job %+v: %v", job, err)
		}
	}
	for _, job := range []map[string]any{
		{"run": "true", "agent_args": []any{"--model"}},
		{"prompt": "review", "agent_args": []any{"--model"}},
		{"prompt": "review", "agent": "claude", "agent_args": []any{}},
		{"prompt": "review", "agent": "claude", "agent_args": []any{"--model", float64(1)}},
	} {
		if err := resolved.Validate(instance(job)); err == nil {
			t.Errorf("schema accepted invalid agent_args job %+v", job)
		}
	}
}

// Env-file validation and canonical ordered paths are an approval boundary.
func TestEnvFileDefinition(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "noon.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		value any
		valid bool
	}{
		{".env", true}, {[]any{".env", "/tmp/other.env"}, true},
		{"", false}, {[]any{}, false}, {[]any{""}, false}, {float64(1), false},
		{[]any{float64(1)}, false}, {map[string]any{"path": ".env"}, false}, {nil, false},
	} {
		instance := map[string]any{"jobs": map[string]any{"tests": map[string]any{"run": "true", "env_file": tc.value}}}
		if err := resolved.Validate(instance); (err == nil) != tc.valid {
			t.Errorf("schema %v: %v", tc.value, err)
		}
	}
	root := t.TempDir()
	load := func(value string) (config.Job, error) {
		put(t, filepath.Join(root, "noon.yaml"), "jobs:\n  tests:\n    run: 'true'\n    env_file: "+value+"\n")
		cfg, err := config.Load(config.Input{CWD: root})
		return cfg.Jobs["tests"], err
	}
	for _, value := range []string{`""`, `[]`, `[""]`, `1`, `[1]`, `{path: .env}`, `null`} {
		_, err := load(value)
		if err == nil || !strings.Contains(err.Error(), "noon.yaml:4: jobs.tests.env_file:") {
			t.Errorf("%s: %v", value, err)
		}
	}
	one, err := load(".env")
	if err != nil || !reflect.DeepEqual(one.EnvFile, []string{".env"}) {
		t.Fatalf("string form: %v %v", one.EnvFile, err)
	}
	list, err := load("[.env]")
	if err != nil || list.Digest != one.Digest {
		t.Fatalf("normalization: %v", err)
	}
	ordered, err := load("[.env, /tmp/other.env]")
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"[/tmp/other.env, .env]", "[.env, /tmp/changed.env]", "/tmp/other.env"} {
		changed, err := load(value)
		if err != nil || changed.Digest == ordered.Digest {
			t.Fatalf("%s did not change digest: %v", value, err)
		}
	}
	put(t, filepath.Join(root, ".env"), "KEY=rotated\n")
	rotated, err := load("[.env, /tmp/other.env]")
	if err != nil || rotated.Digest != ordered.Digest {
		t.Fatalf("contents changed digest: %v", err)
	}
	encoded, err := config.AddJob(nil, "tests", config.Job{Name: "tests", Run: "true", Enabled: true, EnvFile: ordered.EnvFile})
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := config.JobFromSource(encoded, "tests", config.BuiltInAgents(), "", nil, nil, false, "", "")
	if err != nil || roundTrip.Digest != ordered.Digest {
		t.Fatalf("round trip lost paths: %s %v", encoded, err)
	}
}

func TestLoadJobs(t *testing.T) {
	t.Parallel()
	builtins := config.BuiltInAgents()
	if builtins["claude"].SessionIDFlag != "--session-id" || builtins["pi"].SessionIDFlag != "--session-id" {
		t.Fatalf("session ID built-ins: claude=%q pi=%q", builtins["claude"].SessionIDFlag, builtins["pi"].SessionIDFlag)
	}
	for _, name := range []string{"codex", "gemini", "opencode"} {
		if builtins[name].SessionIDFlag != "" {
			t.Errorf("%s unexpectedly supports session IDs: %q", name, builtins[name].SessionIDFlag)
		}
	}
	for _, tc := range []struct {
		name, command, promptArg string
	}{
		{name: "codex", command: "codex exec", promptArg: "-"},
		{name: "gemini", command: "gemini", promptArg: "-p"},
		{name: "opencode", command: "opencode run", promptArg: "--"},
	} {
		profile := builtins[tc.name]
		if got := strings.Join(profile.Command, " "); got != tc.command || !reflect.DeepEqual(profile.PromptArgs, []string{tc.promptArg}) {
			t.Errorf("%s profile = command %q prompt_args %q, want %q and [%q]", tc.name, got, profile.PromptArgs, tc.command, tc.promptArg)
		}
	}
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
			if j.Run != "true" || j.Timeout != "1h" || j.Cooldown != 0 || j.Requests != "merge" || j.Overlap != "skip" || j.Missed != "run_once" || !j.Enabled || j.CWD != c.Project || j.Schedule != nil {
				t.Fatalf("defaults: %+v", j)
			}
		}},
		{name: "splay", shared: "jobs: {tests: {run: 'true', schedule: '@daily', splay: 600s}}", check: func(t *testing.T, c config.Config) {
			job := c.Jobs["tests"]
			if job.Splay != 10*time.Minute {
				t.Fatalf("splay = %s", job.Splay)
			}
			digest, err := config.DigestJobDefinition(job, c.Agents, c.DefaultAgent)
			if err != nil || digest != job.Digest {
				t.Fatalf("splay round trip: %s, %v", digest, err)
			}
		}},
		{name: "jitter", shared: "jobs: {tests: {run: 'true', schedule: every 1h, jitter: 30m}}", check: func(t *testing.T, c config.Config) {
			job := c.Jobs["tests"]
			if job.Jitter != 30*time.Minute {
				t.Fatalf("jitter = %s", job.Jitter)
			}
			digest, err := config.DigestJobDefinition(job, c.Agents, c.DefaultAgent)
			if err != nil || digest != job.Digest {
				t.Fatalf("jitter round trip: %s, %v", digest, err)
			}
		}},
		{name: "jitter cron maximum", shared: "jobs: {tests: {run: hi, schedule: '*/15 * * * *', jitter: 7m30s}}"},
		{name: "jitter exact interval maximum", shared: "jobs: {tests: {run: hi, schedule: every 1h, jitter: 30m}}"},
		{name: "jitter exact midnight wrap maximum", shared: "jobs: {tests: {run: hi, schedule: '0 0,23 * * *', jitter: 30m}}"},
		{name: "jitter exact multi-time maximum", shared: "jobs: {tests: {run: hi, schedule: '59 1,2 * * *', jitter: 30m}}"},
		{name: "jitter manual", shared: "jobs: {tests: {run: hi, jitter: 20m}}", want: "noon.yaml:1: jobs.tests.jitter: requires a schedule"},
		{name: "jitter cron too large", shared: "jobs: {tests: {run: hi, schedule: '*/15 * * * *', jitter: 10m}}", want: "noon.yaml:1: jobs.tests.jitter: must not exceed half the schedule's shortest gap (maximum allowed 7m30s)"},
		{name: "jitter interval too large", shared: "jobs: {tests: {run: hi, schedule: every 1h, jitter: 31m}}", want: "maximum allowed 30m"},
		{name: "jitter midnight wrap too large", shared: "jobs: {tests: {run: hi, schedule: '0 0,23 * * *', jitter: 31m}}", want: "maximum allowed 30m"},
		{name: "jitter zero", shared: "jobs: {tests: {run: hi, schedule: '@daily', jitter: 0s}}", want: "noon.yaml:1: jobs.tests.jitter: must be at least 1s"},
		{name: "jitter number", shared: "jobs: {tests: {run: hi, schedule: '@daily', jitter: 20}}", want: "noon.yaml:1: jobs.tests.jitter: must be a string"},
		{name: "jitter missing unit", shared: "jobs: {tests: {run: hi, schedule: '@daily', jitter: '20'}}", want: "noon.yaml:1: jobs.tests.jitter: invalid duration"},
		{name: "jitter list", shared: "jobs: {tests: {run: hi, schedule: '@daily', jitter: [20m]}}", want: "noon.yaml:1: jobs.tests.jitter: must be a string"},
		{name: "splay manual", shared: "jobs: {tests: {run: hi, splay: 10m}}", want: "noon.yaml:1: jobs.tests.splay: requires a cron, macro, or calendar schedule"},
		{name: "splay interval", shared: "jobs: {tests: {run: hi, schedule: every 6h, splay: 10m}}", want: "noon.yaml:1: jobs.tests.splay: cannot be used with an interval schedule"},
		{name: "splay zero", shared: "jobs: {tests: {run: hi, schedule: '@daily', splay: 0s}}", want: "noon.yaml:1: jobs.tests.splay: must be between 1s and 1h"},
		{name: "splay large", shared: "jobs: {tests: {run: hi, schedule: '@daily', splay: 2h}}", want: "noon.yaml:1: jobs.tests.splay: must be between 1s and 1h"},
		{name: "splay number", shared: "jobs: {tests: {run: hi, schedule: '@daily', splay: 10}}", want: "noon.yaml:1: jobs.tests.splay: must be a string"},
		{name: "splay missing unit", shared: "jobs: {tests: {run: hi, schedule: '@daily', splay: '10'}}", want: "noon.yaml:1: jobs.tests.splay: invalid duration"},
		{name: "splay list", shared: "jobs: {tests: {run: hi, schedule: '@daily', splay: [10m]}}", want: "noon.yaml:1: jobs.tests.splay: must be a string"},
		{name: "cooldown", shared: "jobs: {tests: {run: 'true', cooldown: 2m}}", check: func(t *testing.T, c config.Config) {
			if got := c.Jobs["tests"].Cooldown; got != 2*time.Minute {
				t.Fatalf("cooldown = %s, want 2m", got)
			}
		}},
		{name: "platform constraints", shared: "jobs:\n  tests:\n    run: 'true'\n    os: [linux, darwin]\n    arch: [arm64, amd64]\n", check: func(t *testing.T, c config.Config) {
			job := c.Jobs["tests"]
			if !reflect.DeepEqual(job.OS, []string{"darwin", "linux"}) || !reflect.DeepEqual(job.Arch, []string{"amd64", "arm64"}) {
				t.Fatalf("platform constraints: os=%v arch=%v", job.OS, job.Arch)
			}
		}},
		{name: "macos alias rejected", shared: "jobs:\n  tests:\n    run: hi\n    os: [macos]\n", want: "noon.yaml:4: jobs.tests.os: unsupported value \"macos\"; use darwin"},
		{name: "macOS alias rejected", shared: "jobs:\n  tests:\n    run: hi\n    os: [macOS]\n", want: "noon.yaml:4: jobs.tests.os: unsupported value \"macOS\"; use darwin"},
		{name: "x86_64 alias rejected", shared: "jobs:\n  tests:\n    run: hi\n    arch: [x86_64]\n", want: "noon.yaml:4: jobs.tests.arch: unsupported value \"x86_64\"; use amd64"},
		{name: "x64 alias rejected", shared: "jobs:\n  tests:\n    run: hi\n    arch: [x64]\n", want: "noon.yaml:4: jobs.tests.arch: unsupported value \"x64\"; use amd64"},
		{name: "aarch64 alias rejected", shared: "jobs:\n  tests:\n    run: hi\n    arch: [aarch64]\n", want: "noon.yaml:4: jobs.tests.arch: unsupported value \"aarch64\"; use arm64"},
		{name: "empty os list", shared: "jobs:\n  tests:\n    run: hi\n    os: []\n", want: "noon.yaml:4: jobs.tests.os: must be a non-empty list of strings"},
		{name: "duplicate os", shared: "jobs:\n  tests:\n    run: hi\n    os: [linux, linux]\n", want: "noon.yaml:4: jobs.tests.os: must not contain duplicates"},
		{name: "platform scalar", shared: "jobs:\n  tests:\n    run: hi\n    os: darwin\n", want: "noon.yaml:4: jobs.tests.os: must be a non-empty list of strings"},
		{name: "unsupported os", shared: "jobs:\n  tests:\n    run: hi\n    os: [windows]\n", want: "noon.yaml:4: jobs.tests.os: must be one of darwin, linux"},
		{name: "requests merge", shared: "jobs: {tests: {run: 'true', requests: merge}}", check: func(t *testing.T, c config.Config) {
			if got := c.Jobs["tests"].Requests; got != "merge" {
				t.Fatalf("requests = %q, want merge", got)
			}
		}},
		{name: "requests each", shared: "jobs: {tests: {run: 'true', requests: each}}", check: func(t *testing.T, c config.Config) {
			if got := c.Jobs["tests"].Requests; got != "each" {
				t.Fatalf("requests = %q, want each", got)
			}
		}},
		{name: "agent args allow literal empty item", shared: "jobs:\n  review:\n    prompt: review\n    agent: claude\n    agent_args: [--model, haiku, '', --effort, low]\n", check: func(t *testing.T, c config.Config) {
			if got := c.Jobs["review"].AgentArgs; !reflect.DeepEqual(got, []string{"--model", "haiku", "", "--effort", "low"}) {
				t.Fatalf("agent_args = %#v", got)
			}
		}},
		{name: "agent args on run job", shared: "jobs: {tests: {run: hi, agent: claude, agent_args: [--model]}}", want: "noon.yaml:1: jobs.tests.agent_args: only valid for prompt or prompt_file jobs"},
		{name: "agent args require explicit agent despite default", shared: "jobs:\n  review:\n    prompt: review\n    agent_args: [--model]\n", user: "default_agent: claude\n", want: "noon.yaml:4: jobs.review.agent_args: requires an explicit agent on the job"},
		{name: "agent args empty list", shared: "jobs:\n  review:\n    prompt: review\n    agent: claude\n    agent_args: []\n", want: "noon.yaml:5: jobs.review.agent_args: must be a non-empty list of strings"},
		{name: "agent args non-string item", shared: "jobs:\n  review:\n    prompt: review\n    agent: claude\n    agent_args: [--model, 1]\n", want: "noon.yaml:5: jobs.review.agent_args: must be a string"},
		{name: "agent args NUL", shared: "jobs:\n  review:\n    prompt: review\n    agent: claude\n    agent_args: [--model, \"\\0\"]\n", want: "noon.yaml:5: jobs.review.agent_args: must not contain NUL"},
		{name: "profile prompt args", user: "agents:\n  custom:\n    command: [agent]\n    prompt_args: [--tail, '']\n", check: func(t *testing.T, c config.Config) {
			if got := c.Agents["custom"].PromptArgs; !reflect.DeepEqual(got, []string{"--tail", ""}) {
				t.Fatalf("profile prompt_args = %#v", got)
			}
		}},
		{name: "profile prompt args scalar", user: "agents:\n  custom:\n    command: [agent]\n    prompt_args: --tail\n", want: "config.yaml:4: agents.custom.prompt_args: must be a string list"},
		{name: "profile prompt args non-string item", user: "agents:\n  custom:\n    command: [agent]\n    prompt_args: [--tail, 1]\n", want: "config.yaml:4: agents.custom.prompt_args: must be a string"},
		{name: "noop exit code", shared: "jobs:\n  tests:\n    run: 'true'\n    noop_exit_code: 78\n", check: func(t *testing.T, c config.Config) {
			if got := c.Jobs["tests"].NoopExitCode; got != 78 {
				t.Fatalf("noop_exit_code = %d, want 78", got)
			}
		}},
		{name: "noop exit code zero", shared: "jobs:\n  tests:\n    run: 'true'\n    noop_exit_code: 0\n", want: "noon.yaml:4: jobs.tests.noop_exit_code: must be an integer from 1 to 124"},
		{name: "noop exit code start failure", shared: "jobs:\n  tests:\n    run: 'true'\n    noop_exit_code: 125\n", want: "noon.yaml:4: jobs.tests.noop_exit_code: must be an integer from 1 to 124"},
		{name: "noop exit code signal", shared: "jobs:\n  tests:\n    run: 'true'\n    noop_exit_code: 128\n", want: "noon.yaml:4: jobs.tests.noop_exit_code: must be an integer from 1 to 124"},
		{name: "noop exit code out of range", shared: "jobs:\n  tests:\n    run: 'true'\n    noop_exit_code: 256\n", want: "noon.yaml:4: jobs.tests.noop_exit_code: must be an integer from 1 to 124"},
		{name: "noop exit code string", shared: "jobs:\n  tests:\n    run: 'true'\n    noop_exit_code: \"78\"\n", want: "noon.yaml:4: jobs.tests.noop_exit_code: must be an integer from 1 to 124"},
		{name: "noop exit code on prompt", shared: "jobs:\n  review:\n    prompt: review\n    agent: claude\n    noop_exit_code: 78\n", want: "noon.yaml:5: jobs.review.noop_exit_code: only valid for run jobs"},
		{name: "invalid requests", shared: "jobs:\n  tests:\n    run: 'true'\n    requests: newest\n", want: "noon.yaml:4: jobs.tests.requests: must be merge or each"},
		{name: "requests each conflicts with cooldown", shared: "jobs:\n  tests:\n    run: 'true'\n    requests: each\n    cooldown: 5m\n", want: "noon.yaml:5: jobs.tests.cooldown: cannot be used with requests: each"},
		{name: "webhook", shared: "jobs: {tests: {run: 'true', webhook: {secret_env: GITHUB_SECRET}}}", check: func(t *testing.T, c config.Config) {
			if got := c.Jobs["tests"].Webhook; got == nil || got.SecretEnv != "GITHUB_SECRET" {
				t.Fatalf("webhook: %+v", got)
			}
		}},
		{name: "whole override", shared: "jobs: {tests: {run: original, env: {TOKEN: secret}, schedule: daily, timeout: 5m}}", local: "jobs: {tests: {run: replacement}}", check: func(t *testing.T, c config.Config) {
			j := c.Jobs["tests"]
			if j.Run != "replacement" || len(j.Env) != 0 || j.Schedule != nil || j.Timeout != "1h" || j.Shared || !j.Overrides || !strings.HasSuffix(j.Source, "/.git/noon.yaml") {
				t.Fatalf("override merged fields: %+v", j)
			}
		}},
		{name: "toggle", shared: "jobs: {tests: {run: original, env: {TOKEN: secret}}}", local: "jobs: {tests: {enabled: false}}", check: func(t *testing.T, c config.Config) {
			j := c.Jobs["tests"]
			if j.Enabled || j.Run != "original" || j.Env["TOKEN"] != "secret" || !j.Shared || !j.Overrides || !strings.HasSuffix(j.Source, "/.git/noon.yaml") {
				t.Fatalf("toggle lost execution provenance: %+v", j)
			}
		}},
		{name: "orphan toggle", local: "jobs: {tests: {enabled: false}}", want: "enabled-only override requires a shared job"},
		{name: "user jobs are validated but not imported", user: "jobs: {tests: {run: hi}}", check: func(t *testing.T, c config.Config) {
			if _, exists := c.Jobs["tests"]; exists {
				t.Fatalf("user job leaked into regular project: %+v", c.Jobs)
			}
		}},
		{name: "invalid user job", user: "jobs: {tests: {schedule: every day, run: hi}}", want: "config.yaml"},
		{name: "project hook defaults", shared: "on_success: [notify]\njobs: {tests: {run: hi}}", want: "allowed only in user config or --config files"},
		{name: "name", shared: "jobs:\n  1tests: {run: hi}", want: "noon.yaml:2: jobs.1tests:"},
		{name: "unknown field", shared: "jobs:\n  tests:\n    run: hi\n    typo: nope", want: "noon.yaml:4: jobs.tests.typo: unknown field"},
		{name: "two actions", shared: "jobs: {tests: {run: hi, prompt: nope}}", want: "exactly one of run, prompt, or prompt_file is required"},
		{name: "agent on run", shared: "jobs: {tests: {run: hi, agent: claude}}", want: "valid only for prompt or prompt_file jobs"},
		{name: "unknown agent", shared: "jobs: {tests: {prompt: hi, agent: missing}}", want: `unknown agent "missing"; known agents: claude, codex, gemini, opencode, pi`},
		{name: "prompt needs agent", shared: "jobs: {tests: {prompt: hi}}", want: "requires agent or default_agent; set agent on the job or default_agent"},
		{name: "agents outside user config", shared: "agents: {team: {command: [agent]}}", want: "agents: allowed only in user config or --config files"},
		{name: "default agent outside allowed file", shared: "default_agent: claude", want: "default_agent: allowed only in user config, .git/noon.yaml, or --config files"},
		{name: "invalid profile prompt mode", user: "agents: {team: {command: [agent], prompt: terminal}}", want: "must be stdin or argument"},
		{name: "session ID flag missing dash", user: "agents:\n  custom:\n    command: [agent]\n    session_id_flag: session-id\n", want: "config.yaml:4: agents.custom.session_id_flag: must be one flag token"},
		{name: "session ID flag single dash", user: "agents:\n  custom:\n    command: [agent]\n    session_id_flag: '-'\n", want: "config.yaml:4: agents.custom.session_id_flag: must be one flag token"},
		{name: "session ID flag double dash", user: "agents:\n  custom:\n    command: [agent]\n    session_id_flag: '--'\n", want: "config.yaml:4: agents.custom.session_id_flag: must be one flag token"},
		{name: "session ID flag whitespace", user: "agents:\n  custom:\n    command: [agent]\n    session_id_flag: '--session id'\n", want: "config.yaml:4: agents.custom.session_id_flag: must be one flag token"},
		{name: "session ID flag NUL", user: "agents:\n  custom:\n    command: [agent]\n    session_id_flag: \"--session\\0id\"\n", want: "config.yaml:4: agents.custom.session_id_flag: must be one flag token"},
		{name: "session ID flag wrong type", user: "agents:\n  custom:\n    command: [agent]\n    session_id_flag: true\n", want: "config.yaml:4: agents.custom.session_id_flag: must be a string"},
		{name: "session ID flag override", user: "agents: {claude: {command: [wrapper], session_id_flag: -s}}", check: func(t *testing.T, c config.Config) {
			if c.Agents["claude"].SessionIDFlag != "-s" {
				t.Fatalf("session_id_flag = %q", c.Agents["claude"].SessionIDFlag)
			}
		}},
		{name: "schedule suggestion", shared: "jobs:\n  tests:\n    schedule: every day\n    run: hi", want: "noon.yaml:3: jobs.tests.schedule: every day needs a time: try every day at 9am"},
		{name: "numeric env", shared: "jobs: {tests: {run: hi, env: {COUNT: 1}}}", want: "jobs.tests.env.COUNT: must be a string (quote numbers)"},
		{name: "reserved env", shared: "jobs: {tests: {run: hi, env: {NOON_JOB: secret}}}", want: "NOON_ is reserved"},
		{name: "bad env name", shared: "jobs: {tests: {run: hi, env: {bad-name: secret}}}", want: "invalid or reserved environment name"},
		{name: "null", shared: "jobs: {tests: {run: null}}", want: "jobs.tests.run: must be a string"},
		{name: "alias", shared: "jobs: {tests: {run: &run hi}, other: {run: *run}}", check: func(t *testing.T, c config.Config) {
			if c.Jobs["other"].Run != "hi" || c.Jobs["other"].Digest != c.Jobs["tests"].Digest {
				t.Fatal("alias did not resolve to the same execution definition")
			}
		}},
		{name: "duplicate job", shared: "jobs: {tests: {run: hi}, tests: {run: bye}}", want: "jobs.tests: duplicate field"},
		{name: "duplicate field", shared: "jobs: {tests: {run: hi, run: bye}}", want: "jobs.tests.run: duplicate field"},
		{name: "duplicate env", shared: "jobs: {tests: {run: hi, env: {KEY: a, KEY: b}}}", want: "jobs.tests.env.KEY: duplicate field"},
		{name: "empty run", shared: "jobs: {tests: {run: ''}}", want: "jobs.tests.run: must not be empty"},
		{name: "empty argv", shared: "jobs: {tests: {run: []}}", want: "jobs.tests.run: must not be empty"},
		{name: "numeric argv", shared: "jobs: {tests: {run: [echo, 1]}}", want: "jobs.tests.run: must be a string"},
		{name: "timeout zero", shared: "jobs: {tests: {run: hi, timeout: 0s}}", want: "must be at least 1s"},
		{name: "cooldown zero", shared: "jobs: {tests: {run: hi, cooldown: 0s}}", want: "must be at least 1s"},
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
				put(t, filepath.Join(root, "noon.yaml"), tc.shared)
			}
			if tc.local != "" {
				put(t, filepath.Join(root, ".git", "noon.yaml"), tc.local)
			}
			if tc.user != "" {
				put(t, filepath.Join(user, "noon/config.yaml"), tc.user)
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
	for _, test := range []struct{ manifest, message string }{
		{"jobs: {tests: {run: 'true', webhook: {}}}", "jobs.tests.webhook.secret_env: required"},
		{"jobs: {tests: {run: 'true', webhook: {secret_env: 'NOT VALID'}}}", "must be a valid environment variable name"},
	} {
		root := canonical(t, t.TempDir())
		put(t, filepath.Join(root, "noon.yaml"), test.manifest)
		if _, err := config.Load(config.Input{CWD: root}); err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("webhook config error = %v, want %q", err, test.message)
		}
	}
	t.Run("inherited hook defaults and job opt-out", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		user := filepath.Join(root, "user")
		put(t, filepath.Join(user, "noon/config.yaml"), "on_success: [notify, --quiet]\non_failure: 'notify failure'\n")
		put(t, filepath.Join(root, "noon.yaml"), "jobs:\n  inherited: {run: hi}\n  disabled: {run: hi, on_success: []}\n")
		cfg, err := config.Load(config.Input{CWD: root, UserConfigDir: user})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg.Jobs["inherited"].OnSuccess, []string{"notify", "--quiet"}) || cfg.Jobs["inherited"].OnFailure != "notify failure" {
			t.Fatalf("inherited hooks: %+v", cfg.Jobs["inherited"])
		}
		if hook, ok := cfg.Jobs["disabled"].OnSuccess.([]string); !ok || len(hook) != 0 || cfg.Jobs["disabled"].OnFailure != "notify failure" {
			t.Fatalf("hook opt-out did not preserve independent inherited hook: %+v", cfg.Jobs["disabled"])
		}
	})
	t.Run("explicit config hook defaults", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		file := filepath.Join(root, "custom.yaml")
		put(t, file, "on_success: [notify]\njobs: {tests: {run: hi}}")
		cfg, err := config.Load(config.Input{CWD: root, File: &file})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(cfg.Jobs["tests"].OnSuccess, []string{"notify"}) {
			t.Fatalf("explicit-file default hook: %+v", cfg.Jobs["tests"])
		}
	})
	t.Run("prompt size", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		data := "jobs:\n  tests:\n    prompt: " + strings.Repeat("x", config.MaxPromptBytes+1) + "\n    agent: claude\n"
		put(t, filepath.Join(root, "noon.yaml"), data)
		if _, err := config.Load(config.Input{CWD: root}); err == nil || !strings.Contains(err.Error(), "must be at most 64 KiB") {
			t.Fatalf("oversized prompt accepted: %v", err)
		}
	})
	t.Run("user profiles and default agent", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		user := filepath.Join(root, "user")
		put(t, filepath.Join(user, "noon/config.yaml"), "default_agent: team\nagents:\n  team:\n    command: [fake-agent, --quiet]\n    prompt: argument\n  claude:\n    command: [claude-wrapper]\n")
		put(t, filepath.Join(root, "noon.yaml"), "jobs: {review: {prompt: review}}")
		cfg, err := config.Load(config.Input{CWD: root, UserConfigDir: user})
		if err != nil {
			t.Fatal(err)
		}
		job := cfg.Jobs["review"]
		if job.Agent != "team" || job.PromptMode != "argument" || !reflect.DeepEqual(job.AgentCommand, []string{"fake-agent", "--quiet"}) {
			t.Fatalf("resolved agent: %+v", job)
		}
		if !reflect.DeepEqual(cfg.Agents["codex"].Command, []string{"codex", "exec"}) || !reflect.DeepEqual(cfg.Agents["codex"].PromptArgs, []string{"-"}) || !reflect.DeepEqual(cfg.Agents["claude"].Command, []string{"claude-wrapper"}) || cfg.Agents["claude"].SessionIDFlag != "" {
			t.Fatalf("profiles: %+v", cfg.Agents)
		}
	})
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
		put(t, filepath.Join(root, "noon.yaml"), "jobs:\n  tests:\n"+body)
		put(t, filepath.Join(root, ".git", "noon.yaml"), local)
		cfg, err := config.Load(config.Input{CWD: root})
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Jobs["tests"].Digest
	}
	base := "    run: echo hi\n    schedule: weekdays at 9am\n    env: {A: a, B: b}\n    cwd: subdir\n    timeout: 1h\n    cooldown: 5m\n    os: [linux]\n    arch: [amd64]\n"
	digest := load(base, "{}")
	if len(digest) != 71 || !strings.HasPrefix(digest, "sha256:") {
		t.Fatal(digest)
	}
	equivalent := "    cooldown: 300s\n    timeout: 60m\n    cwd: subdir\n    env: {B: b, A: a}\n    schedule: WEEKDAYS AT 09:00\n    os: [linux]\n    arch: [amd64]\n    run: echo hi # comment\n    description: changed\n    enabled: false\n"
	if load(equivalent, "{}") != digest || load(base, "jobs: {tests: {enabled: false}}") != digest {
		t.Fatal("cosmetic edits/toggle changed digest")
	}
	for _, change := range [][2]string{{"echo hi", "echo bye"}, {"9am", "10am"}, {"A: a", "A: changed"}, {"subdir", "other"}, {"1h", "2h"}, {"5m", "6m"}, {"os: [linux]", "os: [darwin]"}, {"arch: [amd64]", "arch: [arm64]"}} {
		if load(strings.Replace(base, change[0], change[1], 1), "{}") == digest {
			t.Fatalf("%v did not change digest", change)
		}
	}
	// Unset fields must stay omitted; explicit defaults participate in approval.
	if load("    run: hi\n", "{}") == load("    run: hi\n    timeout: 1h\n", "{}") {
		t.Fatal("unset field included in digest")
	}
	legacyDigest := load("    run: 'true'\n", "{}")
	if legacyDigest != "sha256:98dc0d61f34692d1b2b36b24bf272cbcc95c4f81aa817a971f6b700d6857c9cf" {
		t.Fatalf("job without requests changed its pre-feature digest: %s", legacyDigest)
	}
	if mergeDigest := load("    run: 'true'\n    requests: merge\n", "{}"); mergeDigest == legacyDigest {
		t.Fatal("explicit requests: merge was omitted from digest")
	}
	if mergeDigest := load("    run: 'true'\n    requests: merge\n", "{}"); mergeDigest == load("    run: 'true'\n    requests: each\n", "{}") {
		t.Fatal("requests mode did not change digest")
	}
	if load(base+"    noop_exit_code: 78\n", "{}") == digest {
		t.Fatal("noop_exit_code did not change the digest")
	}
	splayed := load(base+"    splay: 10m\n", "{}")
	if splayed == digest || splayed == load(base+"    splay: 11m\n", "{}") {
		t.Fatal("splay did not change digest")
	}
	if splayed != load(base+"    splay: 600s\n", "{}") {
		t.Fatal("equivalent splay changed digest")
	}
	jittered := load(base+"    jitter: 20m\n", "{}")
	if jittered == digest || jittered == load(base+"    jitter: 21m\n", "{}") {
		t.Fatal("jitter did not change digest")
	}
	if jittered != load(base+"    jitter: 1200s\n", "{}") {
		t.Fatal("equivalent jitter changed digest")
	}
	ended := load(base+"    until: 2099-01-01T00:00:00Z\n", "{}")
	if ended == digest || ended == load(base+"    until: 2099-01-02T00:00:00Z\n", "{}") {
		t.Fatal("until was omitted from the approval digest")
	}
	if ended != load(base+"    until: 2098-12-31T17:00:00-07:00\n", "{}") {
		t.Fatal("equivalent end instants changed the digest")
	}
	hookDefinition := "    run: echo hi\n    on_success: [notify, done]\n"
	hookDigest := load(hookDefinition, "{}")
	if load(strings.Replace(hookDefinition, "done", "failed", 1), "{}") == hookDigest {
		t.Fatal("changing a shared job hook did not change its digest")
	}
	webhookBase := "    run: echo hi\n    webhook: {secret_env: SECRET_ONE}\n"
	webhookDigest := load(webhookBase, "{}")
	if load(strings.Replace(webhookBase, "SECRET_ONE", "SECRET_TWO", 1), "{}") == webhookDigest {
		t.Fatal("changing a webhook secret environment name did not change the shared job digest")
	}
	// A user's own hook default must not revoke every shared-job approval when it
	// changes; a default from an approval-gated explicit file must.
	t.Run("inherited hook defaults digest only from explicit files", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		user := filepath.Join(root, "user")
		userConfig := filepath.Join(user, "noon", "config.yaml")
		put(t, filepath.Join(root, "noon.yaml"), "jobs: {tests: {run: hi}}")
		load := func(input config.Input) string {
			t.Helper()
			cfg, err := config.Load(input)
			if err != nil {
				t.Fatal(err)
			}
			return cfg.Jobs["tests"].Digest
		}
		userInput := config.Input{CWD: root, UserConfigDir: user}
		put(t, userConfig, "{}\n")
		without := load(userInput)
		put(t, userConfig, "on_success: [notify-one]\n")
		if load(userInput) != without {
			t.Fatal("a user hook default changed the shared job digest")
		}
		explicit := filepath.Join(root, "deploy.yaml")
		explicitInput := config.Input{CWD: root, UserConfigDir: user, File: &explicit}
		put(t, explicit, "on_success: [notify-one]\njobs: {tests: {run: hi}}\n")
		first := load(explicitInput)
		put(t, explicit, "on_success: [notify-two]\njobs: {tests: {run: hi}}\n")
		if load(explicitInput) == first {
			t.Fatal("changing an explicit-file hook default did not change its shared job digest")
		}
	})
	t.Run("prompt definition and agent name", func(t *testing.T) {
		root := canonical(t, t.TempDir())
		user := filepath.Join(root, "user")
		userConfig := filepath.Join(user, "noon", "config.yaml")
		put(t, userConfig, "agents: {team: {command: [agent-one]}}")
		loadPrompt := func(body string) string {
			t.Helper()
			put(t, filepath.Join(root, "noon.yaml"), "jobs:\n  review:\n"+body)
			cfg, err := config.Load(config.Input{CWD: root, UserConfigDir: user})
			if err != nil {
				t.Fatal(err)
			}
			return cfg.Jobs["review"].Digest
		}
		base := "    prompt: review\n    agent: team\n"
		initial := loadPrompt(base)
		// Captured from the pre-agent_args parser before this feature was implemented.
		const preAgentArgsDigest = "sha256:da8bab3de841d67cb71e01dc587dbcc75ffc4d6b678a2b08347e1498092be533"
		if initial != preAgentArgsDigest {
			t.Fatalf("prompt job without agent_args changed its pre-feature digest: %s", initial)
		}
		if loadPrompt("    prompt: review\n    agent: team\n    agent_args: [--model, haiku]\n") == initial {
			t.Fatal("agent_args was omitted from the prompt job digest")
		}
		if loadPrompt("    prompt: changed\n    agent: team\n") == initial || loadPrompt("    prompt_file: prompts/review.md\n    agent: team\n") == initial || loadPrompt("    prompt: review\n    agent: claude\n") == initial {
			t.Fatal("prompt, prompt-file, or agent changes did not change the digest")
		}
		put(t, userConfig, "default_agent: team\nagents: {team: {command: [agent-two]}}")
		if loadPrompt("    prompt: review\n") != initial {
			t.Fatal("default agent differs from equivalent explicit agent digest")
		}
		if loadPrompt(base) != initial {
			t.Fatal("user-owned agent profile command changed the job digest")
		}
		put(t, userConfig, "default_agent: team\nagents:\n  team:\n    command: [agent-two]\n    session_id_flag: --session-id\n")
		if loadPrompt(base) != initial {
			t.Fatal("user-owned session ID flag changed the job digest")
		}
	})
	t.Run("requested job digest preserves execution fields", func(t *testing.T) {
		job := config.Job{Name: "tests", Run: "echo hi", OS: []string{"linux"}, Arch: []string{"amd64"}, NoopExitCode: 78}
		digest, err := config.DigestJobDefinition(job, config.BuiltInAgents(), "")
		if err != nil {
			t.Fatal(err)
		}
		if want := load("    run: echo hi\n    os: [linux]\n    arch: [amd64]\n    noop_exit_code: 78\n", "{}"); digest != want {
			t.Fatalf("requested execution digest = %s, want %s", digest, want)
		}
	})
}
