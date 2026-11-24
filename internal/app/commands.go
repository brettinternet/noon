package app

import (
	"strings"

	"github.com/brettinternet/noon/internal/config"
)

// ShellArgument quotes value for a POSIX shell only when it needs quoting.
func ShellArgument(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// ProjectCommand targets a noon command at a project and the config file it
// was enabled with, for adapters that don't run inside the project.
func ProjectCommand(project, configFile, command string) string {
	target := ShellArgument(project)
	if configFile != "" {
		target += " --config " + ShellArgument(configFile)
	}
	return "noon -C " + target + strings.TrimPrefix(command, "noon")
}

// AgentResumeCommand continues an agent run's session, or is empty when the
// profile can't resume. withCWD prefixes a cd into the job's directory.
func AgentResumeCommand(job config.Job, sessionID string, withCWD bool) string {
	if job.AgentResumeFlag == "" || sessionID == "" || len(job.AgentCommand) == 0 {
		return ""
	}
	command := ShellArgument(job.AgentCommand[0]) + " " + ShellArgument(job.AgentResumeFlag) + " " + ShellArgument(sessionID)
	if withCWD && job.CWD != "" {
		command = "cd " + ShellArgument(job.CWD) + " && " + command
	}
	return command
}
