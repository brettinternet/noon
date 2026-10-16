package config

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/brettinternet/bump/internal/schedule"
	"go.yaml.in/yaml/v3"
)

// Job is a validated effective definition. Env is deliberately never serialized.
type Job struct {
	Name        string
	Description string
	Schedule    *schedule.Schedule
	Run         any
	CWD         string
	Env         map[string]string `json:"-"`
	Timeout     string
	Overlap     string
	Missed      string
	Enabled     bool
	Source      string
	Shared      bool
	Overrides   bool
	Digest      string
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
		job, err := parseJob(file, path, name, jobs[name], fields)
		if err != nil {
			return err
		}
		job.CWD = resolvePath(cfg.Project, job.CWD)
		job.Shared = kind == "shared"
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

func parseJob(file, path, name string, node *yaml.Node, fields map[string]*yaml.Node) (Job, error) {
	job := Job{Name: name, CWD: ".", Env: map[string]string{}, Timeout: "1h", Overlap: "skip", Missed: "run_once", Enabled: true, Source: file}
	execution := make(map[string]any)
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
			var run any
			if value.Kind == yaml.SequenceNode && value.Tag == "!!seq" {
				if len(value.Content) == 0 {
					return Job{}, jobError(file, value, field, "must not be empty")
				}
				argv := make([]string, 0, len(value.Content))
				for _, arg := range value.Content {
					text, err := jobString(file, field, arg)
					if err != nil {
						return Job{}, err
					}
					argv = append(argv, text)
				}
				if strings.TrimSpace(argv[0]) == "" {
					return Job{}, jobError(file, value, field, "executable must not be empty")
				}
				run = argv
			} else {
				text, err := jobString(file, field, value)
				if err != nil {
					return Job{}, err
				}
				if strings.TrimSpace(text) == "" {
					return Job{}, jobError(file, value, field, "must not be empty")
				}
				run = text
			}
			job.Run, execution[key] = run, run
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
				if !envName.MatchString(key) || strings.HasPrefix(key, "BUMP_") {
					return Job{}, jobError(file, v, field+"."+key, "invalid or reserved environment name; BUMP_ is reserved")
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
	if fields["run"] == nil {
		return Job{}, jobError(file, node, path+".run", "required; enabled-only entries must override a shared job in .bump.yaml")
	}
	// encoding/json sorts string map keys, including nested environment keys.
	data, err := json.Marshal(execution)
	if err != nil {
		return Job{}, err
	}
	job.Digest = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	return job, nil
}
