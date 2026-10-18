package config

import (
	"bytes"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// AddJob splices one rendered job into a supported block mapping. Existing source
// lines are never encoded again.
func AddJob(src []byte, name string, job Job) ([]byte, error) {
	return addJob(src, name, job, "", nil)
}

// AddJobWithComments is AddJob with comments attached to the new job and fields.
func AddJobWithComments(src []byte, name string, job Job, headComment string, fieldComments map[string]string) ([]byte, error) {
	return addJob(src, name, job, headComment, fieldComments)
}

func addJob(src []byte, name string, job Job, headComment string, fieldComments map[string]string) ([]byte, error) {
	if !jobName.MatchString(name) {
		return nil, fmt.Errorf("config_edit: invalid job name %q", name)
	}
	key := yamlKey(name)
	ending := lineEnding(src)
	if len(src) == 0 {
		encoded, err := encodeJob(job, 4, ending, fieldComments)
		if err != nil {
			return nil, err
		}
		return []byte("version: 1" + ending + "jobs:" + ending + commentLines(headComment, 2, ending) + "  " + key + ":" + ending + encoded), nil
	}
	lines := splitLines(src)
	root, err := documentRoot(src)
	if err != nil {
		return nil, err
	}
	jobsNode, jobsLine := rootField(root, "jobs")
	if jobsNode == nil {
		prefix := ""
		if !bytes.HasSuffix(src, []byte("\n")) {
			prefix = ending
		}
		encoded, err := encodeJob(job, 4, ending, fieldComments)
		if err != nil {
			return nil, err
		}
		return []byte(string(src) + prefix + "jobs:" + ending + commentLines(headComment, 2, ending) + "  " + key + ":" + ending + encoded), nil
	}
	if jobsNode.Kind != yaml.MappingNode || len(jobsNode.Content) == 0 || jobsNode.Style&yaml.FlowStyle != 0 {
		return nil, fmt.Errorf("config_edit: jobs must be a non-empty block mapping; edit the file by hand")
	}
	for i := 0; i < len(jobsNode.Content); i += 2 {
		if jobsNode.Content[i].Value == name {
			return nil, fmt.Errorf("config_edit: job %q already exists", name)
		}
	}
	keyIndent := jobsNode.Content[0].Column - 1
	fieldsNode := jobsNode.Content[1]
	if fieldsNode.Kind != yaml.MappingNode || len(fieldsNode.Content) == 0 {
		return nil, fmt.Errorf("config_edit: jobs must contain block job mappings; edit the file by hand")
	}
	fieldIndent := fieldsNode.Content[0].Column - 1
	if fieldsNode.Style&yaml.FlowStyle != 0 {
		fieldIndent = keyIndent + 2
	}

	blockEnd := len(lines)
	for i := 0; i < len(root.Content); i += 2 {
		key := root.Content[i]
		if key.Line > jobsLine && key.Column == 1 {
			blockEnd = key.Line - 1
			break
		}
	}
	// Blank lines consumed by a block scalar belong to its value, not to the
	// document trailer. In particular, moving them changes |+ and >+ values.
	protectedEnd := blockScalarEnd(lines, jobsNode)
	for blockEnd > max(jobsLine, protectedEnd) && (strings.TrimSpace(lines[blockEnd-1]) == "" || strings.HasPrefix(lines[blockEnd-1], "#")) {
		blockEnd--
	}
	insert := blockEnd
	encoded, err := encodeJob(job, fieldIndent, ending, fieldComments)
	if err != nil {
		return nil, err
	}
	insertText := commentLines(headComment, keyIndent, ending) + strings.Repeat(" ", keyIndent) + key + ":" + ending + encoded
	separateFromComment := insert > 0 && isIndentedComment(lines[insert-1], keyIndent)
	if (precededByBlankJobSeparator(lines, jobsNode) && protectedEnd != insert) || separateFromComment {
		insertText = ending + insertText
	}
	if insert > 0 && !strings.HasSuffix(lines[insert-1], "\n") {
		insertText = ending + insertText
	}
	return []byte(strings.Join(lines[:insert], "") + insertText + strings.Join(lines[insert:], "")), nil
}

// RemoveJob removes one job and its head comments. Comments attached to the next
// job remain attached to that job.
func RemoveJob(src []byte, name string) ([]byte, error) {
	root, err := documentRoot(src)
	if err != nil {
		return nil, err
	}
	jobs, jobsLine := rootField(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || jobs.Style&yaml.FlowStyle != 0 {
		return nil, fmt.Errorf("config_edit: jobs must be a block mapping; edit the file by hand")
	}
	idx := -1
	for i := 0; i < len(jobs.Content); i += 2 {
		if jobs.Content[i].Value == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, fmt.Errorf("config_edit: job %q not found", name)
	}
	lines := splitLines(src)
	key := jobs.Content[idx]
	start := key.Line - 1
	for start > jobsLine && isIndentedComment(lines[start-1], key.Column-1) {
		start--
	}
	end := len(lines)
	if idx+2 < len(jobs.Content) {
		nextKey := jobs.Content[idx+2]
		head := nextKey.Line - 1
		for head > jobsLine && isIndentedComment(lines[head-1], nextKey.Column-1) {
			head--
		}
		end = head
	} else {
		for i := 0; i < len(root.Content); i += 2 {
			key := root.Content[i]
			if key.Line > jobsLine && key.Column == 1 {
				end = key.Line - 1
				break
			}
		}
		protectedEnd := blockScalarEnd(lines, jobs.Content[idx+1])
		for end > max(start, protectedEnd) && (strings.TrimSpace(lines[end-1]) == "" || strings.HasPrefix(lines[end-1], "#")) {
			end--
		}
		previousEnd := jobsLine
		if idx > 0 {
			previousEnd = max(previousEnd, blockScalarEnd(lines, jobs.Content[idx-1]))
		}
		for start > previousEnd && strings.TrimSpace(lines[start-1]) == "" {
			start--
		}
	}
	if len(jobs.Content) == 2 {
		start = jobsLine - 1
	}
	return []byte(strings.Join(lines[:start], "") + strings.Join(lines[end:], "")), nil
}

// JobNames returns the names in a syntactically valid jobs block, preserving the
// distinction between a missing block and an unsupported edit shape.
func JobNames(src []byte) ([]string, error) {
	if len(src) == 0 {
		return nil, nil
	}
	root, err := documentRoot(src)
	if err != nil {
		return nil, err
	}
	jobs, _ := rootField(root, "jobs")
	if jobs == nil {
		return nil, nil
	}
	if jobs.Kind != yaml.MappingNode || jobs.Style&yaml.FlowStyle != 0 {
		return nil, fmt.Errorf("config_edit: jobs must be a block mapping; edit the file by hand")
	}
	names := make([]string, 0, len(jobs.Content)/2)
	for i := 0; i < len(jobs.Content); i += 2 {
		if jobs.Content[i].Kind != yaml.ScalarNode || jobs.Content[i].Tag != "!!str" {
			return nil, fmt.Errorf("config_edit: job names must be strings")
		}
		names = append(names, jobs.Content[i].Value)
	}
	return names, nil
}

// JobFromSource validates one raw definition, even when another file shadows it.
func JobFromSource(src []byte, name string, agents map[string]AgentProfile, defaultAgent string, defaultSuccess, defaultFailure any, digestDefaults bool) (Job, error) {
	root, err := documentRoot(src)
	if err != nil {
		return Job{}, err
	}
	jobs, _ := rootField(root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || jobs.Style&yaml.FlowStyle != 0 {
		return Job{}, fmt.Errorf("config_edit: jobs must be a block mapping")
	}
	for i := 0; i < len(jobs.Content); i += 2 {
		key, node := jobs.Content[i], jobs.Content[i+1]
		if key.Value != name {
			continue
		}
		fields, err := jobMapping("<edited>", "jobs."+name, node)
		if err != nil {
			return Job{}, err
		}
		parsed, err := parseJob("<edited>", "jobs."+name, name, node, fields, defaultSuccess, defaultFailure, digestDefaults)
		if err != nil {
			return Job{}, err
		}
		cfg := Config{Jobs: map[string]Job{name: parsed}, Agents: agents, DefaultAgent: defaultAgent}
		if err := cfg.resolveAgents(); err != nil {
			return Job{}, err
		}
		return cfg.Jobs[name], nil
	}
	return Job{}, fmt.Errorf("config_edit: job %q not found", name)
}

// DigestJobDefinition computes the canonical execution digest for a requested
// definition using the same parser and configured agent profiles as Load.
func DigestJobDefinition(job Job, agents map[string]AgentProfile, defaultAgent string) (string, error) {
	fields := jobFields(job)
	encoded, err := yaml.Marshal(fields)
	if err != nil {
		return "", err
	}
	text := "jobs:\n  " + job.Name + ":\n"
	for _, line := range strings.Split(strings.TrimSuffix(string(encoded), "\n"), "\n") {
		text += "    " + line + "\n"
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", err
	}
	root := doc.Content[0].Content[1].Content[1]
	mapping, err := jobMapping("<requested>", "jobs."+job.Name, root)
	if err != nil {
		return "", err
	}
	parsed, err := parseJob("<requested>", "jobs."+job.Name, job.Name, root, mapping, nil, nil, false)
	if err != nil {
		return "", err
	}
	cfg := Config{Jobs: map[string]Job{job.Name: parsed}, Agents: agents, DefaultAgent: defaultAgent}
	if err := cfg.resolveAgents(); err != nil {
		return "", err
	}
	return cfg.Jobs[job.Name].Digest, nil
}

func jobFields(job Job) map[string]any {
	fields := make(map[string]any)
	if job.Description != "" {
		fields["description"] = job.Description
	}
	if job.execution != nil {
		for key, value := range job.execution {
			fields[key] = value
		}
	} else {
		if job.Schedule != nil {
			fields["schedule"] = job.Schedule.String()
		}
		if job.Run != nil {
			fields["run"] = job.Run
		}
		if job.Prompt != nil {
			fields["prompt"] = *job.Prompt
		}
		if job.PromptFile != nil {
			fields["prompt_file"] = *job.PromptFile
		}
		if job.Agent != "" {
			fields["agent"] = job.Agent
		}
		if len(job.AgentArgs) > 0 {
			fields["agent_args"] = job.AgentArgs
		}
		if job.CWD != "" {
			fields["cwd"] = job.CWD
		}
		if len(job.Env) > 0 {
			fields["env"] = job.Env
		}
		if job.Timeout != "" && job.Timeout != "1h" {
			fields["timeout"] = job.Timeout
		}
		if job.NoopExitCode != 0 {
			fields["noop_exit_code"] = job.NoopExitCode
		}
		if job.Overlap != "" && job.Overlap != "skip" {
			fields["overlap"] = job.Overlap
		}
		if job.Missed != "" && job.Missed != "run_once" {
			fields["missed"] = job.Missed
		}
		if len(job.OS) > 0 {
			fields["os"] = job.OS
		}
		if len(job.Arch) > 0 {
			fields["arch"] = job.Arch
		}
	}
	if !job.Enabled {
		fields["enabled"] = false
	}
	return fields
}

func encodeJob(job Job, indent int, ending string, fieldComments map[string]string) (string, error) {
	var fields yaml.Node
	if err := fields.Encode(jobFields(job)); err != nil {
		return "", fmt.Errorf("config_edit: encode job: %w", err)
	}
	quoteMergeStrings(&fields)
	for i := 0; i+1 < len(fields.Content); i += 2 {
		if comment := fieldComments[fields.Content[i].Value]; comment != "" {
			fields.Content[i].HeadComment = comment
		}
	}
	data, err := yaml.Marshal(&fields)
	if err != nil {
		return "", fmt.Errorf("config_edit: encode job: %w", err)
	}
	prefix := strings.Repeat(" ", indent)
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, ending) + ending, nil
}

func commentLines(comment string, indent int, ending string) string {
	if comment == "" {
		return ""
	}
	prefix := strings.Repeat(" ", indent) + "# "
	lines := strings.Split(comment, "\n")
	for i, line := range lines {
		lines[i] = prefix + strings.TrimSuffix(line, "\r")
	}
	return strings.Join(lines, ending) + ending
}

func documentRoot(src []byte) (*yaml.Node, error) {
	if _, err := parse(src); err != nil {
		return nil, fmt.Errorf("config_edit: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("config_edit: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config_edit: edit the file by hand")
	}
	return doc.Content[0], nil
}
func rootField(root *yaml.Node, field string) (*yaml.Node, int) {
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == field {
			return root.Content[i+1], root.Content[i].Line
		}
	}
	return nil, 0
}
func precededByBlankJobSeparator(lines []string, jobs *yaml.Node) bool {
	if len(jobs.Content) < 4 {
		return false
	}
	lastKey := jobs.Content[len(jobs.Content)-2]
	line := lastKey.Line - 2
	for line >= 0 && isIndentedComment(lines[line], lastKey.Column-1) {
		line--
	}
	return line >= 0 && strings.TrimSpace(lines[line]) == ""
}
func isIndentedComment(line string, indent int) bool {
	prefix := strings.Repeat(" ", indent)
	return strings.HasPrefix(line, prefix+"#")
}
func splitLines(src []byte) []string {
	if len(src) == 0 {
		return nil
	}
	lines := strings.SplitAfter(string(src), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// blockScalarEnd includes trailing blank lines that YAML consumes as scalar
// content. Nodes supply positions; source bytes supply the scalar's extent.
func blockScalarEnd(lines []string, node *yaml.Node) int {
	end := 0
	if node.Kind != yaml.MappingNode {
		return end
	}
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if value.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
			line := value.Line
			for line < len(lines) {
				text := lines[line]
				indent := len(text) - len(strings.TrimLeft(text, " "))
				if strings.TrimSpace(text) != "" && indent <= key.Column-1 {
					break
				}
				line++
			}
			end = max(end, line)
		}
		end = max(end, blockScalarEnd(lines, value))
	}
	return end
}
func lineEnding(src []byte) string {
	if bytes.Contains(src, []byte("\r\n")) {
		return "\r\n"
	}
	return "\n"
}

// quoteMergeStrings quotes the string "<<", which the YAML encoder emits as a
// plain merge key that would no longer load as a string.
func quoteMergeStrings(node *yaml.Node) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!merge" {
		node.Tag, node.Style = "!!str", yaml.DoubleQuotedStyle
	}
	for _, child := range node.Content {
		quoteMergeStrings(child)
	}
}

// yamlKey quotes names such as true and null that YAML would not read as strings.
func yamlKey(name string) string {
	data, err := yaml.Marshal(name)
	if err != nil {
		return name
	}
	return strings.TrimSuffix(string(data), "\n")
}
