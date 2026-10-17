package cli

import (
	"strings"

	"github.com/brettinternet/noon/internal/app"
	urfave "github.com/urfave/cli/v3"
)

// Error describes a CLI failure independently of its output format.
type Error = app.Error

func usageError(message, hint string) *Error { return app.UsageError(message, hint) }

func commandHelp(cmd *urfave.Command) string {
	return "Run " + cmd.FullName() + " --help"
}

func flagUsageHint(cmd *urfave.Command, err error) string {
	hint := commandHelp(cmd)
	const prefix = "flag provided but not defined: -"
	if !strings.HasPrefix(err.Error(), prefix) {
		return hint
	}
	name := strings.TrimPrefix(err.Error(), prefix)
	names := []string{}
	for _, ancestor := range cmd.Lineage() {
		for _, flag := range ancestor.VisibleFlags() {
			names = append(names, flag.Names()...)
		}
	}
	return hint + nearbyHint(name, names, "--")
}

// Suggest only a unique, single-edit match; never reinterpret the input.
func nearbyHint(input string, names []string, prefix string) string {
	match := ""
	for _, name := range names {
		if name == input {
			return ""
		}
		if len(name) < 3 || !singleTypo(input, name) {
			continue
		}
		if match != "" && match != name {
			return ""
		}
		match = name
	}
	if match == "" {
		return ""
	}
	return ". Did you mean " + prefix + match + "?"
}

func singleTypo(a, b string) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > 1 {
		return false
	}
	for i := range len(a) {
		if a[i] == b[i] {
			continue
		}
		if len(a) != len(b) {
			return a[i:] == b[i+1:]
		}
		return a[i+1:] == b[i+1:] || (i+1 < len(a) && a[i] == b[i+1] && a[i+1] == b[i] && a[i+2:] == b[i+2:])
	}
	return len(b) == len(a)+1
}

func configError(err error) error {
	if err == nil {
		return nil
	}
	return app.ConfigError(err)
}
