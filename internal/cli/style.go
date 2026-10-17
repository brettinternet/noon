package cli

import (
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"

	urfave "github.com/urfave/cli/v3"
)

// humanStyle only decorates Noon's own presentation, never the output writer:
// subprocess bytes, logs, JSON, and generated shell scripts remain untouched.
type humanStyle bool

func styleFor(cmd *urfave.Command, out io.Writer) humanStyle {
	if cmd.Bool("json") {
		return false
	}
	return colorStyle(cmd.String("color"), out)
}

func colorStyle(mode string, out io.Writer) humanStyle {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	default:
		return humanStyle(os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && terminalWriter(out))
	}
}

// text must already be sanitized. Roles come from application state, not ANSI
// sequences supplied by config. Success timestamps stay in the default color.
func (s humanStyle) render(text, role string) string {
	if !s || text == "" {
		return text
	}
	code := ""
	switch role {
	case "heading":
		code = "1"
	case "failed", "timed_out", "fail", "config_error":
		code = "31"
	case "warn", "needs approval", "paused", "disabled", "cancelled", "offline", "other platform", "orphaned":
		code = "33"
	case "running":
		code = "36"
	case "succeeded", "ok":
		code = "32"
		if strings.HasPrefix(text, "✓") {
			return "\x1b[32m✓\x1b[0m" + strings.TrimPrefix(text, "✓")
		}
	case "noop":
		code = "2"
	}
	if code == "" {
		return text
	}
	prefix, suffix := "", ""
	if strings.HasPrefix(text, "scheduler ") {
		prefix, text = "scheduler ", strings.TrimPrefix(text, "scheduler ")
	}
	if strings.HasPrefix(text, "✗ ") {
		if label, rest, found := strings.Cut(text, " · "); found {
			text, suffix = label, " · "+rest
		}
	} else if role == "running" && strings.HasPrefix(text, "running ") {
		text, suffix = "running", strings.TrimPrefix(text, "running")
	} else if role == "orphaned" && strings.HasPrefix(text, "orphaned ") {
		text, suffix = "orphaned", strings.TrimPrefix(text, "orphaned")
	}
	return prefix + "\x1b[" + code + "m" + text + "\x1b[0m" + suffix
}

// Free-form hints must preserve printable Unicode so quoted paths remain
// copyable. The stricter ASCII table escaping is only for width-bounded cells.
func humanText(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsControl(r) {
			quoted := strconv.QuoteRune(r)
			b.WriteString(quoted[1 : len(quoted)-1])
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// tableCell keeps semantic presentation separate from the untrusted text used
// for sanitization, truncation, and width calculations.
type tableCell struct {
	text string
	role string
}
