package tui

import (
	"fmt"
	"image/color"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

// style is a value-typed set of SGR attributes. Rendering builds a fresh
// sequence every time, so derived styles never share state.
type style struct {
	fg, bg                          color.Color
	bold, faint, underline, reverse bool
}

func (s style) render(text string) string {
	if text == "" {
		return ""
	}
	var sgr ansi.Style
	if s.bold {
		sgr = sgr.Bold()
	}
	if s.faint {
		sgr = sgr.Faint()
	}
	if s.underline {
		sgr = sgr.Underline(true)
	}
	if s.reverse {
		sgr = sgr.Reverse(true)
	}
	if s.fg != nil {
		sgr = sgr.ForegroundColor(s.fg)
	}
	if s.bg != nil {
		sgr = sgr.BackgroundColor(s.bg)
	}
	return sgr.Styled(text)
}

// over lays s on top of a row fill: s keeps its own colors and attributes and
// inherits the fill's background and emphasis.
func (s style) over(fill style) style {
	if s.bg == nil {
		s.bg = fill.bg
	}
	s.bold = s.bold || fill.bold
	if fill.reverse {
		s.reverse, s.fg, s.bg = true, nil, nil
	}
	return s
}

// span is one run of display text in a single style. Text is sanitized when it
// is rendered, so spans may carry untrusted job, config, or log bytes.
type span struct {
	text  string
	style style
}

func spansWidth(spans []span) int {
	width := 0
	for _, part := range spans {
		width += ansi.StringWidth(displayText(part.text))
	}
	return width
}

// clipSpans renders spans in at most width cells, ending overlong text in an
// ellipsis, and returns the rendered string with the cells it uses.
func clipSpans(spans []span, width int, fill style) (string, int) {
	if width <= 0 {
		return "", 0
	}
	var out strings.Builder
	used := 0
	for i, part := range spans {
		text := displayText(part.text)
		textWidth := ansi.StringWidth(text)
		if used+textWidth > width {
			out.WriteString(part.style.over(fill).render(ansi.Truncate(text, width-used, "…")))
			return out.String(), width
		}
		if i < len(spans)-1 && used+textWidth == width && spansWidth(spans[i+1:]) > 0 {
			// More text follows a span that fills the line: show the cut.
			out.WriteString(part.style.over(fill).render(ansi.Truncate(text, textWidth, "…")))
			return out.String(), width
		}
		out.WriteString(part.style.over(fill).render(text))
		used += textWidth
	}
	return out.String(), used
}

// fitSpans renders spans in exactly width cells. Overlong text ends in an
// ellipsis; short text is padded with the fill, whose background extends under
// every span. Right alignment pads on the left.
func fitSpans(spans []span, width int, fill style, alignRight bool) string {
	text, used := clipSpans(spans, width, fill)
	if used >= width {
		return text
	}
	padding := fill.render(strings.Repeat(" ", width-used))
	if alignRight {
		return padding + text
	}
	return text + padding
}

// truncateLeft fits text in width cells by cutting from the left, which keeps
// the most specific end of a path or a search pattern being typed.
func truncateLeft(text string, width int) string {
	text = displayText(text)
	textWidth := ansi.StringWidth(text)
	switch {
	case textWidth <= width:
		return text
	case width < 1:
		return ""
	}
	return "\u2026" + ansi.TruncateLeft(text, textWidth-width+1, "")
}

// justify places right-hand spans at the end of a width-cell line. When both
// sides do not fit, the right side wins only if it leaves the left readable.
func justify(width int, fill style, left, right []span) string {
	leftWidth, rightWidth := spansWidth(left), spansWidth(right)
	if rightWidth == 0 || rightWidth+2 > width/2 && leftWidth+rightWidth+2 > width {
		return fitSpans(left, width, fill, false)
	}
	gap := max(2, width-leftWidth-rightWidth)
	leftRoom := width - rightWidth - gap
	if leftWidth > leftRoom {
		leftWidth = leftRoom
	}
	return fitSpans(left, leftWidth, fill, false) + fill.render(strings.Repeat(" ", width-leftWidth-rightWidth)) + fitSpans(right, rightWidth, fill, false)
}

// palette holds the semantic colors for one background brightness. Values
// come from GitHub Primer's accessible light and dark scales; Bubble Tea
// downsamples them for 256- and 16-color terminals.
type palette struct {
	background, accent, success, failure, warning, running, queued, muted, faint, rule, matchFg, matchBg color.Color
}

var (
	darkPalette = palette{
		background: hex(0x0d1117), accent: hex(0xe3b341),
		success: hex(0x3fb950), failure: hex(0xf85149), warning: hex(0xdb6d28),
		running: hex(0x58a6ff), queued: hex(0xbc8cff),
		muted: hex(0x8b949e), faint: hex(0x6e7681), rule: hex(0x30363d),
		matchFg: hex(0x0d1117), matchBg: hex(0xe3b341),
	}
	lightPalette = palette{
		background: hex(0xffffff), accent: hex(0x9a6700),
		success: hex(0x1a7f37), failure: hex(0xcf222e), warning: hex(0xbc4c00),
		running: hex(0x0969da), queued: hex(0x8250df),
		muted: hex(0x59636e), faint: hex(0x818b98), rule: hex(0xd1d9e0),
		matchFg: hex(0x1f2328), matchBg: hex(0xffd33d),
	}
)

// theme maps semantic roles to styles. Monochrome terminals (NO_COLOR or
// TERM=dumb) keep the hierarchy with bold, faint, underline, and reverse.
type theme struct {
	mono                                                  bool
	text, strong, muted, faint, rule, accent, key         style
	success, failure, warning, running, queued            style
	selected, marker, match, current, tab, tabActive, cmd style
}

func newTheme(profile colorprofile.Profile, dark bool, background color.Color) theme {
	if profile != colorprofile.Unknown && profile <= colorprofile.ASCII {
		return theme{
			mono:   true,
			strong: style{bold: true}, muted: style{faint: true}, faint: style{faint: true}, rule: style{faint: true},
			accent: style{bold: true}, key: style{bold: true},
			failure: style{bold: true}, warning: style{bold: true},
			selected: style{reverse: true}, marker: style{bold: true},
			match: style{underline: true, bold: true}, current: style{reverse: true},
			tab: style{faint: true}, tabActive: style{reverse: true, bold: true}, cmd: style{bold: true},
		}
	}
	colors := darkPalette
	if !dark {
		colors = lightPalette
	}
	if background == nil {
		background = colors.background
	}
	// Tint the actual terminal background so the selection stays subtle on
	// any theme while remaining visible.
	tint := 0.16
	if !dark {
		tint = 0.12
	}
	selection := mix(background, colors.accent, tint)
	current := mix(background, colors.accent, tint*2)
	selected := style{bg: selection}
	if profile == colorprofile.ANSI {
		// Sixteen-color palettes would round a subtle tint to the background.
		selected = style{reverse: true}
	}
	return theme{
		strong:    style{bold: true},
		muted:     style{fg: colors.muted},
		faint:     style{fg: colors.faint},
		rule:      style{fg: colors.rule},
		accent:    style{fg: colors.accent},
		key:       style{fg: colors.accent, bold: true},
		success:   style{fg: colors.success},
		failure:   style{fg: colors.failure},
		warning:   style{fg: colors.warning},
		running:   style{fg: colors.running},
		queued:    style{fg: colors.queued},
		selected:  selected,
		marker:    style{fg: colors.accent},
		match:     style{fg: colors.matchFg, bg: colors.matchBg},
		current:   style{bg: current},
		tab:       style{fg: colors.muted},
		tabActive: style{fg: colors.matchFg, bg: colors.accent, bold: true},
		cmd:       style{fg: colors.accent},
	}
}

func hex(value uint32) color.Color {
	return color.RGBA{R: uint8(value >> 16), G: uint8(value >> 8), B: uint8(value), A: 0xff}
}

// mix linearly blends two colors in sRGB; t=0 returns a and t=1 returns b.
func mix(a, b color.Color, t float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	blend := func(x, y uint32) uint8 {
		return uint8((float64(x>>8)*(1-t) + float64(y>>8)*t) + 0.5)
	}
	return color.RGBA{R: blend(ar, br), G: blend(ag, bg), B: blend(ab, bb), A: 0xff}
}

// displayText escapes control characters so untrusted bytes cannot move the
// cursor, change modes, or break the layout.
func displayText(value string) string {
	clean := true
	for _, r := range value {
		if unicode.IsControl(r) || r == utf8.RuneError {
			clean = false
			break
		}
	}
	if clean {
		return value
	}
	var output strings.Builder
	for _, r := range value {
		if unicode.IsControl(r) {
			if r <= 0xff {
				fmt.Fprintf(&output, "\\x%02x", r)
			} else {
				fmt.Fprintf(&output, "\\u%04x", r)
			}
		} else {
			output.WriteRune(r)
		}
	}
	return output.String()
}

// logLineText prepares one stored log line for display: it drops a trailing
// carriage return and SGR color sequences, expands tabs, and escapes any other
// control bytes. Stored bytes are never interpreted by the terminal.
func logLineText(line string) string {
	line = strings.TrimSuffix(line, "\r")
	if !strings.ContainsAny(line, "\t\x1b") {
		return displayText(line)
	}
	var out strings.Builder
	column := 0
	for i := 0; i < len(line); {
		if line[i] == 0x1b {
			if end := sgrEnd(line, i); end > i {
				i = end
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		i += size
		if r == '\t' {
			spaces := 8 - column%8
			out.WriteString(strings.Repeat(" ", spaces))
			column += spaces
			continue
		}
		text := displayText(string(r))
		out.WriteString(text)
		column += ansi.StringWidth(text)
	}
	return out.String()
}

// sgrEnd returns the index after a complete "ESC [ params m" color sequence
// starting at i, or i when the bytes are anything else.
func sgrEnd(line string, i int) int {
	if i+1 >= len(line) || line[i+1] != '[' {
		return i
	}
	for j := i + 2; j < len(line); j++ {
		switch c := line[j]; {
		case c == 'm':
			return j + 1
		case c >= '0' && c <= '9' || c == ';' || c == ':':
		default:
			return i
		}
	}
	return i
}

// wrapText splits plain display text into rows no wider than width cells. A
// row ends after its last space when that space is in the row's second half,
// so words stay whole without leaving short rows. It returns byte offsets so
// highlights can be mapped onto each row.
func wrapText(text string, width int) []int {
	breaks := []int{0}
	if width < 1 {
		return breaks
	}
	if isASCII(text) {
		for start := 0; len(text)-start > width; {
			end := start + width
			if space := strings.LastIndexByte(text[start:end], ' '); space >= width/2 {
				end = start + space + 1
			}
			breaks = append(breaks, end)
			start = end
		}
		return breaks
	}
	start, used, space := 0, 0, -1
	for i := 0; i < len(text); {
		cluster, clusterWidth := ansi.FirstGraphemeCluster(text[i:], ansi.GraphemeWidth)
		if used+clusterWidth > width && used > 0 {
			end := i
			if space > start && ansi.StringWidth(text[start:space]) >= width/2 {
				end = space + 1
			}
			breaks = append(breaks, end)
			start, used, space = end, ansi.StringWidth(text[end:i]), -1
		}
		if cluster == " " {
			space = i
		}
		used += clusterWidth
		i += len(cluster)
	}
	return breaks
}

func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
