// Package cronimport parses user crontabs into Noon job definitions.
package cronimport

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/brettinternet/noon/internal/config"
	"github.com/brettinternet/noon/internal/schedule"
)

// Job is one supported crontab entry. Line is its one-based source line.
type Job struct {
	Name     string
	Schedule string
	Run      string
	Env      map[string]string
	Line     int
}

var (
	environmentName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	nameSeparators  = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// Parse reads supported crontab entries and reports every invalid line.
func Parse(r io.Reader) ([]Job, error) { return ParseWithExisting(r, nil) }

// ParseWithExisting also avoids names already used by project jobs.
func ParseWithExisting(r io.Reader, existingNames []string) ([]Job, error) {
	reader := bufio.NewReader(r)
	used := make(map[string]struct{}, len(existingNames))
	for _, name := range existingNames {
		used[name] = struct{}{}
	}
	jobs := make([]Job, 0)
	env := make(map[string]string)
	var problems []string
	lineNumber := 0
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			lineNumber++
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			if !utf8.ValidString(line) {
				problems = append(problems, fmt.Sprintf("line %d: invalid UTF-8", lineNumber))
			} else if job, updatedEnv, problem := parseLine(line, lineNumber, env, used); problem != "" {
				problems = append(problems, fmt.Sprintf("line %d: %s", lineNumber, problem))
			} else if updatedEnv != nil {
				env = updatedEnv
			} else if job != nil {
				jobs = append(jobs, *job)
				used[job.Name] = struct{}{}
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				problems = append(problems, fmt.Sprintf("line %d: read error: %v", lineNumber+1, readErr))
			}
			break
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(problems, "\n"))
	}
	return jobs, nil
}

func parseLine(line string, lineNumber int, env map[string]string, used map[string]struct{}) (*Job, map[string]string, string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return nil, nil, ""
	}
	if name, value, ok := environmentLine(trimmed); ok {
		if strings.HasPrefix(name, "NOON_") {
			return nil, nil, fmt.Sprintf("environment name %q uses the reserved NOON_ prefix", name)
		}
		switch name {
		case "MAILTO":
			if value != "" {
				return nil, nil, "non-empty MAILTO is not supported"
			}
			return nil, cloneEnv(env), ""
		case "SHELL":
			if value != "/bin/sh" {
				return nil, nil, "SHELL must be /bin/sh"
			}
			return nil, cloneEnv(env), ""
		case "CRON_TZ":
			return nil, nil, "CRON_TZ is not supported; set the project timezone instead"
		case "TZ":
			return nil, nil, "TZ is not supported; set the project timezone instead"
		}
		updated := cloneEnv(env)
		updated[name] = value
		return nil, updated, ""
	}

	var scheduleText, command string
	if strings.HasPrefix(firstWord(trimmed), "@") {
		macro, rest := splitFirstWord(trimmed)
		if strings.EqualFold(macro, "@reboot") {
			return nil, nil, "@reboot is not supported; Noon has no boot trigger"
		}
		scheduleText, command = macro, strings.TrimSpace(rest)
	} else {
		fields, rest, ok := splitFields(trimmed, 5)
		if !ok {
			return nil, nil, "expected five cron fields followed by a command"
		}
		scheduleText, command = strings.Join(fields, " "), strings.TrimSpace(rest)
		if extraFields, _, hasSixth := splitFields(trimmed, 6); hasSixth && len(extraFields) == 6 && isSecondsField(extraFields[5]) {
			if _, err := schedule.Parse(scheduleText); err != nil {
				return nil, nil, "six-field cron schedules are not supported"
			}
		}
	}
	if command == "" {
		return nil, nil, "entry has no command"
	}
	if strings.Contains(command, "%") {
		return nil, nil, "% in a command is not supported because cron turns it into newlines and stdin"
	}
	parsedSchedule, err := schedule.Parse(scheduleText)
	if err != nil {
		return nil, nil, "unsupported schedule: " + err.Error()
	}
	jobEnv := cloneEnv(env)
	return &Job{
		Name:     allocateName(nameStem(command, lineNumber), used),
		Schedule: parsedSchedule.String(),
		Run:      command,
		Env:      jobEnv,
		Line:     lineNumber,
	}, nil, ""
}

func environmentLine(line string) (string, string, bool) {
	name, value, ok := strings.Cut(line, "=")
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if !ok || !environmentName.MatchString(name) {
		return "", "", false
	}
	if len(value) >= 2 && (value[0] == '\'' && value[len(value)-1] == '\'' || value[0] == '"' && value[len(value)-1] == '"') {
		value = value[1 : len(value)-1]
	}
	return name, value, true
}

func firstWord(text string) string {
	word, _ := splitFirstWord(text)
	return word
}

func splitFirstWord(text string) (string, string) {
	start := 0
	for start < len(text) && isSpace(text[start]) {
		start++
	}
	end := start
	for end < len(text) && !isSpace(text[end]) {
		end++
	}
	return text[start:end], text[end:]
}

func splitFields(text string, count int) ([]string, string, bool) {
	fields := make([]string, 0, count)
	pos := 0
	for range count {
		for pos < len(text) && isSpace(text[pos]) {
			pos++
		}
		if pos == len(text) {
			return nil, "", false
		}
		start := pos
		for pos < len(text) && !isSpace(text[pos]) {
			pos++
		}
		fields = append(fields, text[start:pos])
	}
	return fields, strings.TrimSpace(text[pos:]), true
}

func isSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\v' || value == '\f'
}

func isSecondsField(text string) bool {
	for _, item := range strings.Split(text, ",") {
		base, stepText, stepped := strings.Cut(item, "/")
		if stepped {
			step, err := strconv.Atoi(stepText)
			if err != nil || step < 1 || step > 59 {
				return false
			}
		}
		if base == "*" {
			continue
		}
		startText, endText, ranged := strings.Cut(base, "-")
		start, err := strconv.Atoi(startText)
		if err != nil || start < 0 || start > 59 {
			return false
		}
		if ranged {
			end, err := strconv.Atoi(endText)
			if err != nil || end < start || end > 59 {
				return false
			}
		}
	}
	return true
}

func cloneEnv(env map[string]string) map[string]string {
	copy := make(map[string]string, len(env))
	for name, value := range env {
		copy[name] = value
	}
	return copy
}

func nameStem(command string, lineNumber int) string {
	words := strings.Fields(command)
	if len(words) == 0 {
		return "cron-" + strconv.Itoa(lineNumber)
	}
	stem := strings.ToLower(path.Base(words[0]))
	stem = nameSeparators.ReplaceAllString(stem, "-")
	stem = strings.Trim(stem, "-_")
	if stem == "" {
		return "cron-" + strconv.Itoa(lineNumber)
	}
	if stem[0] >= '0' && stem[0] <= '9' {
		stem = "cron-" + stem
	}
	return stem
}

func allocateName(stem string, used map[string]struct{}) string {
	for suffix := 1; ; suffix++ {
		name := stem
		if suffix > 1 {
			tail := "-" + strconv.Itoa(suffix)
			name = truncateStem(stem, 64-len(tail)) + tail
		} else {
			name = truncateStem(stem, 64)
		}
		if _, exists := used[name]; !exists {
			return name
		}
	}
}

func truncateStem(stem string, limit int) string {
	if len(stem) > limit {
		return stem[:limit]
	}
	return stem
}

// AddJobToYAML appends one imported job without re-encoding existing YAML.
func AddJobToYAML(src []byte, job Job) ([]byte, error) {
	parsedSchedule, err := schedule.Parse(job.Schedule)
	if err != nil {
		return nil, fmt.Errorf("job %q has invalid schedule: %w", job.Name, err)
	}
	return config.AddJobWithComments(src, job.Name, config.Job{
		Name: job.Name, Schedule: &parsedSchedule, Run: job.Run, Env: job.Env,
		Timeout: "none", Missed: "skip", Enabled: true,
	}, fmt.Sprintf("crontab line %d", job.Line), map[string]string{
		"missed":  "Cron never catches up; skip missed runs.",
		"timeout": "Cron has no job timeout.",
	})
}

// RenderYAML renders importable jobs with source-line and cron-semantics comments.
func RenderYAML(jobs []Job) ([]byte, error) {
	if len(jobs) == 0 {
		return []byte("jobs: {}\n"), nil
	}
	data := []byte("version: 1\n")
	for _, job := range jobs {
		var err error
		data, err = AddJobToYAML(data, job)
		if err != nil {
			return nil, err
		}
	}
	return []byte(strings.TrimPrefix(string(data), "version: 1\n")), nil
}

// EnvKeys returns environment variable names in deterministic order.
func (job Job) EnvKeys() []string {
	keys := make([]string, 0, len(job.Env))
	for key := range job.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
