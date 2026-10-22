package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/brettinternet/noon/internal/schedule"
	"go.yaml.in/yaml/v3"
)

// Job is a validated effective definition. Env is deliberately never serialized.
const MaxPromptBytes = 64 * 1024

type AgentProfile struct {
	Command       []string
	Prompt        string
	PromptArgs    []string
	SessionIDFlag string
	// shared marks a profile from an approval-gated --config file; its fields join prompt-job digests.
	shared bool
}

type Webhook struct {
	SecretEnv string
}

type Job struct {
	Name               string
	Description        string
	Schedule           *schedule.Schedule
	ScheduleExpression string
	Splay              time.Duration
	Jitter             time.Duration
	Until              *time.Time
	untilNode          *yaml.Node
	Run                any
	OnSuccess          any
	OnFailure          any
	Prompt             *string
	PromptFile         *string
	Webhook            *Webhook
	Agent              string
	AgentArgs          []string
	AgentCommand       []string
	AgentPromptArgs    []string
	PromptMode         string
	AgentSessionIDFlag string
	CWD                string
	EnvFile            []string
	Env                map[string]string `json:"-"`
	Timeout            string
	NoopExitCode       int
	Cooldown           time.Duration
	Requests           string
	Overlap            string
	Missed             string
	Enabled            bool
	OS                 []string
	Arch               []string
	Source             string
	Shared             bool
	Overrides          bool
	Digest             string
	execution          map[string]any
}

var jobName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var supportedOS = []string{"darwin", "linux"}
var supportedArch = []string{"amd64", "arm64"}

func jobError(file string, node *yaml.Node, path, message string) error {
	// YAML keys and source filenames are untrusted terminal input. Escape
	// control characters without adding quotes around ordinary dotted paths.
	file = strconv.Quote(file)
	path = strconv.Quote(path)
	return fmt.Errorf("config %s:%d: %s: %s", file[1:len(file)-1], node.Line, path[1:len(path)-1], message)
}

// jobMapping rejects YAML features that could silently change a definition.
func jobMapping(file, path string, node *yaml.Node) (map[string]*yaml.Node, error) {
	if node.Kind != yaml.MappingNode || node.Tag != "!!map" {
		return nil, jobError(file, node, path, "must be a mapping")
	}
	fields := make(map[string]*yaml.Node)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, jobError(file, key, path, "keys must be strings")
		}
		field := path + "." + key.Value
		if _, exists := fields[key.Value]; exists {
			return nil, jobError(file, key, field, "duplicate field")
		}
		fields[key.Value] = value
	}
	return fields, nil
}

func jobString(file, path string, node *yaml.Node) (string, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", jobError(file, node, path, "must be a string (quote numbers)")
	}
	return node.Value, nil
}

// Ended reports whether unattended execution has reached its exclusive end.
func (job Job) Ended(now time.Time) bool {
	return job.Until != nil && !now.Before(*job.Until)
}

// ParseUntil parses an absolute end time, never a clock relative to now.
func ParseUntil(value string, loc *time.Location) (time.Time, error) {
	if len(value) == len("2006-01-02 15:04") {
		wall, err := time.Parse("2006-01-02 15:04", value)
		if err == nil {
			return schedule.ResolveWall(wall, loc), nil
		}
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(loc), nil
	}
	return time.Time{}, fmt.Errorf("invalid until time %q: use YYYY-MM-DD HH:MM or RFC 3339", value)
}

func (job Job) MatchesPlatform(platform Platform) bool {
	return (len(job.OS) == 0 || slices.Contains(job.OS, platform.OS)) &&
		(len(job.Arch) == 0 || slices.Contains(job.Arch, platform.Arch))
}

func (job Job) PlatformHint(current Platform) string {
	var allowed []string
	if len(job.OS) > 0 {
		allowed = append(allowed, "OS: "+strings.Join(job.OS, ", "))
	}
	if len(job.Arch) > 0 {
		allowed = append(allowed, "architecture: "+strings.Join(job.Arch, ", "))
	}
	return fmt.Sprintf("Allowed %s; current platform: %s/%s.", strings.Join(allowed, "; allowed "), current.OS, current.Arch)
}

func parsePlatformList(file, path string, node *yaml.Node, supported []string) ([]string, error) {
	if node.Kind != yaml.SequenceNode || node.Tag != "!!seq" || len(node.Content) == 0 {
		return nil, jobError(file, node, path, "must be a non-empty list of strings")
	}
	values := make([]string, 0, len(node.Content))
	seen := make(map[string]bool, len(node.Content))
	for _, item := range node.Content {
		value, err := jobString(file, path, item)
		if err != nil {
			return nil, err
		}
		if seen[value] {
			return nil, jobError(file, item, path, "must not contain duplicates")
		}
		seen[value] = true
		if !slices.Contains(supported, value) {
			detail := "must be one of " + strings.Join(supported, ", ")
			if suggestion := platformAlias(value); suggestion != "" {
				detail = fmt.Sprintf("unsupported value %q; use %s", value, suggestion)
			}
			return nil, jobError(file, item, path, detail)
		}
		values = append(values, value)
	}
	slices.Sort(values)
	return values, nil
}

func platformAlias(value string) string {
	switch strings.ToLower(value) {
	case "macos":
		return "darwin"
	case "x86_64", "x64":
		return "amd64"
	case "aarch64":
		return "arm64"
	default:
		return ""
	}
}

func hasCommand(command any) bool {
	switch value := command.(type) {
	case string:
		return value != ""
	case []string:
		return len(value) > 0
	default:
		return false
	}
}

func parseCommand(file, path string, node *yaml.Node, allowEmptyList bool) (any, error) {
	if node.Kind == yaml.SequenceNode && node.Tag == "!!seq" {
		if len(node.Content) == 0 {
			if allowEmptyList {
				return []string{}, nil
			}
			return nil, jobError(file, node, path, "must not be empty")
		}
		argv := make([]string, 0, len(node.Content))
		for _, arg := range node.Content {
			text, err := jobString(file, path, arg)
			if err != nil {
				return nil, err
			}
			argv = append(argv, text)
		}
		if strings.TrimSpace(argv[0]) == "" {
			return nil, jobError(file, node, path, "executable must not be empty")
		}
		return argv, nil
	}
	text, err := jobString(file, path, node)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(text) == "" {
		return nil, jobError(file, node, path, "must not be empty")
	}
	return text, nil
}

func (cfg *Config) applyJobs(file, kind string, node *yaml.Node) error {
	jobs, err := jobMapping(file, "jobs", node)
	if err != nil {
		return err
	}
	// YAML order makes diagnostics deterministic without depending on map iteration.
	for i := 0; i < len(node.Content); i += 2 {
		name := node.Content[i].Value
		path := "jobs." + name
		if !jobName.MatchString(name) {
			return jobError(file, node.Content[i], path, "name must match ^[a-z][a-z0-9_-]{0,63}$")
		}
		fields, err := jobMapping(file, path, jobs[name])
		if err != nil {
			return err
		}
		previous, exists := cfg.Jobs[name]
		if kind == "local" && len(fields) == 1 && fields["enabled"] != nil {
			enabled, err := jobEnabled(file, path+".enabled", fields["enabled"])
			if err != nil {
				return err
			}
			if !exists || !previous.Shared {
				return jobError(file, jobs[name], path, "enabled-only override requires a shared job")
			}
			// The execution definition remains shared and requires its original approval.
			previous.Enabled, previous.Source, previous.Overrides = enabled, file, true
			cfg.Jobs[name] = previous
			continue
		}
		job, err := parseJob(file, path, name, jobs[name], fields, cfg.OnSuccess, cfg.OnFailure, kind == "explicit")
		if err != nil {
			return err
		}
		job.CWD = resolvePath(cfg.Project, job.CWD)
		job.Shared = kind == "shared" || kind == "explicit"
		job.Overrides = kind == "local" && exists && previous.Shared
		cfg.Jobs[name] = job
	}
	return nil
}

func jobEnabled(file, path string, node *yaml.Node) (bool, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!bool" {
		return false, jobError(file, node, path, "must be a boolean")
	}
	var value bool
	if err := node.Decode(&value); err != nil {
		return false, jobError(file, node, path, "must be a boolean")
	}
	return value, nil
}

func jobNoopExitCode(file, path string, node *yaml.Node) (int, error) {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!int" {
		return 0, jobError(file, node, path, "must be an integer from 1 to 124")
	}
	var code int
	if err := node.Decode(&code); err != nil || code < 1 || code > 124 {
		return 0, jobError(file, node, path, "must be an integer from 1 to 124")
	}
	return code, nil
}

func BuiltInAgents() map[string]AgentProfile {
	return map[string]AgentProfile{
		"claude":   {Command: []string{"claude", "-p", "--permission-prompts", "none"}, Prompt: "stdin", SessionIDFlag: "--session-id"},
		"codex":    {Command: []string{"codex", "exec"}, Prompt: "stdin", PromptArgs: []string{"-"}},
		"gemini":   {Command: []string{"gemini"}, Prompt: "argument", PromptArgs: []string{"-p"}},
		"opencode": {Command: []string{"opencode", "run"}, Prompt: "argument", PromptArgs: []string{"--"}},
		"pi":       {Command: []string{"pi", "--print"}, Prompt: "stdin", SessionIDFlag: "--session-id"},
	}
}

func (cfg *Config) applyAgents(file string, node *yaml.Node, shared bool) error {
	profiles, err := jobMapping(file, "agents", node)
	if err != nil {
		return err
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		name, path := key.Value, "agents."+key.Value
		if !jobName.MatchString(name) {
			return jobError(file, key, path, "name must match ^[a-z][a-z0-9_-]{0,63}$")
		}
		if _, err := jobMapping(file, path, profiles[name]); err != nil {
			return err
		}
		profile := AgentProfile{Prompt: "stdin", shared: shared}
		for j := 0; j < len(value.Content); j += 2 {
			fieldNode, fieldValue := value.Content[j], value.Content[j+1]
			field := path + "." + fieldNode.Value
			switch fieldNode.Value {
			case "command":
				if fieldValue.Kind != yaml.SequenceNode || fieldValue.Tag != "!!seq" || len(fieldValue.Content) == 0 {
					return jobError(file, fieldValue, field, "must be a non-empty string list")
				}
				for _, arg := range fieldValue.Content {
					text, err := jobString(file, field, arg)
					if err != nil {
						return err
					}
					profile.Command = append(profile.Command, text)
				}
				if strings.TrimSpace(profile.Command[0]) == "" {
					return jobError(file, fieldValue, field, "executable must not be empty")
				}
			case "prompt":
				text, err := jobString(file, field, fieldValue)
				if err != nil {
					return err
				}
				if text != "stdin" && text != "argument" {
					return jobError(file, fieldValue, field, "must be stdin or argument")
				}
				profile.Prompt = text
			case "prompt_args":
				if fieldValue.Kind != yaml.SequenceNode || fieldValue.Tag != "!!seq" {
					return jobError(file, fieldValue, field, "must be a string list")
				}
				for _, arg := range fieldValue.Content {
					text, err := jobString(file, field, arg)
					if err != nil {
						return err
					}
					profile.PromptArgs = append(profile.PromptArgs, text)
				}
			case "session_id_flag":
				text, err := jobString(file, field, fieldValue)
				if err != nil {
					return err
				}
				if !validSessionIDFlag(text) {
					return jobError(file, fieldValue, field, "must be one flag token starting with - (not - or --), with no whitespace or NUL")
				}
				profile.SessionIDFlag = text
			default:
				return jobError(file, fieldNode, field, "unknown field")
			}
		}
		if len(profile.Command) == 0 {
			return jobError(file, value, path+".command", "required")
		}
		cfg.Agents[name] = profile
	}
	return nil
}

func validSessionIDFlag(value string) bool {
	if !strings.HasPrefix(value, "-") || value == "-" || value == "--" {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// resolveJobs completes settings that depend on the final effective config:
// until in the project timezone and agent profiles, updating digests.
func (cfg *Config) resolveJobs() error {
	loc := time.Local
	if cfg.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(cfg.Timezone)
		if err != nil {
			return err
		}
	}
	for name, job := range cfg.Jobs {
		if job.untilNode == nil {
			continue
		}
		until, err := ParseUntil(job.untilNode.Value, loc)
		if err != nil {
			return jobError(job.Source, job.untilNode, "jobs."+name+".until", err.Error())
		}
		job.Until = &until
		job.execution["until"] = until.UTC().Format(time.RFC3339Nano)
		data, err := json.Marshal(job.execution)
		if err != nil {
			return err
		}
		job.Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		cfg.Jobs[name] = job
	}
	if cfg.DefaultAgent != "" {
		if _, ok := cfg.Agents[cfg.DefaultAgent]; !ok {
			return fmt.Errorf("default_agent %q is unknown; known agents: %s", cfg.DefaultAgent, strings.Join(agentNames(cfg.Agents), ", "))
		}
	}
	for name, job := range cfg.Jobs {
		if job.Prompt == nil && job.PromptFile == nil {
			continue
		}
		if job.Agent == "" {
			job.Agent = cfg.DefaultAgent
		}
		if job.Agent == "" {
			return fmt.Errorf("job %q: prompt job requires agent or default_agent; set agent on the job or default_agent in user config or .git/noon.yaml", name)
		}
		profile, ok := cfg.Agents[job.Agent]
		if !ok {
			return fmt.Errorf("job %q: unknown agent %q; known agents: %s", name, job.Agent, strings.Join(agentNames(cfg.Agents), ", "))
		}
		job.AgentCommand = append([]string(nil), profile.Command...)
		job.AgentPromptArgs = append([]string(nil), profile.PromptArgs...)
		job.PromptMode = profile.Prompt
		job.AgentSessionIDFlag = profile.SessionIDFlag
		job.execution["agent"] = job.Agent
		digested := job.execution
		if profile.shared {
			// The profile shares the job's approval-gated source, so changing what it runs needs approval.
			digested = maps.Clone(job.execution)
			digested["agent_profile"] = map[string]any{"command": profile.Command, "prompt": profile.Prompt, "prompt_args": profile.PromptArgs, "session_id_flag": profile.SessionIDFlag}
		}
		data, err := json.Marshal(digested)
		if err != nil {
			return err
		}
		job.Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		cfg.Jobs[name] = job
	}
	return nil
}

func agentNames(profiles map[string]AgentProfile) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Inherited hook defaults join the digest only when they come from the same
// approval-gated explicit file; user-config defaults are the user's own and
// must not revoke shared-job approvals when they change.
func parseJob(file, path, name string, node *yaml.Node, fields map[string]*yaml.Node, defaultSuccess, defaultFailure any, digestDefaults bool) (Job, error) {
	job := Job{Name: name, CWD: ".", Env: map[string]string{}, Timeout: "1h", Requests: "merge", Overlap: "skip", Missed: "run_once", Enabled: true, Source: file, OnSuccess: defaultSuccess, OnFailure: defaultFailure}
	execution := make(map[string]any)
	job.execution = execution
	actions := 0
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i].Value, node.Content[i+1]
		field := path + "." + key
		switch key {
		case "enabled":
			enabled, err := jobEnabled(file, field, value)
			if err != nil {
				return Job{}, err
			}
			job.Enabled = enabled
		case "run":
			actions++
			run, err := parseCommand(file, field, value, false)
			if err != nil {
				return Job{}, err
			}
			job.Run, execution[key] = run, run
		case "until":
			if value.Kind != yaml.ScalarNode || (value.Tag != "!!str" && value.Tag != "!!timestamp") {
				return Job{}, jobError(file, value, field, "must be an absolute time string")
			}
			until, err := ParseUntil(value.Value, time.UTC)
			if err != nil {
				return Job{}, jobError(file, value, field, err.Error())
			}
			job.Until, job.untilNode = &until, value
			execution[key] = until.UTC().Format(time.RFC3339Nano)
		case "noop_exit_code":
			code, err := jobNoopExitCode(file, field, value)
			if err != nil {
				return Job{}, err
			}
			job.NoopExitCode, execution[key] = code, code
		case "on_success", "on_failure":
			command, err := parseCommand(file, field, value, true)
			if err != nil {
				return Job{}, err
			}
			if key == "on_success" {
				job.OnSuccess = command
			} else {
				job.OnFailure = command
			}
		case "prompt":
			actions++
			text, err := jobString(file, field, value)
			if err != nil {
				return Job{}, err
			}
			if len(text) > MaxPromptBytes {
				return Job{}, jobError(file, value, field, "must be at most 64 KiB")
			}
			job.Prompt, execution[key] = &text, text
		case "prompt_file":
			actions++
			text, err := jobString(file, field, value)
			if err != nil {
				return Job{}, err
			}
			if strings.TrimSpace(text) == "" || filepath.IsAbs(text) {
				return Job{}, jobError(file, value, field, "must be a non-empty path relative to the project root")
			}
			job.PromptFile, execution[key] = &text, text
		case "webhook":
			webhookFields, err := jobMapping(file, field, value)
			if err != nil {
				return Job{}, err
			}
			webhook := &Webhook{}
			for j := 0; j < len(value.Content); j += 2 {
				fieldNode, fieldValue := value.Content[j], value.Content[j+1]
				webhookField := field + "." + fieldNode.Value
				if fieldNode.Value != "secret_env" {
					return Job{}, jobError(file, fieldNode, webhookField, "unknown field")
				}
				secretEnv, err := jobString(file, webhookField, fieldValue)
				if err != nil {
					return Job{}, err
				}
				if !envName.MatchString(secretEnv) {
					return Job{}, jobError(file, fieldValue, webhookField, "must be a valid environment variable name")
				}
				webhook.SecretEnv = secretEnv
			}
			if _, exists := webhookFields["secret_env"]; !exists {
				return Job{}, jobError(file, value, field+".secret_env", "required")
			}
			job.Webhook = webhook
			execution[key] = map[string]string{"secret_env": webhook.SecretEnv}
		case "os", "arch":
			supported := supportedOS
			if key == "arch" {
				supported = supportedArch
			}
			platforms, err := parsePlatformList(file, field, value, supported)
			if err != nil {
				return Job{}, err
			}
			if key == "os" {
				job.OS = platforms
			} else {
				job.Arch = platforms
			}
			execution[key] = platforms
		case "requests":
			text, err := jobString(file, field, value)
			if err != nil {
				return Job{}, err
			}
			if text != "merge" && text != "each" {
				return Job{}, jobError(file, value, field, "must be merge or each")
			}
			job.Requests = text
			execution[key] = text
		case "agent":
			text, err := jobString(file, field, value)
			if err != nil {
				return Job{}, err
			}
			if !jobName.MatchString(text) {
				return Job{}, jobError(file, value, field, "must be a valid agent profile name")
			}
			job.Agent, execution[key] = text, text
		case "agent_args":
			if value.Kind != yaml.SequenceNode || value.Tag != "!!seq" || len(value.Content) == 0 {
				return Job{}, jobError(file, value, field, "must be a non-empty list of strings")
			}
			for _, arg := range value.Content {
				text, err := jobString(file, field, arg)
				if err != nil {
					return Job{}, err
				}
				if strings.IndexByte(text, 0) >= 0 {
					return Job{}, jobError(file, arg, field, "must not contain NUL")
				}
				job.AgentArgs = append(job.AgentArgs, text)
			}
			execution[key] = job.AgentArgs
		case "env_file":
			paths := []*yaml.Node{value}
			if value.Kind == yaml.SequenceNode && value.Tag == "!!seq" {
				paths = value.Content
				if len(paths) == 0 {
					return Job{}, jobError(file, value, field, "must be a non-empty list of paths")
				}
			}
			for _, item := range paths {
				text, err := jobString(file, field, item)
				if err != nil {
					return Job{}, err
				}
				if strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0) {
					return Job{}, jobError(file, item, field, "must be a non-empty path without NUL")
				}
				job.EnvFile = append(job.EnvFile, text)
			}
			execution[key] = job.EnvFile
		case "env":
			env, err := jobMapping(file, field, value)
			if err != nil {
				return Job{}, err
			}
			keys := make([]string, 0, len(env))
			for key := range env {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			for _, key := range keys {
				v := env[key]
				if !envName.MatchString(key) || strings.HasPrefix(key, "NOON_") {
					return Job{}, jobError(file, v, field+"."+key, "invalid or reserved environment name; NOON_ is reserved")
				}
				text, err := jobString(file, field+"."+key, v)
				if err != nil {
					return Job{}, err
				}
				job.Env[key] = text
			}
			execution[key] = job.Env
		case "description", "schedule", "splay", "jitter", "cwd", "timeout", "cooldown", "overlap", "missed":
			text, err := jobString(file, field, value)
			if err != nil {
				return Job{}, err
			}
			switch key {
			case "description":
				job.Description = text
			case "cwd":
				if strings.TrimSpace(text) == "" {
					return Job{}, jobError(file, value, field, "must not be empty")
				}
				job.CWD = text
			case "schedule":
				parsed, err := schedule.Parse(text)
				if err != nil {
					return Job{}, jobError(file, value, field, err.Error())
				}
				job.Schedule = &parsed
				text = parsed.String()
			case "timeout":
				if text != "none" {
					duration, err := schedule.ParseDuration(text)
					if err != nil {
						return Job{}, jobError(file, value, field, err.Error())
					}
					if duration < time.Second {
						return Job{}, jobError(file, value, field, "must be at least 1s or none")
					}
					text = duration.String()
				}
				job.Timeout = text
			case "splay":
				duration, err := schedule.ParseDuration(text)
				if err != nil {
					return Job{}, jobError(file, value, field, err.Error())
				}
				if duration < time.Second || duration > time.Hour {
					return Job{}, jobError(file, value, field, "must be between 1s and 1h")
				}
				job.Splay = duration
				text = duration.String()
			case "jitter":
				duration, err := schedule.ParseDuration(text)
				if err != nil {
					return Job{}, jobError(file, value, field, err.Error())
				}
				if duration < time.Second {
					return Job{}, jobError(file, value, field, "must be at least 1s")
				}
				job.Jitter = duration
				text = schedule.FormatDuration(duration)
			case "cooldown":
				duration, err := schedule.ParseDuration(text)
				if err != nil {
					return Job{}, jobError(file, value, field, err.Error())
				}
				if duration < time.Second {
					return Job{}, jobError(file, value, field, "must be at least 1s")
				}
				job.Cooldown = duration
				text = duration.String()
			case "overlap":
				if text != "skip" && text != "queue" {
					return Job{}, jobError(file, value, field, "must be skip or queue")
				}
				job.Overlap = text
			case "missed":
				if text != "run_once" && text != "skip" {
					return Job{}, jobError(file, value, field, "must be run_once or skip")
				}
				job.Missed = text
			}
			if key != "description" {
				execution[key] = text
			}
		default:
			return Job{}, jobError(file, node.Content[i], field, "unknown field")
		}
	}
	if actions != 1 {
		return Job{}, jobError(file, node, path, "exactly one of run, prompt, or prompt_file is required")
	}
	if value := fields["noop_exit_code"]; value != nil && job.Run == nil {
		return Job{}, jobError(file, value, path+".noop_exit_code", "only valid for run jobs")
	}
	if value := fields["agent_args"]; value != nil {
		if job.Run != nil {
			return Job{}, jobError(file, value, path+".agent_args", "only valid for prompt or prompt_file jobs")
		}
		if fields["agent"] == nil {
			return Job{}, jobError(file, value, path+".agent_args", "requires an explicit agent on the job")
		}
	}
	if value := fields["splay"]; value != nil {
		if job.Schedule == nil {
			return Job{}, jobError(file, value, path+".splay", "requires a cron, macro, or calendar schedule")
		}
		if _, interval := job.Schedule.Interval(); interval {
			return Job{}, jobError(file, value, path+".splay", "cannot be used with an interval schedule")
		}
	}
	if value := fields["jitter"]; value != nil {
		if job.Schedule == nil {
			return Job{}, jobError(file, value, path+".jitter", "requires a schedule")
		}
		maximum := job.Schedule.ShortestGap() / 2
		if job.Jitter > maximum {
			return Job{}, jobError(file, value, path+".jitter", fmt.Sprintf("must not exceed half the schedule's shortest gap (maximum allowed %s)", schedule.FormatDuration(maximum)))
		}
	}
	if job.Requests == "each" && fields["cooldown"] != nil {
		return Job{}, jobError(file, fields["cooldown"], path+".cooldown", "cannot be used with requests: each")
	}
	if hasCommand(job.OnSuccess) && (digestDefaults || fields["on_success"] != nil) {
		execution["on_success"] = job.OnSuccess
	}
	if hasCommand(job.OnFailure) && (digestDefaults || fields["on_failure"] != nil) {
		execution["on_failure"] = job.OnFailure
	}
	if job.Agent != "" && job.Prompt == nil && job.PromptFile == nil {
		return Job{}, jobError(file, fields["agent"], path+".agent", "valid only for prompt or prompt_file jobs")
	}
	// encoding/json sorts string map keys, including nested environment keys.
	data, err := json.Marshal(execution)
	if err != nil {
		return Job{}, err
	}
	job.Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	return job, nil
}
