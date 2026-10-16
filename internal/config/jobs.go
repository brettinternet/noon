package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/schedule"
	"go.yaml.in/yaml/v3"
)

// Job is a validated effective definition. Env is deliberately never serialized.
const MaxPromptBytes = 64 * 1024

type AgentProfile struct {
	Command []string
	Prompt  string
}

type Job struct {
	Name         string
	Description  string
	Schedule     *schedule.Schedule
	Run          any
	OnSuccess    any
	OnFailure    any
	Prompt       *string
	PromptFile   *string
	Agent        string
	AgentCommand []string
	PromptMode   string
	CWD          string
	Env          map[string]string `json:"-"`
	Timeout      string
	Overlap      string
	Missed       string
	Enabled      bool
	Source       string
	Shared       bool
	Overrides    bool
	Digest       string
	execution    map[string]any
}

var jobName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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
			return nil, jobError(file, key, path, "keys must be strings; aliases and merge keys are not allowed")
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
		job, err := parseJob(file, path, name, jobs[name], fields, cfg.OnSuccess, cfg.OnFailure)
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

func BuiltInAgents() map[string]AgentProfile {
	return map[string]AgentProfile{
		"claude": {Command: []string{"claude", "-p", "--permission-prompts", "none"}, Prompt: "stdin"},
		"codex":  {Command: []string{"codex", "exec", "-"}, Prompt: "stdin"},
	}
}

func (cfg *Config) applyAgents(file string, node *yaml.Node) error {
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
		profile := AgentProfile{Prompt: "stdin"}
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

func (cfg *Config) resolveAgents() error {
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
			return fmt.Errorf("job %q: prompt job requires agent or default_agent; set agent on the job or default_agent in user config or .noon.yaml", name)
		}
		profile, ok := cfg.Agents[job.Agent]
		if !ok {
			return fmt.Errorf("job %q: unknown agent %q; known agents: %s", name, job.Agent, strings.Join(agentNames(cfg.Agents), ", "))
		}
		job.AgentCommand = append([]string(nil), profile.Command...)
		job.PromptMode = profile.Prompt
		job.execution["agent"] = job.Agent
		data, err := json.Marshal(job.execution)
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

func parseJob(file, path, name string, node *yaml.Node, fields map[string]*yaml.Node, defaultSuccess, defaultFailure any) (Job, error) {
	job := Job{Name: name, CWD: ".", Env: map[string]string{}, Timeout: "1h", Overlap: "skip", Missed: "run_once", Enabled: true, Source: file, OnSuccess: defaultSuccess, OnFailure: defaultFailure}
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
		case "agent":
			text, err := jobString(file, field, value)
			if err != nil {
				return Job{}, err
			}
			if !jobName.MatchString(text) {
				return Job{}, jobError(file, value, field, "must be a valid agent profile name")
			}
			job.Agent, execution[key] = text, text
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
		case "description", "schedule", "cwd", "timeout", "overlap", "missed":
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
	if hasCommand(job.OnSuccess) {
		execution["on_success"] = job.OnSuccess
	}
	if hasCommand(job.OnFailure) {
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
