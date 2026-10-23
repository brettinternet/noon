package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/brettinternet/noon/internal/config"
	urfave "github.com/urfave/cli/v3"
)

// Guidance contains only commands authored by Noon, never user-supplied prose.
// Match command prefixes rather than parsing prose or shell arguments.
var guidanceCommand = regexp.MustCompile(`\bnoon (service install|service uninstall|service|serve|config check|config show|config|run|logs|history|cancel|enable|disable|list|add|remove|pause|resume|status|tui)\b`)

func guidance(cmd *urfave.Command, text string) string {
	return guidanceCommand.ReplaceAllStringFunc(text, func(command string) string {
		service := command == "noon serve" || strings.HasPrefix(command, "noon service")
		prefix := ""
		if service {
			// These variables are rejected even when no corresponding flag is passed.
			for _, name := range []string{"NOON_PROJECT", "NOON_CONFIG", "NOON_TIMEZONE", "NOON_JSON"} {
				if _, set := os.LookupEnv(name); set {
					if prefix == "" {
						prefix = "env"
					}
					prefix += " -u " + name
				}
			}
			if prefix != "" {
				prefix += " "
			}
		}
		var serviceState *string
		if service && setting(cmd, "config") != nil {
			if values, err := input(cmd); err == nil {
				if cfg, err := config.LoadForDisable(values); err == nil {
					serviceState = &cfg.StateDir
				}
			}
		}
		if cmd.Bool("user") && !service {
			command = strings.Replace(command, "noon ", "noon --user ", 1)
		}
		for _, name := range []string{"project", "config", "state-dir", "timezone"} {
			if service && name != "state-dir" {
				continue
			}
			value := setting(cmd, name)
			if name == "state-dir" && serviceState != nil {
				value = serviceState
			}
			if value == nil || *value == "" {
				continue
			}
			resolved := *value
			if name != "timezone" {
				if absolute, err := filepath.Abs(resolved); err == nil {
					resolved = absolute
				}
			}
			command += " --" + name + " " + shellArgument(resolved)
		}
		return prefix + command
	})
}

func shellArgument(value string) string {
	if value != "" && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
