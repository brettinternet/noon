package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// VarsFile records a vars_file declaration from one configuration document.
type VarsFile struct {
	Configured string
	Present    bool
}

func resolveDocumentVars(file, project string, values map[string]*yaml.Node) (VarsFile, map[string]string, error) {
	node, ok := values["vars_file"]
	if !ok {
		return VarsFile{}, nil, nil
	}
	configured := node.Value
	if strings.TrimSpace(configured) == "" || strings.ContainsRune(configured, 0) {
		return VarsFile{}, nil, jobError(file, node, "vars_file", "must be a non-empty path without NUL")
	}
	vars, present, err := readVarsFile(project, configured)
	if err != nil {
		return VarsFile{}, nil, fmt.Errorf("config %s: %w", file, err)
	}
	return VarsFile{Configured: configured, Present: present}, vars, nil
}

func readVarsFile(project, configured string) (map[string]string, bool, error) {
	path := configured
	if !filepath.IsAbs(path) {
		path = filepath.Join(project, path)
	}
	values, err := ReadEnvFile(project, configured)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, statErr := os.Lstat(path); errors.Is(statErr, os.ErrNotExist) {
				return map[string]string{}, false, nil
			}
		}
		return nil, false, fmt.Errorf("vars_file %q: %w", configured, err)
	}
	return values, true, nil
}

func expandJobValues(file string, jobs *yaml.Node, vars map[string]string, configuredVars string) error {
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name, job := jobs.Content[i].Value, jobs.Content[i+1]
		if job.Kind != yaml.MappingNode {
			continue
		}
		for j := 0; j+1 < len(job.Content); j += 2 {
			field := job.Content[j].Value
			if !interpolatedJobField(field) {
				continue
			}
			value := job.Content[j+1]
			path := "jobs." + name + "." + field
			if field == "env_file" && value.Kind == yaml.SequenceNode {
				for _, item := range value.Content {
					if err := expandStringNode(file, path, item, vars, configuredVars); err != nil {
						return err
					}
				}
				continue
			}
			if err := expandStringNode(file, path, value, vars, configuredVars); err != nil {
				return err
			}
		}
	}
	return nil
}

func interpolatedJobField(field string) bool {
	switch field {
	case "schedule", "cwd", "env_file", "prompt_file", "timeout", "cooldown", "splay", "jitter":
		return true
	default:
		return false
	}
}

func expandStringNode(file, path string, node *yaml.Node, vars map[string]string, configuredVars string) error {
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return nil
	}
	value, err := expandVariables(node.Value, vars)
	if err != nil {
		var expression *variableExpressionError
		if !errors.As(err, &expression) {
			return err
		}
		message := "invalid variable expression"
		if expression.name != "" {
			message = fmt.Sprintf("variable %q", expression.name)
		}
		if expression.missing {
			message = fmt.Sprintf("variable %q is required but not set", expression.name)
			if configuredVars != "" {
				message += fmt.Sprintf(" in vars_file %q", configuredVars)
			} else {
				message += "; no vars_file is configured"
			}
		} else if configuredVars != "" {
			message += fmt.Sprintf(" in vars_file %q", configuredVars)
		}
		return jobError(file, node, path, message)
	}
	node.Value = value
	return nil
}

type variableExpressionError struct {
	name    string
	missing bool
}

func (e *variableExpressionError) Error() string { return "invalid variable expression" }

func expandVariables(value string, vars map[string]string) (string, error) {
	var out strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '$' {
			out.WriteByte(value[i])
			i++
			continue
		}
		if i+1 >= len(value) || value[i+1] != '$' && value[i+1] != '{' {
			out.WriteByte('$')
			i++
			continue
		}
		if value[i+1] == '$' {
			out.WriteByte('$')
			i += 2
			continue
		}
		end := strings.IndexByte(value[i+2:], '}')
		if end < 0 {
			return "", &variableExpressionError{}
		}
		end += i + 2
		body := value[i+2 : end]
		if strings.ContainsAny(body, "{}") {
			return "", &variableExpressionError{}
		}
		name, fallback, hasFallback := body, "", false
		if index := strings.Index(body, ":-"); index >= 0 {
			name, fallback, hasFallback = body[:index], body[index+2:], true
		} else if strings.ContainsAny(body, ":-") {
			return "", &variableExpressionError{}
		}
		if !envName.MatchString(name) {
			return "", &variableExpressionError{}
		}
		resolved, exists := vars[name]
		if hasFallback {
			if !exists || resolved == "" {
				resolved = fallback
			}
		} else if !exists {
			return "", &variableExpressionError{name: name, missing: true}
		}
		out.WriteString(resolved)
		i = end + 1
	}
	return out.String(), nil
}

// VarsFilePaths returns configured vars files for the documents selected by input.
// It deliberately reads only manifest YAML; this lets the scheduler stamp missing
// or broken vars files even when the previous full config load failed.
func VarsFilePaths(input Input) []string {
	project, err := ProjectDir(input)
	if err != nil {
		return nil
	}
	var configPaths []string
	if IsUserProject(project, input.UserConfigDir) {
		configPaths = append(configPaths, filepath.Join(project, "config.yaml"))
	} else {
		if input.UserConfigDir != "" {
			configPaths = append(configPaths, filepath.Join(input.UserConfigDir, "noon", "config.yaml"))
		}
		if input.File != nil {
			if *input.File != "" {
				configPaths = append(configPaths, resolvePath(input.CWD, *input.File))
			}
		} else {
			configPaths = append(configPaths, filepath.Join(project, "noon.yaml"))
			if local, localErr := LocalPath(project); localErr == nil && local != "" {
				configPaths = append(configPaths, local)
			}
		}
	}
	seen := map[string]bool{}
	paths := make([]string, 0, len(configPaths))
	for _, configPath := range configPaths {
		file, err := os.Open(configPath)
		if err != nil {
			continue
		}
		// Match the full loader's size bound even on failed reloads.
		const maxSize = 1024 * 1024
		data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
		_ = file.Close()
		if err != nil || len(data) > maxSize {
			continue
		}
		configured := varsPathFromDocument(data)
		if configured == "" {
			continue
		}
		path := configured
		if !filepath.IsAbs(path) {
			base := project
			if input.UserConfigDir != "" && !IsUserProject(project, input.UserConfigDir) && configPath == filepath.Join(input.UserConfigDir, "noon", "config.yaml") {
				if userProject, userErr := UserProjectDir(input.UserConfigDir); userErr == nil {
					base = userProject
				}
			}
			path = filepath.Join(base, path)
		}
		path, err = filepath.Abs(path)
		if err != nil {
			continue
		}
		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func varsPathFromDocument(data []byte) string {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return ""
	}
	root, err := resolveYAML(doc.Content[0])
	if err != nil {
		return ""
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "vars_file" && root.Content[i+1].Kind == yaml.ScalarNode && root.Content[i+1].Tag == "!!str" {
			return root.Content[i+1].Value
		}
	}
	return ""
}
