package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/brettinternet/noon/internal/app"
	urfave "github.com/urfave/cli/v3"
)

// Error describes a CLI failure independently of its output format.
type Error = app.Error

func usageError(message, hint string) *Error { return app.UsageError(message, hint) }

// unquotedTime reports a time flag whose date and clock were split by the
// shell, e.g. --until 2026-11-29 11:00, before the clock is read as a name.
func unquotedTime(cmd *urfave.Command) error {
	for _, flag := range []string{"until", "from", "at"} {
		date := cmd.String(flag)
		if _, err := time.Parse("2006-01-02", date); err != nil {
			continue
		}
		for _, arg := range cmd.Args().Slice() {
			if _, err := time.Parse("15:04", arg); err == nil {
				return usageError(fmt.Sprintf("--%s %s %s must be quoted", flag, date, arg), fmt.Sprintf(`Use --%s "%s %s"`, flag, date, arg))
			}
		}
	}
	return nil
}

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
